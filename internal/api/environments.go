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
	"github.com/pessom/nerdui/internal/backup"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/gitstacks"
	"github.com/pessom/nerdui/internal/hostmetrics"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/notify"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/scheduler"
	"github.com/pessom/nerdui/internal/stacks"
	"github.com/pessom/nerdui/internal/templates"
	"github.com/pessom/nerdui/internal/variables"
	"github.com/pessom/nerdui/internal/volbackup"
	"github.com/pessom/nerdui/internal/webhooks"
)

// statusTimeout bounds runtime calls made to report an environment's status.
const statusTimeout = 3 * time.Second

// Handler serves environment endpoints.
type Handler struct {
	Envs *environment.Registry
	Jobs *jobs.Manager
	// EventHistory serves the event feed (#65); nil when recording is disabled.
	EventHistory EventHistory
	// ExecDisabled turns off the web terminal (--disable-exec, #15).
	ExecDisabled bool
	// Settings stores per-environment settings such as the Prometheus connection (#176);
	// nil disables them.
	Settings Settings
	// Stacks finds compose stacks (#24, ADR-0008); nil disables them.
	Stacks *stacks.Service
	// GitStacks deploys stacks from Git repositories (#37, ADR-0039); nil disables them.
	GitStacks *gitstacks.Service
	// Webhooks are the incoming webhooks (#85, #91); nil disables them.
	Webhooks *webhooks.Service
	// ImageUpdates checks images for newer registry digests (#53); nil disables it.
	ImageUpdates ImageUpdates
	// AutoUpdates is the log of automatic updates (#54); nil disables it.
	AutoUpdates AutoUpdates
	// Notifications stores notification channels and Notifier sends through them (#86); nil
	// disables them.
	Notifications NotificationStore
	Notifier      *notify.Service
	// Audit serves the audit log (#79); nil disables it.
	Audit AuditLog
	// PrometheusHTTP is the HTTP client for Prometheus; nil uses a default with a timeout.
	PrometheusHTTP *http.Client
	// Backup is what an application backup contains (#89); nil disables backups.
	Backup *backup.Source
	// Schedules are the scheduled jobs (#82); nil disables them.
	Schedules *scheduler.Service
	// Variables are the global and per-stack variables (#35); nil disables them.
	Variables *variables.Service
	// VolumeBackups back up and restore volumes (#59); nil disables them.
	VolumeBackups *volbackup.Service
	// Templates are the stack templates (#33, #34); nil disables them.
	Templates *templates.Service
	// HostMetrics samples the host nerdui runs on (#64); nil disables host metrics.
	HostMetrics *hostmetrics.Sampler
	Logger      *slog.Logger
}

// Routes registers the environment endpoints under the API root.
func (h *Handler) Routes(r chi.Router, g *authz.Guard) {
	r.Method(http.MethodGet, "/environments", g.Protect(authz.SystemRead, h.list))
	h.jobRoutes(r, g)
	h.notificationRoutes(r, g)
	h.auditRoutes(r, g)
	h.gitRoutes(r, g)
	h.webhookRoutes(r, g)
	h.backupRoutes(r, g)
	h.scheduleRoutes(r, g)
	h.variableRoutes(r, g)
	h.backupTargetRoutes(r, g)
	h.templateRoutes(r, g)
	h.hostMetricRoutes(r, g)
	r.Method(http.MethodGet, "/compose-schema", g.Protect(authz.StacksRead, h.composeSchema))
	r.Route("/environments/{env}", func(r chi.Router) {
		r.Use(h.resolveEnv)
		// Settings live in nerdui's database and do not need a writable nerdctl.
		// Host metrics come from /proc, not containerd (#64).
		r.Method(http.MethodGet, "/host/metrics", g.Protect(authz.SystemRead, h.hostMetrics))
		r.Method(http.MethodGet, "/settings/prometheus", g.Protect(authz.SettingsRead, h.getPrometheusSettings))
		r.Method(http.MethodPut, "/settings/prometheus", g.Protect(authz.SettingsWrite, h.putPrometheusSettings))
		r.Method(http.MethodDelete, "/settings/prometheus", g.Protect(authz.SettingsWrite, h.deletePrometheusSettings))
		r.Method(http.MethodPost, "/settings/prometheus/test", g.Protect(authz.SettingsWrite, h.testPrometheusSettings))
		r.Method(http.MethodGet, "/settings/auto-updates", g.Protect(authz.SettingsRead, h.getAutoUpdateSettings))
		r.Method(http.MethodPut, "/settings/auto-updates", g.Protect(authz.SettingsWrite, h.putAutoUpdateSettings))
		// Update checks only ask registries: they work in read-only mode too (#53).
		r.Method(http.MethodGet, "/image-updates", g.Protect(authz.ImagesRead, h.listImageUpdates))
		r.Method(http.MethodPost, "/image-updates/check", g.Protect(authz.ImagesRead, h.checkImageUpdates))
		r.Method(http.MethodGet, "/auto-updates", g.Protect(authz.ContainersRead, h.listAutoUpdates))
		r.Method(http.MethodPost, "/auto-updates/approve", g.Protect(authz.SettingsWrite, h.approveAutoUpdates))
		// Adopting and forgetting stacks change files and the database, not containerd.
		r.Method(http.MethodGet, "/stack-candidates", g.Protect(authz.StacksRead, h.stackCandidates))
		r.Method(http.MethodPost, "/stacks/adopt", g.Protect(authz.StacksWrite, h.adoptStack))
		r.Method(http.MethodDelete, "/stacks/{name}/registration", g.Protect(authz.StacksWrite, h.forgetStack))
		h.gitAutoRoutes(r, g)
		// Editing stack files (#26) uses compose-go only.
		r.Method(http.MethodPost, "/stacks", g.Protect(authz.StacksWrite, h.createStack))
		r.Method(http.MethodGet, "/stacks/{name}/files/{file}", g.Protect(authz.StacksRead, h.getStackFile))
		r.Method(http.MethodPut, "/stacks/{name}/files/{file}", g.Protect(authz.StacksWrite, h.putStackFile))
		r.Method(http.MethodPost, "/stacks/{name}/validate", g.Protect(authz.StacksWrite, h.validateStack))
		h.stackVariableRoutes(r, g)
		r.Group(func(r chi.Router) {
			r.Use(h.requireWritable)
			h.runtimeRoutes(r, g)
		})
	})
}

// runtimeRoutes are the endpoints backed by the environment's containerd and nerdctl.
func (h *Handler) runtimeRoutes(r chi.Router, g *authz.Guard) {
	r.Method(http.MethodGet, "/", g.Protect(authz.SystemRead, h.get))
	r.Method(http.MethodGet, "/info", g.Protect(authz.SystemRead, h.info))
	r.Method(http.MethodGet, "/namespaces", g.Protect(authz.NamespacesRead, h.namespaces))
	r.Method(http.MethodPost, "/namespaces", g.Protect(authz.NamespacesWrite, h.createNamespace))
	r.Method(http.MethodDelete, "/namespaces/{name}", g.Protect(authz.NamespacesWrite, h.removeNamespace))
	r.Method(http.MethodGet, "/events/stream", g.Protect(authz.SystemRead, h.eventsStream))
	r.Method(http.MethodGet, "/events", g.Protect(authz.SystemRead, h.listEvents))
	r.Method(http.MethodGet, "/containers", g.Protect(authz.ContainersRead, h.listContainers))
	r.Method(http.MethodPost, "/containers", g.Protect(authz.ContainersWrite, h.createContainer))
	r.Method(http.MethodPost, "/containers/command", g.Protect(authz.ContainersWrite, h.containerCommand))
	r.Method(http.MethodGet, "/containers/{id}/inspect", g.Protect(authz.ContainersRead, h.inspectContainer))
	r.Method(http.MethodGet, "/containers/{id}/spec", g.Protect(authz.ContainersRead, h.containerSpec))
	r.Method(http.MethodPost, "/containers/{id}/recreate", g.Protect(authz.ContainersWrite, h.recreateContainer))
	r.Method(http.MethodGet, "/containers/{id}/logs", g.Protect(authz.ContainersRead, h.containerLogsDownload))
	r.Method(http.MethodGet, "/containers/{id}/logs/stream", g.Protect(authz.ContainersRead, h.containerLogsStream))
	r.Method(http.MethodGet, "/containers/{id}/top", g.Protect(authz.ContainersRead, h.containerTop))
	r.Method(http.MethodGet, "/containers/{id}/stats/stream", g.Protect(authz.ContainersRead, h.containerStatsStream))
	r.Method(http.MethodGet, "/containers/{id}/stats/history", g.Protect(authz.ContainersRead, h.containerStatsHistory))
	r.Method(http.MethodGet, "/containers/{id}/exec", g.Protect(authz.ContainersExec, h.containerExec))
	h.containerActionRoutes(r, g)
	r.Method(http.MethodGet, "/images", g.Protect(authz.ImagesRead, h.listImages))
	r.Method(http.MethodGet, "/images/history", g.Protect(authz.ImagesRead, h.imageHistory))
	r.Method(http.MethodGet, "/images/save", g.Protect(authz.ImagesWrite, h.saveImages))
	r.Method(http.MethodPost, "/images/load", g.Protect(authz.ImagesWrite, h.loadImages))
	r.Method(http.MethodPost, "/images/pull", g.Protect(authz.ImagesWrite, h.pullImage))
	r.Method(http.MethodPost, "/images/push", g.Protect(authz.ImagesWrite, h.pushImage))
	r.Method(http.MethodPost, "/images/tag", g.Protect(authz.ImagesWrite, h.tagImage))
	r.Method(http.MethodPost, "/images/remove", g.Protect(authz.ImagesWrite, h.removeImages))
	r.Method(http.MethodGet, "/system/prune", g.Protect(authz.SystemRead, h.prunePreview))
	r.Method(http.MethodPost, "/system/prune", g.Protect(authz.SystemPrune, h.startPrune))
	r.Method(http.MethodGet, "/ports", g.Protect(authz.ContainersRead, h.listPorts))
	r.Method(http.MethodGet, "/summary", g.Protect(authz.SystemRead, h.summary))
	r.Method(http.MethodGet, "/stats/stream", g.Protect(authz.ContainersRead, h.topStream))
	r.Method(http.MethodGet, "/stacks", g.Protect(authz.StacksRead, h.listStacks))
	r.Method(http.MethodGet, "/stacks/{name}", g.Protect(authz.StacksRead, h.getStack))
	r.Method(http.MethodDelete, "/stacks/{name}", g.Protect(authz.StacksWrite, h.deleteStack))
	r.Method(http.MethodPost, "/stacks/{name}/{op}", g.Protect(authz.StacksDeploy, h.stackOperation))
	r.Method(http.MethodPost, "/stacks/{name}/services/{service}/{op}", g.Protect(authz.StacksDeploy, h.serviceOperation))
	h.gitStackRoutes(r, g)
	r.Method(http.MethodGet, "/stacks/{name}/logs/stream", g.Protect(authz.StacksRead, h.stackLogsStream))
	r.Method(http.MethodGet, "/networks", g.Protect(authz.NetworksRead, h.listNetworks))
	r.Method(http.MethodPost, "/networks", g.Protect(authz.NetworksWrite, h.createNetwork))
	r.Method(http.MethodDelete, "/networks/{name}", g.Protect(authz.NetworksWrite, h.removeNetwork))
	r.Method(http.MethodGet, "/volumes", g.Protect(authz.VolumesRead, h.listVolumes))
	r.Method(http.MethodPost, "/volumes", g.Protect(authz.VolumesWrite, h.createVolume))
	r.Method(http.MethodDelete, "/volumes/{name}", g.Protect(authz.VolumesWrite, h.removeVolume))
	// Volume contents may hold secrets: reading them needs volumes:write (#58, ADR-0045).
	r.Method(http.MethodGet, "/volumes/{name}/files", g.Protect(authz.VolumesWrite, h.listVolumeFiles))
	r.Method(http.MethodGet, "/volumes/{name}/files/preview", g.Protect(authz.VolumesWrite, h.previewVolumeFile))
	r.Method(http.MethodGet, "/volumes/{name}/files/download", g.Protect(authz.VolumesWrite, h.downloadVolumeFile))
	h.volumeBackupRoutes(r, g)
	r.Method(http.MethodGet, "/registries", g.Protect(authz.RegistriesRead, h.listRegistries))
	r.Method(http.MethodPost, "/registries", g.Protect(authz.RegistriesWrite, h.loginRegistry))
	r.Method(http.MethodDelete, "/registries", g.Protect(authz.RegistriesWrite, h.logoutRegistry))
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
	// Features reports server settings the UI adapts to.
	Features Features `json:"features"`
}

// Features of the server for an environment.
type Features struct {
	// Exec is false when the web terminal is disabled (--disable-exec).
	Exec bool `json:"exec"`
	// MetricsHistory is true when a Prometheus connection is configured (#176).
	MetricsHistory bool `json:"metricsHistory"`
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
	resp := EnvironmentStatus{Environment: toAPI(e), Status: StatusConnected, Features: Features{
		Exec: !h.ExecDisabled, MetricsHistory: h.metricsHistoryConfigured(ctx, e.ID),
	}}
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
