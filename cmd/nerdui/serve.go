// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/pessom/nerdui/internal/auth"
	"github.com/pessom/nerdui/internal/config"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/server"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/tlsutil"
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
	scheme := "http"
	if cfg.TLS {
		tlsCfg, err := tlsConfig(cfg, logger)
		if err != nil {
			_ = ln.Close()
			return err
		}
		ln = tls.NewListener(ln, tlsCfg)
		scheme = "https"
	} else {
		logger.Warn("TLS is disabled: passwords and terminal sessions travel unencrypted; " +
			"use --tls=false only behind a TLS-terminating reverse proxy")
	}

	authSvc, err := auth.New(ctx, st, auth.Options{SecureCookie: cfg.TLS, Logger: logger})
	if err != nil {
		_ = ln.Close()
		return fmt.Errorf("init authentication: %w", err)
	}
	cleanupCtx, stopCleanup := context.WithCancel(ctx)
	defer stopCleanup()
	go authSvc.CleanupLoop(cleanupCtx)

	envs := environment.NewRegistry(environment.Local(runtime.New(cfg.ContainerdAddress, cfg.Namespace), cfg.Namespace))
	defer func() { _ = envs.Close() }()

	srv := &http.Server{
		Handler:           server.New(server.Config{Auth: authSvc, Environments: envs, UI: web.Dist(), Logger: logger}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	logger.Info("nerdui started",
		"url", scheme+"://"+ln.Addr().String(),
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

// tlsConfig prepares the certificate manager (ADR-0005) and logs what is served so users
// can compare the fingerprint with the one shown by their browser.
func tlsConfig(cfg config.Config, logger *slog.Logger) (*tls.Config, error) {
	logf := func(format string, args ...any) { logger.Info(fmt.Sprintf(format, args...)) }
	var (
		mgr *tlsutil.Manager
		err error
	)
	if cfg.TLSCert != "" {
		mgr, err = tlsutil.FromFiles(cfg.TLSCert, cfg.TLSKey, tlsutil.WithLogf(logf))
	} else {
		mgr, err = tlsutil.SelfSigned(cfg.TLSDir(), tlsutil.LocalHosts(), tlsutil.WithLogf(logf))
	}
	if err != nil {
		return nil, err
	}
	info := mgr.Info()
	ips := make([]string, 0, len(info.IPAddresses))
	for _, ip := range info.IPAddresses {
		ips = append(ips, ip.String())
	}
	logger.Info("TLS certificate",
		"self_signed", info.SelfSigned,
		"file", info.CertFile,
		"expires", info.NotAfter.Format(time.DateOnly),
		"dns_names", info.DNSNames,
		"ip_addresses", ips,
		"sha256", info.SHA256)
	return mgr.TLSConfig(), nil
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
