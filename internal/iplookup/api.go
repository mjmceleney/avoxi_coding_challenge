// Package iplookup checks whether an IP address belongs to one of a set of allowed
// countries, using a MaxMind GeoLite2/GeoIP2 country database.
//
// It can be used directly from Go via Service, or exposed over HTTP via Service.Handler.
package iplookup

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	db "github.com/oschwald/maxminddb-golang"
)

// DefaultReloadInterval is how often Watch checks whether geoipupdate has replaced the
// database file.
const DefaultReloadInterval = time.Minute

var (
	// ErrInvalidIP is returned when the given string is not a valid IPv4 or IPv6 address.
	ErrInvalidIP = errors.New("invalid IP address")
	// ErrNotLoaded is returned when no database has been loaded yet.
	ErrNotLoaded = errors.New("database not loaded")
)

// Result is the outcome of a country check.
type Result struct {
	// Country is the ISO 3166-1 alpha-2 code the IP resolved to. It is empty if the
	// database has no country for the IP (e.g. private or reserved ranges).
	Country string `json:"country"`
	// Allowed reports whether Country is in the allowed list.
	Allowed bool `json:"allowed"`
}

// loadedDB is an in-memory copy of the database, along with the modification time of the
// file it was read from.
type loadedDB struct {
	reader  *db.Reader
	modTime time.Time
}

// Service performs IP country lookups against a database file on disk. It is safe for
// concurrent use.
type Service struct {
	dbPath string

	// current is swapped atomically on reload, so lookups never block and never see a
	// partially loaded database.
	current atomic.Pointer[loadedDB]
}

// New loads the database at dbPath and returns a Service ready for lookups. Call Watch to
// pick up updates to the file.
func New(dbPath string) (*Service, error) {
	s := &Service{dbPath: dbPath}
	if err := s.Reload(); err != nil {
		return nil, fmt.Errorf("loading database %q: %w", dbPath, err)
	}
	return s, nil
}

// Check looks up the country for ip and reports whether it is in allowedCountries
// (ISO 3166-1 alpha-2 codes, compared case-insensitively).
//
// An IP from a disallowed country is not an error: it returns a Result with Allowed set
// to false. Errors are reserved for invalid input or lookup failures.
func (s *Service) Check(ip string, allowedCountries []string) (Result, error) {
	country, err := s.Country(ip)
	if err != nil {
		return Result{}, err
	}

	res := Result{Country: country}
	if country == "" {
		return res, nil
	}
	for _, c := range allowedCountries {
		if strings.EqualFold(strings.TrimSpace(c), country) {
			res.Allowed = true
			break
		}
	}
	return res, nil
}

// Country returns the ISO 3166-1 alpha-2 country code for ip, or "" if the database has
// no country for it.
func (s *Service) Country(ip string) (string, error) {
	// Ensure valid IP address has been passed
	ipAddr := net.ParseIP(strings.TrimSpace(ip))
	if ipAddr == nil {
		return "", fmt.Errorf("%w: %q", ErrInvalidIP, ip)
	}

	loaded := s.current.Load()
	if loaded == nil {
		return "", ErrNotLoaded
	}

	var record struct {
		Country struct {
			IsoCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}
	if err := loaded.reader.Lookup(ipAddr, &record); err != nil {
		return "", fmt.Errorf("looking up %s: %w", ipAddr, err)
	}
	return record.Country.IsoCode, nil
}

// Reload reads the database file into memory and makes it the current database, unless
// the file hasn't changed since it was last loaded.
//
// The file is read fully into memory rather than memory-mapped, so it is only open for
// the duration of the read. geoipupdate can then replace it at any time, even on a
// network volume, without affecting lookups.
func (s *Service) Reload() error {
	info, err := os.Stat(s.dbPath)
	if err != nil {
		return err
	}
	if current := s.current.Load(); current != nil && current.modTime.Equal(info.ModTime()) {
		return nil
	}

	data, err := os.ReadFile(s.dbPath)
	if err != nil {
		return err
	}
	reader, err := db.FromBytes(data)
	if err != nil {
		return err
	}

	// The previous reader is not closed: in-flight lookups may still be using it, and
	// since it isn't memory-mapped the garbage collector frees it once they finish.
	s.current.Store(&loadedDB{reader: reader, modTime: info.ModTime()})
	return nil
}

// Watch reloads the database whenever its modification time changes, until ctx is
// cancelled. The file itself is kept up to date externally by geoipupdate. It blocks, so
// run it in its own goroutine.
func (s *Service) Watch(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Reload(); err != nil {
				// Keep serving the previously loaded database
				log.Println("Error reloading database:", err)
			}
		}
	}
}
