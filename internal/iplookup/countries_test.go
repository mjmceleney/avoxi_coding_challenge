package iplookup

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"slices"
	"testing"
)

func TestValidCountryCodes(t *testing.T) {
	valid, invalid := validCountryCodes([]string{"gb", " US ", "XK", "UK", "USA", "", "United States"})

	if want := []string{"GB", "US", "XK"}; !slices.Equal(valid, want) {
		t.Errorf("valid = %q, want %q", valid, want)
	}
	if want := []string{"UK", "USA", "", "United States"}; !slices.Equal(invalid, want) {
		t.Errorf("invalid = %q, want %q", invalid, want)
	}
	// 249 ISO 3166-1 codes plus XK.
	if len(countryCodes) != 250 {
		t.Errorf("len(countryCodes) = %d, want 250", len(countryCodes))
	}
}

// captureLogs sends slog's default logger to a buffer for the rest of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestCheckIgnoresInvalidCountryCodes(t *testing.T) {
	s := newTestService(t)
	logs := captureLogs(t)

	// "UK" is a common mistake for GB. It is ignored, so the valid GB entry still matches.
	res, err := s.Check("81.2.69.160", []string{"UK", "gb"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if want := (Result{Country: "GB", Allowed: true}); res != want {
		t.Errorf("got %+v, want %+v", res, want)
	}

	var entry struct {
		Level   string   `json:"level"`
		Msg     string   `json:"msg"`
		Invalid []string `json:"invalid"`
	}
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("decoding log %q: %v", logs, err)
	}
	if entry.Level != "ERROR" || !slices.Equal(entry.Invalid, []string{"UK"}) {
		t.Errorf("got log %+v, want an ERROR naming the invalid code UK", entry)
	}

	// With no valid entries left, nothing can match.
	res, err = s.Check("81.2.69.160", []string{"UK"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if want := (Result{Country: "GB", Reason: ReasonCountryNotAllowed}); res != want {
		t.Errorf("got %+v, want %+v", res, want)
	}
}

func TestCheckValidCountriesNotLogged(t *testing.T) {
	s := newTestService(t)
	logs := captureLogs(t)

	if _, err := s.Check("81.2.69.160", []string{"GB", "us"}); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if logs.Len() != 0 {
		t.Errorf("unexpected log output: %s", logs)
	}
}
