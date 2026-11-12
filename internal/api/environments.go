// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package api implements the REST handlers for environment-scoped resources (ADR-0006):
// /api/v1/environments and /api/v1/environments/{env}/...
package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/containerd/containerd/v2/pkg/identifiers"
	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
)

// statusTimeout bounds runtime calls made to report an environment's status.
const statusTimeout = 3 * time.Second

// Handler serves environment endpoints.
type Handler struct {
	Envs   *environment.Registry
	Jobs   *jobs.Manager
	Logger *slog.Logger
}

// Routes registers the environment endpoints under the API root.
func (h *Handler) Routes(r chi.Router, g *authz.Guard) {
	r.Method(http.MethodGet, "/environments", g.Protect(authz.SystemRead, h.list))
	h.jobRoutes(r, g)
	r.Route("/environments/{env}", func(r chi.Router) {
		r.Use(h.resolveEnv, h.requireWritable)
		r.Method(http.MethodGet, "/", g.Protect(authz.SystemRead, h.get))
		r.Method(http.MethodGet, "/info", g.Protect(authz.SystemRead, h.info))
		r.Method(http.MethodGet, "/namespaces", g.Protect(authz.SystemRead, h.namespaces))
		r.Method(http.MethodGet, "/events/stream", g.Protect(authz.SystemRead, h.eventsStream))
		r.Method(http.MethodGet, "/containers", g.Protect(authz.ContainersRead, h.listContainers))
		r.Method(http.MethodGet, "/containers/{id}/inspect", g.Protect(authz.ContainersRead, h.inspectContainer))
		r.Method(http.MethodGet, "/containers/{id}/logs", g.Protect(authz.ContainersRead, h.containerLogsDownload))
		r.Method(http.MethodGet, "/containers/{id}/logs/stream", g.Protect(authz.ContainersRead, h.containerLogsStream))
		h.containerActionRoutes(r, g)
		r.Method(http.MethodGet, "/images", g.Protect(authz.ImagesRead, h.listImages))
		r.Method(http.MethodPost, "/images/pull", g.Protect(authz.ImagesWrite, h.pullImage))
	})
}

// Environment is the API representation. Keep in sync with api/openapi.yaml.
type Environment struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	DefaultNamespace string `json:"defaultNamespace"`
}

// EnvironmentStatus adds live connectivity information.
type EnvironmentStatus struct {
	Environment
	Status  string            `json:"status"` // connected | unavailable
	Error   string            `json:"error,omitempty"`
	Runtime *runtime.Versions `json:"runtime,omitempty"`
}

// Environment statuses.
const (
	StatusConnected   = "connected"
	StatusUnavailable = "unavailable"
)

func toAPI(e *environment.Environment) Environment {
	return Environment{ID: e.ID, Name: e.Name, Kind: e.Kind, DefaultNamespace: e.DefaultNamespace}
}

func (h *Handler) list(w http.ResponseWriter, _ *http.Request) {
	envs := h.Envs.List()
	out := make([]Environment, 0, len(envs))
	for _, e := range envs {
		out = append(out, toAPI(e))
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"environments": out})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	e := EnvFrom(r.Context())
	ctx, cancel := context.WithTimeout(r.Context(), statusTimeout)
	defer cancel()
	resp := EnvironmentStatus{Environment: toAPI(e), Status: StatusConnected}
	v, err := e.Runtime.System().Versions(ctx)
	if err != nil {
		resp.Status = StatusUnavailable
		resp.Error = err.Error()
	}
	// Partial versions (nerdctl status, library) are useful even when containerd is down.
	resp.Runtime = &v
	httpjson.Write(w, http.StatusOK, resp)
}

// info returns host and runtime information (#66): like `nerdctl info` and `nerdctl
// version`, plus containerd plugins and CNI.
func (h *Handler) info(w http.ResponseWriter, r *http.Request) {
	info, err := EnvFrom(r.Context()).Runtime.System().Info(r.Context())
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, info)
}

func (h *Handler) namespaces(w http.ResponseWriter, r *http.Request) {
	e := EnvFrom(r.Context())
	ns, err := e.Runtime.System().Namespaces(r.Context())
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"namespaces": ns, "default": e.DefaultNamespace})
}

type envKey struct{}

// EnvFrom returns the environment resolved from the {env} path parameter.
func EnvFrom(ctx context.Context) *environment.Environment {
	e, _ := ctx.Value(envKey{}).(*environment.Environment)
	return e
}

// resolveEnv looks up {env} and answers 404 for unknown environments.
func (h *Handler) resolveEnv(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "env")
		e, ok := h.Envs.Get(id)
		if !ok {
			httpjson.WriteError(w, http.StatusNotFound, httpjson.CodeNotFound, fmt.Sprintf("environment %q not found", id))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), envKey{}, e)))
	})
}

// requireWritable rejects mutating requests while the environment is read-only because the
// host nerdctl is missing or incompatible (ADR-0011).
func (h *Handler) requireWritable(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		st := EnvFrom(r.Context()).Runtime.System().Nerdctl(r.Context())
		if !st.Writable() {
			httpjson.WriteError(w, http.StatusServiceUnavailable, httpjson.CodeNerdctlIncompatible, ReadOnlyReason(st))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ReadOnlyReason explains why an environment is read-only.
func ReadOnlyReason(st runtime.NerdctlStatus) string {
	if st.Version == "" {
		return fmt.Sprintf("read-only mode: host nerdctl %s.x is required (%s)", st.Required, st.Error)
	}
	return fmt.Sprintf("read-only mode: host nerdctl %s is incompatible, %s.x is required", st.Version, st.Required)
}

// Namespace returns the containerd namespace for the request: the "namespace" query
// parameter, or the environment's default. Invalid names are rejected (ADR-0006).
func Namespace(r *http.Request) (string, error) {
	ns := r.URL.Query().Get("namespace")
	if ns == "" {
		if e := EnvFrom(r.Context()); e != nil {
			return e.DefaultNamespace, nil
		}
		return "", fmt.Errorf("%w: namespace is required", errdefs.ErrInvalidArgument)
	}
	if err := identifiers.Validate(ns); err != nil {
		return "", fmt.Errorf("namespace %q: %w", ns, err) // identifiers errors wrap errdefs.ErrInvalidArgument
	}
	return ns, nil
}
