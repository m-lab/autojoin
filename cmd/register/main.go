package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"time"

	v0 "github.com/m-lab/autojoin/api/v0"
	"github.com/m-lab/go/rtx"
	v2 "github.com/m-lab/locate/api/v2"
	"github.com/m-lab/uuid-annotator/annotator"
)

const (
	registerEndpoint   = "https://autojoin-dot-mlab-sandbox.appspot.com/autojoin/v0/node/register"
	heartbeatFilename  = "registration.json"
	annotationFilename = "annotation.json"
	hostnameFilename   = "hostname"
)

var (
	endpoint   = flag.String("endpoint", registerEndpoint, "Endpoint of the autojoin service")
	apiKey     = flag.String("key", "", "API key for the autojoin service")
	service    = flag.String("service", "ndt", "Service name to register with the autojoin service")
	org        = flag.String("organization", "", "Organization to register with the autojoin service")
	iata       = flag.String("iata", "", "IATA code to register with the autojoin service")
	ipv4       = flag.String("ipv4", "", "IPv4 address to register with the autojoin service")
	ipv6       = flag.String("ipv6", "", "IPv6 address to register with the autojoin service")
	interval   = flag.Duration("interval", 1*time.Hour, "Registration interval")
	outputPath = flag.String("output", "", "Output folder")
)

func main() {
	flag.Parse()

	if *endpoint == "" || *apiKey == "" || *service == "" || *org == "" || *iata == "" {
		panic("-key, -service, -organization, and -iata are required.")
	}

	register()

	// Keep retrying registration every configured interval.
	t := time.NewTicker(*interval)
	go func() {
		for range t.C {
			register()
		}
	}()

	<-context.Background().Done()
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
	q.Add("organization", *org)
	q.Add("iata", *iata)
	q.Add("ipv4", *ipv4)
	q.Add("ipv6", *ipv6)
	registerURL.RawQuery = q.Encode()

	log.Printf("Registering with %s", registerURL)

	resp, err := http.Post(registerURL.String(), "application/json", nil)
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
	annotation := map[string]annotator.ServerAnnotations{r.Registration.Hostname: r.Registration.Annotation.Annotation}

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

	log.Printf("Registration successful with hostname: %s", r.Registration.Hostname)
}
