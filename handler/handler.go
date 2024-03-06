package handler

import (
	"context"
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
	"github.com/m-lab/autojoin/internal/register"
	"github.com/m-lab/go/rtx"
	v2 "github.com/m-lab/locate/api/v2"
	"github.com/m-lab/uuid-annotator/annotator"
	"github.com/oschwald/geoip2-golang"
)

var (
	errLocationNotFound = errors.New("location not found")
	errLocationFormat   = errors.New("location could not be parsed")

	validName = regexp.MustCompile(`[a-z0-9]+`)
)

// Server maintains shared state for the server.
type Server struct {
	Project string
	Iata    IataFinder
	Maxmind MaxmindFinder
	ASN     ASNFinder
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
	resp := v0.RegisterResponse{}
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
	// TODO(soltesz): discover this from a given API key.
	param.Org = req.URL.Query().Get("organization")
	if !isValidName(param.Org) {
		resp.Error = &v2.Error{
			Type:   "?organization=<organization>",
			Title:  "could not determine organization from request",
			Status: http.StatusBadRequest,
		}
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}
	// TODO: check value.
	param.IPv6 = req.URL.Query().Get("ipv6") // optional.
	param.IPv4 = getClientIP(req)
	ip := net.ParseIP(param.IPv4)
	if ip == nil {
		resp.Error = &v2.Error{
			Type:   "?ipv4=<ipv4>",
			Title:  "could not determine client ip from request",
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
	r := register.CreateRegisterResponse(param)

	// TODO: register hostname in Cloud DNS.

	b, _ := json.MarshalIndent(r, "", " ")
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
