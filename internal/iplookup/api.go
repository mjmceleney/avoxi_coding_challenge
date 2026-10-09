// Package iplookup checks whether an IP address belongs to one of a set of allowed
// countries, using a MaxMind GeoLite2/GeoIP2 country database.
//
// It can be used directly from Go via Service, or exposed over HTTP via Service.Handler
// and gRPC via Service.GRPCServer.
package iplookup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	db "github.com/oschwald/maxminddb-golang"
)

// DefaultReloadInterval is how often Watch checks whether geoipupdate has replaced the
// database file.
const DefaultReloadInterval = time.Minute

// ErrInvalidArgument wraps every input validation failure, so callers can map them all to
// a single client error (HTTP 400, gRPC INVALID_ARGUMENT).
var ErrInvalidArgument = errors.New("invalid argument")

// Reason explains why a Result is not allowed.
type Reason string

const (
	// ReasonCountryNotAllowed means the IP resolved to a country outside the allowed list.
	ReasonCountryNotAllowed Reason = "country_not_allowed"
	// ReasonCountryUnknown means the database has no country for the IP (e.g. private or
	// reserved ranges), so it can't be allowed.
	ReasonCountryUnknown Reason = "country_unknown"
)

// Result is the outcome of a country check.
type Result struct {
	// Country is the ISO 3166-1 alpha-2 code the IP resolved to. It is empty if the
	// database has no country for the IP.
	Country string `json:"country"`
	// Allowed reports whether Country is in the allowed list.
	Allowed bool `json:"allowed"`
	// Reason explains why the IP is not allowed. It is empty when Allowed is true.
	Reason Reason `json:"reason,omitempty"`
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
// An IP that isn't allowed is not an error: it returns a Result with Allowed set to false
// and a Reason. Errors are reserved for invalid input (wrapping ErrInvalidArgument) or
// lookup failures.
//
// Entries in allowedCountries that aren't valid country codes (such as "USA" or "UK") are
// logged as errors and ignored, rather than failing the whole check. A typo in a
// customer's list then only blocks logins from the country it was meant to allow, and the
// error log shows what needs fixing.
func (s *Service) Check(ip string, allowedCountries []string) (Result, error) {
	addr := net.ParseIP(strings.TrimSpace(ip))
	if addr == nil {
		return Result{}, fmt.Errorf("%w: invalid IP address %q", ErrInvalidArgument, ip)
	}
	if len(allowedCountries) == 0 {
		return Result{}, fmt.Errorf("%w: allowed_countries must not be empty", ErrInvalidArgument)
	}
	countries, invalid := validCountryCodes(allowedCountries)
	if len(invalid) > 0 {
		slog.Error("ignoring invalid country codes in allowed_countries", "invalid", invalid)
	}

	var record struct {
		Country struct {
			IsoCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}
	// Current is guaranteed to be a valid pointer, so we don't need any additional nil checks.
	if err := s.current.Load().reader.Lookup(addr, &record); err != nil {
		return Result{}, fmt.Errorf("looking up %s: %w", addr, err)
	}

	country := record.Country.IsoCode
	if country == "" {
		return Result{Reason: ReasonCountryUnknown}, nil
	}
	if !slices.Contains(countries, country) {
		return Result{Country: country, Reason: ReasonCountryNotAllowed}, nil
	}
	return Result{Country: country, Allowed: true}, nil
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
	slog.Info("loaded database", "path", s.dbPath, "built", s.DatabaseBuild())
	return nil
}

// DatabaseBuild returns when MaxMind built the currently loaded database, which shows how
// fresh the data is.
func (s *Service) DatabaseBuild() time.Time {
	return time.Unix(int64(s.current.Load().reader.Metadata.BuildEpoch), 0).UTC()
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
				slog.Warn("reloading database failed, still serving the previous copy", "path", s.dbPath, "error", err)
			}
		}
	}
}
