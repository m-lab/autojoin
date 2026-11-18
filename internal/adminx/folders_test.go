package adminx

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/storage/v1"
)

const (
	testProject       = "mlab-foo"
	testProjectNumber = "123456789"
	testPool          = "autojoin"
	testOrg           = "foobar"
	testBucket        = "archive-mlab-foo"
	testFolder        = "autoload/v2/foobar/"
	testMember        = "principalSet://iam.googleapis.com/projects/123456789/locations/global/workloadIdentityPools/autojoin/attribute.org/foobar"
)

type fakeManagedFolders struct {
	getFolder    *storage.ManagedFolder
	getFolderErr error
	insertErr    error
	getPolicy    *storage.Policy
	getPolicyErr error
	setPolicyErr error

	getCalls       int
	insertCalls    int
	getPolicyCalls int
	setPolicyCalls int

	insertBucket    string
	insertFolder    *storage.ManagedFolder
	getBucket       string
	getName         string
	getPolicyBucket string
	getPolicyName   string
	setBucket       string
	setName         string
	setPolicy       *storage.Policy
}

func (f *fakeManagedFolders) GetManagedFolder(ctx context.Context, bucket, folder string) (*storage.ManagedFolder, error) {
	f.getCalls++
	f.getBucket = bucket
	f.getName = folder
	return f.getFolder, f.getFolderErr
}

func (f *fakeManagedFolders) InsertManagedFolder(ctx context.Context, bucket string, folder *storage.ManagedFolder) (*storage.ManagedFolder, error) {
	f.insertCalls++
	f.insertBucket = bucket
	f.insertFolder = folder
	if f.insertErr != nil {
		return nil, f.insertErr
	}
	return folder, nil
}

func (f *fakeManagedFolders) GetManagedFolderIamPolicy(ctx context.Context, bucket, folder string) (*storage.Policy, error) {
	f.getPolicyCalls++
	f.getPolicyBucket = bucket
	f.getPolicyName = folder
	return f.getPolicy, f.getPolicyErr
}

func (f *fakeManagedFolders) SetManagedFolderIamPolicy(ctx context.Context, bucket, folder string, policy *storage.Policy) (*storage.Policy, error) {
	f.setPolicyCalls++
	f.setBucket = bucket
	f.setName = folder
	f.setPolicy = policy
	if f.setPolicyErr != nil {
		return nil, f.setPolicyErr
	}
	return policy, nil
}

func notFound() error {
	return &googleapi.Error{Code: http.StatusNotFound, Message: "not found"}
}

func TestValidateOrgName(t *testing.T) {
	valid := []string{"a", "foo", "foobar", "rnp", "mlab", "mattmathis", "sbs", "foo-bar", "org1", "1org", "abcdefghijklmnopqrstu"}
	for _, org := range valid {
		if err := ValidateOrgName(org); err != nil {
			t.Errorf("ValidateOrgName(%q) = %v, want nil", org, err)
		}
	}
	invalid := []string{
		"", "*", "foo/bar", "foo/", "/foo", "..", "../foo", "foo*", "Foo", "FOO",
		"-foo", "foo-", "foo_bar", "foo.bar", "foo bar", " foo", "foo\n", "foo%2f",
		"fóo", "abcdefghijklmnopqrstuv", "foo:bar", "foo@bar",
	}
	for _, org := range invalid {
		if err := ValidateOrgName(org); err == nil {
			t.Errorf("ValidateOrgName(%q) = nil, want error", org)
		}
	}
}

func TestNewFolderManager(t *testing.T) {
	tests := []struct {
		name          string
		project       string
		projectNumber string
		pool          string
		wantErr       bool
	}{
		{name: "success", project: testProject, projectNumber: testProjectNumber, pool: testPool},
		{name: "success-pool-with-hyphen", project: testProject, projectNumber: testProjectNumber, pool: "auto-join"},
		{name: "error-empty-project", project: "", projectNumber: testProjectNumber, pool: testPool, wantErr: true},
		{name: "error-empty-project-number", project: testProject, projectNumber: "", pool: testPool, wantErr: true},
		{name: "error-project-id-as-number", project: testProject, projectNumber: "mlab-foo", pool: testPool, wantErr: true},
		{name: "error-project-number-slash", project: testProject, projectNumber: "123/456", pool: testPool, wantErr: true},
		{name: "error-empty-pool", project: testProject, projectNumber: testProjectNumber, pool: "", wantErr: true},
		{name: "error-pool-slash", project: testProject, projectNumber: testProjectNumber, pool: "auto/join", wantErr: true},
		{name: "error-pool-wildcard", project: testProject, projectNumber: testProjectNumber, pool: "*", wantErr: true},
		{name: "error-pool-too-short", project: testProject, projectNumber: testProjectNumber, pool: "abc", wantErr: true},
		{name: "error-pool-uppercase", project: testProject, projectNumber: testProjectNumber, pool: "AutoJoin", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewFolderManager(&fakeManagedFolders{}, tt.project, tt.projectNumber, tt.pool)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewFolderManager() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFolderManager_Names(t *testing.T) {
	fm, err := NewFolderManager(&fakeManagedFolders{}, testProject, testProjectNumber, testPool)
	if err != nil {
		t.Fatalf("NewFolderManager() error = %v", err)
	}
	if got := fm.Bucket(); got != testBucket {
		t.Errorf("Bucket() = %q, want %q", got, testBucket)
	}
	folder, err := fm.FolderName(testOrg)
	if err != nil || folder != testFolder {
		t.Errorf("FolderName() = %q, %v, want %q, nil", folder, err, testFolder)
	}
	member, err := fm.OrgMember(testOrg)
	if err != nil || member != testMember {
		t.Errorf("OrgMember() = %q, %v, want %q, nil", member, err, testMember)
	}
	for _, org := range []string{"", "*", "foo/bar", "../foo"} {
		if got, err := fm.FolderName(org); err == nil {
			t.Errorf("FolderName(%q) = %q, want error", org, got)
		}
		if got, err := fm.OrgMember(org); err == nil {
			t.Errorf("OrgMember(%q) = %q, want error", org, got)
		}
	}
}

func TestFolderManager_EnsureOrgFolder(t *testing.T) {
	existingFolder := &storage.ManagedFolder{Bucket: testBucket, Name: testFolder}
	otherBinding := &storage.PolicyBindings{
		Role:    "roles/storage.admin",
		Members: []string{"group:admins@example.com"},
	}
	tests := []struct {
		name       string
		org        string
		fake       *fakeManagedFolders
		wantErr    bool
		wantInsert int
		wantGetPol int
		wantSet    int
		wantPolicy *storage.Policy
	}{
		{
			name: "folder-missing",
			org:  testOrg,
			fake: &fakeManagedFolders{
				getFolderErr: notFound(),
				getPolicy:    &storage.Policy{Etag: "etag-1", Version: 1},
			},
			wantInsert: 1,
			wantGetPol: 1,
			wantSet:    1,
			wantPolicy: &storage.Policy{
				Etag:    "etag-1",
				Version: 1,
				Bindings: []*storage.PolicyBindings{
					{Role: "roles/storage.objectCreator", Members: []string{testMember}},
				},
			},
		},
		{
			name: "folder-missing-insert-conflict",
			org:  testOrg,
			fake: &fakeManagedFolders{
				getFolderErr: notFound(),
				insertErr:    &googleapi.Error{Code: http.StatusConflict, Message: "exists"},
				getPolicy:    &storage.Policy{Etag: "etag-1", Version: 1},
			},
			wantInsert: 1,
			wantGetPol: 1,
			wantSet:    1,
			wantPolicy: &storage.Policy{
				Etag:    "etag-1",
				Version: 1,
				Bindings: []*storage.PolicyBindings{
					{Role: "roles/storage.objectCreator", Members: []string{testMember}},
				},
			},
		},
		{
			name: "folder-present-without-binding",
			org:  testOrg,
			fake: &fakeManagedFolders{
				getFolder: existingFolder,
				getPolicy: &storage.Policy{
					Etag:     "etag-2",
					Version:  1,
					Bindings: []*storage.PolicyBindings{otherBinding},
				},
			},
			wantGetPol: 1,
			wantSet:    1,
			wantPolicy: &storage.Policy{
				Etag:    "etag-2",
				Version: 1,
				Bindings: []*storage.PolicyBindings{
					otherBinding,
					{Role: "roles/storage.objectCreator", Members: []string{testMember}},
				},
			},
		},
		{
			name: "folder-present-role-bound-to-other-member",
			org:  testOrg,
			fake: &fakeManagedFolders{
				getFolder: existingFolder,
				getPolicy: &storage.Policy{
					Etag:    "etag-3",
					Version: 1,
					Bindings: []*storage.PolicyBindings{
						otherBinding,
						{Role: "roles/storage.objectCreator", Members: []string{"user:alice@example.com"}},
					},
				},
			},
			wantGetPol: 1,
			wantSet:    1,
			wantPolicy: &storage.Policy{
				Etag:    "etag-3",
				Version: 1,
				Bindings: []*storage.PolicyBindings{
					otherBinding,
					{Role: "roles/storage.objectCreator", Members: []string{"user:alice@example.com", testMember}},
				},
			},
		},
		{
			name: "folder-present-conditional-binding-only",
			org:  testOrg,
			fake: &fakeManagedFolders{
				getFolder: existingFolder,
				getPolicy: &storage.Policy{
					Etag:    "etag-4",
					Version: 3,
					Bindings: []*storage.PolicyBindings{
						{
							Role:      "roles/storage.objectCreator",
							Members:   []string{testMember},
							Condition: &storage.Expr{Title: "t", Expression: "request.time < timestamp(\"2000-01-01T00:00:00Z\")"},
						},
					},
				},
			},
			wantGetPol: 1,
			wantSet:    1,
			wantPolicy: &storage.Policy{
				Etag:    "etag-4",
				Version: 3,
				Bindings: []*storage.PolicyBindings{
					{
						Role:      "roles/storage.objectCreator",
						Members:   []string{testMember},
						Condition: &storage.Expr{Title: "t", Expression: "request.time < timestamp(\"2000-01-01T00:00:00Z\")"},
					},
					{Role: "roles/storage.objectCreator", Members: []string{testMember}},
				},
			},
		},
		{
			name: "folder-present-binding-for-other-org-only",
			org:  testOrg,
			fake: &fakeManagedFolders{
				getFolder: existingFolder,
				getPolicy: &storage.Policy{
					Etag:    "etag-5",
					Version: 1,
					Bindings: []*storage.PolicyBindings{
						{Role: "roles/storage.objectCreator", Members: []string{testMember + "x"}},
					},
				},
			},
			wantGetPol: 1,
			wantSet:    1,
			wantPolicy: &storage.Policy{
				Etag:    "etag-5",
				Version: 1,
				Bindings: []*storage.PolicyBindings{
					{Role: "roles/storage.objectCreator", Members: []string{testMember + "x", testMember}},
				},
			},
		},
		{
			name: "folder-present-with-binding",
			org:  testOrg,
			fake: &fakeManagedFolders{
				getFolder: existingFolder,
				getPolicy: &storage.Policy{
					Etag:    "etag-6",
					Version: 1,
					Bindings: []*storage.PolicyBindings{
						otherBinding,
						{Role: "roles/storage.objectCreator", Members: []string{"user:alice@example.com", testMember}},
					},
				},
			},
			wantGetPol: 1,
			wantSet:    0,
		},
		{
			name: "error-get-folder",
			org:  testOrg,
			fake: &fakeManagedFolders{
				getFolderErr: &googleapi.Error{Code: http.StatusForbidden, Message: "denied"},
			},
			wantErr: true,
		},
		{
			name: "error-get-folder-non-api",
			org:  testOrg,
			fake: &fakeManagedFolders{
				getFolderErr: errors.New("network down"),
			},
			wantErr: true,
		},
		{
			name: "error-insert-folder",
			org:  testOrg,
			fake: &fakeManagedFolders{
				getFolderErr: notFound(),
				insertErr:    &googleapi.Error{Code: http.StatusForbidden, Message: "denied"},
			},
			wantErr:    true,
			wantInsert: 1,
		},
		{
			name: "error-get-policy",
			org:  testOrg,
			fake: &fakeManagedFolders{
				getFolder:    existingFolder,
				getPolicyErr: errors.New("fake get policy error"),
			},
			wantErr:    true,
			wantGetPol: 1,
		},
		{
			name: "error-get-policy-nil",
			org:  testOrg,
			fake: &fakeManagedFolders{
				getFolder: existingFolder,
			},
			wantErr:    true,
			wantGetPol: 1,
		},
		{
			name: "error-set-policy",
			org:  testOrg,
			fake: &fakeManagedFolders{
				getFolder:    existingFolder,
				getPolicy:    &storage.Policy{Etag: "etag-7"},
				setPolicyErr: &googleapi.Error{Code: http.StatusPreconditionFailed, Message: "etag mismatch"},
			},
			wantErr:    true,
			wantGetPol: 1,
			wantSet:    1,
		},
		{
			name:    "error-invalid-org-empty",
			org:     "",
			fake:    &fakeManagedFolders{},
			wantErr: true,
		},
		{
			name:    "error-invalid-org-slash",
			org:     "foo/bar",
			fake:    &fakeManagedFolders{},
			wantErr: true,
		},
		{
			name:    "error-invalid-org-wildcard",
			org:     "*",
			fake:    &fakeManagedFolders{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, err := NewFolderManager(tt.fake, testProject, testProjectNumber, testPool)
			if err != nil {
				t.Fatalf("NewFolderManager() error = %v", err)
			}
			err = fm.EnsureOrgFolder(context.Background(), tt.org)
			if (err != nil) != tt.wantErr {
				t.Fatalf("EnsureOrgFolder() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.fake.insertCalls != tt.wantInsert {
				t.Errorf("insert calls = %d, want %d", tt.fake.insertCalls, tt.wantInsert)
			}
			if tt.fake.getPolicyCalls != tt.wantGetPol {
				t.Errorf("get policy calls = %d, want %d", tt.fake.getPolicyCalls, tt.wantGetPol)
			}
			if tt.fake.setPolicyCalls != tt.wantSet {
				t.Errorf("set policy calls = %d, want %d", tt.fake.setPolicyCalls, tt.wantSet)
			}
			if tt.fake.getCalls > 0 && (tt.fake.getBucket != testBucket || tt.fake.getName != testFolder) {
				t.Errorf("get folder = %q/%q, want %q/%q", tt.fake.getBucket, tt.fake.getName, testBucket, testFolder)
			}
			if tt.fake.insertCalls > 0 {
				if tt.fake.insertBucket != testBucket || tt.fake.insertFolder == nil || tt.fake.insertFolder.Name != testFolder {
					t.Errorf("insert folder = %q/%#v, want %q/%q", tt.fake.insertBucket, tt.fake.insertFolder, testBucket, testFolder)
				}
			}
			if tt.fake.getPolicyCalls > 0 && (tt.fake.getPolicyBucket != testBucket || tt.fake.getPolicyName != testFolder) {
				t.Errorf("get policy = %q/%q, want %q/%q", tt.fake.getPolicyBucket, tt.fake.getPolicyName, testBucket, testFolder)
			}
			if tt.fake.setPolicyCalls > 0 && (tt.fake.setBucket != testBucket || tt.fake.setName != testFolder) {
				t.Errorf("set policy = %q/%q, want %q/%q", tt.fake.setBucket, tt.fake.setName, testBucket, testFolder)
			}
			if tt.wantPolicy != nil && !reflect.DeepEqual(tt.fake.setPolicy, tt.wantPolicy) {
				t.Errorf("set policy = %s, want %s", policyString(tt.fake.setPolicy), policyString(tt.wantPolicy))
			}
		})
	}
}

func TestFolderManager_EnsureOrgFolderDoesNotMutateFetchedPolicy(t *testing.T) {
	orig := &storage.PolicyBindings{Role: "roles/storage.objectCreator", Members: []string{"user:alice@example.com"}}
	fake := &fakeManagedFolders{
		getFolder: &storage.ManagedFolder{Name: testFolder},
		getPolicy: &storage.Policy{Etag: "e", Bindings: []*storage.PolicyBindings{orig}},
	}
	fm, err := NewFolderManager(fake, testProject, testProjectNumber, testPool)
	if err != nil {
		t.Fatalf("NewFolderManager() error = %v", err)
	}
	if err := fm.EnsureOrgFolder(context.Background(), testOrg); err != nil {
		t.Fatalf("EnsureOrgFolder() error = %v", err)
	}
	if len(orig.Members) != 1 {
		t.Errorf("fetched binding was mutated: %v", orig.Members)
	}
}

func policyString(p *storage.Policy) string {
	if p == nil {
		return "<nil>"
	}
	b, _ := p.MarshalJSON()
	return string(b)
}
