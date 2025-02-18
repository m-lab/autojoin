package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"sync/atomic"
	"time"

	v0 "github.com/m-lab/autojoin/api/v0"
	"github.com/m-lab/go/flagx"
	"github.com/m-lab/go/memoryless"
	"github.com/m-lab/go/rtx"
	v2 "github.com/m-lab/locate/api/v2"
)

const (
	registerEndpoint       = "https://autojoin-dot-mlab-sandbox.appspot.com/autojoin/v0/node/register"
	heartbeatFilename      = "registration.json"
	annotationFilename     = "annotation.json"
	serviceAccountFilename = "service-account-autojoin.json"
	hostnameFilename       = "hostname"
)

var (
	endpoint    = flag.String("endpoint", registerEndpoint, "Endpoint of the autojoin service")
	apiKey      = flag.String("key", "", "API key for the autojoin service")
	service     = flag.String("service", "ndt", "Service name to register with the autojoin service")
	iata        = flagx.StringFile{}
	ipv4        = flagx.StringFile{}
	ipv6        = flagx.StringFile{}
	machineType = flag.String("type", "", "The type of machine: physical or virtual")
	uplink      = flag.String("uplink", "", "The speed of the uplink e.g., 1g, 10g, etc.")
	interval    = flag.Duration("interval.expected", 1*time.Hour, "Expected registration interval")
	intervalMin = flag.Duration("interval.min", 55*time.Minute, "Minimum registration interval")
	intervalMax = flag.Duration("interval.max", 65*time.Minute, "Maximum registration interval")
	outputPath  = flag.String("output", "", "Output folder")
	siteProb    = flagx.StringFile{}
	defaultProb = 1.0
	ports       = flagx.StringArray{}

	hcAddr          = flag.String("healthcheck-addr", "localhost:8001", "Address to serve the /ready endpoint on")
	registerSuccess atomic.Bool
)

func init() {
	flag.Var(&ports, "ports", "Ports to monitor for this service")
	flag.Var(&iata, "iata", "IATA code to register with the autojoin service")
	flag.Var(&ipv4, "ipv4", "IPv4 address to register with the autojoin service")
	flag.Var(&ipv6, "ipv6", "IPv6 address to register with the autojoin service")
	flag.Var(&siteProb, "probability", "Default probability of returning this site for a Locate result")
}

func Ready(rw http.ResponseWriter, req *http.Request) {
	if registerSuccess.Load() {
		rw.WriteHeader(http.StatusOK)
	} else {
		rw.WriteHeader(http.StatusServiceUnavailable)
	}
}

func main() {
	flag.Parse()

	var probability float64
	var err error

	if siteProb.Value == "" {
		probability = defaultProb
	} else {
		probability, err = strconv.ParseFloat(siteProb.Value, 64)
		if err != nil {
			panic("unable to parse -probability flag value")
		}
	}

	if *endpoint == "" || *apiKey == "" || *service == "" || iata.Value == "" ||
		*machineType == "" || *uplink == "" {
		panic("-key, -service, -iata, -type and -uplink are required.")
	}
	if probability <= 0.0 || probability > 1.0 {
		panic("-probability must be in the range (0, 1]")
	}

	siteProb.Value = fmt.Sprintf("%f", probability)

	// Set up health server.
	mux := http.NewServeMux()
	mux.HandleFunc("/ready", Ready)
	go http.ListenAndServe(*hcAddr, mux)

	// Register for the first time.
	register()

	// Keep retrying registration every configured interval.
	t, err := memoryless.NewTicker(context.Background(), memoryless.Config{
		Expected: *interval,
		Min:      *intervalMin,
		Max:      *intervalMax,
	})
	rtx.Must(err, "Failed to create ticker")

	for range t.C {
		register()
	}
}

// Make a call to the register endpoint and write the resulting config files to
// disk. If the node is registered already, this is effectively a no-op for the
// autojoin API and will just touch the output files' last-modified time.
func register() {
	// Make a HTTP call to the autojoin service to register this node.
	registerURL, err := url.Parse(*endpoint)
	rtx.Must(err, "Failed to parse autojoin service URL")
	q := registerURL.Query()
	q.Add("api_key", *apiKey)
	q.Add("service", *service)
	q.Add("iata", iata.Value)
	q.Add("ipv4", ipv4.Value)
	q.Add("ipv6", ipv6.Value)
	q.Add("type", *machineType)
	q.Add("uplink", *uplink)
	q.Add("probability", siteProb.Value)
	for _, port := range ports {
		q.Add("ports", port)
	}
	registerURL.RawQuery = q.Encode()

	log.Printf("Registering with %s", registerURL)
	resp, err := ipv4HTTPClient().Post(registerURL.String(), "application/json", nil)
	rtx.Must(err, "POST autojoin/v0/node/register failed")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		panic("Failed to register with autojoin service")
	}

	body, err := io.ReadAll(resp.Body)
	rtx.Must(err, "Failed to read response body")

	var r v0.RegisterResponse
	json.Unmarshal(body, &r)
	if r.Error != nil {
		panic(r.Error.Title)
	}

	heartbeat := map[string]v2.Registration{r.Registration.Hostname: *r.Registration.Heartbeat}
	annotation := map[string]v0.ServerAnnotation{r.Registration.Hostname: *r.Registration.Annotation}

	// Write the hostname to a file.
	err = os.WriteFile(path.Join(*outputPath, hostnameFilename), []byte(r.Registration.Hostname), 0644)
	rtx.Must(err, "Failed to write hostname to file")

	// Marshall and write the heartbeat and annotation config files.
	heartbeatJSON, err := json.Marshal(heartbeat)
	rtx.Must(err, "Failed to marshal heartbeat")
	annotationJSON, err := json.Marshal(annotation)
	rtx.Must(err, "Failed to marshal annotation")

	err = os.WriteFile(path.Join(*outputPath, heartbeatFilename), heartbeatJSON, 0644)
	rtx.Must(err, "Failed to write heartbeat file")
	err = os.WriteFile(path.Join(*outputPath, annotationFilename), annotationJSON, 0644)
	rtx.Must(err, "Failed to write annotation file")

	if r.Registration.Credentials == nil {
		log.Fatalf("Registration credentials are nil:\n%s", body)
	}
	// Service account credentials.
	key, err := base64.StdEncoding.DecodeString(r.Registration.Credentials.ServiceAccountKey)
	rtx.Must(err, "Failed to decode service account key")
	err = os.WriteFile(path.Join(*outputPath, serviceAccountFilename), key, 0644)
	rtx.Must(err, "Failed to write annotation file")

	log.Printf("Registration successful with hostname: %s", r.Registration.Hostname)
	registerSuccess.Store(true)
}

// ipv4HTTPClient returns an HTTP client that always uses IPv4.
// Default timeouts are from https://go.dev/src/net/http/transport.go
func ipv4HTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network string, addr string) (net.Conn, error) {
				return (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext(ctx, "tcp4", addr)
			},
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}
