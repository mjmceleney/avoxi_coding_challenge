package iplookup

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testDBPath is MaxMind's test database (from github.com/maxmind/MaxMind-DB), which has
// fixed records, so the tests don't depend on live GeoLite data.
const testDBPath = "testdata/GeoIP2-Country-Test.mmdb"

func newTestService(t *testing.T) *Service {
	t.Helper()
	s, err := New(testDBPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestCheck(t *testing.T) {
	s := newTestService(t)

	// IPV4
	// 81.2.69.160 is in GB in the test database
	res, err := s.Check("81.2.69.160", []string{"gb", "FR"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Country != "GB" || !res.Allowed {
		t.Errorf("got %+v, want GB allowed", res)
	}

	// Its registered country is US, which must not be used in place of where it is located
	res, err = s.Check("81.2.69.160", []string{"US"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if want := (Result{Country: "GB", Reason: ReasonCountryNotAllowed}); res != want {
		t.Errorf("got %+v, want %+v", res, want)
	}

	// IPV6
	// 2001:218::/32 is in JP in the test database
	res, err = s.Check("2001:218::1", []string{"jp", "GB"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Country != "JP" || !res.Allowed {
		t.Errorf("got %+v, want JP allowed", res)
	}

	res, err = s.Check("2001:218::1", []string{"US"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if want := (Result{Country: "JP", Reason: ReasonCountryNotAllowed}); res != want {
		t.Errorf("got %+v, want %+v", res, want)
	}

	// Private ranges have no country in the database
	res, err = s.Check("10.0.0.1", []string{"US"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if want := (Result{Reason: ReasonCountryUnknown}); res != want {
		t.Errorf("got %+v, want %+v", res, want)
	}

	if _, err := s.Check("not-an-ip", []string{"US"}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("invalid IP: got err %v, want ErrInvalidArgument", err)
	}
	if _, err := s.Check("2001:218::1", nil); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("no countries: got err %v, want ErrInvalidArgument", err)
	}
}

func TestHandler(t *testing.T) {
	h := newTestService(t).Handler()

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantResult *Result
	}{
		{"allowed", `{"ip":"81.2.69.160","allowed_countries":["GB"]}`, http.StatusOK, &Result{Country: "GB", Allowed: true}},
		{"not allowed", `{"ip":"81.2.69.160","allowed_countries":["JP"]}`, http.StatusOK, &Result{Country: "GB", Reason: ReasonCountryNotAllowed}},
		{"unknown country", `{"ip":"10.0.0.1","allowed_countries":["US"]}`, http.StatusOK, &Result{Reason: ReasonCountryUnknown}},
		{"invalid ip", `{"ip":"nope","allowed_countries":["US"]}`, http.StatusBadRequest, nil},
		{"no countries", `{"ip":"81.2.69.160"}`, http.StatusBadRequest, nil},
		{"bad json", `{`, http.StatusBadRequest, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantResult != nil {
				var got Result
				if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
					t.Fatalf("decoding response: %v", err)
				}
				if got != *tt.wantResult {
					t.Errorf("got %+v, want %+v", got, *tt.wantResult)
				}
			}
		})
	}
}

func TestHealth(t *testing.T) {
	s := newTestService(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got.Status != "ok" || !got.DatabaseBuild.Equal(s.DatabaseBuild()) || got.DatabaseBuild.IsZero() {
		t.Errorf("got %+v, want status ok and database_build %v", got, s.DatabaseBuild())
	}
}
