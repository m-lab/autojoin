package register

import (
	"strings"
	"testing"

	"github.com/go-test/deep"
	v0 "github.com/m-lab/autojoin/api/v0"
	"github.com/m-lab/autojoin/iata"
	v2 "github.com/m-lab/locate/api/v2"
	"github.com/m-lab/uuid-annotator/annotator"
	"github.com/oschwald/geoip2-golang"
)

func TestCreateRegisterResponse(t *testing.T) {
	tests := []struct {
		name string
		p    *Params
		want v0.RegisterResponse
	}{
		{
			name: "success",
			p: &Params{
				Project: "mlab-sandbox",
				Service: "ndt",
				Org:     "bar",
				IPv4:    "192.168.0.1",
				IPv6:    "",
				Geo: &geoip2.City{
					Country: struct {
						GeoNameID         uint              `maxminddb:"geoname_id"`
						IsInEuropeanUnion bool              `maxminddb:"is_in_european_union"`
						IsoCode           string            `maxminddb:"iso_code"`
						Names             map[string]string `maxminddb:"names"`
					}{
						IsoCode: "US",
					},
					Subdivisions: []struct {
						GeoNameID uint              `maxminddb:"geoname_id"`
						IsoCode   string            `maxminddb:"iso_code"`
						Names     map[string]string `maxminddb:"names"`
					}{
						{IsoCode: "NY", Names: map[string]string{"en": "New York"}},
						{IsoCode: "ZZ", Names: map[string]string{"en": "fake thing"}},
					},
					Location: struct {
						AccuracyRadius uint16  `maxminddb:"accuracy_radius"`
						Latitude       float64 `maxminddb:"latitude"`
						Longitude      float64 `maxminddb:"longitude"`
						MetroCode      uint    `maxminddb:"metro_code"`
						TimeZone       string  `maxminddb:"time_zone"`
					}{
						Latitude:  41,
						Longitude: -73,
					},
				},
				Metro: iata.Row{
					IATA:      "lga",
					Latitude:  -10,
					Longitude: -10,
				},
				Network: &annotator.Network{
					ASNumber: 12345,
				},
			},
			want: v0.RegisterResponse{
				Registration: &v0.Registration{
					Hostname: "ndt-lga12345-c0a80001.bar.sandbox.measurement-lab.org.",
					Annotation: &v0.ServerAnnotation{
						Annotation: annotator.ServerAnnotations{
							Site:    "lga12345",
							Machine: "c0a80001",
							Geo: &annotator.Geolocation{
								CountryCode:         "US",
								Subdivision1ISOCode: "NY",
								Subdivision1Name:    "New York",
								Subdivision2ISOCode: "ZZ",
								Subdivision2Name:    "fake thing",
								Latitude:            -10,
								Longitude:           -10,
							},
							Network: &annotator.Network{
								ASNumber: 12345,
							},
						},
						Network: v0.Network{
							IPv4: "192.168.0.1",
						},
						Type: "unknown",
					},
					Heartbeat: &v2.Registration{
						CountryCode: "US",
						Experiment:  "ndt",
						Hostname:    "ndt-lga12345-c0a80001.bar.sandbox.measurement-lab.org.",
						Latitude:    -10,
						Longitude:   -10,
						Machine:     "c0a80001",
						Metro:       "lga",
						Project:     "mlab-sandbox",
						Probability: 1,
						Site:        "lga12345",
						Type:        "unknown",
						Uplink:      "unknown",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CreateRegisterResponse(tt.p)
			if diff := deep.Equal(got, tt.want); diff != nil {
				t.Errorf("CreateRegisterResponse() returned != expected: \n%s", strings.Join(diff, "\n"))
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
