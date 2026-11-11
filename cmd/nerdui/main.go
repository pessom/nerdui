// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Command nerdui is a web UI for nerdctl/containerd.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/server"
	"github.com/pessom/nerdui/internal/version"
	"github.com/pessom/nerdui/web"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "nerdui:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		listen      = flag.String("listen", envOr("NERDUI_LISTEN", ":8080"), "HTTP listen address")
		address     = flag.String("address", envOr("NERDUI_ADDRESS", runtime.DefaultAddress), "containerd socket address")
		namespace   = flag.String("namespace", envOr("NERDUI_NAMESPACE", runtime.DefaultNamespace), "default containerd namespace")
		logLevel    = flag.String("log-level", envOr("NERDUI_LOG_LEVEL", "info"), "log level: debug, info, warn, error")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Get())
		return nil
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		return fmt.Errorf("invalid --log-level: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	rt := runtime.New(*address, *namespace)
	defer func() { _ = rt.Close() }()

	srv := &http.Server{
		Addr:              *listen,
		Handler:           server.New(server.Config{Runtime: rt, UI: web.Dist(), Logger: logger}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("nerdui started",
			"listen", *listen, "containerd", *address, "namespace", *namespace,
			"version", version.Version)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
	}
	return nil
}

func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}
