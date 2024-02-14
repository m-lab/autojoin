package maxmind

import (
	"context"
	"net"
	"sync"

	"github.com/m-lab/go/content"
	"github.com/oschwald/geoip2-golang"

	"github.com/m-lab/uuid-annotator/tarreader"
)

// NewMaxmindManger creates a new MaxmindManager and loads Maxmind data from the
// given content.Provider.
func NewMaxmind(src content.Provider) *Maxmind {
	return &Maxmind{src: src}
}

// MaxmindManager manages access to the maxmind database.
type Maxmind struct {
	mu      sync.RWMutex
	src     content.Provider
	Maxmind *geoip2.Reader
}

// City searches for metadata associated with the given IP.
func (mm *Maxmind) City(ip net.IP) (*geoip2.City, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	return mm.Maxmind.City(ip)
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
