package dnsx

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/go-test/deep"
	"github.com/m-lab/autojoin/internal/dnsname"
	"github.com/m-lab/autojoin/internal/dnsx/dnsiface"
	"google.golang.org/api/dns/v1"
	"google.golang.org/api/googleapi"
)

type result struct {
	get  *dns.ResourceRecordSet
	chg  *dns.Change
	zone *dns.ManagedZone
	err  error
}
type fakeDNS2 struct {
	results map[string]result
}

func (f *fakeDNS2) ResourceRecordSetsGet(ctx context.Context, project string, zone string, name string, rtype string) (*dns.ResourceRecordSet, error) {
	r := f.results["get-"+zone+"-"+name+"-"+rtype]
	return r.get, r.err
}
func (f *fakeDNS2) ChangeCreate(ctx context.Context, project string, zone string, change *dns.Change) (*dns.Change, error) {
	r := f.results["chg-"+zone]
	return r.chg, r.err
}
func (f *fakeDNS2) GetManagedZone(ctx context.Context, project, zoneName string) (*dns.ManagedZone, error) {
	r := f.results["getzone-"+zoneName]
	return r.zone, r.err
}
func (f *fakeDNS2) CreateManagedZone(ctx context.Context, project string, zone *dns.ManagedZone) (*dns.ManagedZone, error) {
	r := f.results["createzone-"+zone.Name]
	return r.zone, r.err
}

type fakeDNS struct {
	record []*dns.ResourceRecordSet
	i      int
	getErr error
	chgErr error
}

func (f *fakeDNS) ResourceRecordSetsGet(ctx context.Context, project string, zone string, name string, rtype string) (*dns.ResourceRecordSet, error) {
	var x *dns.ResourceRecordSet
	if f.i < len(f.record) {
		x = f.record[f.i]
		f.i++
	}
	return x, f.getErr
}

func (f *fakeDNS) ChangeCreate(ctx context.Context, project string, zone string, change *dns.Change) (*dns.Change, error) {
	if change.Additions == nil && change.Deletions == nil {
		return nil, errors.New("fake change create error")
	}
	if f.chgErr != nil {
		return nil, f.chgErr
	}
	return change, nil
}

func (f *fakeDNS) CreateManagedZone(ctx context.Context, project string, zone *dns.ManagedZone) (*dns.ManagedZone, error) {
	return nil, nil
}

func (f *fakeDNS) GetManagedZone(ctx context.Context, project, zoneName string) (*dns.ManagedZone, error) {
	return nil, nil
}

func TestManager_Register(t *testing.T) {
	tests := []struct {
		name     string
		zone     string
		service  dnsiface.Service
		hostname string
		ipv4     string
		ipv6     string
		want     *dns.Change
		wantErr  bool
	}{
		{
			name:     "success",
			zone:     "sandbox-measurement-lab-org",
			service:  &fakeDNS{getErr: &googleapi.Error{Code: 404}},
			hostname: "foo.sandbox.measurement-lab.org",
			ipv4:     "192.168.0.1",
			ipv6:     "",
			want: &dns.Change{
				Additions: []*dns.ResourceRecordSet{
					{
						Name:    "foo.sandbox.measurement-lab.org",
						Type:    "A",
						Ttl:     300,
						Rrdatas: []string{"192.168.0.1"},
					},
				},
			},
		},
		{
			name: "success-ipv6",
			zone: "sandbox-measurement-lab-org",
			service: &fakeDNS{record: []*dns.ResourceRecordSet{
				{
					Name:    "foo.sandbox.measurement-lab.org",
					Type:    "A",
					Ttl:     300,
					Rrdatas: []string{"127.0.0.1"}, // will be removed.
				},
				{
					Name:    "foo.sandbox.measurement-lab.org",
					Type:    "AAAA",
					Ttl:     300,
					Rrdatas: []string{"fe80::1002:161f:ae39:a2c9"}, // will be kept.
				},
			}},
			hostname: "foo.sandbox.measurement-lab.org",
			ipv4:     "192.168.0.1",
			ipv6:     "fe80::1002:161f:ae39:a2c9",
			want: &dns.Change{
				Additions: []*dns.ResourceRecordSet{
					{
						Name:    "foo.sandbox.measurement-lab.org",
						Type:    "A",
						Ttl:     300,
						Rrdatas: []string{"192.168.0.1"},
					},
				},
				Deletions: []*dns.ResourceRecordSet{
					{
						Name:    "foo.sandbox.measurement-lab.org",
						Type:    "A",
						Ttl:     300,
						Rrdatas: []string{"127.0.0.1"},
					},
				},
			},
		},
		{
			name: "success-ipv6-replace",
			zone: "sandbox-measurement-lab-org",
			service: &fakeDNS{record: []*dns.ResourceRecordSet{
				{
					Name:    "foo.sandbox.measurement-lab.org",
					Type:    "A",
					Ttl:     300,
					Rrdatas: []string{"192.168.0.1"}, // will be kept.
				},
				{
					Name:    "foo.sandbox.measurement-lab.org",
					Type:    "AAAA",
					Ttl:     300,
					Rrdatas: []string{"abc:def::1"}, // will be removed.
				},
			}},
			hostname: "foo.sandbox.measurement-lab.org",
			ipv4:     "192.168.0.1",
			ipv6:     "fe80::1002:161f:ae39:a2c9",
			want: &dns.Change{
				Additions: []*dns.ResourceRecordSet{
					{
						Name:    "foo.sandbox.measurement-lab.org",
						Type:    "AAAA",
						Ttl:     300,
						Rrdatas: []string{"fe80::1002:161f:ae39:a2c9"},
					},
				},
				Deletions: []*dns.ResourceRecordSet{
					{
						Name:    "foo.sandbox.measurement-lab.org",
						Type:    "AAAA",
						Ttl:     300,
						Rrdatas: []string{"abc:def::1"},
					},
				},
			},
		},
		{
			name:     "error-change",
			zone:     "sandbox-measurement-lab-org",
			service:  &fakeDNS{getErr: &googleapi.Error{Code: 404}, chgErr: errors.New("err")},
			hostname: "foo.sandbox.measurement-lab.org",
			ipv4:     "192.168.0.1",
			ipv6:     "fe80::1002:161f:ae39:a2c9",
			wantErr:  true,
		},
		{
			name:     "error-non-google",
			zone:     "sandbox-measurement-lab-org",
			service:  &fakeDNS{getErr: errors.New("different error"), chgErr: errors.New("err")},
			hostname: "foo.sandbox.measurement-lab.org",
			ipv4:     "192.168.0.1",
			ipv6:     "fe80::1002:161f:ae39:a2c9",
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewManager(tt.service, "mlab-sandbox", tt.zone)
			got, err := d.Register(context.Background(), tt.hostname, tt.ipv4, tt.ipv6)
			if (err != nil) != tt.wantErr {
				t.Errorf("Manager.Register() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := deep.Equal(got, tt.want); diff != nil {
				t.Errorf("Manager.Register() change returned != change expected: %s", strings.Join(diff, "\n"))
			}
		})
	}
}

func TestManager_Delete(t *testing.T) {
	tests := []struct {
		name     string
		zone     string
		service  dnsiface.Service
		hostname string
		want     *dns.Change
		wantErr  bool
	}{
		{
			name: "success",
			zone: "sandbox-measurement-lab-org",
			service: &fakeDNS{record: []*dns.ResourceRecordSet{
				{
					Name:    "foo.sandbox.measurement-lab.org",
					Type:    "A",
					Ttl:     300,
					Rrdatas: []string{"192.168.0.1"},
				},
				{
					Name:    "foo.sandbox.measurement-lab.org",
					Type:    "AAAA",
					Ttl:     300,
					Rrdatas: []string{"fe80::1002:161f:ae39:a2c9"},
				},
			}},
			hostname: "foo.sandbox.measurement-lab.org",
			want: &dns.Change{
				Deletions: []*dns.ResourceRecordSet{
					{
						Name:    "foo.sandbox.measurement-lab.org",
						Type:    "A",
						Ttl:     300,
						Rrdatas: []string{"192.168.0.1"},
					},
					{
						Name:    "foo.sandbox.measurement-lab.org",
						Type:    "AAAA",
						Ttl:     300,
						Rrdatas: []string{"fe80::1002:161f:ae39:a2c9"},
					},
				},
			},
		},
		{
			name:     "error-change",
			zone:     "sandbox-measurement-lab-org",
			service:  &fakeDNS{getErr: &googleapi.Error{Code: 404}, chgErr: errors.New("err")},
			hostname: "foo.sandbox.measurement-lab.org",
			wantErr:  true,
		},
		{
			name:     "error-non-google",
			zone:     "sandbox-measurement-lab-org",
			service:  &fakeDNS{getErr: errors.New("different error"), chgErr: errors.New("err")},
			hostname: "foo.sandbox.measurement-lab.org",
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewManager(tt.service, "mlab-sandbox", tt.zone)

			got, err := d.Delete(context.Background(), tt.hostname)
			if (err != nil) != tt.wantErr {
				t.Errorf("Manager.Delete() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := deep.Equal(got, tt.want); diff != nil {
				t.Errorf("Manager.Delete() change returned != change expected: %s", strings.Join(diff, "\n"))
			}
		})
	}
}

func TestManager_RegisterZone(t *testing.T) {
	tests := []struct {
		name    string
		project string
		service dnsiface.Service
		zone    *dns.ManagedZone
		want    *dns.ManagedZone
		wantErr bool
	}{
		{
			name:    "success-get-zone",
			project: "mlab-sandbox",
			service: &fakeDNS2{
				results: map[string]result{
					"getzone-new-zone": {zone: &dns.ManagedZone{Name: "new-zone"}},
				},
			},
			zone: &dns.ManagedZone{
				Name:    "new-zone",
				DnsName: "new.zone.",
			},
			want: &dns.ManagedZone{
				Name: "new-zone",
			},
		},
		{
			name:    "success-create-zone",
			project: "mlab-sandbox",
			service: &fakeDNS2{
				results: map[string]result{
					"getzone-new-zone":    {err: &googleapi.Error{Code: 404}},
					"createzone-new-zone": {zone: &dns.ManagedZone{Name: "fake-zone"}},
				},
			},
			zone: &dns.ManagedZone{
				Name:    "new-zone",
				DnsName: "new.zone.",
			},
			want: &dns.ManagedZone{
				Name: "fake-zone",
			},
		},
		{
			name:    "error-get-zone",
			project: "mlab-sandbox",
			service: &fakeDNS2{
				results: map[string]result{
					"getzone-new-zone": {err: fmt.Errorf("failed to get zone")},
				},
			},
			zone: &dns.ManagedZone{
				Name: "new-zone",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewManager(tt.service, tt.project, dnsname.ProjectZone(tt.project))
			got, err := d.RegisterZone(context.Background(), tt.zone)
			if (err != nil) != tt.wantErr {
				t.Errorf("Manager.RegisterZone() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Manager.RegisterZone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManager_RegisterZoneSplit(t *testing.T) {
	fakeRR := &dns.ResourceRecordSet{
		Name:    "foo.mlab.net.",
		Type:    "A",
		Rrdatas: []string{"1.2.3.4"},
	}
	tests := []struct {
		name    string
		project string
		service dnsiface.Service
		zone    *dns.ManagedZone
		want    *dns.ResourceRecordSet
		wantErr bool
	}{
		{
			name:    "success-found",
			project: "mlab-sandbox",
			service: &fakeDNS2{
				results: map[string]result{
					"get-autojoin-sandbox-measurement-lab-org-fake.zone.-NS": {get: fakeRR},
				},
			},
			zone: &dns.ManagedZone{
				Name:    "fake-zone",
				DnsName: "fake.zone.",
			},
			want: fakeRR,
		},
		{
			name:    "success-missing-then-created",
			project: "mlab-sandbox",
			service: &fakeDNS2{
				results: map[string]result{
					"get-autojoin-sandbox-measurement-lab-org-fake.zone.-NS": {get: nil, err: &googleapi.Error{Code: 404}},
					"get-fake-zone-fake.zone.-NS": {
						get: fakeRR,
						err: nil,
					},
					"chg-autojoin-sandbox-measurement-lab-org": {
						chg: &dns.Change{
							Additions: []*dns.ResourceRecordSet{fakeRR},
						},
					},
				},
			},
			zone: &dns.ManagedZone{
				Name:    "fake-zone",
				DnsName: "fake.zone.",
			},
			want: fakeRR,
		},
		{
			name:    "error-other-error",
			project: "mlab-sandbox",
			service: &fakeDNS2{
				results: map[string]result{
					"get-autojoin-sandbox-measurement-lab-org-fake.zone.-NS": {err: fmt.Errorf("somefake error condition")},
				},
			},
			zone: &dns.ManagedZone{
				Name:    "fake-zone",
				DnsName: "fake.zone.",
			},
			wantErr: true,
		},
		{
			name:    "error-get-zone-ns-records",
			project: "mlab-sandbox",
			service: &fakeDNS2{
				results: map[string]result{
					"get-autojoin-sandbox-measurement-lab-org-fake.zone.-NS": {get: nil, err: &googleapi.Error{Code: 404}},
					"get-fake-zone-fake.zone.-NS": {
						err: fmt.Errorf("fake get resource record error"),
					},
				},
			},
			zone: &dns.ManagedZone{
				Name:    "fake-zone",
				DnsName: "fake.zone.",
			},
			wantErr: true,
		},
		{
			name:    "error-get-zone-ns-records",
			project: "mlab-sandbox",
			service: &fakeDNS2{
				results: map[string]result{
					"get-autojoin-sandbox-measurement-lab-org-fake.zone.-NS": {get: nil, err: &googleapi.Error{Code: 404}},
					"get-fake-zone-fake.zone.-NS":                            {get: fakeRR},
					"chg-autojoin-sandbox-measurement-lab-org": {
						err: fmt.Errorf("failed to change record"),
					},
				},
			},
			zone: &dns.ManagedZone{
				Name:    "fake-zone",
				DnsName: "fake.zone.",
			},
			wantErr: true,
		},
		{
			name:    "error-invalid-change-result",
			project: "mlab-sandbox",
			service: &fakeDNS2{
				results: map[string]result{
					"get-autojoin-sandbox-measurement-lab-org-fake.zone.-NS": {get: nil, err: &googleapi.Error{Code: 404}},
					"get-fake-zone-fake.zone.-NS":                            {get: fakeRR},
					"chg-autojoin-sandbox-measurement-lab-org": {
						chg: &dns.Change{
							Additions: nil, // this is invalid.
						},
					},
				},
			},
			zone: &dns.ManagedZone{
				Name:    "fake-zone",
				DnsName: "fake.zone.",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewManager(tt.service, tt.project, dnsname.ProjectZone(tt.project))
			got, err := d.RegisterZoneSplit(context.Background(), tt.zone)
			if (err != nil) != tt.wantErr {
				t.Errorf("Manager.RegisterZoneSplit() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Manager.RegisterZoneSplit() = %v, want %v", got, tt.want)
			}
		})
	}
}
