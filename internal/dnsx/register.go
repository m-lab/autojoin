package dnsx

import (
	"context"
	"errors"

	"github.com/m-lab/autojoin/internal/dnsx/dnsiface"
	"google.golang.org/api/dns/v1"
	"google.golang.org/api/googleapi"
)

var (
	// ErrBadIPFormat is returned when registering a hostname with a malformed IP.
	ErrBadIPFormat = errors.New("bad ip format")

	recordTypeA    = "A"
	recordTypeAAAA = "AAAA"
)

// Manager contains state needed for managing DNS recors.
type Manager struct {
	Project string
	Zone    string
	Service dnsiface.Service
}

// NewManager creates a new Manager instance.
func NewManager(s dnsiface.Service, project, zone string) *Manager {
	return &Manager{
		Project: project,
		Zone:    zone,
		Service: s,
	}
}

func appendDeletions(chg *dns.Change, rr *dns.ResourceRecordSet, hostname string) {
	chg.Deletions = append(chg.Deletions,
		&dns.ResourceRecordSet{
			Name:    hostname,
			Type:    rr.Type,
			Ttl:     rr.Ttl,
			Rrdatas: rr.Rrdatas,
		},
	)
}

func appendAdditions(chg *dns.Change, hostname, ip, rtype string) {
	chg.Additions = append(chg.Additions,
		&dns.ResourceRecordSet{
			Name:    hostname,
			Type:    rtype,
			Ttl:     300,
			Rrdatas: []string{ip},
		},
	)
}

// Register creates a new resource record for hostname with the given ipv4 and ipv6 adresses.
func (d *Manager) Register(ctx context.Context, hostname, ipv4, ipv6 string) (*dns.Change, error) {
	chg := &dns.Change{}
	var err error
	var rr *dns.ResourceRecordSet

	// IPv4 is required. An empty ipv4 value will generate an error.
	rr, err = d.get(ctx, hostname, recordTypeA)
	if isNotFound(err) {
		appendAdditions(chg, hostname, ipv4, recordTypeA)
	}
	if rr != nil {
		// Record matches given parameters, so we do not need to add or delete it.
		matches := (len(rr.Rrdatas) == 1 && rr.Rrdatas[0] == ipv4)
		if !matches {
			// We found an existing resource record that doesn't match the given address.
			// Remove the old one and add a new one.
			appendDeletions(chg, rr, hostname)
			appendAdditions(chg, hostname, ipv4, recordTypeA)
		}
	}

	// IPv6 remains optional for now.
	if ipv6 != "" {
		rr, err = d.get(ctx, hostname, recordTypeAAAA)
		if isNotFound(err) {
			appendAdditions(chg, hostname, ipv6, recordTypeAAAA)
		}
		if rr != nil {
			matches := (len(rr.Rrdatas) == 1 && rr.Rrdatas[0] == ipv6)
			if !matches {
				appendDeletions(chg, rr, hostname)
				appendAdditions(chg, hostname, ipv6, recordTypeAAAA)
			}
		}
	}

	if chg.Additions == nil && chg.Deletions == nil {
		// Without any actions, the ChangeCreate will fail.
		return nil, err
	}

	return d.Service.ChangeCreate(ctx, d.Project, d.Zone, chg)
}

// Delete removes all resource records associated with the given hostname.
func (d *Manager) Delete(ctx context.Context, hostname string) (*dns.Change, error) {
	chg := &dns.Change{}
	for _, rtype := range []string{recordTypeA, recordTypeAAAA} {
		rr, err := d.get(ctx, hostname, rtype)
		if err != nil && !isNotFound(err) {
			// A different error occured. The host record may or may not exist.
			return nil, err
		}
		if rr != nil {
			// Remove the record we found.
			appendDeletions(chg, rr, hostname)
		}
	}
	return d.Service.ChangeCreate(ctx, d.Project, d.Zone, chg)
}

// get retrieves a resource record for the given hostname and rtype.
func (d *Manager) get(ctx context.Context, hostname, rtype string) (*dns.ResourceRecordSet, error) {
	return d.Service.ResourceRecordSetsGet(ctx, d.Project, d.Zone, hostname, rtype)
}

// checks whether this is a googleapi.Error for "not found".
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		return gerr.Code == 404
	}
	return false
}
