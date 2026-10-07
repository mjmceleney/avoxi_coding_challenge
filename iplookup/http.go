package iplookup

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
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
//	GET  /healthz    -> 200 once a database is loaded
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
	if len(req.AllowedCountries) == 0 {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "allowed_countries must not be empty"})
		return
	}

	res, err := s.Check(req.IP, req.AllowedCountries)
	switch {
	case errors.Is(err, ErrInvalidIP):
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
	case errors.Is(err, ErrNotLoaded):
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: err.Error()})
	case err != nil:
		log.Println("Error checking IP:", err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "lookup failed"})
	default:
		writeJSON(w, http.StatusOK, res)
	}
}

func (s *Service) handleHealth(w http.ResponseWriter, r *http.Request) {
	if s.current.Load() == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: ErrNotLoaded.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Println("Error writing response:", err)
	}
}
