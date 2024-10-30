package adminx

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"testing"

	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/m-lab/autojoin/internal/dnsname"
	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/dns/v1"
	"google.golang.org/api/iam/v1"
)

func init() {
	// Silence package logging during tests.
	log.SetOutput(io.Discard)
}

type fakeCRM struct {
	getPolicy    *cloudresourcemanager.Policy
	getPolicyErr error
	setPolicyErr error
	bindingCount int
	policy       *cloudresourcemanager.Policy
}

func (f *fakeCRM) GetIamPolicy(ctx context.Context, req *cloudresourcemanager.GetIamPolicyRequest) (*cloudresourcemanager.Policy, error) {
	return f.getPolicy, f.getPolicyErr
}

func (f *fakeCRM) SetIamPolicy(ctx context.Context, req *cloudresourcemanager.SetIamPolicyRequest) error {
	f.bindingCount = len(req.Policy.Bindings)
	f.policy = req.Policy
	return f.setPolicyErr
}

type fakeDNS struct {
	regZone     *dns.ManagedZone
	regZoneErr  error
	regSplit    *dns.ResourceRecordSet
	regSplitErr error
}

func (f *fakeDNS) RegisterZone(ctx context.Context, zone *dns.ManagedZone) (*dns.ManagedZone, error) {
	return f.regZone, f.regZoneErr
}

func (f *fakeDNS) RegisterZoneSplit(ctx context.Context, zone *dns.ManagedZone) (*dns.ResourceRecordSet, error) {
	return f.regSplit, f.regSplitErr
}

type fakeAPIKeys struct {
	createKey    string
	createKeyErr error
}

func (f *fakeAPIKeys) CreateKey(ctx context.Context, org string) (string, error) {
	return f.createKey, f.createKeyErr
}

func TestOrg_Setup(t *testing.T) {
	tests := []struct {
		name         string
		project      string
		crm          *fakeCRM
		sam          IAMService
		smc          SecretManagerClient
		dns          DNS
		org          string
		keys         Keys
		updateTables bool
		bindingCount int
		wantErr      bool
	}{
		{
			name: "success",
			crm: &fakeCRM{
				getPolicy: &cloudresourcemanager.Policy{
					Bindings: []*cloudresourcemanager.Binding{
						{
							Members: []string{"foo"},
							Role:    "roles/fooWriter",
						},
					},
				},
			},
			sam: &fakeIAMService{
				getAcct: &iam.ServiceAccount{
					Name: "foo",
				},
			},
			smc: &fakeSMC{
				getSec: &secretmanagerpb.Secret{Name: "okay"},
			},
			dns: &fakeDNS{
				regZone: &dns.ManagedZone{
					Name:    dnsname.OrgZone("foo", "mlab-foo"),
					DnsName: dnsname.OrgDNS("foo", "mlab-foo"),
				},
			},
			keys: &fakeAPIKeys{
				createKey: "this-is-a-fake-key",
			},
			bindingCount: 3,
		},
		{
			name: "error-register-zone",
			crm: &fakeCRM{
				getPolicy: &cloudresourcemanager.Policy{
					Bindings: []*cloudresourcemanager.Binding{
						{
							Members: []string{"foo"},
							Role:    "roles/fooWriter",
						},
					},
				},
			},
			sam: &fakeIAMService{
				getAcct: &iam.ServiceAccount{
					Name: "foo",
				},
			},
			smc: &fakeSMC{
				getSec: &secretmanagerpb.Secret{Name: "okay"},
			},
			dns: &fakeDNS{
				regZoneErr: fmt.Errorf("fake zone registration error"),
			},
			wantErr: true,
		},
		{
			name: "error-register-split",
			crm: &fakeCRM{
				getPolicy: &cloudresourcemanager.Policy{
					Bindings: []*cloudresourcemanager.Binding{
						{
							Members: []string{"foo"},
							Role:    "roles/fooWriter",
						},
					},
				},
			},
			sam: &fakeIAMService{
				getAcct: &iam.ServiceAccount{
					Name: "foo",
				},
			},
			smc: &fakeSMC{
				getSec: &secretmanagerpb.Secret{Name: "okay"},
			},
			dns: &fakeDNS{
				regZone: &dns.ManagedZone{
					Name:    dnsname.OrgZone("foo", "mlab-foo"),
					DnsName: dnsname.OrgDNS("foo", "mlab-foo"),
				},
				regSplitErr: fmt.Errorf("fake split register error"),
			},
			wantErr: true,
		},
		{
			name: "success-equal-bindings",
			crm: &fakeCRM{
				getPolicy: &cloudresourcemanager.Policy{
					Bindings: []*cloudresourcemanager.Binding{
						{
							Members: []string{"foo"},
							Role:    "roles/fooWriter",
						},
						{
							Condition: &cloudresourcemanager.Expr{
								Expression: "resource.name.startsWith(\"projects/_/buckets/archive-mlab-foo/objects/autoload/v2/foobar\") || resource.name.startsWith(\"projects/_/buckets/staging-mlab-foo/objects/autoload/v2/foobar\")",
								Title:      "Upload restriction for foobar",
							},
							Members: []string{"serviceAccount:"},
							Role:    "roles/storage.objectCreator",
						},
					},
				},
			},
			sam: &fakeIAMService{
				getAcct: &iam.ServiceAccount{
					Name: "foo",
				},
			},
			smc: &fakeSMC{
				getSec: &secretmanagerpb.Secret{Name: "okay"},
			},
			dns: &fakeDNS{
				regZone: &dns.ManagedZone{
					Name:    dnsname.OrgZone("foo", "mlab-foo"),
					DnsName: dnsname.OrgDNS("foo", "mlab-foo"),
				},
			},
			keys: &fakeAPIKeys{
				createKey: "this-is-a-fake-key",
			},
			bindingCount: 3,
		},
		{
			name: "error-create-service-account",
			sam: &fakeIAMService{
				getAcctErr: fmt.Errorf("fake error messages"),
			},
			wantErr: true,
		},
		{
			name: "error-create-secret",
			crm: &fakeCRM{
				getPolicy: &cloudresourcemanager.Policy{
					Bindings: []*cloudresourcemanager.Binding{
						{
							Members: []string{"foo"},
							Role:    "roles/fooWriter",
						},
					},
				},
			},
			sam: &fakeIAMService{
				getAcct: &iam.ServiceAccount{
					Name: "foo",
				},
			},
			smc: &fakeSMC{
				getSecErr: fmt.Errorf("fake create secret error"),
			},
			wantErr: true,
		},
		{
			name: "error-getiam",
			crm: &fakeCRM{
				getPolicyErr: fmt.Errorf("fake get iam policy error"),
			},
			sam: &fakeIAMService{
				getAcct: &iam.ServiceAccount{
					Name: "foo",
				},
			},
			wantErr: true,
		},
		{
			name: "error-setiam",
			crm: &fakeCRM{
				getPolicy: &cloudresourcemanager.Policy{
					Bindings: []*cloudresourcemanager.Binding{
						{
							Members: []string{"foo"},
							Role:    "roles/fooWriter",
						},
					},
				},
				setPolicyErr: fmt.Errorf("fake set iam policy error"),
			},
			sam: &fakeIAMService{
				getAcct: &iam.ServiceAccount{
					Name: "foo",
				},
			},
			wantErr: true,
		},
		{
			name: "success-update-tables-policy",
			crm: &fakeCRM{
				getPolicy: &cloudresourcemanager.Policy{
					Bindings: []*cloudresourcemanager.Binding{
						{
							Members: []string{"foo"},
							Role:    "roles/fooWriter",
						},
					},
				},
			},
			sam: &fakeIAMService{
				getAcct: &iam.ServiceAccount{
					Name: "foo",
				},
			},
			smc: &fakeSMC{
				getSec: &secretmanagerpb.Secret{Name: "okay"},
			},
			dns: &fakeDNS{
				regZone: &dns.ManagedZone{
					Name:    dnsname.OrgZone("foo", "mlab-foo"),
					DnsName: dnsname.OrgDNS("foo", "mlab-foo"),
				},
			},
			keys: &fakeAPIKeys{
				createKey: "this-is-a-fake-key",
			},
			updateTables: true,
			bindingCount: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := NewNamer("mlab-foo")
			sam := NewServiceAccountsManager(tt.sam, n)
			sm := NewSecretManager(tt.smc, n, sam)
			o := NewOrg("mlab-foo", tt.crm, sam, sm, tt.dns, tt.keys, tt.updateTables)
			if _, err := o.Setup(context.Background(), "foobar"); (err != nil) != tt.wantErr {
				t.Errorf("Org.Setup() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && tt.crm != nil && tt.crm.bindingCount != tt.bindingCount {
				t.Errorf("Org.Setup() failed to count bindings = %d, want %d", tt.crm.bindingCount, tt.bindingCount)
			}
			if tt.wantErr {
				return
			}
			foundTables := false
			for _, binding := range tt.crm.policy.Bindings {
				if binding.Condition != nil {
					if strings.Contains(binding.Condition.Expression, "tables") {
						foundTables = true
					}
				}
			}
			if foundTables != tt.updateTables {
				t.Errorf("Org.Setup() failed to update tables correctly = %t, want %t", foundTables, tt.updateTables)
			}
		})
	}
}

func TestBindingIsEqual(t *testing.T) {
	tests := []struct {
		name string
		a    *cloudresourcemanager.Binding
		b    *cloudresourcemanager.Binding
		want bool
	}{
		{
			name: "equal",
			a: &cloudresourcemanager.Binding{
				Condition: &cloudresourcemanager.Expr{
					Title:      "my condition",
					Expression: "resource.name.startsWith(\"projects/_/buckets/archive-mlab-foo/objects/autoload/v2/foobar\")",
				},
				Members: []string{"a", "b"},
				Role:    "roles/tests.fooWriter",
			},
			b: &cloudresourcemanager.Binding{
				Condition: &cloudresourcemanager.Expr{
					Title:      "my condition",
					Expression: "resource.name.startsWith(\"projects/_/buckets/archive-mlab-foo/objects/autoload/v2/foobar\")",
				},
				Members: []string{"a", "b"},
				Role:    "roles/tests.fooWriter",
			},
			want: true,
		},
		{
			name: "a-missing-members-in-b",
			a: &cloudresourcemanager.Binding{
				Members: []string{"a"},
				Role:    "roles/tests.fooWriter",
			},
			b: &cloudresourcemanager.Binding{
				Members: []string{"a", "b", "c"},
				Role:    "roles/tests.fooWriter",
			},
			want: false,
		},
		{
			name: "a-missing-members-in-b",
			a: &cloudresourcemanager.Binding{
				Members: []string{"a", "b", "c"},
				Role:    "roles/tests.fooWriter",
			},
			b: &cloudresourcemanager.Binding{
				Members: []string{"a"},
				Role:    "roles/tests.fooWriter",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BindingIsEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("BindingIsEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}
