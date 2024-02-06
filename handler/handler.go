package handler

import (
	"fmt"
	"net/http"
)

// Server maintains shared state for the server.
type Server struct {
	Project string
}

// NewServer creates a new Server instance for request handling.
func NewServer(project string) *Server {
	return &Server{
		Project: project,
	}
}

// Lookup is a handler used to loookup a nearest IATA given client IP or lat/lon metadata.
func (s *Server) Lookup(rw http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(rw, "TODO(soltesz): complete lookup logic\n")
}

// Register is a handler used by autonodes to register with M-Lab on startup.
func (s *Server) Register(rw http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(rw, "TODO(soltesz): complete register logic\n")
}

// Live is a handler to report whether the system is live.
func (s *Server) Live(rw http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(rw, "ok")
}

func (s *Server) Ready(rw http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(rw, "ok")
}
