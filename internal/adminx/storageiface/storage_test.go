package storageiface

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/api/option"
	"google.golang.org/api/storage/v1"
)

type request struct {
	method string
	path   string
	query  string
	body   string
}

func newTestStorage(t *testing.T, reqs *[]request) *Storage {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*reqs = append(*reqs, request{method: r.Method, path: r.URL.EscapedPath(), query: r.URL.RawQuery, body: string(b)})
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	sc, err := storage.NewService(context.Background(), option.WithEndpoint(srv.URL+"/storage/v1/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatalf("storage.NewService() error = %v", err)
	}
	return NewStorage(sc)
}

func TestStorage(t *testing.T) {
	var reqs []request
	s := newTestStorage(t, &reqs)
	ctx := context.Background()
	const bucket = "archive-mlab-foo"
	const folder = "autoload/v3/org=foobar/"
	const escaped = "/storage/v1/b/archive-mlab-foo/managedFolders/autoload%2Fv3%2Forg%3Dfoobar%2F"

	if _, err := s.GetManagedFolder(ctx, bucket, folder); err != nil {
		t.Fatalf("GetManagedFolder() error = %v", err)
	}
	if _, err := s.InsertManagedFolder(ctx, bucket, &storage.ManagedFolder{Name: folder}); err != nil {
		t.Fatalf("InsertManagedFolder() error = %v", err)
	}
	if _, err := s.GetManagedFolderIamPolicy(ctx, bucket, folder); err != nil {
		t.Fatalf("GetManagedFolderIamPolicy() error = %v", err)
	}
	p := &storage.Policy{
		Etag:     "etag-1",
		Version:  3,
		Bindings: []*storage.PolicyBindings{{Role: "roles/storage.objectCreator", Members: []string{"m"}}},
	}
	if _, err := s.SetManagedFolderIamPolicy(ctx, bucket, folder, p); err != nil {
		t.Fatalf("SetManagedFolderIamPolicy() error = %v", err)
	}
	if len(reqs) != 4 {
		t.Fatalf("got %d requests, want 4", len(reqs))
	}

	if reqs[0].method != http.MethodGet || reqs[0].path != escaped {
		t.Errorf("get folder = %s %s, want GET %s", reqs[0].method, reqs[0].path, escaped)
	}

	if reqs[1].method != http.MethodPost || reqs[1].path != "/storage/v1/b/archive-mlab-foo/managedFolders" {
		t.Errorf("insert folder = %s %s", reqs[1].method, reqs[1].path)
	}
	var mf storage.ManagedFolder
	if err := json.Unmarshal([]byte(reqs[1].body), &mf); err != nil || mf.Name != folder {
		t.Errorf("insert body = %s, want name %q", reqs[1].body, folder)
	}

	if reqs[2].method != http.MethodGet || reqs[2].path != escaped+"/iam" {
		t.Errorf("get policy = %s %s, want GET %s/iam", reqs[2].method, reqs[2].path, escaped)
	}
	if want := "optionsRequestedPolicyVersion=3"; !strings.Contains(reqs[2].query, want) {
		t.Errorf("get policy query = %q, want it to contain %q", reqs[2].query, want)
	}

	if reqs[3].method != http.MethodPut || reqs[3].path != escaped+"/iam" {
		t.Errorf("set policy = %s %s, want PUT %s/iam", reqs[3].method, reqs[3].path, escaped)
	}
	var got storage.Policy
	if err := json.Unmarshal([]byte(reqs[3].body), &got); err != nil {
		t.Fatalf("set policy body %q: %v", reqs[3].body, err)
	}
	if got.Etag != "etag-1" || got.Version != 3 || len(got.Bindings) != 1 || got.Bindings[0].Members[0] != "m" {
		t.Errorf("set policy body = %s", reqs[3].body)
	}
}
