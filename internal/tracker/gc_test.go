package tracker

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/m-lab/locate/memorystore"
	"google.golang.org/api/dns/v1"
)

type fakeDNS struct {
	chgErr error
	getErr error
}

func (f *fakeDNS) ResourceRecordSetsGet(ctx context.Context, project string, zone string, name string, rtype string) (*dns.ResourceRecordSet, error) {
	return nil, f.getErr
}
func (f *fakeDNS) ChangeCreate(ctx context.Context, project string, zone string, change *dns.Change) (*dns.Change, error) {
	return nil, f.chgErr
}
func (f *fakeDNS) CreateManagedZone(ctx context.Context, project string, z *dns.ManagedZone) (*dns.ManagedZone, error) {
	return nil, nil
}
func (f *fakeDNS) GetManagedZone(ctx context.Context, project, zoneName string) (*dns.ManagedZone, error) {
	return nil, nil
}

type fakeMemorystoreClient[V any] struct {
	putErr error
	delErr error
	getErr error
	m      map[string]V
}

// Put returns nil.
func (c *fakeMemorystoreClient[V]) Put(key string, field string, value redis.Scanner, opts *memorystore.PutOptions) error {
	return c.putErr
}

// GetAll returns an empty map and a nil error.
func (c *fakeMemorystoreClient[V]) GetAll() (map[string]V, error) {
	return c.m, c.getErr
}

// Del returns nil
func (c *fakeMemorystoreClient[V]) Del(key string) error {
	delete(c.m, key)
	return c.delErr
}

// FakeAdd mimics adding a new value to Memorystore for testing.
func (c *fakeMemorystoreClient[V]) FakeAdd(key string, value V) {
	c.m[key] = value
}

func TestNewGarbageCollector(t *testing.T) {
	dns := &fakeDNS{}
	fakeMSClient := &fakeMemorystoreClient[Status]{}
	before := runtime.NumGoroutine()
	gc := NewGarbageCollector(dns, "test-project", fakeMSClient, 3*time.Hour, 200*time.Millisecond)

	if gc.dns != dns || gc.project != "test-project" || gc.ttl != 3*time.Hour ||
		gc.MemorystoreClient != fakeMSClient {
		t.Errorf("NewGarbageCollector() = %v, want %v", gc, reflect.Value{})
	}

	if runtime.NumGoroutine() != before+1 {
		t.Errorf("NewGarbageCollector() did not spawn a new goroutine.")
	}

	// Let the GC one or more times.
	time.Sleep(500 * time.Millisecond)

	gc.Stop()
	time.Sleep(500 * time.Millisecond)
	if runtime.NumGoroutine() != before {
		t.Errorf("NewGarbageCollector() did not stop the goroutine (got %d, exp: %d).", runtime.NumGoroutine(), before)
	}
}

func TestGarbageCollector_Update(t *testing.T) {
	dns := &fakeDNS{}
	fakeMSClient := &fakeMemorystoreClient[Status]{}
	gc := NewGarbageCollector(dns, "test-project", fakeMSClient, 3*time.Hour, 1*time.Hour)

	err := gc.Update("foo-lga12345-c0a80001.bar.sandbox.measurement-lab.org", nil)
	if err != nil {
		t.Errorf("Update() returned err, expected nil: %v", err)
	}

	err = gc.Delete("foo-lga12345-c0a80001.bar.sandbox.measurement-lab.org")
	if err != nil {
		t.Errorf("Delete() returned err, expected nil: %v", err)
	}
}

func TestGarbageCollector_List(t *testing.T) {
	dns := &fakeDNS{}
	fakeMSClient := &fakeMemorystoreClient[Status]{
		m: map[string]Status{
			"foo-lga12345-c0a80001.bar.sandbox.measurement-lab.org": {
				DNS: &DNSRecord{
					LastUpdate: 0,
				},
			},
			"foo-lga12345-c0a80002.bar.sandbox.measurement-lab.org": {
				DNS: &DNSRecord{
					// Make sure this is >= Now() so it's guaranteed not to be
					// expired.
					LastUpdate: time.Now().Add(1 * time.Minute).Unix(),
				},
			},
		},
	}

	gc := NewGarbageCollector(dns, "test-project", fakeMSClient, 3*time.Hour, 1*time.Hour)

	gc.List()
	// Check that the expired record was removed.
	if _, ok := fakeMSClient.m["foo-lga12345-c0a80001.bar.sandbox.measurement-lab.org"]; ok {
		t.Errorf("List() failed to remove expired record.")
	}
	// Check that the non-expired record was NOT removed.
	if _, ok := fakeMSClient.m["foo-lga12345-c0a80002.bar.sandbox.measurement-lab.org"]; !ok {
		t.Errorf("List() removed a non-expired record.")
	}

	// Add un-parseable hostname
	fakeMSClient.m["invalid"] = Status{
		DNS: &DNSRecord{
			LastUpdate: 0,
		},
	}
	gc.List()
	// Check that the un-parseable hostname was ignored.
	if _, ok := fakeMSClient.m["invalid"]; !ok {
		t.Errorf("List() failed to ignore an un-parseable hostname.")
	}

	// Inject error into GetAll
	fakeMSClient.getErr = errors.New("fake getall error")
	_, _, err := gc.List()
	if err != fakeMSClient.getErr {
		t.Errorf("List() failed for unexpected reason; got %v; want %v", err, fakeMSClient.getErr)
	}
}

func TestGarbageCollector_Delete(t *testing.T) {
	dns := &fakeDNS{}
	fakeMSClient := &fakeMemorystoreClient[Status]{
		m: map[string]Status{
			"foo-lga12345-c0a80001.bar.sandbox.measurement-lab.org": {
				DNS: &DNSRecord{
					LastUpdate: 0,
				},
			},
		},
	}
	gc := NewGarbageCollector(dns, "test-project", fakeMSClient, 3*time.Hour, 1*time.Hour)
	err := gc.Delete("foo-lga12345-c0a80001.bar.sandbox.measurement-lab.org")
	if err != nil {
		t.Errorf("Delete() returned err, expected nil: %v", err)
	}
	// Check that the requested hostname was deleted.
	if _, ok := fakeMSClient.m["foo-lga12345-c0a80001.bar.sandbox.measurement-lab.org"]; ok {
		t.Errorf("Delete() failed to remove expired record.")
	}

	fakeMSClient.delErr = errors.New("fake error")
	err = gc.Delete("foo-lga12345-c0a80001.bar.sandbox.measurement-lab.org")
	// Check that the error was propagated.
	if err == nil {
		t.Errorf("Delete() did not propagate errors.")
	}
}
