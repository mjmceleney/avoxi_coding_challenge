package iplookup

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testDBPath = "../../data/GeoLite2-Country.mmdb"

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

	// 8.8.8.8 is Google DNS, located in the US
	res, err := s.Check("8.8.8.8", []string{"us", "GB"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Country != "US" || !res.Allowed {
		t.Errorf("got %+v, want US allowed", res)
	}

	res, err = s.Check("8.8.8.8", []string{"JP"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Allowed {
		t.Errorf("got %+v, want not allowed", res)
	}

	if _, err := s.Check("not-an-ip", []string{"US"}); !errors.Is(err, ErrInvalidIP) {
		t.Errorf("got err %v, want ErrInvalidIP", err)
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
		{"allowed", `{"ip":"8.8.8.8","allowed_countries":["US"]}`, http.StatusOK, &Result{Country: "US", Allowed: true}},
		{"not allowed", `{"ip":"8.8.8.8","allowed_countries":["JP"]}`, http.StatusOK, &Result{Country: "US", Allowed: false}},
		{"invalid ip", `{"ip":"nope","allowed_countries":["US"]}`, http.StatusBadRequest, nil},
		{"no countries", `{"ip":"8.8.8.8"}`, http.StatusBadRequest, nil},
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
