package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v0 "github.com/m-lab/autojoin/api/v0"
	v2 "github.com/m-lab/locate/api/v2"
	"golang.org/x/oauth2/google"
)

const (
	testAudience = "//iam.googleapis.com/projects/123/locations/global/workloadIdentityPools/autojoin/providers/token-exchange"
	testJWT      = "header.payload.signature"
)

func TestExchangeAPIKeyForJWT(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr bool
	}{
		{name: "success", status: http.StatusOK, body: `{"token":"abc"}`, want: "abc"},
		{name: "error-status", status: http.StatusUnauthorized, body: `{"error":"bad key"}`, wantErr: true},
		{name: "error-empty-token", status: http.StatusOK, body: `{"token":""}`, wantErr: true},
		{name: "error-bad-json", status: http.StatusOK, body: `not json`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				var got map[string]string
				if req.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", req.Method)
				}
				if err := json.NewDecoder(req.Body).Decode(&got); err != nil || got["api_key"] != "the-key" {
					t.Errorf("request body = %v (%v), want api_key=the-key", got, err)
				}
				rw.WriteHeader(tt.status)
				io.WriteString(rw, tt.body)
			}))
			defer srv.Close()
			got, err := exchangeAPIKeyForJWT(srv.URL+"/v0/token/storage", "the-key")
			if (err != nil) != tt.wantErr {
				t.Fatalf("exchangeAPIKeyForJWT() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("exchangeAPIKeyForJWT() = %q, want %q", got, tt.want)
			}
		})
	}
	t.Run("error-bad-endpoint", func(t *testing.T) {
		if _, err := exchangeAPIKeyForJWT("://bad", "k"); err == nil {
			t.Errorf("exchangeAPIKeyForJWT() with bad endpoint returned nil error")
		}
	})
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "secret")

	// A pre-existing world-readable file must be replaced, not reused.
	if err := os.WriteFile(name, []byte("old contents that are longer"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(name, []byte("new"), 0600); err != nil {
		t.Fatalf("writeFileAtomic() error = %v", err)
	}
	got, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("contents = %q, want %q", got, "new")
	}
	fi, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	// No temporary files are left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory has %d entries, want 1: %v", len(entries), entries)
	}

	if err := writeFileAtomic(filepath.Join(dir, "missing", "secret"), []byte("x"), 0600); err == nil {
		t.Errorf("writeFileAtomic() into a missing directory returned nil error")
	}
	// A target that cannot be replaced (a non-empty directory) fails and
	// leaves no temporary file.
	blocker := filepath.Join(dir, "blocker")
	if err := os.MkdirAll(filepath.Join(blocker, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(blocker, []byte("x"), 0600); err == nil {
		t.Errorf("writeFileAtomic() over a directory returned nil error")
	}
	entries, err = os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("directory has %d entries after failed write, want 2: %v", len(entries), entries)
	}
}

func TestCredentialConfig(t *testing.T) {
	got, err := credentialConfig(testAudience, "/var/lib/node/storage-token.jwt")
	if err != nil {
		t.Fatalf("credentialConfig() error = %v", err)
	}
	want := `{
  "type": "external_account",
  "audience": "//iam.googleapis.com/projects/123/locations/global/workloadIdentityPools/autojoin/providers/token-exchange",
  "subject_token_type": "urn:ietf:params:oauth:token-type:jwt",
  "token_url": "https://sts.googleapis.com/v1/token",
  "credential_source": {
    "file": "/var/lib/node/storage-token.jwt",
    "format": {
      "type": "text"
    }
  }
}
`
	if string(got) != want {
		t.Errorf("credentialConfig() =\n%s\nwant\n%s", got, want)
	}
}

func TestWriteStorageCredentials(t *testing.T) {
	tests := []struct {
		name       string
		sourcePath string
		wantSource func(dir string) string
	}{
		{
			name:       "default-source-path",
			sourcePath: "",
			wantSource: func(dir string) string { return filepath.Join(dir, storageTokenFilename) },
		},
		{
			name:       "override-source-path",
			sourcePath: "/autonode/storage-token.jwt",
			wantSource: func(string) string { return "/autonode/storage-token.jwt" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := writeStorageCredentials(dir, testJWT, testAudience, tt.sourcePath); err != nil {
				t.Fatalf("writeStorageCredentials() error = %v", err)
			}
			for _, f := range []string{storageTokenFilename, gcsCredentialsFilename} {
				fi, err := os.Stat(filepath.Join(dir, f))
				if err != nil {
					t.Fatalf("stat %s: %v", f, err)
				}
				if fi.Mode().Perm() != 0600 {
					t.Errorf("%s mode = %v, want 0600", f, fi.Mode().Perm())
				}
			}
			tok, err := os.ReadFile(filepath.Join(dir, storageTokenFilename))
			if err != nil {
				t.Fatal(err)
			}
			if string(tok) != testJWT {
				t.Errorf("token file = %q, want %q", tok, testJWT)
			}
			cfg := readConfig(t, filepath.Join(dir, gcsCredentialsFilename))
			if cfg.CredentialSource.File != tt.wantSource(dir) {
				t.Errorf("credential_source.file = %q, want %q", cfg.CredentialSource.File, tt.wantSource(dir))
			}
			if cfg.Audience != testAudience {
				t.Errorf("audience = %q, want %q", cfg.Audience, testAudience)
			}
		})
	}

	t.Run("relative-dir-yields-absolute-source", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		if err := os.Mkdir("secure", 0700); err != nil {
			t.Fatal(err)
		}
		if err := writeStorageCredentials("secure", testJWT, testAudience, ""); err != nil {
			t.Fatalf("writeStorageCredentials() error = %v", err)
		}
		cfg := readConfig(t, filepath.Join("secure", gcsCredentialsFilename))
		if !filepath.IsAbs(cfg.CredentialSource.File) {
			t.Errorf("credential_source.file = %q, want an absolute path", cfg.CredentialSource.File)
		}
	})

	t.Run("error-missing-dir", func(t *testing.T) {
		if err := writeStorageCredentials(filepath.Join(t.TempDir(), "missing"), testJWT, testAudience, ""); err == nil {
			t.Errorf("writeStorageCredentials() into a missing directory returned nil error")
		}
	})
}

func readConfig(t *testing.T, name string) externalAccountConfig {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var cfg externalAccountConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("unmarshal %s: %v", name, err)
	}
	return cfg
}

// TestCredentialConfigWithGoogleAuth checks that Google's Go auth library
// accepts the written configuration and sends the JWT file contents to STS.
// Only token_url is redirected, to a local fake STS.
func TestCredentialConfigWithGoogleAuth(t *testing.T) {
	var gotForm url.Values
	sts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if err := req.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		gotForm = req.PostForm
		rw.Header().Set("Content-Type", "application/json")
		io.WriteString(rw, `{"access_token":"federated","issued_token_type":"urn:ietf:params:oauth:token-type:access_token","token_type":"Bearer","expires_in":3600}`)
	}))
	defer sts.Close()

	dir := t.TempDir()
	if err := writeStorageCredentials(dir, testJWT, testAudience, ""); err != nil {
		t.Fatalf("writeStorageCredentials() error = %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, gcsCredentialsFilename))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	raw["token_url"] = sts.URL
	b, err = json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}

	creds, err := google.CredentialsFromJSON(context.Background(), b, "https://www.googleapis.com/auth/devstorage.read_write")
	if err != nil {
		t.Fatalf("CredentialsFromJSON() error = %v", err)
	}
	tok, err := creds.TokenSource.Token()
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if tok.AccessToken != "federated" {
		t.Errorf("access token = %q, want %q", tok.AccessToken, "federated")
	}
	if gotForm.Get("subject_token") != testJWT {
		t.Errorf("subject_token = %q, want %q", gotForm.Get("subject_token"), testJWT)
	}
	if gotForm.Get("audience") != testAudience {
		t.Errorf("audience = %q, want %q", gotForm.Get("audience"), testAudience)
	}
	if gotForm.Get("subject_token_type") != "urn:ietf:params:oauth:token-type:jwt" {
		t.Errorf("subject_token_type = %q", gotForm.Get("subject_token_type"))
	}
}

func TestDecodeCredentials(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte(`{"type":"service_account"}`))
	tests := []struct {
		name    string
		creds   *v0.Credentials
		want    string
		wantErr bool
	}{
		{name: "key-only", creds: &v0.Credentials{ServiceAccountKey: key}, want: `{"type":"service_account"}`},
		{name: "key-and-audience", creds: &v0.Credentials{ServiceAccountKey: key, WorkloadIdentityAudience: testAudience}, want: `{"type":"service_account"}`},
		{name: "audience-only", creds: &v0.Credentials{WorkloadIdentityAudience: testAudience}, want: ""},
		{name: "error-nil", creds: nil, wantErr: true},
		{name: "error-neither", creds: &v0.Credentials{}, wantErr: true},
		{name: "error-bad-base64", creds: &v0.Credentials{ServiceAccountKey: "!!!"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeCredentials(tt.creds)
			if (err != nil) != tt.wantErr {
				t.Fatalf("decodeCredentials() error = %v, wantErr %v", err, tt.wantErr)
			}
			if string(got) != tt.want {
				t.Errorf("decodeCredentials() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRegister runs a full registration pass against local fake servers and
// checks which credential files are written.
func TestRegister(t *testing.T) {
	saKey := `{"type":"service_account","private_key":"SA-SECRET"}`
	tests := []struct {
		name            string
		key             string
		audience        string
		wantSAFile      bool
		wantStorage     bool
		preexistingSA   bool
		wantSAFileKept  bool
		sourcePathFlag  string
		wantSourceInCfg string
	}{
		{name: "key-only", key: saKey, wantSAFile: true},
		{name: "key-and-audience", key: saKey, audience: testAudience, wantSAFile: true, wantStorage: true},
		{name: "audience-only", audience: testAudience, wantStorage: true},
		{name: "audience-only-keeps-existing-key-file", audience: testAudience, wantStorage: true, preexistingSA: true, wantSAFileKept: true},
		{name: "audience-only-source-override", audience: testAudience, wantStorage: true, sourcePathFlag: "/autonode/storage-token.jwt", wantSourceInCfg: "/autonode/storage-token.jwt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var storageCalls int
			tokenSrv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case "/v0/token/autojoin":
					io.WriteString(rw, `{"token":"autojoin-jwt"}`)
				case "/v0/token/storage":
					storageCalls++
					io.WriteString(rw, `{"token":"`+testJWT+`"}`)
				default:
					http.NotFound(rw, req)
				}
			}))
			defer tokenSrv.Close()
			regSrv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				if req.Header.Get("Authorization") != "Bearer autojoin-jwt" {
					t.Errorf("Authorization = %q", req.Header.Get("Authorization"))
				}
				resp := v0.RegisterResponse{
					Registration: &v0.Registration{
						Hostname:   "ndt-lga12345-c0a80001.bar.sandbox.measurement-lab.org",
						Annotation: &v0.ServerAnnotation{},
						Heartbeat:  &v2.Registration{},
						Credentials: &v0.Credentials{
							ServiceAccountKey:        base64.StdEncoding.EncodeToString([]byte(tt.key)),
							WorkloadIdentityAudience: tt.audience,
						},
					},
				}
				json.NewEncoder(rw).Encode(resp)
			}))
			defer regSrv.Close()

			pub, sec := t.TempDir(), t.TempDir()
			setFlags(t, regSrv.URL, tokenSrv.URL, pub, sec, tt.sourcePathFlag)
			if tt.preexistingSA {
				if err := os.WriteFile(filepath.Join(sec, serviceAccountFilename), []byte("old key"), 0600); err != nil {
					t.Fatal(err)
				}
			}

			var logs bytes.Buffer
			log.SetOutput(&logs)
			t.Cleanup(func() { log.SetOutput(os.Stderr) })
			registerSuccess.Store(false)

			register()

			if !registerSuccess.Load() {
				t.Errorf("registerSuccess = false, want true")
			}
			for _, secret := range []string{testJWT, "SA-SECRET"} {
				if strings.Contains(logs.String(), secret) {
					t.Errorf("logs contain secret %q:\n%s", secret, logs.String())
				}
			}

			saPath := filepath.Join(sec, serviceAccountFilename)
			saBytes, saErr := os.ReadFile(saPath)
			switch {
			case tt.wantSAFile:
				if saErr != nil || string(saBytes) != saKey {
					t.Errorf("service account file = %q (%v), want %q", saBytes, saErr, saKey)
				}
			case tt.wantSAFileKept:
				if saErr != nil || string(saBytes) != "old key" {
					t.Errorf("service account file = %q (%v), want it left untouched", saBytes, saErr)
				}
			default:
				if !os.IsNotExist(saErr) {
					t.Errorf("service account file exists (%v), want none", saErr)
				}
			}

			_, cfgErr := os.Stat(filepath.Join(sec, gcsCredentialsFilename))
			_, jwtErr := os.Stat(filepath.Join(sec, storageTokenFilename))
			if tt.wantStorage {
				if cfgErr != nil || jwtErr != nil {
					t.Fatalf("storage credentials missing: config %v, jwt %v", cfgErr, jwtErr)
				}
				if storageCalls != 1 {
					t.Errorf("storage token requests = %d, want 1", storageCalls)
				}
				cfg := readConfig(t, filepath.Join(sec, gcsCredentialsFilename))
				want := tt.wantSourceInCfg
				if want == "" {
					want = filepath.Join(sec, storageTokenFilename)
				}
				if cfg.CredentialSource.File != want {
					t.Errorf("credential_source.file = %q, want %q", cfg.CredentialSource.File, want)
				}
			} else {
				if !os.IsNotExist(cfgErr) || !os.IsNotExist(jwtErr) {
					t.Errorf("storage credentials written without an audience: config %v, jwt %v", cfgErr, jwtErr)
				}
				if storageCalls != 0 {
					t.Errorf("storage token requests = %d, want 0", storageCalls)
				}
			}
			// Public files are still written to the public directory.
			for _, f := range []string{hostnameFilename, heartbeatFilename, annotationFilename} {
				if _, err := os.Stat(filepath.Join(pub, f)); err != nil {
					t.Errorf("public file %s: %v", f, err)
				}
			}
		})
	}
}

func setFlags(t *testing.T, regURL, tokenURL, pub, sec, sourcePath string) {
	t.Helper()
	old := struct {
		endpoint, tk, stk, key, svc, typ, uplink, out, outSec, src string
		iata, ipv4, prob                                           string
	}{*endpoint, *tkEndpoint, *storageTkEndpoint, *apiKey, *service, *machineType, *uplink, *outputPath, *outputSecurePath, *credentialSourcePath, iata.Value, ipv4.Value, siteProb.Value}
	t.Cleanup(func() {
		*endpoint, *tkEndpoint, *storageTkEndpoint, *apiKey = old.endpoint, old.tk, old.stk, old.key
		*service, *machineType, *uplink = old.svc, old.typ, old.uplink
		*outputPath, *outputSecurePath, *credentialSourcePath = old.out, old.outSec, old.src
		iata.Value, ipv4.Value, siteProb.Value = old.iata, old.ipv4, old.prob
	})
	*endpoint = regURL + "/autojoin/v0/node/register/token"
	*tkEndpoint = tokenURL + "/v0/token/autojoin"
	*storageTkEndpoint = tokenURL + "/v0/token/storage"
	*apiKey = "the-key"
	*service = "ndt"
	*machineType = "virtual"
	*uplink = "1g"
	*outputPath = pub
	*outputSecurePath = sec
	*credentialSourcePath = sourcePath
	iata.Value = "lga"
	ipv4.Value = "192.168.0.1"
	siteProb.Value = "1.000000"
}
