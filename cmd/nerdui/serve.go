// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/pessom/nerdui/internal/config"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/server"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/version"
	"github.com/pessom/nerdui/web"
)

const shutdownTimeout = 15 * time.Second

// runServe validates the configuration, prepares the data directory and serves HTTP
// until ctx is canceled.
func runServe(ctx context.Context, cfg config.Config, logOut io.Writer) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration:\n%w", err)
	}

	logger := slog.New(slog.NewJSONHandler(logOut, &slog.HandlerOptions{Level: cfg.SlogLevel()}))
	slog.SetDefault(logger)

	insecure, err := cfg.EnsureDataDir()
	if err != nil {
		return err
	}
	if insecure {
		logger.Warn("data directory is accessible by group or others; restrict it with chmod 700",
			"data_dir", cfg.DataDir)
	}

	st, err := store.Open(ctx, cfg.DataDir, store.WithLogf(func(format string, args ...any) {
		logger.Info(fmt.Sprintf(format, args...))
	}))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = st.Close() }()
	schema, err := st.SchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	ln, err := listen(ctx, cfg.Listen)
	if err != nil {
		return err
	}

	rt := runtime.New(cfg.ContainerdAddress, cfg.Namespace)
	defer func() { _ = rt.Close() }()

	srv := &http.Server{
		Handler:           server.New(server.Config{Runtime: rt, UI: web.Dist(), Logger: logger}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	logger.Info("nerdui started",
		"listen", ln.Addr().String(),
		"containerd", cfg.ContainerdAddress,
		"namespace", cfg.Namespace,
		"data_dir", cfg.DataDir,
		"schema_version", schema,
		"version", version.Version)

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}

// listen opens the TCP listener and turns common failures into actionable messages.
func listen(ctx context.Context, addr string) (net.Listener, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	switch {
	case err == nil:
		return ln, nil
	case errors.Is(err, syscall.EADDRINUSE):
		return nil, fmt.Errorf("address %s is already in use; choose another one with --listen (or NERDUI_LISTEN)", addr)
	case errors.Is(err, syscall.EACCES):
		return nil, fmt.Errorf("permission denied to listen on %s; use a port above 1024 or run as root", addr)
	default:
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}
}
