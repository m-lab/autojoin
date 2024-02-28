package dnsx

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/go-test/deep"
	"github.com/m-lab/autojoin/internal/dnsx/iface"
	"google.golang.org/api/dns/v1"
	"google.golang.org/api/googleapi"
)

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
	return change, f.chgErr
}

func TestManager_Register(t *testing.T) {
	tests := []struct {
		name     string
		zone     string
		service  iface.DNS
		hostname string
		ipv4     string
		ipv6     string
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
		service  iface.DNS
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
