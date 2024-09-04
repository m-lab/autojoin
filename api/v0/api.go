package v0

import (
	"github.com/m-lab/gcp-service-discovery/discovery"
	v2 "github.com/m-lab/locate/api/v2"
	"github.com/m-lab/uuid-annotator/annotator"
)

// LookupResponse is returned by a lookup request.
type LookupResponse struct {
	Error  *v2.Error `json:",omitempty"`
	Lookup *Lookup   `json:",omitempty"`
}

// Lookup is returned for a successful lookup request.
type Lookup struct {
	IATA string
}

// RegisterResponse is returned by a register request.
type RegisterResponse struct {
	Error        *v2.Error     `json:",omitempty"`
	Registration *Registration `json:",omitempty"`
}

// DeleteResponse is returned by a delete request.
type DeleteResponse struct {
	Error *v2.Error `json:",omitempty"`
}

// ListResponse is returned by a list request.
type ListResponse struct {
	Error        *v2.Error                `json:",omitempty"`
	StaticConfig []discovery.StaticConfig `json:",omitempty"`
}

// Network contains IPv4 and IPv6 addresses.
type Network struct {
	IPv4 string
	IPv6 string
}

// ServerAnnotation is used by the uuid-annotator.
// From: https://github.com/m-lab/uuid-annotator/blob/main/siteannotator/server.go#L83-L90
type ServerAnnotation struct {
	Annotation annotator.ServerAnnotations
	Network    Network
	Type       string
}

// Credentials contains data for the node to authorize or validate operations.
type Credentials struct {
	// ServiceAccountKey contains the base64 encoded service account key for use
	// by the registered node.
	ServiceAccountKey string
}

// Registration is returned for a successful registration request.
type Registration struct {
	// Hostname is the dynamic DNS name. Hostname should be available immediately.
	Hostname string

	// Annotation is the metadata used by the uuid-annotator for all server annotations.
	Annotation *ServerAnnotation `json:",omitempty"`
	// Heartbeat is the registration message used by the heartbeat service to register with the Locate API.
	Heartbeat *v2.Registration `json:",omitempty"`

	// Credentials contains node key data.
	Credentials *Credentials `json:",omitempty"`
}
