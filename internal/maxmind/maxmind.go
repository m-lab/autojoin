package maxmind

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/m-lab/go/content"
	"github.com/oschwald/geoip2-golang"

	"github.com/m-lab/uuid-annotator/tarreader"
)

var (
	// ErrNotFound is returned when City lookups return no results.
	ErrNotFound = errors.New("no results found during lookup")
)

// Maxmind manages access to the maxmind database.
type Maxmind struct {
	mu      sync.RWMutex
	src     content.Provider
	Maxmind *geoip2.Reader
}

// NewMaxmind creates a new Maxmind instance which loads data from the given
// content.Provider. Callers must call Reload() at least once on the returned
// Maxmind instance before calling City().
func NewMaxmind(src content.Provider) *Maxmind {
	return &Maxmind{src: src}
}

// City searches for metadata associated with the given IP.
func (mm *Maxmind) City(ip net.IP) (*geoip2.City, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	record, err := mm.Maxmind.City(ip)
	if err != nil {
		return nil, err
	}
	if isEmpty(record) {
		return nil, ErrNotFound
	}
	return record, nil
}

func isEmpty(r *geoip2.City) bool {
	// The record has no associated city, country, or continent.
	return r.City.GeoNameID == 0 && r.Country.GeoNameID == 0 && r.Continent.GeoNameID == 0
}

// Reload is intended to be called regularly to update the local dataset with
// newer information from the provider.
func (mm *Maxmind) Reload(ctx context.Context) error {
	tgz, err := mm.src.Get(ctx)
	if err == content.ErrNoChange {
		return nil
	}
	if err != nil {
		return err
	}
	data, err := tarreader.FromTarGZ(tgz, "GeoLite2-City.mmdb")
	if err != nil {
		return err
	}
	// Parse the raw data.
	mmtmp, err := geoip2.FromBytes(data)
	if err != nil {
		return err
	}
	// Don't acquire the lock until after the data is in RAM.
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.Maxmind = mmtmp
	return nil
}
