package maxmind

import (
	"context"
	"net"
	"net/url"
	"reflect"
	"testing"

	"github.com/m-lab/go/content"
	"github.com/m-lab/go/testingx"
)

func TestMaxmind_Reload(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantErr bool
	}{
		{
			name: "success",
			src:  "file:testdata/fake-geolite2.tar.gz",
		},
		{
			name:    "error-url",
			src:     "file:testdata/file-does-not-exist.tar.gz",
			wantErr: true,
		},
		{
			name:    "error-empty",
			src:     "file:testdata/empty.tar.gz",
			wantErr: true,
		},
		{
			name:    "error-wrong-dbtype",
			src:     "file:testdata/wrongtype.tar.gz",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := url.Parse(tt.src)
			testingx.Must(t, err, "failed to parse url")
			src, err := content.FromURL(context.Background(), p)
			testingx.Must(t, err, "failed to get url")
			mm := NewMaxmind(src)
			err = mm.Reload(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Maxmind.Reload() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			mm.Reload(context.Background()) // no change.

		})
	}
}

func TestMaxmind_City(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		ip      net.IP
		wantLat float64
		wantLon float64
		wantErr bool
	}{
		{
			name:    "success",
			src:     "file:testdata/fake-geolite2.tar.gz",
			ip:      net.ParseIP("2.125.160.216"), // an ip known to be in test data.
			wantLat: 51.75,
			wantLon: -1.25,
		},
		{
			name:    "error-invalid-ip",
			src:     "file:testdata/fake-geolite2.tar.gz",
			ip:      net.IP([]byte{0, 0}), // invalid/corrup input IP.
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := url.Parse(tt.src)
			testingx.Must(t, err, "failed to parse url")
			src, err := content.FromURL(context.Background(), p)
			testingx.Must(t, err, "failed to get url")
			mm := NewMaxmind(src)
			err = mm.Reload(context.Background())
			testingx.Must(t, err, "failed to load data")

			got, err := mm.City(tt.ip)
			if (err != nil) != tt.wantErr {
				t.Errorf("Maxmind.City() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if !reflect.DeepEqual(got.Location.Latitude, tt.wantLat) {
				t.Errorf("Maxmind.City() = %#v, want %#v", got.Location.Latitude, tt.wantLat)
			}
			if !reflect.DeepEqual(got.Location.Longitude, tt.wantLon) {
				t.Errorf("Maxmind.City() = %#v, want %#v", got.Location.Longitude, tt.wantLon)
			}
		})
	}
}
