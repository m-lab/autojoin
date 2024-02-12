package iata

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/m-lab/go/content"
	"github.com/m-lab/go/mathx"
)

// Client manages the IATA data.
type Client struct {
	src  content.Provider
	mu   sync.Mutex
	rows []Row
}

// Row is a single row in the IATA dataset.
type Row struct {
	CountryCode string
	IATA        string
	Latitude    float64
	Longitude   float64
}

// New creates a new Client from IATA data contained at the given URL. Any
// URL supported m-lab/go/content may be provided.
func New(ctx context.Context, u *url.URL) (*Client, error) {
	p, err := content.FromURL(ctx, u)
	if err != nil {
		return nil, err
	}
	c := &Client{
		src: p,
	}
	return c, nil
}

// Load downloads and parses the iata data from the provider source.
func (c *Client) Load(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Download raw data.
	raw, err := c.src.Get(ctx)
	if err != nil {
		return err
	}
	// Parse as a CSV. NOTE: the parser preserves values between quotes and removes quotes.
	b := bytes.NewBuffer(raw)
	r := csv.NewReader(b)
	// Header and field positions.
	// "country_code","region_name","iata","icao","airport","latitude","longitude"
	// "US","New York","LGA","KLGA","LaGuardia Airport","40.775","-73.875"
	var rows []Row
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if len(record) < 7 {
			// We index up to the seventh element, so past this point, each row
			// must have at least seven fields.
			continue
		}
		lat, err := strconv.ParseFloat(record[5], 64)
		if err != nil {
			continue
		}
		lon, err := strconv.ParseFloat(record[6], 64)
		if err != nil {
			continue
		}
		row := Row{
			CountryCode: record[0],
			IATA:        strings.ToLower(record[2]),
			Latitude:    lat,
			Longitude:   lon,
		}
		rows = append(rows, row)
	}
	c.rows = rows
	return nil
}

type dist struct {
	iata     string
	distance float64
}

// ErrNoAirports is returned if Lookup can find no airports.
var ErrNoAirports = errors.New("no airports in country")

// Lookup searches for the IATA code closest to the given lat/lon within the given country.
func (c *Client) Lookup(country string, lat, lon float64) (string, error) {
	// Find all distances to airports in country.
	airports := []dist{}
	for i := range c.rows {
		r := c.rows[i]
		if r.CountryCode == country {
			distance := mathx.GetHaversineDistance(lat, lon, r.Latitude, r.Longitude)
			d := dist{
				iata:     r.IATA,
				distance: distance,
			}
			airports = append(airports, d)
		}
	}
	if len(airports) == 0 {
		return "", ErrNoAirports
	}
	// Sort distances closest to furthest.
	sort.Slice(airports, func(i, j int) bool {
		return airports[i].distance < airports[j].distance
	})
	// Return closest.
	return airports[0].iata, nil
}
