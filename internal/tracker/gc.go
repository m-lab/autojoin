package tracker

import (
	"context"
	"log"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/m-lab/autojoin/internal/dnsx"
	"github.com/m-lab/autojoin/internal/dnsx/dnsiface"
	"github.com/m-lab/autojoin/internal/register"
	"github.com/m-lab/go/host"
	"github.com/m-lab/locate/memorystore"
)

type StatusTracker struct {
	DNS *DNSRecord
}

type DNSRecord struct {
	Expiration int64
}

// MemorystoreClient is a client for reading and writing data in Memorystore.
// The interface takes in a type argument which specifies the types of values
// that are stored and can be retrieved.
type MemorystoreClient[V any] interface {
	Put(key string, field string, value redis.Scanner, opts *memorystore.PutOptions) error
	GetAll() (map[string]V, error)
	Del(key string) error
}

// GarbageCollector is a tracker that implements automatic garbage collection
// of stale entities - i.e. entities whose registration has not been updated
// for longer than the configured TTL.
//
// When the GarbageCollector is created, it spawns a goroutine that
// periodically lists all entities in Memorystore and checks if their
// registration has expired. If an entity has expired, it is deleted from the
// DNS server and also removed from Memorystore.
type GarbageCollector struct {
	MemorystoreClient[StatusTracker]
	stop    chan bool
	project string
	ttl     time.Duration
	dns     dnsiface.Service
}

// NewGarbageCollector returns a new garbage-collected tracker for DNS entries.
func NewGarbageCollector(dns dnsiface.Service, project string, msClient MemorystoreClient[StatusTracker],
	ttl, interval time.Duration) *GarbageCollector {
	st := &GarbageCollector{
		MemorystoreClient: msClient,
		stop:              make(chan bool),
		project:           project,
		ttl:               ttl,
		dns:               dns,
	}

	// Start a goroutine to periodically check and remove expired entities.
	go func(t *GarbageCollector) {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-t.stop:
				return
			case <-ticker.C:
				log.Printf("Checking for expired memorystore entities...")
				t.updateAndRemoveExpired()
			}
		}
	}(st)

	return st
}

// Update creates a new entry in memorystore for the given hostname or updates
// the existing one with a new Expiration time.
func (t *GarbageCollector) Update(hostname string) error {
	entry := &DNSRecord{
		Expiration: time.Now().Add(t.ttl).Unix(),
	}
	return t.Put(hostname, "DNS", entry, &memorystore.PutOptions{})
}

func (gc *GarbageCollector) delete(hostname string) error {
	// Parse hostname.
	name, err := host.Parse(hostname)
	if err != nil {
		log.Printf("Failed to parse hostname %s: %v", hostname, err)
		return err
	}

	m := dnsx.NewManager(gc.dns, gc.project, register.OrgZone(name.Org, gc.project))
	_, err = m.Delete(context.Background(), name.StringAll()+".")
	if err != nil {
		log.Printf("Failed to delete DNS entry for %s: %v", name, err)
		return err
	}

	log.Printf("Deleting %s from memorystore", hostname)
	err = gc.Del(hostname)
	if err != nil {
		log.Printf("Failed to delete %s from memorystore: %v", hostname, err)
	}
	return nil
}

func (t *GarbageCollector) updateAndRemoveExpired() {
	values, err := t.GetAll()

	if err != nil {
		// TODO(rd): count errors with a Prometheus metric.
		return
	}

	// Iterate over values and check if they are expired.
	for k, v := range values {
		exp := time.Unix(v.DNS.Expiration, 0)
		if time.Now().After(exp) {
			log.Printf("%s expired on %s, deleting from memorystore", k, exp)
			// Remove expired hostname from memorystore.
			err := t.delete(k)
			if err != nil {
				log.Printf("Failed to delete %s: %v", k, err)
				// TODO(rd): count errors with a Prometheus metric
			}
		}
	}

}
