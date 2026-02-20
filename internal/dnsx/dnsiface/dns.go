package dnsiface

import (
	"context"

	"google.golang.org/api/dns/v1"
)

// Service interface used by the dnsx logic.
type Service interface {
	ResourceRecordSetsGet(ctx context.Context, project string, zone string, name string, type_ string) (*dns.ResourceRecordSet, error)
	ChangeCreate(ctx context.Context, project string, zone string, change *dns.Change) (*dns.Change, error)
	GetManagedZone(ctx context.Context, project, zoneName string) (*dns.ManagedZone, error)
	CreateManagedZone(ctx context.Context, project string, z *dns.ManagedZone) (*dns.ManagedZone, error)
	DeleteManagedZone(ctx context.Context, project, zoneName string) error
}

// CloudDNSService implements the DNS Service interface.
type CloudDNSService struct {
	Service *dns.Service
}

// NewCloudDNSService creates a new instance of the CloudDNSService.
func NewCloudDNSService(s *dns.Service) *CloudDNSService {
	return &CloudDNSService{Service: s}
}

// ResourceRecordSetsGet gets an existing resource record set, if present.
func (c *CloudDNSService) ResourceRecordSetsGet(ctx context.Context, project string, zone string, name string, rtype string) (*dns.ResourceRecordSet, error) {
	return c.Service.ResourceRecordSets.Get(project, zone, name, rtype).Context(ctx).Do()
}

// ChangeCreate applies the given change set.
func (c *CloudDNSService) ChangeCreate(ctx context.Context, project string, zone string, change *dns.Change) (*dns.Change, error) {
	return c.Service.Changes.Create(project, zone, change).Context(ctx).Do()
}

// GetManagedZone gets the named zone.
func (c *CloudDNSService) GetManagedZone(ctx context.Context, project, zoneName string) (*dns.ManagedZone, error) {
	return c.Service.ManagedZones.Get(project, zoneName).Context(ctx).Do()
}

// CreateManagedZone creates the given zone.
func (c *CloudDNSService) CreateManagedZone(ctx context.Context, project string, zone *dns.ManagedZone) (*dns.ManagedZone, error) {
	return c.Service.ManagedZones.Create(project, zone).Context(ctx).Do()
}

// DeleteManagedZone deletes the named managed zone.
func (c *CloudDNSService) DeleteManagedZone(ctx context.Context, project, zoneName string) error {
	return c.Service.ManagedZones.Delete(project, zoneName).Context(ctx).Do()
}
