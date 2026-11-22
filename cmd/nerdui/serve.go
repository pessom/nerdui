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
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/pessom/nerdui/internal/api"
	"github.com/pessom/nerdui/internal/audit"
	"github.com/pessom/nerdui/internal/auth"
	"github.com/pessom/nerdui/internal/autoheal"
	"github.com/pessom/nerdui/internal/backup"
	"github.com/pessom/nerdui/internal/config"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/events"
	"github.com/pessom/nerdui/internal/gitstacks"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/notify"
	"github.com/pessom/nerdui/internal/proxy"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/server"
	"github.com/pessom/nerdui/internal/stacks"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/tlsutil"
	"github.com/pessom/nerdui/internal/updates"
	"github.com/pessom/nerdui/internal/version"
	"github.com/pessom/nerdui/internal/webhooks"
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
	var acmeCerts *tlsutil.ACME
	if cfg.TLS {
		tlsCfg, a, err := tlsConfig(cfg, logger)
		if err != nil {
			_ = ln.Close()
			return err
		}
		ln = tls.NewListener(ln, tlsCfg)
		scheme = "https"
		acmeCerts = a
	}
	basePath, err := config.NormalizeBasePath(cfg.BasePath) // validated with the config
	if err != nil {
		return err
	}
	trusted, err := proxy.ParsePrefixes(cfg.TrustedProxies) // validated with the config
	if err != nil {
		_ = ln.Close()
		return err
	}
	switch {
	case !cfg.TLS && len(trusted) == 0:
		logger.Warn("TLS is disabled and no --trusted-proxies are set: passwords and terminal sessions " +
			"travel unencrypted unless a reverse proxy terminates TLS; set --trusted-proxies to its address " +
			"so session cookies are marked Secure and client addresses are logged")
	case len(trusted) > 0:
		logger.Info("trusting X-Forwarded-* headers", "proxies", cfg.TrustedProxies)
	}

	// The audit log (#79) records requests through the authorization guard and logins.
	auditLog := &audit.Recorder{Store: st.Write(), Retention: cfg.AuditRetention, Logger: logger}
	authSvc, err := auth.New(ctx, st, auth.Options{SecureCookie: cfg.TLS, CookiePath: basePath, Logger: logger, Audit: auditLog})
	if err != nil {
		_ = ln.Close()
		return fmt.Errorf("init authentication: %w", err)
	}
	cleanupCtx, stopCleanup := context.WithCancel(ctx)
	defer stopCleanup()
	go authSvc.CleanupLoop(cleanupCtx)

	ncfg, ncfgPath, err := runtime.LoadNerdctlConfig()
	if err != nil {
		logger.Warn("cannot read nerdctl config, using nerdctl defaults", "path", ncfgPath, "error", err)
	}
	logger.Info("nerdctl config", "path", ncfgPath, "data_root", ncfg.DataRoot)
	rt := runtime.New(cfg.ContainerdAddress, cfg.Namespace,
		runtime.WithNerdctl(cfg.NerdctlPath, cfg.SkipNerdctlVersionCheck),
		runtime.WithNerdctlConfig(ncfg))
	envs := environment.NewRegistry(environment.Local(rt, cfg.Namespace))
	defer func() { _ = envs.Close() }()
	// Background work (compatibility check, event hubs) does not delay startup; it is
	// canceled and awaited when serving ends, so nothing outlives runServe.
	var checks sync.WaitGroup
	checkCtx, cancelChecks := context.WithCancel(ctx)
	defer checks.Wait()
	defer cancelChecks()
	checks.Go(func() { logRuntimeCompatibility(checkCtx, logger, rt) })
	for _, env := range envs.List() {
		checks.Go(func() { env.Events.Run(checkCtx) })
		if cfg.EventsRetention > 0 {
			rec := &events.Recorder{
				Environment: env.ID, Hub: env.Events, Store: st, Names: rt.ContainerName,
				Retention: cfg.EventsRetention, Logger: logger,
			}
			checks.Go(func() { rec.Run(checkCtx) })
		}
	}

	// Notifications (#86) are sent in the background.
	hostname, _ := os.Hostname()
	notifier := &notify.Service{Store: st.Write(), Host: hostname, Logger: logger}
	checks.Go(func() { notifier.Run(checkCtx) })
	checks.Go(func() { auditLog.Run(checkCtx) })
	for _, env := range envs.List() {
		w := &notify.Watcher{Env: env, Notifier: notifier, Logger: logger}
		checks.Go(func() { w.Run(checkCtx) })
	}
	// Unhealthy containers are reported, and restarted when labeled (#22).
	healer := &autoheal.Healer{Environments: envs, Notify: notifier.Notify, Logger: logger}
	checks.Go(func() { healer.Run(checkCtx) })

	// Background jobs (#126) are canceled and awaited when serving ends.
	jobManager := jobs.NewManager()
	defer jobManager.Close()
	jobManager.OnFinish = func(j jobs.Job) {
		auditLog.JobFinished(j)
		// Automatic updates report their own failures.
		if j.Status == jobs.StatusFailed && j.User != updates.AutoUser {
			notifier.Notify(notify.Event{
				Kind: notify.KindJobFailed, Severity: notify.SeverityError, Environment: j.Environment,
				Namespace: j.Namespace, Title: j.Title + " failed", Message: j.Error, Object: j.ID,
			})
		}
	}
	// Git stacks (#37) are cloned into <data-dir>/git.
	gitDir := filepath.Join(cfg.DataDir, "git")
	stackService := &stacks.Service{
		Dir: cfg.EffectiveStacksDir(), Registry: st.Read(), Writer: st.Write(), ScanDirs: cfg.StacksScanDirs,
		Git: st.Read(), GitDir: gitDir,
	}
	gitStacks := &gitstacks.Service{Dir: gitDir, Store: st.Write(), Stacks: stackService}
	gitAuto := &gitstacks.Auto{
		Service: gitStacks, Environments: envs, Jobs: jobManager, Notify: notifier.Notify, Logger: logger, Base: checkCtx,
	}
	checks.Go(func() { gitAuto.Run(checkCtx) })
	incoming := &webhooks.Service{Store: st.Write(), Environments: envs, Stacks: stackService, Jobs: jobManager}
	// Image update checks (#53) on each environment's schedule, each followed by the automatic
	// updates of opted-in containers and stacks (#54).
	autoUpdates := &updates.Updater{
		Store: st.Write(), Jobs: jobManager, Stacks: stackService, Base: checkCtx, Notify: notifier.Notify,
	}
	imageUpdates := &updates.Checker{
		Environments: envs, Store: st.Write(), Logger: logger, AfterCheck: autoUpdates.Run, Notify: notifier.Notify,
	}
	checks.Go(func() { imageUpdates.Run(checkCtx) })

	// Hijacked WebSocket connections are neither tracked nor closed by Shutdown; their
	// request contexts derive from streamsCtx, which is canceled after Shutdown so streams
	// end with "going away" (ADR-0007).
	streamsCtx, cancelStreams := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelStreams()
	var eventHistory api.EventHistory
	if cfg.EventsRetention > 0 {
		eventHistory = st.Read()
	}
	srv := &http.Server{
		Handler: server.New(server.Config{
			BasePath: basePath,
			Backup:   &backup.Source{Store: st, DataDir: cfg.DataDir, StacksDir: cfg.EffectiveStacksDir()},
			Auth:     authSvc, Environments: envs, UI: web.Dist(), Jobs: jobManager, TrustedProxies: trusted,
			ACME:         acmeCerts,
			EventHistory: eventHistory, DisableExec: cfg.DisableExec, Settings: st.Write(),
			Stacks:        stackService,
			GitStacks:     gitStacks,
			GitAuto:       gitAuto,
			Webhooks:      incoming,
			ImageUpdates:  imageUpdates,
			AutoUpdates:   autoUpdates,
			Notifications: st.Write(), Notifier: notifier, Audit: auditLog,
			Logger: logger,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return streamsCtx },
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	if acmeCerts != nil {
		checks.Go(func() { acmeCerts.Warm(checkCtx, acmeRetry) })
		if cfg.TLSACMEHTTP != "" {
			httpSrv, err := acmeHTTPServer(ctx, cfg, acmeCerts)
			if err != nil {
				_ = srv.Close()
				return err
			}
			defer func() { _ = httpSrv.Close() }()
		}
	}

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
		err := srv.Shutdown(shutdownCtx)
		cancelStreams()
		if err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}

// logRuntimeCompatibility reports the nerdctl and containerd checks at startup
// (ADR-0011, ADR-0012). It never fails startup: problems put nerdui into read-only mode or
// show warnings in the UI.
func logRuntimeCompatibility(ctx context.Context, logger *slog.Logger, rt *runtime.Client) {
	// nerdctl is checked first: it does not depend on containerd being reachable.
	st := rt.Nerdctl(ctx)
	switch {
	case st.Compatible:
		logger.Info("nerdctl compatible", "path", st.Path, "version", st.Version, "required", st.Required+".x")
	case st.CheckSkipped:
		logger.Warn("nerdctl version check skipped (--skip-nerdctl-version-check): unsupported configuration",
			"path", st.Path, "version", st.Version, "required", st.Required+".x", "error", st.Error)
	default:
		logger.Warn(api.ReadOnlyReason(st), "path", st.Path)
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	v, err := rt.Versions(ctx)
	switch {
	case err != nil:
		if ctx.Err() == nil || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			logger.Warn("containerd is not reachable yet", "error", err)
		}
	case !v.ContainerdSupported:
		logger.Warn("unsupported containerd version: nerdui works but is not tested with it",
			"version", v.Containerd, "supported", runtime.SupportedContainerdMinors(), "nerdctl", runtime.NerdctlLibraryVersion())
	default:
		logger.Info("containerd connected", "version", v.Containerd)
	}
}

// tlsConfig prepares the certificate manager (ADR-0005) and logs what is served so users
// can compare the fingerprint with the one shown by their browser.
// acmeRetry is the pause between attempts to obtain ACME certificates at start.
const acmeRetry = 10 * time.Minute

// acmeHTTPServer answers HTTP-01 challenges and redirects to HTTPS (#113).
func acmeHTTPServer(ctx context.Context, cfg config.Config, a *tlsutil.ACME) (*http.Server, error) {
	ln, err := listen(ctx, cfg.TLSACMEHTTP)
	if err != nil {
		return nil, fmt.Errorf("--tls-acme-http: %w", err)
	}
	_, port, _ := net.SplitHostPort(cfg.Listen)
	srv := &http.Server{Handler: a.HTTPHandler(port), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return srv, nil
}

func tlsConfig(cfg config.Config, logger *slog.Logger) (*tls.Config, *tlsutil.ACME, error) {
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
		return nil, nil, err
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
	if len(cfg.TLSACMEDomains) == 0 {
		return mgr.TLSConfig(), nil, nil
	}
	dir, _ := tlsutil.ACMEDirectory(cfg.TLSACMECA) // validated with the config
	a := tlsutil.NewACME(mgr, tlsutil.ACMEOptions{
		Domains: cfg.TLSACMEDomains, Email: cfg.TLSACMEEmail, DirectoryURL: dir,
		CacheDir: filepath.Join(cfg.TLSDir(), "acme"), Logf: logf,
	})
	logger.Info("ACME certificates", "domains", cfg.TLSACMEDomains, "ca", dir, "http01", cfg.TLSACMEHTTP)
	return a.TLSConfig(), a, nil
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
