package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	v0 "github.com/m-lab/autojoin/api/v0"
	v2 "github.com/m-lab/locate/api/v2"
)

// Server maintains shared state for the server.
type Server struct {
	Project string
	Iata    IataFinder
}

// IataFinder is an interface used by the Server to manage IATA information.
type IataFinder interface {
	Lookup(country string, lat, lon float64) (string, error)
	Load(ctx context.Context) error
}

// NewServer creates a new Server instance for request handling.
func NewServer(project string, finder IataFinder) *Server {
	return &Server{
		Project: project,
		Iata:    finder,
	}
}

// Reload reloads all resources used by the Server.
func (s *Server) Reload(ctx context.Context) {
	s.Iata.Load(ctx)
}

// Lookup is a handler used to find the nearest IATA given client IP or lat/lon metadata.
func (s *Server) Lookup(rw http.ResponseWriter, req *http.Request) {
	resp := v0.LookupResponse{}
	country := rawCountry(req)
	if country == "" {
		resp.Error = &v2.Error{
			Type:   "?country=<country>",
			Title:  "could not determine country from request",
			Status: http.StatusBadRequest,
		}
		rw.WriteHeader(resp.Error.Status)
		writeResponse(rw, resp)
		return
	}
	rlat, rlon := rawLatLon(req)
	lat, errLat := strconv.ParseFloat(rlat, 64)
	lon, errLon := strconv.ParseFloat(rlon, 64)
	if errLat != nil || errLon != nil {
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

func writeResponse(rw http.ResponseWriter, resp interface{}) error {
	b, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return err
	}
	rw.Write(b)
	return nil
}
