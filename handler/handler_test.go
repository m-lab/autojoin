package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v0 "github.com/m-lab/autojoin/api/v0"
	"github.com/m-lab/autojoin/iata"
	"github.com/m-lab/go/testingx"
	"github.com/m-lab/uuid-annotator/annotator"
	"github.com/oschwald/geoip2-golang"
)

type fakeIataFinder struct {
	iata  string
	err   error
	loads int
	row   iata.Row
}

func (f *fakeIataFinder) Lookup(country string, lat, lon float64) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.iata, nil
}
func (f *fakeIataFinder) Find(airport string) (iata.Row, error) {
	return f.row, nil
}

func (f *fakeIataFinder) Load(ctx context.Context) error {
	f.loads++
	return nil
}

type fakeMaxmind struct {
	city *geoip2.City
	err  error
}

func (f *fakeMaxmind) City(ip net.IP) (*geoip2.City, error) {
	return f.city, f.err
}
func (f *fakeMaxmind) Reload(ctx context.Context) error {
	return nil
}

type fakeAsn struct {
	ann *annotator.Network
}

func (f *fakeAsn) AnnotateIP(src string) *annotator.Network {
	return f.ann
}
func (f *fakeAsn) Reload(ctx context.Context) {}

func TestServer_Lookup(t *testing.T) {
	tests := []struct {
		name     string
		project  string
		iata     *fakeIataFinder
		request  string
		headers  map[string]string
		wantCode int
		wantIata string
	}{
		{
			name:     "success-parameters",
			iata:     &fakeIataFinder{iata: "jfk"},
			request:  "?country=US&lat=43&lon=-70",
			wantCode: http.StatusOK,
			wantIata: "jfk",
		},
		{
			name:     "no-country",
			iata:     &fakeIataFinder{iata: "jfk"},
			request:  "?lat=43&lon=-70",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "bad-lat-lon",
			iata:     &fakeIataFinder{iata: "jfk"},
			request:  "?country=US&lat=ten&lon=twelve",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "error-lookup",
			iata:     &fakeIataFinder{err: errors.New("fake error")},
			request:  "?country=US&lat=43&lon=-70",
			wantCode: http.StatusInternalServerError,
		},
		{
			name: "success-headers",
			iata: &fakeIataFinder{iata: "jfk"},
			headers: map[string]string{
				"X-AppEngine-Country":     "US",
				"X-AppEngine-CityLatLong": "43,-73",
			},
			wantCode: http.StatusOK,
			wantIata: "jfk",
		},
		{
			name: "error-bad-latlon-headers",
			iata: &fakeIataFinder{iata: "jfk"},
			headers: map[string]string{
				"X-AppEngine-Country":     "US",
				"X-AppEngine-CityLatLong": "xx,zz,yy",
			},
			wantCode: http.StatusBadRequest,
		},
		{
			name: "error-unknown-latlon-headers",
			iata: &fakeIataFinder{iata: "jfk"},
			headers: map[string]string{
				"X-AppEngine-Country":     "US",
				"X-AppEngine-CityLatLong": "0.000000,0.000000",
			},
			wantCode: http.StatusBadRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewServer(tt.project, tt.iata, &fakeMaxmind{}, &fakeAsn{})
			rw := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/autojoin/v0/lookup"+tt.request, nil)
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}
			s.Lookup(rw, req)
			if rw.Code != tt.wantCode {
				t.Errorf("Lookup() returned wrong code; got %d, want %d", rw.Code, tt.wantCode)
			}
			resp := strings.Trim(rw.Body.String(), "\n")
			if rw.Code == http.StatusOK && resp != tt.wantIata {
				t.Errorf("Lookup() returned wrong iata; got %s, want %s", resp, tt.wantIata)
			}
		})
	}
}

func TestServer_Reload(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		f := &fakeIataFinder{}
		s := NewServer("mlab-sandbox", f, &fakeMaxmind{}, &fakeAsn{})
		s.Reload(context.Background())
		if f.loads != 1 {
			t.Errorf("Reload failed to call iata loader")
		}
	})
}

func TestServer_LiveAndReady(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		f := &fakeIataFinder{}
		s := NewServer("mlab-sandbox", f, &fakeMaxmind{}, &fakeAsn{})
		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		s.Live(rw, req)
		s.Ready(rw, req)
	})
}

func TestServer_Register(t *testing.T) {
	tests := []struct {
		name     string
		Project  string
		Iata     IataFinder
		Maxmind  MaxmindFinder
		ASN      ASNFinder
		params   string
		wantName string
		wantCode int
	}{
		{
			name:   "success",
			params: "?service=foo&organization=bar&iata=lga&ipv4=192.168.0.1",
			Iata: &fakeIataFinder{
				row: iata.Row{
					IATA:      "abc",
					Latitude:  -10,
					Longitude: -10,
				},
			},
			Maxmind: &fakeMaxmind{
				// NOTE: this riduculous declaration is needed due to anonymous structs in the geoop2 package.
				city: &geoip2.City{
					Country: struct {
						GeoNameID         uint              `maxminddb:"geoname_id"`
						IsInEuropeanUnion bool              `maxminddb:"is_in_european_union"`
						IsoCode           string            `maxminddb:"iso_code"`
						Names             map[string]string `maxminddb:"names"`
					}{
						IsoCode: "US",
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
			},
			ASN: &fakeAsn{
				ann: &annotator.Network{
					ASNumber: 12345,
				},
			},
			wantName: "foo-lga12345-c0a80001.bar.sandbox.measurement-lab.org",
			wantCode: http.StatusOK,
		},
		{
			name:     "error-bad-service",
			params:   "?service=-BAD-NAME-",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "error-bad-organization",
			params:   "?service=foo&organization=-BAD-NAME-",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "error-bad-ip",
			params:   "?service=foo&organization=bar&ipv4=-BAD-IP-",
			wantCode: http.StatusBadRequest,
		},
		{
			name: "error-bad-iata",
			Iata: &fakeIataFinder{err: errors.New("bad lookup")},
			Maxmind: &fakeMaxmind{
				err: errors.New("bad lookup"),
			},
			params:   "?service=foo&organization=bar&ipv4=192.168.0.1&iata=-invalid-",
			wantCode: http.StatusBadRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewServer("mlab-sandbox", tt.Iata, tt.Maxmind, tt.ASN)
			rw := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/autojoin/v0/node/register"+tt.params, nil)

			s.Register(rw, req)

			if rw.Code != tt.wantCode {
				t.Errorf("Register() returned wrong code; got %d, want %d", rw.Code, tt.wantCode)
			}
			if rw.Code != http.StatusOK {
				return
			}

			// Check response content.
			resp := v0.RegisterResponse{}
			raw := rw.Body.Bytes()
			err := json.Unmarshal(raw, &resp)
			testingx.Must(t, err, "failed to unmarshal response")

			if resp.Registration.Hostname != tt.wantName {
				t.Errorf("Register() returned wrong hostname; got %s, want %s", resp.Registration.Hostname, tt.wantName)
			}

		})
	}
}
