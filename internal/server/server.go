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
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/pessom/nerdui/internal/auth"
	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/version"
)

// Runtime is the subset of the container runtime used by the HTTP layer.
type Runtime interface {
	Versions(ctx context.Context) (runtime.Versions, error)
}

// Config configures the HTTP server.
type Config struct {
	// Auth provides sessions and the authentication endpoints (required).
	Auth *auth.Service
	// Authorizer decides per-action access; defaults to authz.AdminOnly (ADR-0009).
	Authorizer authz.Authorizer
	Runtime    Runtime
	// UI is the compiled frontend. When nil, the UI is not served.
	UI     fs.FS
	Logger *slog.Logger
}

// VersionResponse is returned by GET /api/v1/version. Keep in sync with api/openapi.yaml.
// Build information (including the AGPL source link) is public; runtime details are only
// returned to callers allowed to perform system:read.
type VersionResponse struct {
	Nerdui  version.Info      `json:"nerdui"`
	Runtime *runtime.Versions `json:"runtime,omitempty"`
	Error   string            `json:"error,omitempty"`
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

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger(cfg.Logger))
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpjson.Write(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	guard := authz.NewGuard(cfg.Authorizer, auth.SubjectFromRequest, cfg.Logger)
	r.Route("/api/v1", func(r chi.Router) {
		cfg.Auth.Routes(r, guard)
		r.With(cfg.Auth.OptionalSession).Method(http.MethodGet, "/version", authz.Public(versionHandler(cfg.Runtime, guard)))
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

func versionHandler(rt Runtime, guard *authz.Guard) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := VersionResponse{Nerdui: version.Get()}
		if rt != nil && guard.Allowed(r, authz.SystemRead) {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			v, err := rt.Versions(ctx)
			resp.Runtime = &v
			if err != nil {
				resp.Error = err.Error()
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

// securityHeaders sets conservative browser security headers on every response.
// A Content-Security-Policy is added together with the frontend shell (#128).
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
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
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()),
				slog.Duration("duration", time.Since(start)),
				slog.String("request_id", middleware.GetReqID(r.Context())),
			)
		})
	}
}
