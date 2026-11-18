package adminx

import (
	"context"
	"fmt"
	"log"

	"github.com/m-lab/autojoin/internal/dnsname"
	"github.com/m-lab/token-exchange/store"

	"google.golang.org/api/dns/v1"
)

// DNS is a simplified interface to the Google Cloud DNS API.
type DNS interface {
	RegisterZone(ctx context.Context, zone *dns.ManagedZone) (*dns.ManagedZone, error)
	RegisterZoneSplit(ctx context.Context, zone *dns.ManagedZone) (*dns.ResourceRecordSet, error)
}

// OrganizationManager defines the interface for managing organizations and their API keys
type OrganizationManager interface {
	CreateOrganization(ctx context.Context, name, email string) error
	GetOrganization(ctx context.Context, name string) (*store.AutojoinOrganization, error)
	CreateAPIKeyWithValue(ctx context.Context, org, value string) (string, error)
	GetAPIKeys(ctx context.Context, org string) ([]string, error)
}

// OrgFolders ensures an org's upload folder and its write permission exist.
type OrgFolders interface {
	EnsureOrgFolder(ctx context.Context, org string) error
}

// Org contains fields needed to setup a new organization for Autojoined nodes.
type Org struct {
	Project string
	folders OrgFolders
	orgm    OrganizationManager
	dns     DNS
}

// NewOrg creates a new Org instance for setting up a new organization.
func NewOrg(project string, folders OrgFolders, dns DNS, orgm OrganizationManager) *Org {
	return &Org{
		Project: project,
		folders: folders,
		orgm:    orgm,
		dns:     dns,
	}
}

// Setup should be run once on org creation to create all Google Cloud resources needed by the Autojoin API.
//
// NOTE: CreateOrganization overwrites an existing Datastore entity (email,
// creation time and probability multiplier). Use SetupStorage for orgs that
// already exist.
func (o *Org) Setup(ctx context.Context, org string, email string) error {
	if err := ValidateOrgName(org); err != nil {
		return err
	}
	// Create organization in Datastore
	err := o.orgm.CreateOrganization(ctx, org, email)
	if err != nil {
		return err
	}
	// Create the org's upload folder and grant the org's federated identities write access.
	err = o.folders.EnsureOrgFolder(ctx, org)
	if err != nil {
		return err
	}
	// Create DNS zone and zone split.
	err = o.RegisterDNS(ctx, org)
	if err != nil {
		return err
	}
	return nil
}

// SetupStorage creates the upload folder and its write permission for an org
// that already exists in Datastore. It does not modify the Datastore entity,
// DNS, or API keys.
func (o *Org) SetupStorage(ctx context.Context, org string) error {
	if err := ValidateOrgName(org); err != nil {
		return err
	}
	if _, err := o.orgm.GetOrganization(ctx, org); err != nil {
		return fmt.Errorf("failed to get organization %q: %w", org, err)
	}
	return o.folders.EnsureOrgFolder(ctx, org)
}

// RegisterDNS creates the organization zone and the zone split within the project zone.
func (o *Org) RegisterDNS(ctx context.Context, org string) error {
	zone, err := o.dns.RegisterZone(ctx, &dns.ManagedZone{
		Description: "Autojoin registered nodes from org: " + org,
		Name:        dnsname.OrgZone(org, o.Project),
		DnsName:     dnsname.OrgDNS(org, o.Project),
		DnssecConfig: &dns.ManagedZoneDnsSecConfig{
			State: "on",
		},
	})
	if err != nil {
		log.Println("failed to register zone:", dnsname.OrgZone(org, o.Project), err)
		return err
	}
	_, err = o.dns.RegisterZoneSplit(ctx, zone)
	if err != nil {
		log.Println("failed to register zone split:", dnsname.OrgZone(org, o.Project), err)
		return err
	}
	return nil
}

func (o *Org) CreateAPIKeyWithValue(ctx context.Context, org, val string) (string, error) {
	return o.orgm.CreateAPIKeyWithValue(ctx, org, val)
}
