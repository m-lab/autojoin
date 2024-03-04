package dnsiface

import (
	"context"

	"google.golang.org/api/dns/v1"
)

// Service interface used by the dnsx logic.
type Service interface {
	ResourceRecordSetsGet(ctx context.Context, project string, zone string, name string, type_ string) (*dns.ResourceRecordSet, error)
	ChangeCreate(ctx context.Context, project string, zone string, change *dns.Change) (*dns.Change, error)
}

// CloudDNSService implements the DNS Service interface.
type CloudDNSService struct {
	Service *dns.Service
}

// ResourceRecordSetsGet gets an existing resource record set, if present.
func (c *CloudDNSService) ResourceRecordSetsGet(ctx context.Context, project string, zone string, name string, rtype string) (*dns.ResourceRecordSet, error) {
	return c.Service.ResourceRecordSets.Get(project, zone, name, rtype).Context(ctx).Do()
}

// ChangeCreate applies the given change set.
func (c *CloudDNSService) ChangeCreate(ctx context.Context, project string, zone string, change *dns.Change) (*dns.Change, error) {
	return c.Service.Changes.Create(project, zone, change).Context(ctx).Do()
}
