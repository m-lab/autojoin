package dnsname

import "testing"

func TestProjectZone(t *testing.T) {
	tests := []struct {
		name    string
		project string
		want    string
	}{
		{
			name:    "success",
			project: "mlab-sandbox",
			want:    "autojoin-sandbox-measurement-lab-org",
		},
		{
			name:    "success",
			project: "mlab-autojoin",
			want:    "autojoin-autojoin-measurement-lab-org",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ProjectZone(tt.project); got != tt.want {
				t.Errorf("ProjectZone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOrgZone(t *testing.T) {
	tests := []struct {
		name    string
		org     string
		project string
		want    string
	}{
		{
			name:    "success",
			org:     "mlab",
			project: "mlab-sandbox",
			want:    "autojoin-mlab-sandbox-measurement-lab-org",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OrgZone(tt.org, tt.project); got != tt.want {
				t.Errorf("OrgZone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOrgDNS(t *testing.T) {
	tests := []struct {
		name    string
		org     string
		project string
		want    string
	}{
		{
			name:    "success",
			org:     "foo",
			project: "mlab-sandbox",
			want:    "foo.sandbox.measurement-lab.org.",
		},
		{
			name:    "success",
			org:     "mlab",
			project: "mlab-autojoin",
			want:    "mlab.autojoin.measurement-lab.org.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OrgDNS(tt.org, tt.project); got != tt.want {
				t.Errorf("OrgDNS() = %v, want %v", got, tt.want)
			}
		})
	}
}
