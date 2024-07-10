package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"

	v0 "github.com/m-lab/autojoin/api/v0"
	"github.com/m-lab/go/rtx"
	v2 "github.com/m-lab/locate/api/v2"
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
	outputPath = flag.String("output", "", "Output folder")
)

func main() {
	flag.Parse()

	if *endpoint == "" || *apiKey == "" || *service == "" || *org == "" || *iata == "" {
		panic("-key, -service, -organization, and -iata are required.")
	}

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

	resp, err := http.Post(registerURL.String(), "application/json", nil)
	rtx.Must(err, "POST autojoin/v0/node/register failed")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Println(resp.Status)
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
}
