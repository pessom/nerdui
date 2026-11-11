// Package server implements the nerdui HTTP API and serves the web UI.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/version"
)

// Runtime is the subset of the container runtime used by the HTTP layer.
type Runtime interface {
	Versions(ctx context.Context) (runtime.Versions, error)
}

// Config configures the HTTP server.
type Config struct {
	Runtime Runtime
	// UI is the compiled frontend. When nil, the UI is not served.
	UI     fs.FS
	Logger *slog.Logger
}

// VersionResponse is returned by GET /api/v1/version. Keep in sync with api/openapi.yaml.
type VersionResponse struct {
	Nerdui  version.Info     `json:"nerdui"`
	Runtime runtime.Versions `json:"runtime"`
	Error   string           `json:"error,omitempty"`
}

// New returns the root HTTP handler.
func New(cfg Config) http.Handler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger(cfg.Logger))
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/version", versionHandler(cfg.Runtime))
	})

	if cfg.UI != nil {
		r.NotFound(spaHandler(cfg.UI))
	}
	return r
}

func versionHandler(rt Runtime) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := VersionResponse{Nerdui: version.Get()}
		if rt != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			v, err := rt.Versions(ctx)
			resp.Runtime = v
			if err != nil {
				resp.Error = err.Error()
			}
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// spaHandler serves static files and falls back to index.html for client-side routes.
func spaHandler(ui fs.FS) http.HandlerFunc {
	files := http.FileServerFS(ui)
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
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
