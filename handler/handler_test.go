package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	v0 "github.com/m-lab/autojoin/api/v0"
	"github.com/m-lab/go/testingx"
	"github.com/oschwald/geoip2-golang"
)

type fakeIataFinder struct {
	iata  string
	err   error
	loads int
}

func (f *fakeIataFinder) Lookup(country string, lat, lon float64) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.iata, nil
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

func TestServer_Lookup(t *testing.T) {
	tests := []struct {
		name     string
		iata     *fakeIataFinder
		maxmind  *fakeMaxmind
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
			maxmind:  &fakeMaxmind{err: errors.New("fake error")},
			request:  "?lat=43&lon=-70",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "no-country-with-ipv4",
			iata:     &fakeIataFinder{iata: "jfk"},
			maxmind:  &fakeMaxmind{err: errors.New("fake error")},
			request:  "?lat=43&lon=-70&ipv4=192.168.0.1",
			wantCode: http.StatusBadRequest,
		},
		{
			name:    "no-country-with-ipv4-headers",
			iata:    &fakeIataFinder{iata: "jfk"},
			maxmind: &fakeMaxmind{err: errors.New("fake error")},
			headers: map[string]string{
				"X-Forwarded-For": "192.168.0.1",
			},
			request:  "?lat=43&lon=-70",
			wantCode: http.StatusBadRequest,
		},
		{
			name: "country-from-maxmind",
			iata: &fakeIataFinder{iata: "jfk"},
			maxmind: &fakeMaxmind{
				city: &geoip2.City{
					Country: struct {
						GeoNameID         uint              `maxminddb:"geoname_id"`
						IsInEuropeanUnion bool              `maxminddb:"is_in_european_union"`
						IsoCode           string            `maxminddb:"iso_code"`
						Names             map[string]string `maxminddb:"names"`
					}{
						IsoCode: "US",
					},
				},
			},
			request:  "?lat=43&lon=-70",
			wantIata: "jfk",
			wantCode: http.StatusOK,
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
			name:    "error-bad-latlon-headers",
			iata:    &fakeIataFinder{iata: "jfk"},
			maxmind: &fakeMaxmind{err: ErrIPNotFound},
			headers: map[string]string{
				"X-AppEngine-Country":     "US",
				"X-AppEngine-CityLatLong": "xx,zz,yy",
			},
			wantCode: http.StatusBadRequest,
		},
		{
			name: "latlon-headers-from-maxmind",
			iata: &fakeIataFinder{iata: "jfk"},
			maxmind: &fakeMaxmind{
				city: &geoip2.City{
					Location: struct {
						AccuracyRadius uint16  `maxminddb:"accuracy_radius"`
						Latitude       float64 `maxminddb:"latitude"`
						Longitude      float64 `maxminddb:"longitude"`
						MetroCode      uint    `maxminddb:"metro_code"`
						TimeZone       string  `maxminddb:"time_zone"`
					}{
						Latitude:  40,
						Longitude: -71,
					},
				},
			},
			headers: map[string]string{
				"X-AppEngine-Country":     "US",
				"X-AppEngine-CityLatLong": "xx,zz,yy",
			},
			wantCode: http.StatusOK,
			wantIata: "jfk",
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
			s := NewServer("mlab-sandbox", tt.iata, tt.maxmind)
			rw := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/autojoin/v0/lookup"+tt.request, nil)
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}
			s.Lookup(rw, req)
			if rw.Code != tt.wantCode {
				t.Errorf("Lookup() returned wrong code; got %d, want %d", rw.Code, tt.wantCode)
			}
			resp := &v0.LookupResponse{}
			testingx.Must(t, json.Unmarshal(rw.Body.Bytes(), resp), "failed to parse response")
			if rw.Code == http.StatusOK && (resp.Lookup == nil || resp.Lookup.IATA != tt.wantIata) {
				t.Errorf("Lookup() returned wrong iata; got %#v, want %s", resp, tt.wantIata)
			}
		})
	}
}

func TestServer_Reload(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		f := &fakeIataFinder{}
		s := NewServer("mlab-sandbox", f, &fakeMaxmind{})
		s.Reload(context.Background())
		if f.loads != 1 {
			t.Errorf("Reload failed to call iata loader")
		}
	})
}

func TestServer_LiveAndReady(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := NewServer("mlab-sandbox", &fakeIataFinder{}, &fakeMaxmind{})
		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		s.Live(rw, req)
		s.Ready(rw, req)

		// TODO: remove once handler has a real implementation.
		s.Register(rw, req)
	})
}
