// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package server implements the nerdui HTTP API and serves the web UI.
package server

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	apispec "github.com/pessom/nerdui/api"
	"github.com/pessom/nerdui/internal/api"
	"github.com/pessom/nerdui/internal/audit"
	"github.com/pessom/nerdui/internal/auth"
	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/gitstacks"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/notify"
	"github.com/pessom/nerdui/internal/proxy"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/stacks"
	"github.com/pessom/nerdui/internal/tlsutil"
	"github.com/pessom/nerdui/internal/version"
	"github.com/pessom/nerdui/internal/webhooks"
)

// Config configures the HTTP server.
type Config struct {
	// Auth provides sessions and the authentication endpoints (required).
	Auth *auth.Service
	// Authorizer decides per-action access; defaults to authz.AdminOnly (ADR-0009).
	Authorizer authz.Authorizer
	// Environments are the managed containerd hosts (required).
	Environments *environment.Registry
	// UI is the compiled frontend. When nil, the UI is not served.
	UI fs.FS
	// Jobs runs long operations (#126); when nil, a manager is created (tests).
	Jobs *jobs.Manager
	// EventHistory serves recorded events (#65); nil when recording is disabled.
	EventHistory api.EventHistory
	// DisableExec turns off the web terminal (#15).
	DisableExec bool
	// Settings stores per-environment settings (#176); nil disables them.
	Settings api.Settings
	// Stacks finds compose stacks (#24); nil disables them.
	Stacks *stacks.Service
	// GitStacks deploys stacks from Git repositories (#37); nil disables them.
	GitStacks *gitstacks.Service
	// GitAuto handles webhooks of Git stacks (#38); nil answers 501.
	GitAuto *gitstacks.Auto
	// Webhooks are the incoming webhooks (#85, #91); nil answers 501.
	Webhooks *webhooks.Service
	// ImageUpdates checks images for newer registry digests (#53); nil disables it.
	ImageUpdates api.ImageUpdates
	// AutoUpdates is the log of automatic updates (#54); nil disables it.
	AutoUpdates api.AutoUpdates
	// TrustedProxies are reverse proxies whose X-Forwarded-* headers are honored (#112).
	TrustedProxies []netip.Prefix
	// ACME serves certificates from an ACME CA (#113); nil when not configured.
	ACME *tlsutil.ACME
	// Notifications stores notification channels and Notifier sends through them (#86); nil
	// disables them.
	Notifications api.NotificationStore
	Notifier      *notify.Service
	// Audit records and serves the audit log (#79); nil disables it.
	Audit  *audit.Recorder
	Logger *slog.Logger
}

// VersionResponse is returned by GET /api/v1/version. Keep in sync with api/openapi.yaml.
// Build information (including the AGPL source link) is public; runtime details are only
// returned to callers allowed to perform system:read.
type VersionResponse struct {
	Nerdui  version.Info      `json:"nerdui"`
	Runtime *runtime.Versions `json:"runtime,omitempty"`
	Error   string            `json:"error,omitempty"`
	// ACME describes ACME certificates when configured (#113).
	ACME *tlsutil.ACMEStatus `json:"acme,omitempty"`
}

// APIPrefix is the path prefix of API routes subject to the default-deny check.
const APIPrefix = "/api/"

// New returns the root HTTP handler. It panics if an API route does not declare its
// authorization (programming error caught by tests).
func New(cfg Config) http.Handler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Authorizer == nil {
		cfg.Authorizer = authz.AdminOnly{}
	}
	if cfg.Jobs == nil {
		cfg.Jobs = jobs.NewManager()
	}

	r := chi.NewRouter()
	// Before logging and authentication, so both see the client behind a trusted proxy (#112).
	r.Use(proxy.Middleware(cfg.TrustedProxies))
	r.Use(middleware.RequestID)
	r.Use(requestLogger(cfg.Logger))
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpjson.Write(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	guard := authz.NewGuard(cfg.Authorizer, auth.SubjectFromRequest, cfg.Logger)
	var auditLog api.AuditLog
	if cfg.Audit != nil {
		guard.Auditor = cfg.Audit
		auditLog = cfg.Audit
	}
	// /api/docs leads to the documentation page of the UI (#87).
	r.Method(http.MethodGet, "/api/docs", authz.Public(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/api-docs", http.StatusFound)
	}))
	r.Route("/api/v1", func(r chi.Router) {
		cfg.Auth.Routes(r, guard)
		r.With(cfg.Auth.OptionalSession).Method(http.MethodGet, "/version", authz.Public(versionHandler(cfg.Environments, guard, cfg.ACME)))
		// The API specification is public, like the source code (#87).
		r.Method(http.MethodGet, "/openapi.yaml", authz.Public(specHandler("application/yaml", func() ([]byte, error) { return apispec.OpenAPIYAML, nil })))
		r.Method(http.MethodGet, "/openapi.json", authz.Public(specHandler("application/json", apispec.OpenAPIJSON)))
		// Git hosts call webhooks without a session: the secret token in the path is the
		// credential (#38, ADR-0040).
		r.Method(http.MethodPost, "/hooks/git/{token}", authz.Public(api.GitWebhook(cfg.GitAuto, cfg.Logger)))
		r.Method(http.MethodPost, "/hooks/{token}", authz.Public(api.IncomingWebhook(cfg.Webhooks, cfg.Logger)))
		r.Group(func(r chi.Router) {
			r.Use(cfg.Auth.RequireSession, auth.CSRF, auth.PasswordChanged, cfg.Auth.MFAEnrolled)
			(&api.Handler{
				Envs: cfg.Environments, Jobs: cfg.Jobs, EventHistory: cfg.EventHistory,
				ExecDisabled: cfg.DisableExec, Settings: cfg.Settings, Stacks: cfg.Stacks, GitStacks: cfg.GitStacks, Webhooks: cfg.Webhooks,
				ImageUpdates: cfg.ImageUpdates, AutoUpdates: cfg.AutoUpdates, Logger: cfg.Logger,
				Notifications: cfg.Notifications, Notifier: cfg.Notifier, Audit: auditLog,
			}).Routes(r, guard)
		})
	})

	// Default deny (ADR-0009): every API route must declare an action or be explicitly public.
	if _, err := authz.Inspect(r, APIPrefix); err != nil {
		panic(err)
	}

	if cfg.UI != nil {
		r.NotFound(spaHandler(cfg.UI))
	}
	return r
}

// specHandler serves the embedded API specification.
func specHandler(contentType string, spec func() ([]byte, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		b, err := spec()
		if err != nil {
			httpjson.WriteError(w, http.StatusInternalServerError, httpjson.CodeInternal, "specification unavailable")
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(b)
	}
}

func versionHandler(envs *environment.Registry, guard *authz.Guard, acmeCerts *tlsutil.ACME) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := VersionResponse{Nerdui: version.Get()}
		local, ok := envs.Get(environment.LocalID)
		if ok && guard.Allowed(r, authz.SystemRead) {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			v, err := local.Runtime.System().Versions(ctx)
			resp.Runtime = &v
			if err != nil {
				resp.Error = err.Error()
			}
			if acmeCerts != nil {
				st := acmeCerts.Status()
				resp.ACME = &st
			}
		}
		httpjson.Write(w, http.StatusOK, resp)
	}
}

// spaHandler serves static files and falls back to index.html for client-side routes.
func spaHandler(ui fs.FS) http.HandlerFunc {
	files := http.FileServerFS(ui)
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			httpjson.WriteError(w, http.StatusNotFound, httpjson.CodeNotFound, "not found")
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(ui, name); errors.Is(err, fs.ErrNotExist) {
			if _, err := fs.Stat(ui, "index.html"); err != nil {
				http.Error(w, "web UI is not built: run `make web` and rebuild nerdui", http.StatusNotFound)
				return
			}
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	}
}

// ContentSecurityPolicy restricts the UI to its own origin. Inline styles are allowed because
// Mantine sets style attributes; scripts are only loaded from the embedded bundle.
const ContentSecurityPolicy = "default-src 'self'; script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; " +
	"connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'"

// securityHeaders sets browser security headers on every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", ContentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			log.LogAttrs(r.Context(), slog.LevelDebug, "http request",
				slog.String("method", r.Method),
				slog.String("path", redactPath(r.URL.Path)),
				slog.String("remote", r.RemoteAddr),
				slog.Int("status", ww.Status()),
				slog.Duration("duration", time.Since(start)),
				slog.String("request_id", middleware.GetReqID(r.Context())),
			)
		})
	}
}

// redactPath hides the secret tokens of webhook paths in logs.
func redactPath(p string) string {
	for _, hooks := range []string{"/api/v1/hooks/git/", "/api/v1/hooks/"} {
		if strings.HasPrefix(p, hooks) && len(p) > len(hooks) {
			return hooks + "<token>"
		}
	}
	return p
}
