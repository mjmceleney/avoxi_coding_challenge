package iplookup

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// Request logs record only the method, outcome and duration. Request bodies are not
// logged, so end users' IP addresses (personal data) stay out of the logs.

// statusRecorder captures the status code written by an HTTP handler.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// logRequests logs each HTTP request after it completes. Health checks are logged at debug
// level, since Kubernetes probes them every few seconds.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		level := slog.LevelInfo
		if r.URL.Path == "/health" {
			level = slog.LevelDebug
		}
		slog.Log(r.Context(), level, "http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", milliseconds(time.Since(start)),
		)
	})
}

// logUnary logs each unary gRPC call after it completes. Health checks are logged at debug
// level, matching logRequests.
func logUnary(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	start := time.Now()
	resp, err := handler(ctx, req)

	level := slog.LevelInfo
	if strings.HasPrefix(info.FullMethod, "/grpc.health.v1.") {
		level = slog.LevelDebug
	}
	slog.Log(ctx, level, "grpc request",
		"method", info.FullMethod,
		"code", status.Code(err).String(),
		"duration_ms", milliseconds(time.Since(start)),
	)
	return resp, err
}

// milliseconds converts d to fractional milliseconds, which read more naturally in logs
// than slog's default of integer nanoseconds.
func milliseconds(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}
