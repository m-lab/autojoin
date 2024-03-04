package iface

import (
	"context"

	"google.golang.org/api/dns/v1"
)

// DNS interface used by the dnsx logic.
type DNS interface {
	ResourceRecordSetsGet(ctx context.Context, project string, zone string, name string, type_ string) (*dns.ResourceRecordSet, error)
	ChangeCreate(ctx context.Context, project string, zone string, change *dns.Change) (*dns.Change, error)
}

// DNSImpl implements the DNS interface.
type DNSImpl struct {
	Service *dns.Service
}

// ResourceRecordSetsGet gets an existing resource record set, if present.
func (d *DNSImpl) ResourceRecordSetsGet(ctx context.Context, project string, zone string, name string, rtype string) (*dns.ResourceRecordSet, error) {
	return d.Service.ResourceRecordSets.Get(project, zone, name, rtype).Context(ctx).Do()
}

// ChangeCreate applies the given change set.
func (d *DNSImpl) ChangeCreate(ctx context.Context, project string, zone string, change *dns.Change) (*dns.Change, error) {
	return d.Service.Changes.Create(project, zone, change).Context(ctx).Do()
}
