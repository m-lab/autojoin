package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
	v0 "github.com/m-lab/autojoin/api/v0"
	"github.com/m-lab/autojoin/iata"
	"github.com/m-lab/autojoin/internal/adminx"
	"github.com/m-lab/autojoin/internal/dnsname"
	"github.com/m-lab/autojoin/internal/dnsx"
	"github.com/m-lab/autojoin/internal/dnsx/dnsiface"
	"github.com/m-lab/autojoin/internal/register"
	"github.com/m-lab/gcp-service-discovery/discovery"
	"github.com/m-lab/go/host"
	"github.com/m-lab/go/rtx"
	v2 "github.com/m-lab/locate/api/v2"
	"github.com/m-lab/uuid-annotator/annotator"
	"github.com/oschwald/geoip2-golang"
)

var (
	errLocationNotFound = errors.New("location not found")
	errLocationFormat   = errors.New("location could not be parsed")

	validName = regexp.MustCompile(`[a-zA-Z0-9]+`)
)

// Server maintains shared state for the server.
type Server struct {
	Project    string
	Iata       IataFinder
	Maxmind    MaxmindFinder
	ASN        ASNFinder
	DNS        dnsiface.Service
	minVersion *semver.Version

	sm         ServiceAccountSecretManager
	dnsTracker DNSTracker
	dsm        Datastore
}

// ASNFinder is an interface used by the Server to manage ASN information.
type ASNFinder interface {
	AnnotateIP(src string) *annotator.Network
	Reload(ctx context.Context)
}

// MaxmindFinder is an interface used by the Server to manage Maxmind information.
type MaxmindFinder interface {
	City(ip net.IP) (*geoip2.City, error)
	Reload(ctx context.Context) error
}

// IataFinder is an interface used by the Server to manage IATA information.
type IataFinder interface {
	Lookup(country string, lat, lon float64) (string, error)
	Find(iata string) (iata.Row, error)
	Load(ctx context.Context) error
}

type DNSTracker interface {
	Update(string, []string) error
	Delete(string) error
	List() ([]string, [][]string, error)
}

// ServiceAccountSecretManager is an interface used by the server to allocate service account keys.
type ServiceAccountSecretManager interface {
	LoadOrCreateKey(ctx context.Context, org string) (string, error)
}

type Datastore interface {
	GetOrganization(ctx context.Context, name string) (*adminx.Organization, error)
}

// NewServer creates a new Server instance for request handling.
func NewServer(project string, finder IataFinder, maxmind MaxmindFinder, asn ASNFinder,
	ds dnsiface.Service, tracker DNSTracker, sm ServiceAccountSecretManager, dsm Datastore,
	minVersion string) *Server {
	v, err := semver.NewVersion(minVersion)
	rtx.Must(err, "invalid minimum version")
	return &Server{
		Project:    project,
		Iata:       finder,
		Maxmind:    maxmind,
		ASN:        asn,
		DNS:        ds,
		sm:         sm,
		minVersion: v,

		dnsTracker: tracker,
		dsm:        dsm,
	}
}

// Reload reloads all resources used by the Server.
func (s *Server) Reload(ctx context.Context) {
	s.Iata.Load(ctx)
	s.Maxmind.Reload(ctx)
}

// Lookup is a handler used to find the nearest IATA given client IP or lat/lon metadata.
func (s *Server) Lookup(rw http.ResponseWriter, req *http.Request) {
	resp := v0.LookupResponse{}
	country, err := s.getCountry(req)
	if country == "" || err != nil {
		resp.Error = &v2.Error{
			Type:   "?country=<country>",
			Title:  "could not determine country from request",
			Status: http.StatusBadRequest,
		}
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}
	lat, lon, err := s.getLocation(req)
	if err != nil {
		resp.Error = &v2.Error{
			Type:   "?lat=<lat>&lon=<lon>",
			Title:  "could not determine lat/lon from request",
			Status: http.StatusBadRequest,
		}
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}
	code, err := s.Iata.Lookup(country, lat, lon)
	if err != nil {
		resp.Error = &v2.Error{
			Type:   "internal error",
			Title:  "could not determine iata from request",
			Status: http.StatusInternalServerError,
		}
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}
	resp.Lookup = &v0.Lookup{
		IATA: code,
	}
	writeResponse(rw, resp)
}

// Register handler is used by autonodes to register their hostname with M-Lab
// on startup and receive additional needed configuration metadata.
func (s *Server) Register(rw http.ResponseWriter, req *http.Request) {
	// All replies, errors and successes, should be json.
	rw.Header().Set("Content-Type", "application/json")

	resp := v0.RegisterResponse{}

	// Check version first.
	versionStr := req.URL.Query().Get("version")
	// If no version is provided, default to v0.0.0. This allows existing clients
	// that do not provide the version yet to keep working until a minVersion is set.
	if versionStr == "" {
		versionStr = "v0.0.0"
	}

	// Parse the provided version.
	clientVersion, err := semver.NewVersion(versionStr)
	if err != nil {
		resp.Error = &v2.Error{
			Type:   "version.invalid",
			Title:  "invalid version format - must be semantic version (e.g. v1.2.3)",
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}

	if clientVersion.LessThan(s.minVersion) {
		resp.Error = &v2.Error{
			Type: "version.outdated",
			Title: fmt.Sprintf("version %s is below minimum required version %s",
				clientVersion.String(), s.minVersion.String()),
			Status: http.StatusForbidden,
		}
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}

	param := &register.Params{Project: s.Project}
	param.Service = req.URL.Query().Get("service")
	if !isValidName(param.Service) {
		resp.Error = &v2.Error{
			Type:   "?service=<service>",
			Title:  "could not determine service from request",
			Status: http.StatusBadRequest,
		}
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}

	// Get the organization from the context.
	org, ok := req.Context().Value(orgContextKey).(string)
	if !ok {
		resp.Error = &v2.Error{
			Type:   "auth.context",
			Title:  "missing organization in context",
			Status: http.StatusInternalServerError,
		}
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}
	param.Org = org
	param.IPv6 = checkIP(req.URL.Query().Get("ipv6")) // optional.
	param.IPv4 = checkIP(getClientIP(req))
	ip := net.ParseIP(param.IPv4)
	if ip == nil || ip.To4() == nil {
		resp.Error = &v2.Error{
			Type:   "?ipv4=<ipv4>",
			Title:  "could not determine client ipv4 from request",
			Status: http.StatusBadRequest,
		}
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}
	param.Type = req.URL.Query().Get("type")
	if !isValidType(param.Type) {
		resp.Error = &v2.Error{
			Type:   "?type=<type>",
			Title:  "invalid machine type from request",
			Status: http.StatusBadRequest,
		}
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}
	param.Uplink = req.URL.Query().Get("uplink")
	if !isValidUplink(param.Uplink) {
		resp.Error = &v2.Error{
			Type:   "?uplink=<uplink>",
			Title:  "invalid uplink speed from request",
			Status: http.StatusBadRequest,
		}
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}
	iata := getClientIata(req)
	if iata == "" {
		resp.Error = &v2.Error{
			Type:   "?iata=<iata>",
			Title:  "could not determine iata from request",
			Status: http.StatusBadRequest,
		}
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}
	row, err := s.Iata.Find(iata)
	if err != nil {
		resp.Error = &v2.Error{
			Type:   "iata.find",
			Title:  "could not find given iata in dataset",
			Status: http.StatusInternalServerError,
		}
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}
	param.Metro = row
	record, err := s.Maxmind.City(ip)
	if err != nil {
		resp.Error = &v2.Error{
			Type:   "maxmind.city",
			Title:  "could not find city metadata from ip",
			Status: http.StatusInternalServerError,
		}
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}
	param.Geo = record
	param.Network = s.ASN.AnnotateIP(param.IPv4)

	// Get the organization probability multiplier.
	orgEntity, err := s.dsm.GetOrganization(req.Context(), param.Org)
	orgMultiplier := 1.0
	if err == nil && orgEntity != nil && orgEntity.ProbabilityMultiplier != nil {
		orgMultiplier = *orgEntity.ProbabilityMultiplier
	}
	// Assign the probability by multiplying the org multiplier with the
	// probability requested by the client.
	param.Probability = getProbability(req) * orgMultiplier
	r := register.CreateRegisterResponse(param)

	key, err := s.sm.LoadOrCreateKey(req.Context(), param.Org)
	if err != nil {
		resp.Error = &v2.Error{
			Type:   "load.serviceaccount.key",
			Title:  "could not load service account key for node",
			Status: http.StatusInternalServerError,
		}
		log.Println("loading service account key failure:", err)
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}
	r.Registration.Credentials = &v0.Credentials{
		ServiceAccountKey: key,
	}

	// Register the hostname under the organization zone.
	m := dnsx.NewManager(s.DNS, s.Project, dnsname.OrgZone(param.Org, s.Project))
	_, err = m.Register(req.Context(), r.Registration.Hostname+".", param.IPv4, param.IPv6)
	if err != nil {
		resp.Error = &v2.Error{
			Type:   "dns.register",
			Title:  "could not register dynamic hostname",
			Status: http.StatusInternalServerError,
		}
		log.Println("dns register failure:", err)
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}

	// Add the hostname to the DNS tracker.
	err = s.dnsTracker.Update(r.Registration.Hostname, getPorts(req))
	if err != nil {
		resp.Error = &v2.Error{
			Type:   "tracker.gc",
			Title:  "could not update DNS tracker",
			Status: http.StatusInternalServerError,
		}
		log.Println("dns gc update failure:", err)
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}

	b, _ := json.MarshalIndent(r, "", " ")
	rw.Write(b)
}

// Delete handler is used by operators to delete a previously registered
// hostname from DNS.
func (s *Server) Delete(rw http.ResponseWriter, req *http.Request) {
	// All replies, errors and successes, should be json.
	rw.Header().Set("Content-Type", "application/json")

	resp := v0.DeleteResponse{}
	hostname := req.URL.Query().Get("hostname")
	name, err := host.Parse(hostname)
	if err != nil {
		resp.Error = &v2.Error{
			Type:   "dns.delete",
			Title:  "failed to parse hostname",
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}
		log.Println("dns delete (parse) failure:", err)
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}

	m := dnsx.NewManager(s.DNS, s.Project, dnsname.OrgZone(name.Org, s.Project))
	_, err = m.Delete(req.Context(), name.StringAll()+".")
	if err != nil {
		resp.Error = &v2.Error{
			Type:   "dns.delete",
			Title:  "failed to delete hostname",
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}
		log.Println("dns delete failure:", err)
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}

	err = s.dnsTracker.Delete(name.StringAll())
	if err != nil {
		resp.Error = &v2.Error{
			Type:   "tracker.gc",
			Title:  "failed to delete hostname from DNS tracker",
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}
		log.Println("dns gc delete failure:", err)
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}

	b, err := json.MarshalIndent(resp, "", " ")
	rtx.Must(err, "failed to marshal DNS delete response")
	rw.Write(b)
}

// List handler is used by monitoring to generate a list of known, active
// hostnames previously registered with the Autojoin API.
func (s *Server) List(rw http.ResponseWriter, req *http.Request) {
	// Set CORS policy to allow third-party websites to use returned resources.
	rw.Header().Set("Content-Type", "application/json")
	rw.Header().Set("Access-Control-Allow-Origin", "*")
	rw.Header().Set("Cache-Control", "no-store") // Prevent caching of result.

	configs := []discovery.StaticConfig{}
	resp := v0.ListResponse{}
	hosts, ports, err := s.dnsTracker.List()
	if err != nil {
		resp.Error = &v2.Error{
			Type:   "list",
			Title:  "failed to list node records",
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}
		log.Println("list failure:", err)
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}

	org := req.URL.Query().Get("org")
	format := req.URL.Query().Get("format")
	sites := map[string]bool{}

	// Create a prometheus StaticConfig for each known host.
	for i := range hosts {
		h, err := host.Parse(hosts[i])
		if err != nil {
			continue
		}
		if org != "" && org != h.Org {
			// Skip hosts that are not part of the given org.
			continue
		}
		sites[h.Site] = true
		if format == "script-exporter" {
			// NOTE: do not assign any ports for script exporter.
			ports[i] = []string{""}
		} else {
			// Convert port strings to ":<port>".
			p := []string{}
			for j := range ports[i] {
				p = append(p, ":"+ports[i][j])
			}
			ports[i] = p
		}
		for _, port := range ports[i] {
			labels := map[string]string{
				"machine":    hosts[i],
				"type":       "virtual",
				"deployment": "byos",
				"managed":    "none",
				"org":        h.Org,
			}
			if req.URL.Query().Get("service") != "" {
				labels["service"] = req.URL.Query().Get("service")
			}
			// We create one record per host to add a unique "machine" label to each one.
			configs = append(configs, discovery.StaticConfig{
				Targets: []string{hosts[i] + port},
				Labels:  labels,
			})
		}
	}

	var results interface{}
	switch format {
	case "script-exporter":
		fallthrough
	case "blackbox":
		fallthrough
	case "prometheus":
		results = configs
	case "servers":
		resp.Servers = hosts
		results = resp
	case "sites":
		for k := range sites {
			resp.Sites = append(resp.Sites, k)
		}
		results = resp
	default:
		resp.Servers = hosts
		results = resp
	}
	// Generate as JSON; the list may be empty.
	b, err := json.MarshalIndent(results, "", " ")
	rtx.Must(err, "failed to marshal DNS delete response")
	rw.Write(b)
}

// Live reports whether the system is live.
func (s *Server) Live(rw http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(rw, "ok")
}

// Ready reports whether the server is ready.
func (s *Server) Ready(rw http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(rw, "ok")
}

func getClientIata(req *http.Request) string {
	iata := req.URL.Query().Get("iata")
	if iata != "" && len(iata) == 3 && isValidName(iata) {
		return strings.ToLower(iata)
	}
	return ""
}

func isValidName(s string) bool {
	if s == "" {
		return false
	}
	if len(s) > 10 {
		return false
	}
	return validName.MatchString(s)
}

func isValidType(s string) bool {
	switch s {
	case "physical", "virtual":
		return true
	default:
		return false
	}
}

func isValidUplink(s string) bool {
	// Minimally make sure the uplink speed specification looks like some
	// numbers followed by "g".
	matched, _ := regexp.MatchString("[0-9]+g", s)
	return matched
}

func (s *Server) getCountry(req *http.Request) (string, error) {
	c := req.URL.Query().Get("country")
	if c != "" {
		return c, nil
	}
	c = req.Header.Get("X-AppEngine-Country")
	if c != "" {
		return c, nil
	}
	record, err := s.Maxmind.City(net.ParseIP(getClientIP(req)))
	if err != nil {
		return "", err
	}
	return record.Country.IsoCode, nil
}

func rawLatLon(req *http.Request) (string, string, error) {
	lat := req.URL.Query().Get("lat")
	lon := req.URL.Query().Get("lon")
	if lat != "" && lon != "" {
		return lat, lon, nil
	}
	latlon := req.Header.Get("X-AppEngine-CityLatLong")
	if latlon != "0.000000,0.000000" {
		fields := strings.Split(latlon, ",")
		if len(fields) == 2 {
			return fields[0], fields[1], nil
		}
	}
	return "", "", errLocationNotFound
}

func (s *Server) getLocation(req *http.Request) (float64, float64, error) {
	rlat, rlon, err := rawLatLon(req)
	if err == nil {
		lat, errLat := strconv.ParseFloat(rlat, 64)
		lon, errLon := strconv.ParseFloat(rlon, 64)
		if errLat != nil || errLon != nil {
			return 0, 0, errLocationFormat
		}
		return lat, lon, nil
	}
	// Fall back to lookup with request IP.
	record, err := s.Maxmind.City(net.ParseIP(getClientIP(req)))
	if err != nil {
		return 0, 0, err
	}
	return record.Location.Latitude, record.Location.Longitude, nil
}

func writeResponse(rw http.ResponseWriter, resp interface{}) {
	b, err := json.MarshalIndent(resp, "", "  ")
	// NOTE: marshal can only fail on incompatible types, like functions. The
	// panic will be caught by the http server handler.
	rtx.PanicOnError(err, "failed to marshal response")
	rw.Write(b)
}

func checkIP(ip string) string {
	if net.ParseIP(ip) != nil {
		return ip
	}
	return ""
}

func getClientIP(req *http.Request) string {
	// Use given IP parameter.
	rawip := req.URL.Query().Get("ipv4")
	if rawip != "" {
		return rawip
	}
	// Use AppEngine's forwarded client address.
	fwdIPs := strings.Split(req.Header.Get("X-Forwarded-For"), ", ")
	if fwdIPs[0] != "" {
		return fwdIPs[0]
	}
	// Use remote client address.
	hip, _, _ := net.SplitHostPort(req.RemoteAddr)
	return hip
}

func getProbability(req *http.Request) float64 {
	prob := req.URL.Query().Get("probability")
	if prob == "" {
		return 1.0
	}
	p, err := strconv.ParseFloat(prob, 64)
	if err != nil {
		return 1.0
	}
	return p
}

func getPorts(req *http.Request) []string {
	result := []string{}
	ports := req.URL.Query()["ports"]
	for _, port := range ports {
		// Verify this is a valid number.
		_, err := strconv.ParseInt(port, 10, 64)
		if err != nil {
			// Skip if not.
			continue
		}
		result = append(result, port)
	}
	if len(result) == 0 {
		return []string{"9990"} // default port
	}
	return result
}
