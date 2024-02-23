package v0

import (
	v2 "github.com/m-lab/locate/api/v2"
	"github.com/m-lab/uuid-annotator/annotator"
)

// RegisterResponse is returned by a successful.
type RegisterResponse struct {
	Error        *v2.Error `json:"error,omitempty"`
	Registration *Registration
}

// Network contains IPv4 and IPv6 addresses.
type Network struct {
	IPv4 string
	IPv6 string
}

// ServerAnnotation is a record used by the uuid-annotator.
// From: https://github.com/m-lab/uuid-annotator/blob/main/siteannotator/server.go#L83-L90
type ServerAnnotation struct {
	Annotation annotator.ServerAnnotations
	Network    Network
	Type       string
}

// Registration is returned for a successful registration request.
type Registration struct {
	// Hostname is the dynamic DNS name. Hostname should be available immediately.
	Hostname string

	// Server is the annotation record used by the uuid-annotator for all server annotations.
	Server *ServerAnnotation `json:",omitempty"`
	// Heartbeat is the registration message used by the heartbeat service to register with Locate API.
	Heartbeat *v2.Registration `json:",omitempty"`
}
