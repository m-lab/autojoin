package handler

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	v0 "github.com/m-lab/autojoin/api/v0"
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
	iata, err := s.rawIata(req, ip)
	if err != nil {
		fmt.Println("raw iata")
		rw.WriteHeader(http.StatusBadRequest)
		return
	}

	ann := s.ASN.AnnotateIP(rawip)
	hexip := hex.EncodeToString(ip.To4())
	r := v0.RegisterResponse{
		Registration: &v0.Registration{
			Hostname: fmt.Sprintf("%s-%s%d-%s.%s.autojoin.measurement-lab.org", service, iata, ann.ASNumber, hexip, org),
		},
	}
	b, _ := json.MarshalIndent(r, "", " ")
	fmt.Print(string(b))
	fmt.Fprint(rw, string(b))
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

func (s *Server) rawIata(req *http.Request, ip net.IP) (string, error) {
	iata := req.URL.Query().Get("iata")
	if iata != "" && len(iata) == 3 && isValidName(iata) {
		return strings.ToLower(iata), nil
	}
	record, err := s.Maxmind.City(ip)
	if err != nil {
		return "", ErrInternalError
	}
	log.Println(record)
	iata, err = s.Iata.Lookup(record.Country.IsoCode, record.Location.Latitude, record.Location.Longitude)
	if err != nil {
		return "", ErrInternalError
	}
	return iata, nil
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
	rawip := req.URL.Query().Get("ip")
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
