package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	v0 "github.com/m-lab/autojoin/api/v0"
	"github.com/m-lab/go/rtx"
	v2 "github.com/m-lab/locate/api/v2"
	"github.com/oschwald/geoip2-golang"
)

var (
	errLocationNotFound = errors.New("location not found")
	errLocationFormat   = errors.New("location could not be parsed")
)

// Server maintains shared state for the server.
type Server struct {
	Project string
	Iata    IataFinder
	Maxmind MaxmindFinder
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

// NewServer creates a new Server instance for request handling.
func NewServer(project string, finder IataFinder, maxmind MaxmindFinder) *Server {
	return &Server{
		Project: project,
		Iata:    finder,
		Maxmind: maxmind,
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

// Register is a handler used by autonodes to register with M-Lab on startup.
func (s *Server) Register(rw http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(rw, "TODO(soltesz): complete register logic\n")
}

// Live reports whether the system is live.
func (s *Server) Live(rw http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(rw, "ok")
}

// Ready reports whether the server is ready.
func (s *Server) Ready(rw http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(rw, "ok")
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
