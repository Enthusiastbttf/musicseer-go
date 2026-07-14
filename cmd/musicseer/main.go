// MusicSeer 2 — music discovery and request management for Navidrome + Lidarr,
// rebuilt as a single Go binary with an embedded web UI and SQLite storage.
//
// Usage:
//
//	musicseer                      start the server (default)
//	musicseer migrate <postgres-dsn>   import users/instances/requests from
//	                                   an original MusicSeer PostgreSQL DB
//	musicseer version
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"musicseer/internal/config"
	"musicseer/internal/engine"
	"musicseer/internal/migrate"
	"musicseer/internal/secrets"
	"musicseer/internal/server"
	"musicseer/internal/store"
)

func main() {
	cfg := config.Load()

	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		log.Error("cannot create data dir", "dir", cfg.DataDir, "err", err)
		os.Exit(1)
	}
	box, err := secrets.Open(cfg.DataDir)
	if err != nil {
		log.Error("cannot open secret key", "err", err)
		os.Exit(1)
	}
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		log.Error("cannot open database", "err", err)
		os.Exit(1)
	}

	// Subcommands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			fmt.Println("musicseer", server.Version)
			return
		case "migrate":
			if len(os.Args) < 3 {
				fmt.Fprintln(os.Stderr, "usage: musicseer migrate 'postgres://user:pass@host:5432/musicseer?sslmode=disable'")
				os.Exit(2)
			}
			res, err := migrate.FromPostgres(context.Background(), os.Args[2], st, box)
			if err != nil {
				log.Error("migration failed", "err", err)
				os.Exit(1)
			}
			fmt.Printf("Imported: %d users (%d already present), %d instances (%d already present), %d requests (%d skipped)\n",
				res.Users, res.UsersSkipped, res.Instances, res.InstancesSkipped, res.Requests, res.RequestsSkipped)
			for _, w := range res.Warnings {
				fmt.Println("  note:", w)
			}
			fmt.Println("The old database was NOT modified. Start the server and log in with your existing credentials.")
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
			os.Exit(2)
		}
	}

	// Server mode
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	eng := engine.New(cfg, st, box, log)
	eng.Start(ctx)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           server.New(cfg, st, box, eng, log).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	if cfg.LastFMKey == "" {
		log.Info("no LASTFM_API_KEY set — using the keyless ListenBrainz/MusicBrainz discovery backend (set a key to switch to Last.fm)")
	}
	log.Info("musicseer started", "port", cfg.Port, "data", cfg.DataDir, "version", server.Version)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server error", "err", err)
		os.Exit(1)
	}
	log.Info("shutdown complete")
}
