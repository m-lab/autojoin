package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
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
	defaultRegisterJWTEndpoint = "https://autojoin-dot-mlab-sandbox.appspot.com/autojoin/v0/node/register/token"
	defaultTokenEndpoint       = "https://auth.mlab-sandbox.measurementlab.net/v0/token/autojoin"
	defaultStorageTkEndpoint   = "https://auth.mlab-sandbox.measurementlab.net/v0/token/storage"
	heartbeatFilename          = "registration.json"
	annotationFilename         = "annotation.json"
	serviceAccountFilename     = "service-account-autojoin.json"
	hostnameFilename           = "hostname"
	storageTokenFilename       = "storage-token.jwt"
	gcsCredentialsFilename     = "gcs-credentials.json"

	stsTokenURL      = "https://sts.googleapis.com/v1/token"
	jwtSubjectTkType = "urn:ietf:params:oauth:token-type:jwt"
)

var (
	// Replaced by the linker with the current version at build time.
	Version = "0.0.0"

	endpoint         = flag.String("endpoint", defaultRegisterJWTEndpoint, "Endpoint of the autojoin service")
	tkEndpoint       = flag.String("token-endpoint", defaultTokenEndpoint, "Token-exchange endpoint")
	apiKey           = flag.String("key", "", "API key for the autojoin service")
	service          = flag.String("service", "ndt", "Service name to register with the autojoin service")
	iata             = flagx.StringFile{}
	ipv4             = flagx.StringFile{}
	ipv6             = flagx.StringFile{}
	machineType      = flag.String("type", "", "The type of machine: physical or virtual")
	uplink           = flag.String("uplink", "", "The speed of the uplink e.g., 1g, 10g, etc.")
	interval         = flag.Duration("interval.expected", 1*time.Hour, "Expected registration interval")
	intervalMin      = flag.Duration("interval.min", 55*time.Minute, "Minimum registration interval")
	intervalMax      = flag.Duration("interval.max", 65*time.Minute, "Maximum registration interval")
	outputPath       = flag.String("output", "", "Output directory for public files (default: current directory)")
	outputSecurePath = flag.String("output-secure", "", "Output directory for credentials (default: use '-output' value)")
	siteProb         = flagx.StringFile{}
	defaultProb      = 1.0
	ports            = flagx.StringArray{}

	storageTkEndpoint = flag.String("storage-token-endpoint", defaultStorageTkEndpoint,
		"Token-exchange endpoint for the GCS storage token, used when the register response carries a workload identity audience")
	credentialSourcePath = flag.String("credential-source-path", "",
		"Directory holding "+storageTokenFilename+" as seen by the credential consumers, used for credential_source.file in "+
			gcsCredentialsFilename+" (default: absolute path of the secure output directory)")

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

	var (
		probability float64
		err         error
	)

	if siteProb.Value == "" {
		probability = defaultProb
	} else {
		probability, err = strconv.ParseFloat(siteProb.Value, 64)
		if err != nil {
			panic("unable to parse -probability flag value")
		}
	}

	if *endpoint == "" || *apiKey == "" || *service == "" || iata.Value == "" ||
		*machineType == "" || *uplink == "" || ipv4.Value == "" {
		panic("-key, -ipv4, -service, -iata, -type and -uplink are required.")
	}
	if probability <= 0.0 || probability > 1.0 {
		panic("-probability must be in the range (0, 1]")
	}

	siteProb.Value = fmt.Sprintf("%f", probability)

	// Set up health server. If it dies, exit so the supervisor restarts us:
	// a silently dead /ready endpoint makes readiness checks fail forever
	// while the process still looks healthy.
	mux := http.NewServeMux()
	mux.HandleFunc("/ready", Ready)
	go func() {
		rtx.Must(http.ListenAndServe(*hcAddr, mux), "Failed to serve /ready endpoint")
	}()

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

// Call the register endpoint and write the resulting config files to disk
// with appropriate permissions. If the node is already registered, this is
// effectively a no-op for the autojoin API and will just touch the output
// files' last-modified time.
func register() {
	// 1. Exchange API key for JWT.
	token, err := exchangeAPIKeyForJWT(*tkEndpoint, *apiKey)
	rtx.Must(err, "Failed to exchange API key for JWT")

	// 2. Prepare the register request.
	registerURL, err := url.Parse(*endpoint)
	rtx.Must(err, "Failed to parse autojoin service URL")
	q := registerURL.Query()
	q.Add("service", *service)
	q.Add("iata", iata.Value)
	q.Add("ipv4", ipv4.Value)
	q.Add("ipv6", ipv6.Value)
	q.Add("type", *machineType)
	q.Add("uplink", *uplink)
	q.Add("probability", siteProb.Value)
	q.Add("version", Version)
	// Declares that this client can use workload identity federation, so
	// the server may answer without a service account key.
	q.Add("wif", "true")

	for _, port := range ports {
		q.Add("ports", port)
	}
	registerURL.RawQuery = q.Encode()

	log.Printf("Registering with %s", registerURL)
	req, err := http.NewRequest("POST", registerURL.String(), nil)
	rtx.Must(err, "Failed to create register request")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	// See https://github.com/m-lab/autojoin/pull/34 for context
	// about why we need to use an IPv4 client.
	resp, err := ipv4HTTPClient().Do(req)
	rtx.Must(err, "POST autojoin/v0/node/register failed")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	rtx.Must(err, "Failed to read response body")

	// Note: below we avoid logging the body because it would potentially
	// put the service account key into the logs. Here logging the body has
	// some utility. The response may come from a middleware and it might
	// contain clues about why we didn't get a 200 OK.
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Failed to register with autojoin service:\n%s", string(body))
	}

	// Unmarshal and ensure the response is successful and well formed
	// before starting to write files on disk.
	var r v0.RegisterResponse
	err = json.Unmarshal(body, &r)
	rtx.Must(err, "Failed to unmarshal the register response")
	if r.Error != nil {
		log.Fatalf("Register response indicates failure: %s", r.Error.Title)
	}
	if r.Registration == nil {
		log.Fatal("The .Registration field is nil")
	}
	if r.Registration.Heartbeat == nil {
		log.Fatal("The .Registration.Heartbeat field is nil")
	}
	if r.Registration.Annotation == nil {
		log.Fatal("The .Registration.Annotation field is nil")
	}
	if r.Registration.Hostname == "" {
		log.Fatal("The .Registration.Hostname field is empty")
	}
	saKey, err := decodeCredentials(r.Registration.Credentials)
	rtx.Must(err, "Invalid .Registration.Credentials")

	// When the server supports workload identity federation it sends an
	// audience for every org. Fetch the storage token now, so that no file is
	// written unless every request succeeded.
	audience := r.Registration.Credentials.WorkloadIdentityAudience
	var storageToken string
	if audience != "" {
		storageToken, err = exchangeAPIKeyForJWT(*storageTkEndpoint, *apiKey)
		rtx.Must(err, "Failed to exchange API key for the storage token")
	}

	heartbeat := map[string]v2.Registration{r.Registration.Hostname: *r.Registration.Heartbeat}
	annotation := map[string]v0.ServerAnnotation{r.Registration.Hostname: *r.Registration.Annotation}

	// Write the hostname to a file.
	err = os.WriteFile(path.Join(*outputPath, hostnameFilename), []byte(r.Registration.Hostname), 0644)
	rtx.Must(err, "Failed to write hostname to file")

	// Marshal and write the heartbeat and annotation config files.
	heartbeatJSON, err := json.Marshal(heartbeat)
	rtx.Must(err, "Failed to marshal heartbeat")

	annotationJSON, err := json.Marshal(annotation)
	rtx.Must(err, "Failed to marshal annotation")

	err = os.WriteFile(path.Join(*outputPath, heartbeatFilename), heartbeatJSON, 0644)
	rtx.Must(err, "Failed to write heartbeat file")

	err = os.WriteFile(path.Join(*outputPath, annotationFilename), annotationJSON, 0644)
	rtx.Must(err, "Failed to write annotation file")

	// Service account credentials with 0600 permissions.
	//
	// We take care both of keys already written on disk via os.Chmod
	// and new keys, addressed via os.WriteFile.
	actualSecPath := *outputSecurePath
	if actualSecPath == "" {
		actualSecPath = *outputPath // keep backward compatibility
	}
	// The key is empty for orgs that use workload identity federation only.
	// Do not overwrite a key file that may already be on disk with nothing.
	if len(saKey) > 0 {
		serviceAccountFilePath := path.Join(actualSecPath, serviceAccountFilename)

		rtx.Must(
			os.WriteFile(serviceAccountFilePath, saKey, 0600),
			"Failed to write the service-account-key file",
		)

		rtx.Must(
			os.Chmod(serviceAccountFilePath, 0600),
			"Failed to fix the service-account-key file perms",
		)
	}

	if audience != "" {
		rtx.Must(
			writeStorageCredentials(actualSecPath, storageToken, audience, *credentialSourcePath),
			"Failed to write the storage credentials",
		)
	}

	log.Printf("Registration successful with hostname: %s", r.Registration.Hostname)
	registerSuccess.Store(true)
}

// decodeCredentials checks the credentials of a register response and returns
// the decoded service account key. The key is empty when the org uses
// workload identity federation only; a response with neither a key nor an
// audience is an error.
func decodeCredentials(c *v0.Credentials) ([]byte, error) {
	if c == nil {
		return nil, errors.New("the .Registration.Credentials field is nil")
	}
	key, err := base64.StdEncoding.DecodeString(c.ServiceAccountKey)
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode the service-account credentials string: %w", err)
	}
	if len(key) == 0 && c.WorkloadIdentityAudience == "" {
		return nil, errors.New("credentials contain neither a service account key nor a workload identity audience")
	}
	return key, nil
}

// externalAccountConfig is a Google external_account credential configuration
// that exchanges a JWT read from a file for a federated access token.
type externalAccountConfig struct {
	Type             string           `json:"type"`
	Audience         string           `json:"audience"`
	SubjectTokenType string           `json:"subject_token_type"`
	TokenURL         string           `json:"token_url"`
	CredentialSource credentialSource `json:"credential_source"`
}

type credentialSource struct {
	File   string           `json:"file"`
	Format credentialFormat `json:"format"`
}

type credentialFormat struct {
	Type string `json:"type"`
}

// credentialConfig returns the external_account configuration for the given
// STS audience, reading the subject token from tokenFile.
func credentialConfig(audience, tokenFile string) ([]byte, error) {
	cfg := externalAccountConfig{
		Type:             "external_account",
		Audience:         audience,
		SubjectTokenType: jwtSubjectTkType,
		TokenURL:         stsTokenURL,
		CredentialSource: credentialSource{
			File:   tokenFile,
			Format: credentialFormat{Type: "text"},
		},
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// writeStorageCredentials writes the storage token and the external_account
// configuration that points at it into dir. sourcePath, when not empty,
// replaces dir in the token path written to the configuration, for consumers
// that see dir under a different mount point. Neither the token nor the
// configuration is logged.
func writeStorageCredentials(dir, token, audience, sourcePath string) error {
	tokenPath := filepath.Join(dir, storageTokenFilename)
	if sourcePath == "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		sourcePath = abs
	}
	cfg, err := credentialConfig(audience, filepath.Join(sourcePath, storageTokenFilename))
	if err != nil {
		return err
	}
	// Write the token first, so the configuration never points at a file
	// that does not exist yet.
	if err := writeFileAtomic(tokenPath, []byte(token), 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", storageTokenFilename, err)
	}
	if err := writeFileAtomic(filepath.Join(dir, gcsCredentialsFilename), cfg, 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", gcsCredentialsFilename, err)
	}
	return nil
}

// writeFileAtomic writes data to a temporary file in the same directory as
// name and renames it over name, so readers see either the old or the new
// contents and never a partial file. The file has mode perm.
func writeFileAtomic(name string, data []byte, perm os.FileMode) (err error) {
	f, err := os.CreateTemp(filepath.Dir(name), "."+filepath.Base(name)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if err = f.Chmod(perm); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, name)
}

// exchangeAPIKeyForJWT exchanges an API key for a JWT at the given
// token-exchange endpoint.
func exchangeAPIKeyForJWT(endpoint, apiKey string) (string, error) {
	payload := map[string]string{
		"api_key": apiKey,
	}
	payloadBytes, err := json.Marshal(payload)
	// json.Marshal cannot fail: the map above is always marshalable.
	rtx.Must(err, "Failed to marshal payload")
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token exchange failed: %s", string(b))
	}
	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Token == "" {
		return "", fmt.Errorf("token exchange returned empty token")
	}
	log.Println("Token exchange successful")
	return result.Token, nil
}

// ipv4HTTPClient returns an HTTP client that always uses IPv4.
//
// See https://github.com/m-lab/autojoin/pull/34 for context regarding why
// we need to use an IPv4 client for registering a node.
//
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
