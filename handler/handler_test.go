package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v0 "github.com/m-lab/autojoin/api/v0"
	"github.com/m-lab/autojoin/iata"
	"github.com/m-lab/autojoin/internal/adminx"
	"github.com/m-lab/autojoin/internal/dnsx/dnsiface"
	"github.com/m-lab/gcp-service-discovery/discovery"
	"github.com/m-lab/go/host"
	"github.com/m-lab/go/testingx"
	"github.com/m-lab/uuid-annotator/annotator"
	"github.com/oschwald/geoip2-golang"
	"google.golang.org/api/dns/v1"
)

const defaultMinVersion = "v0.0.0"

type fakeIataFinder struct {
	iata      string
	lookupErr error
	loads     int
	findRow   iata.Row
	findErr   error
}

func (f *fakeIataFinder) Lookup(country string, lat, lon float64) (string, error) {
	if f.lookupErr != nil {
		return "", f.lookupErr
	}
	return f.iata, nil
}
func (f *fakeIataFinder) Find(airport string) (iata.Row, error) {
	return f.findRow, f.findErr
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

type fakeDNS struct {
	chgErr error
	getErr error
}

func (f *fakeDNS) ResourceRecordSetsGet(ctx context.Context, project string, zone string, name string, rtype string) (*dns.ResourceRecordSet, error) {
	return nil, f.getErr
}
func (f *fakeDNS) ChangeCreate(ctx context.Context, project string, zone string, change *dns.Change) (*dns.Change, error) {
	return nil, f.chgErr
}
func (f *fakeDNS) CreateManagedZone(ctx context.Context, project string, zone *dns.ManagedZone) (*dns.ManagedZone, error) {
	return nil, nil
}
func (f *fakeDNS) GetManagedZone(ctx context.Context, project, zoneName string) (*dns.ManagedZone, error) {
	return nil, nil
}

type fakeStatusTracker struct {
	updateErr error
	deleteErr error
	nodes     []string
	ports     [][]string
	listErr   error
}

func (f *fakeStatusTracker) Update(string, []string) error {
	return f.updateErr
}

func (f *fakeStatusTracker) Delete(string) error {
	return f.deleteErr
}

func (f *fakeStatusTracker) List() ([]string, [][]string, error) {
	return f.nodes, f.ports, f.listErr
}

type fakeSecretManager struct {
	key string
	err error
}

func (f *fakeSecretManager) LoadOrCreateKey(ctx context.Context, org string) (string, error) {
	return f.key, f.err
}

type fakeDatastoreOrgManager struct {
	org *adminx.Organization
	err error
}

func (f *fakeDatastoreOrgManager) GetOrganization(ctx context.Context, orgName string) (*adminx.Organization, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.org == nil {
		// Return default if not specified
		return &adminx.Organization{
			Name:                  orgName,
			ProbabilityMultiplier: float64Ptr(1.0),
		}, nil
	}
	return f.org, nil
}

func float64Ptr(v float64) *float64 {
	return &v
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
			iata:     &fakeIataFinder{lookupErr: errors.New("fake error")},
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
			maxmind: &fakeMaxmind{err: errors.New("fake error")},
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
			name:    "error-unknown-latlon-headers",
			iata:    &fakeIataFinder{iata: "jfk"},
			maxmind: &fakeMaxmind{err: errors.New("fake error")},
			headers: map[string]string{
				"X-AppEngine-Country":     "US",
				"X-AppEngine-CityLatLong": "0.000000,0.000000",
			},
			wantCode: http.StatusBadRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewServer("mlab-sandbox", tt.iata, tt.maxmind, &fakeAsn{}, &fakeDNS{}, &fakeStatusTracker{}, nil, nil, defaultMinVersion)
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
		s := NewServer("mlab-sandbox", f, &fakeMaxmind{}, &fakeAsn{}, &fakeDNS{}, &fakeStatusTracker{}, nil, nil, defaultMinVersion)
		s.Reload(context.Background())
		if f.loads != 1 {
			t.Errorf("Reload failed to call iata loader")
		}
	})
}

func TestServer_LiveAndReady(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := NewServer("mlab-sandbox", &fakeIataFinder{}, &fakeMaxmind{}, &fakeAsn{}, &fakeDNS{}, &fakeStatusTracker{}, nil, nil, defaultMinVersion)
		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		s.Live(rw, req)
		s.Ready(rw, req)
	})
}

func TestServer_Register(t *testing.T) {
	iataFinder := &fakeIataFinder{
		findRow: iata.Row{
			IATA:      "lga",
			Latitude:  -10,
			Longitude: -10,
		},
	}
	maxmind := &fakeMaxmind{
		// NOTE: this ridiculous declaration is needed due to anonymous structs in the geoip2 package.
		city: &geoip2.City{
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
	}

	fakeASN := &fakeAsn{
		ann: &annotator.Network{
			ASNumber: 12345,
		},
	}

	tests := []struct {
		name            string
		Iata            IataFinder
		Maxmind         MaxmindFinder
		ASN             ASNFinder
		DNS             dnsiface.Service
		Tracker         DNSTracker
		dsm             Datastore
		sm              ServiceAccountSecretManager
		params          string
		org             string
		minVersion      string
		wantName        string
		wantCode        int
		wantProbability float64
	}{
		{
			name:    "success",
			params:  "?service=foo&iata=lga&ipv4=192.168.0.1&probability=1.0&ports=9990&type=physical&uplink=10g",
			org:     "bar",
			Iata:    iataFinder,
			Maxmind: maxmind,
			ASN:     fakeASN,
			DNS:     &fakeDNS{},
			Tracker: &fakeStatusTracker{},
			dsm:     &fakeDatastoreOrgManager{},
			sm: &fakeSecretManager{
				key: "fake key data",
			},
			minVersion:      defaultMinVersion,
			wantName:        "foo-lga12345-c0a80001.bar.sandbox.measurement-lab.org",
			wantCode:        http.StatusOK,
			wantProbability: 1.0,
		},
		{
			name:    "success-probability-invalid-ports-invalid",
			params:  "?service=foo&iata=lga&ipv4=192.168.0.1&probability=invalid&ports=invalid&type=virtual&uplink=10g",
			org:     "bar",
			Iata:    iataFinder,
			Maxmind: maxmind,
			ASN:     fakeASN,
			DNS:     &fakeDNS{},
			Tracker: &fakeStatusTracker{},
			dsm:     &fakeDatastoreOrgManager{},
			sm: &fakeSecretManager{
				key: "fake key data",
			},
			minVersion:      defaultMinVersion,
			wantName:        "foo-lga12345-c0a80001.bar.sandbox.measurement-lab.org",
			wantCode:        http.StatusOK,
			wantProbability: 1.0,
		},
		{
			name:       "error-service-empty",
			params:     "?service=",
			minVersion: defaultMinVersion,
			wantCode:   http.StatusBadRequest,
		},
		{
			name:       "error-service-too-long",
			params:     "?service=abcdefghijklm",
			minVersion: defaultMinVersion,
			wantCode:   http.StatusBadRequest,
		},
		{
			name:       "error-bad-type",
			params:     "?service=foo&iata=lga&ipv4=192.168.0.1&probability=0.5&ports=9990&type=dell&uplink=50g",
			org:        "bar",
			minVersion: defaultMinVersion,
			wantCode:   http.StatusBadRequest,
		},
		{
			name:       "error-bad-uplink",
			params:     "?service=foo&iata=lga&ipv4=192.168.0.1&probability=0.5&ports=9990&type=virtual&uplink=10",
			org:        "bar",
			minVersion: defaultMinVersion,
			wantCode:   http.StatusBadRequest,
		},
		{
			name:       "error-bad-ip",
			params:     "?service=foo&ipv4=-BAD-IP-",
			org:        "bar",
			minVersion: defaultMinVersion,
			wantCode:   http.StatusBadRequest,
		},
		{
			name:       "error-invalid-iata",
			params:     "?service=foo&ipv4=192.168.0.1&iata=-invalid-",
			org:        "bar",
			minVersion: defaultMinVersion,
			wantCode:   http.StatusBadRequest,
		},
		{
			name:       "error-bad-iata-find",
			Iata:       &fakeIataFinder{findErr: errors.New("find err")},
			params:     "?service=foo&ipv4=192.168.0.1&iata=123&type=physical&uplink=20g",
			org:        "bar",
			minVersion: defaultMinVersion,
			wantCode:   http.StatusInternalServerError,
		},
		{
			name:       "error-bad-maxmind-city",
			Iata:       &fakeIataFinder{findRow: iata.Row{}},
			Maxmind:    &fakeMaxmind{err: errors.New("fake maxmind error")},
			params:     "?service=foo&ipv4=192.168.0.1&iata=abc&type=virtual&uplink=1000g",
			org:        "bar",
			minVersion: defaultMinVersion,
			wantCode:   http.StatusInternalServerError,
		},
		{
			name:    "error-loading-key",
			params:  "?service=foo&iata=lga&ipv4=192.168.0.1&type=physical&uplink=10g",
			org:     "bar",
			Iata:    iataFinder,
			Maxmind: maxmind,
			ASN:     fakeASN,
			DNS:     &fakeDNS{getErr: errors.New("fake get error")},
			dsm:     &fakeDatastoreOrgManager{},
			sm: &fakeSecretManager{
				err: fmt.Errorf("fake key load error"),
			},
			minVersion: defaultMinVersion,
			wantCode:   http.StatusInternalServerError,
		},
		{
			name:    "error-registration",
			params:  "?service=foo&iata=lga&ipv4=192.168.0.1&type=virtual&uplink=1g",
			org:     "bar",
			Iata:    iataFinder,
			Maxmind: maxmind,
			ASN:     fakeASN,
			DNS:     &fakeDNS{getErr: errors.New("fake get error")},
			dsm:     &fakeDatastoreOrgManager{},
			sm: &fakeSecretManager{
				key: "fake key data",
			},
			minVersion: defaultMinVersion,
			wantCode:   http.StatusInternalServerError,
		},
		{
			name:    "error-tracker-update-error",
			params:  "?service=foo&iata=lga&ipv4=192.168.0.1&type=physical&uplink=20g",
			org:     "bar",
			Iata:    iataFinder,
			Maxmind: maxmind,
			ASN:     fakeASN,
			DNS:     &fakeDNS{},
			Tracker: &fakeStatusTracker{updateErr: errors.New("update error")},
			dsm:     &fakeDatastoreOrgManager{},
			sm: &fakeSecretManager{
				key: "fake key data",
			},
			minVersion: defaultMinVersion,
			wantName:   "foo-lga12345-c0a80001.bar.sandbox.measurement-lab.org",
			wantCode:   http.StatusInternalServerError,
		},
		{
			name:    "success-with-version",
			params:  "?service=foo&iata=lga&ipv4=192.168.0.1&probability=1.0&ports=9990&type=physical&uplink=10g&version=1.0.0",
			org:     "bar",
			Iata:    iataFinder,
			Maxmind: maxmind,
			ASN:     fakeASN,
			DNS:     &fakeDNS{},
			Tracker: &fakeStatusTracker{},
			dsm:     &fakeDatastoreOrgManager{},
			sm: &fakeSecretManager{
				key: "fake key data",
			},
			minVersion:      "1.0.0",
			wantName:        "foo-lga12345-c0a80001.bar.sandbox.measurement-lab.org",
			wantCode:        http.StatusOK,
			wantProbability: 1.0,
		},
		{
			name:    "success-no-version",
			params:  "?service=foo&iata=lga&ipv4=192.168.0.1&probability=1.0&ports=9990&type=physical&uplink=10g",
			org:     "bar",
			Iata:    iataFinder,
			Maxmind: maxmind,
			ASN:     fakeASN,
			DNS:     &fakeDNS{},
			Tracker: &fakeStatusTracker{},
			dsm:     &fakeDatastoreOrgManager{},
			sm: &fakeSecretManager{
				key: "fake key data",
			},
			minVersion:      defaultMinVersion,
			wantName:        "foo-lga12345-c0a80001.bar.sandbox.measurement-lab.org",
			wantCode:        http.StatusOK,
			wantProbability: 1.0,
		},
		{
			name:    "success-newer-version",
			params:  "?service=foo&iata=lga&ipv4=192.168.0.1&probability=1.0&ports=9990&type=physical&uplink=10g&version=2.0.0",
			org:     "bar",
			Iata:    iataFinder,
			Maxmind: maxmind,
			ASN:     fakeASN,
			DNS:     &fakeDNS{},
			Tracker: &fakeStatusTracker{},
			dsm:     &fakeDatastoreOrgManager{},
			sm: &fakeSecretManager{
				key: "fake key data",
			},
			minVersion:      "1.0.0",
			wantName:        "foo-lga12345-c0a80001.bar.sandbox.measurement-lab.org",
			wantCode:        http.StatusOK,
			wantProbability: 1.0,
		},
		{
			name:    "error-invalid-version-format",
			params:  "?service=foo&iata=lga&ipv4=192.168.0.1&type=physical&uplink=10g&version=not.valid.version",
			org:     "bar",
			Iata:    iataFinder,
			Maxmind: maxmind,
			ASN:     fakeASN,
			DNS:     &fakeDNS{},
			Tracker: &fakeStatusTracker{},
			dsm:     &fakeDatastoreOrgManager{},
			sm: &fakeSecretManager{
				key: "fake key data",
			},
			minVersion: "1.0.0",
			wantCode:   http.StatusBadRequest,
		},
		{
			name:    "error-version-too-old",
			params:  "?service=foo&iata=lga&ipv4=192.168.0.1&type=physical&uplink=10g&version=0.9.0",
			org:     "bar",
			Iata:    iataFinder,
			Maxmind: maxmind,
			ASN:     fakeASN,
			DNS:     &fakeDNS{},
			Tracker: &fakeStatusTracker{},
			dsm:     &fakeDatastoreOrgManager{},
			sm: &fakeSecretManager{
				key: "fake key data",
			},
			minVersion: "1.0.0",
			wantCode:   http.StatusForbidden,
		},
		{
			name:    "error-no-version-when-minimum-required",
			params:  "?service=foo&iata=lga&ipv4=192.168.0.1&type=physical&uplink=10g",
			org:     "bar",
			Iata:    iataFinder,
			Maxmind: maxmind,
			ASN:     fakeASN,
			DNS:     &fakeDNS{},
			Tracker: &fakeStatusTracker{},
			dsm:     &fakeDatastoreOrgManager{},
			sm: &fakeSecretManager{
				key: "fake key data",
			},
			minVersion: "1.0.0",
			wantCode:   http.StatusForbidden,
		},
		{
			name:    "success-with-org-multiplier",
			params:  "?service=foo&iata=lga&ipv4=192.168.0.1&probability=0.5&ports=9990&type=physical&uplink=10g",
			org:     "bar",
			Iata:    iataFinder,
			Maxmind: maxmind,
			ASN:     fakeASN,
			DNS:     &fakeDNS{},
			Tracker: &fakeStatusTracker{},
			sm: &fakeSecretManager{
				key: "fake key data",
			},
			dsm: &fakeDatastoreOrgManager{
				org: &adminx.Organization{
					Name:                  "bar",
					ProbabilityMultiplier: float64Ptr(2.0),
				},
			},
			minVersion:      defaultMinVersion,
			wantName:        "foo-lga12345-c0a80001.bar.sandbox.measurement-lab.org",
			wantCode:        http.StatusOK,
			wantProbability: 1.0, // 0.5 * 2.0
		},
		{
			name:    "success-with-nil-org-multiplier",
			params:  "?service=foo&iata=lga&ipv4=192.168.0.1&probability=0.5&ports=9990&type=physical&uplink=10g",
			org:     "bar",
			Iata:    iataFinder,
			Maxmind: maxmind,
			ASN:     fakeASN,
			DNS:     &fakeDNS{},
			Tracker: &fakeStatusTracker{},
			sm: &fakeSecretManager{
				key: "fake key data",
			},
			dsm: &fakeDatastoreOrgManager{
				org: &adminx.Organization{
					Name:                  "bar",
					ProbabilityMultiplier: nil,
				},
			},
			minVersion:      defaultMinVersion,
			wantName:        "foo-lga12345-c0a80001.bar.sandbox.measurement-lab.org",
			wantCode:        http.StatusOK,
			wantProbability: 0.5,
		},
		{
			name:    "success-with-datastore-error",
			params:  "?service=foo&iata=lga&ipv4=192.168.0.1&probability=0.5&ports=9990&type=physical&uplink=10g",
			org:     "bar",
			Iata:    iataFinder,
			Maxmind: maxmind,
			ASN:     fakeASN,
			DNS:     &fakeDNS{},
			Tracker: &fakeStatusTracker{},
			sm: &fakeSecretManager{
				key: "fake key data",
			},
			dsm: &fakeDatastoreOrgManager{
				err: errors.New("datastore error"),
			},
			minVersion:      defaultMinVersion,
			wantName:        "foo-lga12345-c0a80001.bar.sandbox.measurement-lab.org",
			wantCode:        http.StatusOK,
			wantProbability: 0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewServer("mlab-sandbox", tt.Iata, tt.Maxmind, tt.ASN, tt.DNS, tt.Tracker, tt.sm, tt.dsm, tt.minVersion)
			rw := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/autojoin/v0/node/register"+tt.params, nil)

			// Inject fake organization into context.
			ctx := context.WithValue(req.Context(), orgContextKey, tt.org)
			req = req.WithContext(ctx)
			s.Register(rw, req)

			if rw.Code != tt.wantCode {
				t.Errorf("Register() returned wrong code; got %d, want %d", rw.Code, tt.wantCode)
			}

			// Check response content is valid.
			resp := v0.RegisterResponse{}
			raw := rw.Body.Bytes()
			err := json.Unmarshal(raw, &resp)
			testingx.Must(t, err, "failed to unmarshal response")

			// One or the other should be defined.
			if resp.Error == nil && resp.Registration == nil {
				t.Errorf("Register() returned empty result; got %q", raw)
			}
			// Do not value check error cases.
			if rw.Code != http.StatusOK {
				return
			}

			if resp.Registration.Hostname != tt.wantName {
				t.Errorf("Register() returned wrong hostname; got %s, want %s", resp.Registration.Hostname, tt.wantName)
			}

			if _, err := host.Parse(resp.Registration.Hostname); err != nil {
				t.Errorf("Register() returned unparsable hostname; got %v, want nil", err)
			}

			if resp.Registration.Heartbeat.Probability != tt.wantProbability {
				t.Errorf("Register() returned wrong probability; got %f, want %f",
					resp.Registration.Heartbeat.Probability, tt.wantProbability)
			}

		})
	}
}

func TestServer_Delete(t *testing.T) {
	tests := []struct {
		name     string
		DNS      dnsiface.Service
		Tracker  DNSTracker
		qs       string
		wantName string
		wantCode int
	}{
		{
			name:     "success",
			qs:       "?hostname=ndt-lga3269-4f20bd89.mlab.sandbox.measurement-lab.org",
			wantCode: http.StatusOK,
			DNS:      &fakeDNS{},
			Tracker:  &fakeStatusTracker{},
		},
		{
			name:     "error-hostname-empty",
			qs:       "?hostname=",
			wantCode: http.StatusBadRequest,
			Tracker:  &fakeStatusTracker{},
		},
		{
			name:     "error-hostname-invalid",
			qs:       "?hostname=this-is-not-valid.foo",
			wantCode: http.StatusBadRequest,
			Tracker:  &fakeStatusTracker{},
		},
		{
			name:     "error-request-failed",
			qs:       "?hostname=ndt-lga3269-4f20bd89.mlab.sandbox.measurement-lab.org",
			wantCode: http.StatusInternalServerError,
			DNS:      &fakeDNS{getErr: errors.New("fake error")},
			Tracker:  &fakeStatusTracker{},
		},
		{
			name: "error-tracker-failed",

			qs:       "?hostname=ndt-lga3269-4f20bd89.mlab.sandbox.measurement-lab.org",
			wantCode: http.StatusInternalServerError,
			DNS:      &fakeDNS{},
			Tracker:  &fakeStatusTracker{deleteErr: errors.New("delete failed")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewServer("mlab-sandbox", nil, nil, nil, tt.DNS, tt.Tracker, nil, nil, defaultMinVersion)
			rw := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/autojoin/v0/node/delete"+tt.qs, nil)
			s.Delete(rw, req)

			if rw.Code != tt.wantCode {
				t.Errorf("Delete() returned wrong code; got %d, want %d", rw.Code, tt.wantCode)
			}
		})
	}
}

func TestServer_List(t *testing.T) {
	tests := []struct {
		name       string
		params     string
		lister     DNSTracker
		wantCode   int
		wantLength int
	}{
		{
			name:   "success",
			params: "",
			lister: &fakeStatusTracker{
				// Fake node name must parse correctly.
				nodes: []string{"ndt-lga3356-040e9f4b.mlab.autojoin.measurement-lab.org"},
				ports: [][]string{{"9990", "9991"}},
			},
			wantCode:   http.StatusOK,
			wantLength: 1,
		},
		{
			name:   "success-prometheus",
			params: "?format=prometheus",
			lister: &fakeStatusTracker{
				nodes: []string{"ndt-lga3356-040e9f4b.mlab.autojoin.measurement-lab.org"},
				ports: [][]string{{"9990"}},
			},
			wantCode:   http.StatusOK,
			wantLength: 1,
		},
		{
			name:   "success-prometheus",
			params: "?format=prometheus",
			lister: &fakeStatusTracker{
				nodes: []string{"test1"},
				ports: [][]string{{}},
			},
			wantCode:   http.StatusOK,
			wantLength: 0,
		},
		{
			name:   "success-servers",
			params: "?format=servers",
			lister: &fakeStatusTracker{
				nodes: []string{"ndt-lga3356-040e9f4b.mlab.autojoin.measurement-lab.org"},
				ports: [][]string{{"9990"}},
			},
			wantCode:   http.StatusOK,
			wantLength: 1,
		},
		{
			name:   "success-sites",
			params: "?format=sites&org=foo",
			lister: &fakeStatusTracker{
				nodes: []string{"ndt-lga3356-040e9f4b.mlab.autojoin.measurement-lab.org"},
				ports: [][]string{{"9990"}},
			},
			wantCode:   http.StatusOK,
			wantLength: 0,
		},
		{
			name:   "success-one-site-two-nodes",
			params: "?format=sites&org=mlab",
			lister: &fakeStatusTracker{
				nodes: []string{
					"ndt-lga3356-040e9f4b.mlab.autojoin.measurement-lab.org",
					"ndt-lga3356-abcdef12.mlab.autojoin.measurement-lab.org"},
				ports: [][]string{{"9990"}, {"9990"}},
			},
			wantCode:   http.StatusOK,
			wantLength: 1,
		},
		{
			name:   "success-script-exporter",
			params: "?format=script-exporter&service=ndt7_client_byos",
			lister: &fakeStatusTracker{
				nodes: []string{"ndt-lga3356-040e9f4b.mlab.autojoin.measurement-lab.org"},
				ports: [][]string{{"9990"}},
			},
			wantCode:   http.StatusOK,
			wantLength: 1,
		},
		{
			name:   "error-internal",
			params: "",
			lister: &fakeStatusTracker{
				listErr: errors.New("fake list error"),
			},
			wantCode: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewServer("mlab-sandbox", nil, nil, nil, nil, tt.lister, nil, nil, defaultMinVersion)
			rw := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/autojoin/v0/node/list"+tt.params, nil)

			s.List(rw, req)

			if rw.Code != tt.wantCode {
				t.Errorf("List() returned wrong code; got %d, want %d", rw.Code, tt.wantCode)
			}

			// Check response content is valid.
			var err error
			raw := rw.Body.Bytes()
			configs := []discovery.StaticConfig{}
			length := 0
			if strings.Contains(tt.params, "prometheus") || strings.Contains(tt.params, "script-exporter") {
				err = json.Unmarshal(raw, &configs)
				length = len(configs)
			} else if strings.Contains(tt.params, "servers") {
				resp := v0.ListResponse{}
				err = json.Unmarshal(raw, &resp)
				length = len(resp.Servers)
			} else if strings.Contains(tt.params, "sites") {
				resp := v0.ListResponse{}
				err = json.Unmarshal(raw, &resp)
				length = len(resp.Sites)
			} else {
				resp := v0.ListResponse{}
				err = json.Unmarshal(raw, &resp)
				length = len(resp.Servers)
			}
			testingx.Must(t, err, "failed to unmarshal response")

			if length != tt.wantLength {
				t.Errorf("List() returned wrong length; got %d, want %d", length, tt.wantLength)
			}
		})
	}
}
