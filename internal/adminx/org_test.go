package adminx

import (
	"context"
	"fmt"
	"io"
	"log"
	"testing"

	"cloud.google.com/go/datastore"
	"github.com/m-lab/autojoin/internal/dnsname"
	"github.com/m-lab/token-exchange/store"
	"google.golang.org/api/dns/v1"
	"google.golang.org/api/storage/v1"
)

func init() {
	// Silence package logging during tests.
	log.SetOutput(io.Discard)
}

type fakeOrgManager struct {
	createOrgErr error
	createKeyErr error
	getKeysErr   error
	getOrgErr    error
	keyString    string

	createOrgCalls int
	getOrgCalls    int
}

func (f *fakeOrgManager) CreateOrganization(ctx context.Context, name, email string) error {
	f.createOrgCalls++
	return f.createOrgErr
}

func (f *fakeOrgManager) GetOrganization(ctx context.Context, name string) (*store.AutojoinOrganization, error) {
	f.getOrgCalls++
	if f.getOrgErr != nil {
		return nil, f.getOrgErr
	}
	return &store.AutojoinOrganization{Name: name}, nil
}

func (f *fakeOrgManager) CreateAPIKeyWithValue(ctx context.Context, org, val string) (string, error) {
	if f.createKeyErr != nil {
		return "", f.createKeyErr
	}
	if f.keyString == "" {
		// Return a dummy key if none specified
		return "test-api-key", nil
	}
	return f.keyString, nil
}

func (f *fakeOrgManager) GetAPIKeys(ctx context.Context, org string) ([]string, error) {
	return nil, f.getKeysErr
}

type fakeDNS struct {
	regZone     *dns.ManagedZone
	regZoneErr  error
	regSplit    *dns.ResourceRecordSet
	regSplitErr error

	regZoneCalls int
}

func (f *fakeDNS) RegisterZone(ctx context.Context, zone *dns.ManagedZone) (*dns.ManagedZone, error) {
	f.regZoneCalls++
	return f.regZone, f.regZoneErr
}

func (f *fakeDNS) RegisterZoneSplit(ctx context.Context, zone *dns.ManagedZone) (*dns.ResourceRecordSet, error) {
	return f.regSplit, f.regSplitErr
}

func okZone() *dns.ManagedZone {
	return &dns.ManagedZone{
		Name:    dnsname.OrgZone("foobar", "mlab-foo"),
		DnsName: dnsname.OrgDNS("foobar", "mlab-foo"),
	}
}

func TestOrg_Setup(t *testing.T) {
	tests := []struct {
		name          string
		org           string
		folders       *fakeManagedFolders
		dns           *fakeDNS
		orgm          *fakeOrgManager
		wantErr       bool
		wantCreateOrg int
		wantSetPolicy int
		wantDNS       int
	}{
		{
			name: "success",
			org:  testOrg,
			folders: &fakeManagedFolders{
				getFolderErr: notFound(),
				getPolicy:    &storage.Policy{Etag: "e"},
			},
			dns:           &fakeDNS{regZone: okZone()},
			orgm:          &fakeOrgManager{},
			wantCreateOrg: 1,
			wantSetPolicy: 1,
			wantDNS:       1,
		},
		{
			name:    "error-invalid-org",
			org:     "foo/bar",
			folders: &fakeManagedFolders{},
			dns:     &fakeDNS{},
			orgm:    &fakeOrgManager{},
			wantErr: true,
		},
		{
			name:    "error-datastore",
			org:     testOrg,
			folders: &fakeManagedFolders{},
			dns:     &fakeDNS{},
			orgm: &fakeOrgManager{
				createOrgErr: fmt.Errorf("datastore error"),
			},
			wantErr:       true,
			wantCreateOrg: 1,
		},
		{
			name: "error-folder",
			org:  testOrg,
			folders: &fakeManagedFolders{
				getFolder:    &storage.ManagedFolder{Name: testFolder},
				getPolicy:    &storage.Policy{Etag: "e"},
				setPolicyErr: fmt.Errorf("fake set policy error"),
			},
			dns:           &fakeDNS{regZone: okZone()},
			orgm:          &fakeOrgManager{},
			wantErr:       true,
			wantCreateOrg: 1,
			wantSetPolicy: 1,
		},
		{
			name: "error-register-zone",
			org:  testOrg,
			folders: &fakeManagedFolders{
				getFolder: &storage.ManagedFolder{Name: testFolder},
				getPolicy: &storage.Policy{Etag: "e"},
			},
			dns: &fakeDNS{
				regZoneErr: fmt.Errorf("fake zone registration error"),
			},
			orgm:          &fakeOrgManager{},
			wantErr:       true,
			wantCreateOrg: 1,
			wantSetPolicy: 1,
			wantDNS:       1,
		},
		{
			name: "error-register-split",
			org:  testOrg,
			folders: &fakeManagedFolders{
				getFolder: &storage.ManagedFolder{Name: testFolder},
				getPolicy: &storage.Policy{Etag: "e"},
			},
			dns: &fakeDNS{
				regZone:     okZone(),
				regSplitErr: fmt.Errorf("fake split register error"),
			},
			orgm:          &fakeOrgManager{},
			wantErr:       true,
			wantCreateOrg: 1,
			wantSetPolicy: 1,
			wantDNS:       1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, err := NewFolderManager(tt.folders, testProject, testProjectNumber, testPool)
			if err != nil {
				t.Fatalf("NewFolderManager() error = %v", err)
			}
			o := NewOrg(testProject, fm, tt.dns, tt.orgm)
			if err := o.Setup(context.Background(), tt.org, "testemail"); (err != nil) != tt.wantErr {
				t.Errorf("Org.Setup() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.orgm.createOrgCalls != tt.wantCreateOrg {
				t.Errorf("CreateOrganization calls = %d, want %d", tt.orgm.createOrgCalls, tt.wantCreateOrg)
			}
			if tt.folders.setPolicyCalls != tt.wantSetPolicy {
				t.Errorf("SetIamPolicy calls = %d, want %d", tt.folders.setPolicyCalls, tt.wantSetPolicy)
			}
			if tt.dns.regZoneCalls != tt.wantDNS {
				t.Errorf("RegisterZone calls = %d, want %d", tt.dns.regZoneCalls, tt.wantDNS)
			}
		})
	}
}

func TestOrg_SetupStorage(t *testing.T) {
	tests := []struct {
		name          string
		org           string
		folders       *fakeManagedFolders
		orgm          *fakeOrgManager
		wantErr       bool
		wantGetOrg    int
		wantSetPolicy int
	}{
		{
			name: "success",
			org:  testOrg,
			folders: &fakeManagedFolders{
				getFolderErr: notFound(),
				getPolicy:    &storage.Policy{Etag: "e"},
			},
			orgm:          &fakeOrgManager{},
			wantGetOrg:    1,
			wantSetPolicy: 1,
		},
		{
			name: "success-already-configured",
			org:  testOrg,
			folders: &fakeManagedFolders{
				getFolder: &storage.ManagedFolder{Name: testFolder},
				getPolicy: &storage.Policy{
					Etag: "e",
					Bindings: []*storage.PolicyBindings{
						{Role: "roles/storage.objectCreator", Members: []string{testMember}},
					},
				},
			},
			orgm:       &fakeOrgManager{},
			wantGetOrg: 1,
		},
		{
			name:    "error-invalid-org",
			org:     "*",
			folders: &fakeManagedFolders{},
			orgm:    &fakeOrgManager{},
			wantErr: true,
		},
		{
			name:    "error-org-not-found",
			org:     testOrg,
			folders: &fakeManagedFolders{},
			orgm: &fakeOrgManager{
				getOrgErr: datastore.ErrNoSuchEntity,
			},
			wantErr:    true,
			wantGetOrg: 1,
		},
		{
			name: "error-folder",
			org:  testOrg,
			folders: &fakeManagedFolders{
				getFolderErr: fmt.Errorf("fake get folder error"),
			},
			orgm:       &fakeOrgManager{},
			wantErr:    true,
			wantGetOrg: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, err := NewFolderManager(tt.folders, testProject, testProjectNumber, testPool)
			if err != nil {
				t.Fatalf("NewFolderManager() error = %v", err)
			}
			d := &fakeDNS{}
			o := NewOrg(testProject, fm, d, tt.orgm)
			if err := o.SetupStorage(context.Background(), tt.org); (err != nil) != tt.wantErr {
				t.Errorf("Org.SetupStorage() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.orgm.createOrgCalls != 0 {
				t.Errorf("SetupStorage called CreateOrganization %d times, want 0", tt.orgm.createOrgCalls)
			}
			if d.regZoneCalls != 0 {
				t.Errorf("SetupStorage called RegisterZone %d times, want 0", d.regZoneCalls)
			}
			if tt.orgm.getOrgCalls != tt.wantGetOrg {
				t.Errorf("GetOrganization calls = %d, want %d", tt.orgm.getOrgCalls, tt.wantGetOrg)
			}
			if tt.folders.setPolicyCalls != tt.wantSetPolicy {
				t.Errorf("SetIamPolicy calls = %d, want %d", tt.folders.setPolicyCalls, tt.wantSetPolicy)
			}
		})
	}
}
