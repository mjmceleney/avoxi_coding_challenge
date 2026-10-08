package iplookup

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"
)

// CheckRequest is the JSON body accepted by the check endpoint.
type CheckRequest struct {
	IP               string   `json:"ip"`
	AllowedCountries []string `json:"allowed_countries"`
}

// errorResponse is the JSON body returned for failed requests.
type errorResponse struct {
	Error string `json:"error"`
}

// maxRequestBytes caps the size of a request body.
const maxRequestBytes = 64 << 10

// Handler returns an http.Handler exposing the service:
//
//	POST /v1/check   {"ip": "1.2.3.4", "allowed_countries": ["US", "GB"]}
//	              -> {"country": "US", "allowed": true}
//	GET  /health    -> {"status": "ok", "database_build": "2026-10-06T21:21:33Z"}
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/check", s.handleCheck)
	mux.HandleFunc("GET /health", s.handleHealth)
	return mux
}

func (s *Service) handleCheck(w http.ResponseWriter, r *http.Request) {
	var req CheckRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid JSON body: " + err.Error()})
		return
	}

	res, err := s.Check(req.IP, req.AllowedCountries)
	switch {
	case errors.Is(err, ErrInvalidArgument):
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
	case err != nil:
		log.Println("Error checking IP:", err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "lookup failed"})
	default:
		writeJSON(w, http.StatusOK, res)
	}
}

// healthResponse is the JSON body returned by the health endpoint.
type healthResponse struct {
	Status string `json:"status"`
	// DatabaseBuild is when MaxMind built the loaded database, so monitoring can alert on
	// stale data.
	DatabaseBuild time.Time `json:"database_build"`
}

// handleHealth always reports ok: New guarantees a database is loaded, and a failed reload
// keeps serving the previous one. A stale database is reported but doesn't fail the
// check, since every replica would go stale at once and take the whole service down.
func (s *Service) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok", DatabaseBuild: s.DatabaseBuild()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Println("Error writing response:", err)
	}
}
