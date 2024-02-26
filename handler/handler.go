package handler

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	v0 "github.com/m-lab/autojoin/api/v0"
	"github.com/m-lab/autojoin/iata"
	v2 "github.com/m-lab/locate/api/v2"
	"github.com/m-lab/uuid-annotator/annotator"
	"github.com/oschwald/geoip2-golang"
)

// Server maintains shared state for the server.
type Server struct {
	Project string
	Iata    IataFinder
	Maxmind MaxmindFinder
	ASN     ASNFinder
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

// ASNFinder is an interface used by the Server to manage ASN information.
type ASNFinder interface {
	AnnotateIP(src string) *annotator.Network
	Reload(ctx context.Context)
}

// NewServer creates a new Server instance for request handling.
func NewServer(project string, finder IataFinder, maxmind MaxmindFinder, asn ASNFinder) *Server {
	return &Server{
		Project: project,
		Iata:    finder,
		Maxmind: maxmind,
		ASN:     asn,
	}
}

// Reload reloads all resources used by the Server.
func (s *Server) Reload(ctx context.Context) {
	s.Iata.Load(ctx)
	s.Maxmind.Reload(ctx)
	s.ASN.Reload(ctx)
}

// Lookup is a handler used to find the nearest IATA given client IP or lat/lon metadata.
func (s *Server) Lookup(rw http.ResponseWriter, req *http.Request) {
	// lookup country - param, app engine, maxmind(ip)
	// lookup latlon - param, app engine, maxmind(ip)
	country := rawCountry(req)
	if country == "" {
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "could not determine country")
		return
	}
	rlat, rlon := rawLatLon(req)
	lat, errLat := strconv.ParseFloat(rlat, 64)
	lon, errLon := strconv.ParseFloat(rlon, 64)
	if errLat != nil || errLon != nil {
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "could not determine lat/lon")
		return
	}
	code, err := s.Iata.Lookup(country, lat, lon)
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(rw, "could not determine iata code: %v", err)
		return
	}
	fmt.Fprintf(rw, "%s\n", code)
}

// Register is a handler used by autonodes to register with M-Lab on startup.
func (s *Server) Register(rw http.ResponseWriter, req *http.Request) {
	// service - required, param
	// org - required, param, api key
	// iata - required
	// ip - geo locations
	// asn - from ip
	service := req.URL.Query().Get("service")
	if !isValidName(service) {
		fmt.Println("service")
		rw.WriteHeader(http.StatusBadRequest)
		return
	}
	// TODO(soltesz): discover this from a given API key.
	org := req.URL.Query().Get("organization")
	if !isValidName(org) {
		fmt.Println("org")
		rw.WriteHeader(http.StatusBadRequest)
		return
	}
	rawip := rawIPFromRequest(req)
	ip := net.ParseIP(rawip)
	if ip == nil {
		fmt.Println("parse ip")
		rw.WriteHeader(http.StatusBadRequest)
		return
	}
	iata, err := rawIata(req)
	if err != nil {
		fmt.Println("raw iata")
		rw.WriteHeader(http.StatusBadRequest)
		return
	}
	row, err := s.Iata.Find(iata)
	if err != nil {
		fmt.Println("iata not found")
		rw.WriteHeader(http.StatusBadRequest)
		return
	}

	record, err := s.Maxmind.City(ip)
	if err != nil {
		fmt.Println("maxmind lookup failure")
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	tmp := &annotator.Geolocation{
		ContinentCode: record.Continent.Code,
		CountryCode:   record.Country.IsoCode,
		CountryName:   record.Country.Names["en"],
		MetroCode:     int64(record.Location.MetroCode),
		City:          record.City.Names["en"],
		PostalCode:    record.Postal.Code,
		// Use iata location as authoritative.
		Latitude:  row.Latitude,
		Longitude: row.Longitude,
	}
	// Collect subdivision information, if found.
	if len(record.Subdivisions) > 0 {
		tmp.Subdivision1ISOCode = record.Subdivisions[0].IsoCode
		tmp.Subdivision1Name = record.Subdivisions[0].Names["en"]
		if len(record.Subdivisions) > 1 {
			tmp.Subdivision2ISOCode = record.Subdivisions[1].IsoCode
			tmp.Subdivision2Name = record.Subdivisions[1].Names["en"]
		}
	}

	ann := s.ASN.AnnotateIP(rawip)
	machine := hex.EncodeToString(ip.To4())
	site := fmt.Sprintf("%s%d", iata, ann.ASNumber)
	// TODO: populate project by local project.
	hostname := fmt.Sprintf("%s-%s-%s.%s.%s.measurement-lab.org", service, site, machine, org, strings.TrimPrefix(s.Project, "mlab-"))
	r := v0.RegisterResponse{
		Registration: &v0.Registration{
			Hostname: hostname,
			Annotation: &v0.ServerAnnotation{
				Annotation: annotator.ServerAnnotations{
					Site:    site,
					Machine: machine,
					Geo:     tmp,
					Network: ann,
				},
				Network: v0.Network{
					IPv4: rawip,
					IPv6: "",
				},
				Type: "unknown",
			},
			Heartbeat: &v2.Registration{
				City:          tmp.City,
				CountryCode:   tmp.CountryCode,
				ContinentCode: tmp.ContinentCode,
				Experiment:    service,
				Hostname:      hostname,
				Latitude:      tmp.Latitude,
				Longitude:     tmp.Longitude,
				Machine:       machine,
				Metro:         site[:3],
				Project:       s.Project,
				Probability:   1,
				Site:          site,
				Type:          "unknown", // should be overridden by node.
				Uplink:        "unknown", // should be overridden by node.
			},
		},
	}
	b, _ := json.MarshalIndent(r, "", " ")
	// fmt.Print(string(b))
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

var (
	ErrBadRequest    = errors.New("bad request")
	ErrInternalError = errors.New("internal error")
)

func rawIata(req *http.Request) (string, error) {
	iata := req.URL.Query().Get("iata")
	if iata != "" && len(iata) == 3 && isValidName(iata) {
		return strings.ToLower(iata), nil
	}
	return "", ErrInternalError
}

func rawCountry(req *http.Request) string {
	c := req.URL.Query().Get("country")
	if c != "" {
		return c
	}
	c = req.Header.Get("X-AppEngine-Country")
	if c != "" {
		return c
	}
	// TODO: lookup with request IP.
	return ""
}

func rawLatLon(req *http.Request) (string, string) {
	lat := req.URL.Query().Get("lat")
	lon := req.URL.Query().Get("lon")
	if lat != "" && lon != "" {
		return lat, lon
	}
	latlon := req.Header.Get("X-AppEngine-CityLatLong")
	if latlon == "0.000000,0.000000" {
		// TODO: lookup with request IP.
		return "", ""
	}
	fields := strings.Split(latlon, ",")
	if len(fields) == 2 {
		return fields[0], fields[1]
	}
	// TODO: lookup with request IP.
	return "", ""
}

func rawIPFromRequest(req *http.Request) string {
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
	if hip != "" {
		return hip
	}
	return ""
}

// TODO: ideally this is filtered on pre-registered / defined services, or organizations.
func isValidName(s string) bool {
	if s == "" {
		return false
	}
	if len(s) > 10 {
		return false
	}
	matched, err := regexp.MatchString(`[a-z0-9]+`, s)
	if err != nil {
		return false
	}
	return matched
}

/*
}*/
