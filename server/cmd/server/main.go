// Server is the durable Go coordination service. Coding runs execute on Workers.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/phodal/pero/server/internal/github"
	"github.com/phodal/pero/server/internal/httpapi"
	"github.com/phodal/pero/server/internal/store"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	url, token := os.Getenv("PERO_DATABASE_URL"), os.Getenv("PERO_OPERATOR_TOKEN")
	if url == "" || token == "" {
		return fmt.Errorf("PERO_DATABASE_URL and PERO_OPERATOR_TOKEN are required")
	}
	seconds := 60
	if raw := os.Getenv("PERO_LEASE_SECONDS"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 3600 {
			return fmt.Errorf("PERO_LEASE_SECONDS must be between 1 and 3600")
		}
		seconds = value
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	db, err := store.Open(startup, url, time.Duration(seconds)*time.Second)
	cancel()
	if err != nil {
		return fmt.Errorf("open control-plane database: %w", err)
	}
	defer func() { stop(); db.Close() }()
	repositories, err := github.ParseRepositories(os.Getenv("PERO_GITHUB_REPOSITORIES"))
	if err != nil {
		return err
	}
	sources, err := github.New(db, repositories, os.Getenv("PERO_GITHUB_TOKEN"))
	if err != nil {
		return err
	}
	sources.Start(ctx)
	listen := os.Getenv("PERO_LISTEN")
	if listen == "" {
		listen = "127.0.0.1:8787"
	}
	server := &http.Server{Addr: listen, Handler: httpapi.Handler(db, token, sources), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				op, done := context.WithTimeout(ctx, 5*time.Second)
				err := db.Reap(op)
				done()
				if err != nil && ctx.Err() == nil {
					log.Printf("lease reconciliation: %v", err)
				}
			}
		}
	}()
	go func() {
		<-ctx.Done()
		shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("Pero control plane listening on %s", listen)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
