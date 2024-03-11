package dnsx

import (
	"context"
	"errors"
	"log"

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

// Register creates a new resource record for hostname with the given ipv4 and ipv6 adresses.
func (d *Manager) Register(ctx context.Context, hostname, ipv4, ipv6 string) (*dns.Change, error) {
	chg := &dns.Change{}
	records := []struct {
		ip    string
		rtype string
	}{
		{ip: ipv4, rtype: recordTypeA},
		{ip: ipv6, rtype: recordTypeAAAA},
	}
	for _, record := range records {
		add := false
		rr, err := d.get(ctx, hostname, record.rtype)
		if rr != nil {
			// Found a registration.
			if len(rr.Rrdatas) == 1 && rr.Rrdatas[0] == record.ip {
				// Record matches given parameters, so we do not need to add or
				// delete it.
				continue
			}
			// But, this record is different from the given parameters, so remove it.
			chg.Deletions = append(chg.Deletions,
				&dns.ResourceRecordSet{
					Name:    hostname,
					Type:    rr.Type,
					Ttl:     rr.Ttl,
					Rrdatas: rr.Rrdatas,
				},
			)
			add = true
		}
		// If the error is because the record was not found, add it. Ignore other errors.
		log.Printf("dns get record: %#v", err)
		if add || (err != nil && isNotFound(err)) {
			// Register.
			chg.Additions = append(chg.Additions,
				&dns.ResourceRecordSet{
					Name:    hostname,
					Type:    record.rtype,
					Ttl:     300,
					Rrdatas: []string{record.ip},
				},
			)
		}
	}
	// Apply changes.
	result, err := d.Service.ChangeCreate(ctx, d.Project, d.Zone, chg)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Delete removes all resource records associated with the given hostname.
func (d *Manager) Delete(ctx context.Context, hostname string) (*dns.Change, error) {
	chg := &dns.Change{}
	for _, rtype := range []string{recordTypeA, recordTypeAAAA} {
		rr, err := d.get(ctx, hostname, rtype)
		if rr != nil {
			// A record was found, so let's plan to delete it.
			chg.Deletions = append(chg.Deletions, &dns.ResourceRecordSet{
				Name:    rr.Name,
				Type:    rr.Type,
				Ttl:     rr.Ttl,
				Rrdatas: rr.Rrdatas,
			})
		}
		if err != nil && !isNotFound(err) {
			// A different error occured. The host record may or may not exist.
			return nil, err
		}
	}
	result, err := d.Service.ChangeCreate(ctx, d.Project, d.Zone, chg)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// get retrieves a resource record for the given hostname and rtype.
func (d *Manager) get(ctx context.Context, hostname, rtype string) (*dns.ResourceRecordSet, error) {
	return d.Service.ResourceRecordSetsGet(ctx, d.Project, d.Zone, hostname, rtype)
}

// checks whether this is a googleapi.Error for "not found".
func isNotFound(err error) bool {
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		log.Printf("googleapi.Error: %#v", gerr)
		return gerr.Code == 404
	}
	return false
}
