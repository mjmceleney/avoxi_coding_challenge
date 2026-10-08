package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"iplookup/internal/iplookup"
)

// shutdownTimeout is how long in-flight requests get to finish after a shutdown signal
// before connections are closed forcibly.
const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// run starts the HTTP and gRPC servers and blocks until a shutdown signal is received or
// either server fails, then shuts both down gracefully.
func run() error {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "data/GeoLite2-Country.mmdb"
	}
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	grpcAddr := os.Getenv("GRPC_ADDR")
	if grpcAddr == "" {
		grpcAddr = ":9090"
	}

	svc, err := iplookup.New(dbPath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go svc.Watch(ctx, iplookup.DefaultReloadInterval)

	// Listen up front so a bad address fails startup instead of surfacing later.
	httpLis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listening for HTTP on %s: %w", addr, err)
	}
	grpcLis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		httpLis.Close()
		return fmt.Errorf("listening for gRPC on %s: %w", grpcAddr, err)
	}

	srv := &http.Server{
		Handler:           svc.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		// ReadTimeout also covers the body, so a client can't hold a connection by
		// trickling it in slowly.
		ReadTimeout: 10 * time.Second,
		// Set explicitly because it otherwise falls back to ReadTimeout, which would drop
		// the gateway's pooled keep-alive connections after only 10s idle.
		IdleTimeout: 2 * time.Minute,
	}
	grpcSrv := svc.GRPCServer()

	// Buffered so a server that fails after shutdown has begun doesn't block forever.
	serveErr := make(chan error, 2)
	go func() {
		log.Printf("HTTP listening on %s (database: %s)", addr, dbPath)
		if err := srv.Serve(httpLis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- fmt.Errorf("HTTP server: %w", err)
		}
	}()
	go func() {
		log.Printf("gRPC listening on %s", grpcAddr)
		if err := grpcSrv.Serve(grpcLis); err != nil {
			serveErr <- fmt.Errorf("gRPC server: %w", err)
		}
	}()

	// Wait for a signal or for either server to fail; both cases shut everything down.
	var runErr error
	select {
	case <-ctx.Done():
		log.Println("Shutdown signal received, draining connections")
	case runErr = <-serveErr:
		log.Println("Shutting down after server error:", runErr)
	}
	stop() // restore default signal handling, so a second Ctrl+C kills immediately

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	// Drain both servers concurrently so they share the timeout rather than each
	// getting it in turn. GracefulStop has no deadline of its own, so fall back to Stop.
	grpcStopped := make(chan struct{})
	go func() {
		grpcSrv.GracefulStop()
		close(grpcStopped)
	}()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Println("HTTP shutdown incomplete, closing remaining connections:", err)
		srv.Close()
	}
	select {
	case <-grpcStopped:
	case <-shutdownCtx.Done():
		log.Println("gRPC shutdown incomplete, closing remaining connections")
		grpcSrv.Stop()
	}

	log.Println("Shutdown complete")
	return runErr
}
