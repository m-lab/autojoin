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

// Status is the entity written to memorystore to track DNS hostnames.
// The key for the entity is the hostname.
type Status struct {
	// DNS represents a DNS record
	DNS *DNSRecord
}

// DNSRecord represents a DNS record with a last update time to verify if the
// hostname is still active or expired.
type DNSRecord struct {
	// LastUpdate is the last update time as a Unix timestamp.
	LastUpdate int64
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
// periodically reads all entities in Memorystore and checks if their
// registration has expired. If an entity has expired, it is deleted from both
// Cloud DNS and Memorystore.
type GarbageCollector struct {
	MemorystoreClient[Status]
	stop    chan bool
	project string
	ttl     time.Duration
	dns     dnsiface.Service
}

// NewGarbageCollector returns a new garbage-collected tracker for DNS entries
// and spawns a goroutine to periodically check and delete expired entities.
func NewGarbageCollector(dns dnsiface.Service, project string, msClient MemorystoreClient[Status],
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
				t.checkAndRemoveExpired()
			}
		}
	}(st)

	return st
}

// Update creates a new entry in memorystore for the given hostname or updates
// the existing one with a new LastUpdate time.
func (t *GarbageCollector) Update(hostname string) error {
	entry := &DNSRecord{
		LastUpdate: time.Now().UTC().Unix(),
	}
	return t.Put(hostname, "DNS", entry, &memorystore.PutOptions{})
}

func (gc *GarbageCollector) Delete(hostname string) error {
	log.Printf("Deleting %s from memorystore", hostname)
	err := gc.Del(hostname)
	if err != nil {
		log.Printf("Failed to delete %s from memorystore: %v", hostname, err)
		return err
	}
	return nil
}

func (t *GarbageCollector) checkAndRemoveExpired() {
	values, err := t.GetAll()

	if err != nil {
		// TODO(rd): count errors with a Prometheus metric.
		return
	}

	// Iterate over values and check if they are expired.
	for k, v := range values {
		lastUpdate := time.Unix(v.DNS.LastUpdate, 0)
		if time.Since(lastUpdate) > t.ttl {
			log.Printf("%s expired on %s, deleting from Cloud DNS and memorystore", k, lastUpdate.Add(t.ttl))

			// Parse hostname.
			name, err := host.Parse(k)
			if err != nil {
				log.Printf("Failed to parse hostname %s: %v", k, err)
				continue
				// TODO(rd): count errors with a Prometheus metric
			}

			m := dnsx.NewManager(t.dns, t.project, register.OrgZone(name.Org, t.project))
			_, err = m.Delete(context.Background(), name.StringAll()+".")
			if err != nil {
				log.Printf("Failed to delete DNS entry for %s: %v", name, err)
				// If the deletion fails, we do not want to remove the entry
				// from memorystore so the deletion can be retried next time.
				continue
				// TODO(rd): count errors with a Prometheus metric
			}

			// Remove expired hostname from memorystore.
			err = t.Delete(k)
			if err != nil {
				log.Printf("Failed to delete %s: %v", k, err)
				// TODO(rd): count errors with a Prometheus metric
			}
		}
	}

}

func (t *GarbageCollector) Stop() {
	t.stop <- true
	close(t.stop)
}
