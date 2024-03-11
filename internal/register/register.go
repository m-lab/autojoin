package register

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

var (
	mlabDomain = "measurement-lab.org"
)

// Params is used internally to collect multiple parameters.
type Params struct {
	Project string
	Service string
	Org     string
	IPv4    string
	IPv6    string
	Geo     *geoip2.City
	Metro   iata.Row
	Network *annotator.Network
}

// OrgZone generates the organization zone name based the organization and project.
func OrgZone(org, project string) string {
	// NOTE: prefix prevents name collision with existing zones when the org is "mlab".
	return "autojoin-" + org + "-" + strings.TrimPrefix(project, "mlab-") + "-measurement-lab-org"
}

// CreateRegisterResponse generates a RegisterResponse from the given
// parameters. As an internal package, the caller is required to validate all
// input parameters.
func CreateRegisterResponse(p *Params) v0.RegisterResponse {
	// Calculate machine, site, and hostname.
	machine := hex.EncodeToString(net.ParseIP(p.IPv4).To4())
	site := fmt.Sprintf("%s%d", p.Metro.IATA, p.Network.ASNumber)
	hostname := fmt.Sprintf("%s-%s-%s.%s.%s.%s", p.Service, site, machine, p.Org, strings.TrimPrefix(p.Project, "mlab-"), mlabDomain)

	// Using these, create geo annotation.
	geo := &annotator.Geolocation{
		ContinentCode: p.Geo.Continent.Code,
		CountryCode:   p.Geo.Country.IsoCode,
		CountryName:   p.Geo.Country.Names["en"],
		MetroCode:     int64(p.Geo.Location.MetroCode),
		City:          p.Geo.City.Names["en"],
		PostalCode:    p.Geo.Postal.Code,
		// Use iata location as authoritative.
		Latitude:  p.Metro.Latitude,
		Longitude: p.Metro.Longitude,
	}
	if len(p.Geo.Subdivisions) > 0 {
		geo.Subdivision1ISOCode = p.Geo.Subdivisions[0].IsoCode
		geo.Subdivision1Name = p.Geo.Subdivisions[0].Names["en"]
		if len(p.Geo.Subdivisions) > 1 {
			geo.Subdivision2ISOCode = p.Geo.Subdivisions[1].IsoCode
			geo.Subdivision2Name = p.Geo.Subdivisions[1].Names["en"]
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
					Network: p.Network,
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
