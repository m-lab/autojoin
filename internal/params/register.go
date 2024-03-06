package params

import (
	"encoding/hex"
	"fmt"
	"net"
	"strings"

	v0 "github.com/m-lab/autojoin/api/v0"
	"github.com/m-lab/autojoin/iata"
	v2 "github.com/m-lab/locate/api/v2"
	"github.com/m-lab/uuid-annotator/annotator"
	"github.com/oschwald/geoip2-golang"
)

// Register is used internally to pass many parameters.
type Register struct {
	Service string
	Org     string
	Project string
	IPv4    string
	IPv6    string
	Row     iata.Row
	Record  *geoip2.City
	Ann     *annotator.Network
}

// CreateRegisterResponse generates a RegisterResponse from the given Register parameters.
func CreateRegisterResponse(p *Register) v0.RegisterResponse {
	// Calculate machine, site, and hostname.
	machine := hex.EncodeToString(net.ParseIP(p.IPv4).To4())
	site := fmt.Sprintf("%s%d", p.Row.IATA, p.Ann.ASNumber)
	hostname := fmt.Sprintf("%s-%s-%s.%s.%s.measurement-lab.org", p.Service, site, machine, p.Org, strings.TrimPrefix(p.Project, "mlab-"))

	// Using these, create geo annotation.
	geo := &annotator.Geolocation{
		ContinentCode: p.Record.Continent.Code,
		CountryCode:   p.Record.Country.IsoCode,
		CountryName:   p.Record.Country.Names["en"],
		MetroCode:     int64(p.Record.Location.MetroCode),
		City:          p.Record.City.Names["en"],
		PostalCode:    p.Record.Postal.Code,
		// Use iata location as authoritative.
		Latitude:  p.Row.Latitude,
		Longitude: p.Row.Longitude,
	}
	if len(p.Record.Subdivisions) > 0 {
		geo.Subdivision1ISOCode = p.Record.Subdivisions[0].IsoCode
		geo.Subdivision1Name = p.Record.Subdivisions[0].Names["en"]
		if len(p.Record.Subdivisions) > 1 {
			geo.Subdivision2ISOCode = p.Record.Subdivisions[1].IsoCode
			geo.Subdivision2Name = p.Record.Subdivisions[1].Names["en"]
		}
	}

	// Put everything together into a RegisterResponse.
	r := v0.RegisterResponse{
		Registration: &v0.Registration{
			Hostname: hostname,
			Annotation: &v0.ServerAnnotation{
				Annotation: annotator.ServerAnnotations{
					Site:    site,
					Machine: machine,
					Geo:     geo,
					Network: p.Ann,
				},
				Network: v0.Network{
					IPv4: p.IPv4,
					IPv6: p.IPv6,
				},
				Type: "unknown", // should be overridden by node.
			},
			Heartbeat: &v2.Registration{
				City:          geo.City,
				CountryCode:   geo.CountryCode,
				ContinentCode: geo.ContinentCode,
				Experiment:    p.Service,
				Hostname:      hostname,
				Latitude:      geo.Latitude,
				Longitude:     geo.Longitude,
				Machine:       machine,
				Metro:         site[:3],
				Project:       p.Project,
				Probability:   1,
				Site:          site,
				Type:          "unknown", // should be overridden by node.
				Uplink:        "unknown", // should be overridden by node.
			},
		},
	}
	return r
}
