package tracker

import (
	"context"
	"log"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/m-lab/autojoin/internal/dnsname"
	"github.com/m-lab/autojoin/internal/dnsx"
	"github.com/m-lab/autojoin/internal/dnsx/dnsiface"
	"github.com/m-lab/autojoin/internal/metrics"
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
	// Ports contains a list of service ports to monitor
	Ports []string
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
func (gc *GarbageCollector) Update(hostname string, ports []string) error {
	entry := &DNSRecord{
		LastUpdate: time.Now().UTC().Unix(),
		Ports:      ports,
	}
	return gc.Put(hostname, "DNS", entry, &memorystore.PutOptions{})
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

func (gc *GarbageCollector) List() ([]string, [][]string, error) {
	return gc.checkAndRemoveExpired()
}

func (gc *GarbageCollector) checkAndRemoveExpired() ([]string, [][]string, error) {
	nodes := []string{}
	ports := [][]string{}
	values, err := gc.GetAll()

	if err != nil {
		metrics.GarbageCollectorOperations.WithLabelValues("memorystore_getall", "error").Inc()
		return nil, nil, err
	}
	metrics.GarbageCollectorOperations.WithLabelValues("memorystore_getall", "success").Inc()

	// Iterate over values and check if they are expired.
	for k, v := range values {
		lastUpdate := time.Unix(v.DNS.LastUpdate, 0)
		metrics.DNSExpiration.WithLabelValues(k).Set(float64(lastUpdate.Add(gc.ttl).Unix()))
		if time.Since(lastUpdate) > gc.ttl {
			log.Printf("%s expired on %s, deleting from Cloud DNS and memorystore", k, lastUpdate.Add(gc.ttl))

			// Parse hostname.
			name, err := host.Parse(k)
			if err != nil {
				log.Printf("Failed to parse hostname %s: %v", k, err)
				metrics.GarbageCollectorOperations.WithLabelValues("hostname_parse", "error").Inc()
				continue
			}
			metrics.GarbageCollectorOperations.WithLabelValues("hostname_parse", "success").Inc()

			m := dnsx.NewManager(gc.dns, gc.project, dnsname.OrgZone(name.Org, gc.project))
			_, err = m.Delete(context.Background(), name.StringAll()+".")
			if err != nil {
				log.Printf("Failed to delete DNS entry for %s: %v", name, err)
				metrics.GarbageCollectorOperations.WithLabelValues("dns_delete", "error").Inc()
				// If the deletion fails, we do not want to remove the entry
				// from memorystore so the deletion can be retried next time.
				continue
			}
			metrics.GarbageCollectorOperations.WithLabelValues("dns_delete", "success").Inc()

			// Remove expired hostname from memorystore.
			err = gc.Delete(k)
			if err != nil {
				log.Printf("Failed to delete %s: %v", k, err)
				metrics.GarbageCollectorOperations.WithLabelValues("memorystore_delete", "error").Inc()
			} else {
				metrics.GarbageCollectorOperations.WithLabelValues("memorystore_delete", "success").Inc()
			}
		} else {
			nodes = append(nodes, k)
			ports = append(ports, v.DNS.Ports)
		}
	}
	return nodes, ports, nil
}

func (gc *GarbageCollector) Stop() {
	gc.stop <- true
	close(gc.stop)
}
