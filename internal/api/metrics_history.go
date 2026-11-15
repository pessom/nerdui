// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/prometheus"
	"github.com/pessom/nerdui/internal/store/db"
)

// Settings stores key/value settings; *db.Queries of the write connection implements it.
type Settings interface {
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, arg db.SetSettingParams) error
	DeleteSetting(ctx context.Context, key string) (int64, error)
}

// PrometheusSettings is the API view of an environment's Prometheus connection (#176):
// the secret is write-only. Keep in sync with api/openapi.yaml.
type PrometheusSettings struct {
	Configured bool   `json:"configured"`
	URL        string `json:"url,omitempty"`
	Auth       string `json:"auth,omitempty"`
	Username   string `json:"username,omitempty"`
	HasSecret  bool   `json:"hasSecret"`
}

// PrometheusSettingsInput sets the connection. An empty secret keeps the stored one when
// the authentication mode is unchanged.
type PrometheusSettingsInput struct {
	URL      string `json:"url"`
	Auth     string `json:"auth"`
	Username string `json:"username"`
	Secret   string `json:"secret"`
}

// PrometheusCheck is the result of testing a connection.
type PrometheusCheck struct {
	Version string `json:"version,omitempty"`
	// Containers is the number of containers with containerd CPU metrics in Prometheus.
	Containers int `json:"containers"`
}

// log returns the handler's logger or the default one (tests leave it nil).
func (h *Handler) log() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

func prometheusKey(env string) string { return "environments/" + env + "/prometheus" }

// prometheusConfig returns the stored connection of the environment, nil when none.
func (h *Handler) prometheusConfig(ctx context.Context, env string) (*prometheus.Config, error) {
	if h.Settings == nil {
		return nil, nil
	}
	v, err := h.Settings.GetSetting(ctx, prometheusKey(env))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg prometheus.Config
	if err := json.Unmarshal([]byte(v), &cfg); err != nil {
		return nil, fmt.Errorf("stored prometheus settings: %w", err)
	}
	return &cfg, nil
}

func (h *Handler) prometheusClient(cfg prometheus.Config) *prometheus.Client {
	return prometheus.New(cfg, h.PrometheusHTTP)
}

func toPrometheusSettings(cfg *prometheus.Config) PrometheusSettings {
	if cfg == nil {
		return PrometheusSettings{}
	}
	return PrometheusSettings{
		Configured: true, URL: cfg.URL, Auth: cfg.Auth, Username: cfg.Username, HasSecret: cfg.Secret != "",
	}
}

// GET /api/v1/environments/{env}/settings/prometheus
func (h *Handler) getPrometheusSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.prometheusConfig(r.Context(), EnvFrom(r.Context()).ID)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, toPrometheusSettings(cfg))
}

// resolvePrometheusInput validates the input, reusing the stored secret when allowed.
func (h *Handler) resolvePrometheusInput(r *http.Request, in PrometheusSettingsInput) (prometheus.Config, error) {
	cfg := prometheus.Config{URL: in.URL, Auth: in.Auth, Username: in.Username, Secret: in.Secret}
	if cfg.Secret == "" && cfg.Auth != "" && cfg.Auth != prometheus.AuthNone {
		stored, err := h.prometheusConfig(r.Context(), EnvFrom(r.Context()).ID)
		if err != nil {
			return prometheus.Config{}, err
		}
		if stored != nil && stored.Auth == cfg.Auth {
			cfg.Secret = stored.Secret
		}
	}
	if err := cfg.Validate(); err != nil {
		return prometheus.Config{}, err
	}
	return cfg, nil
}

// PUT /api/v1/environments/{env}/settings/prometheus
func (h *Handler) putPrometheusSettings(w http.ResponseWriter, r *http.Request) {
	if h.Settings == nil {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: settings are not available", errdefs.ErrNotImplemented))
		return
	}
	var in PrometheusSettingsInput
	if !httpjson.Decode(w, r, &in) {
		return
	}
	cfg, err := h.resolvePrometheusInput(r, in)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	b, err := json.Marshal(cfg) //nolint:gosec // G117: stored in the database; the API never returns it
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	env := EnvFrom(r.Context()).ID
	if err := h.Settings.SetSetting(r.Context(), db.SetSettingParams{
		Key: prometheusKey(env), Value: string(b), Now: time.Now().UnixMilli(),
	}); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("prometheus settings saved", "environment", env, "url", cfg.URL, "auth", cfg.Auth)
	httpjson.Write(w, http.StatusOK, toPrometheusSettings(&cfg))
}

// DELETE /api/v1/environments/{env}/settings/prometheus
func (h *Handler) deletePrometheusSettings(w http.ResponseWriter, r *http.Request) {
	if h.Settings != nil {
		env := EnvFrom(r.Context()).ID
		if _, err := h.Settings.DeleteSetting(r.Context(), prometheusKey(env)); err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}
		h.log().Info("prometheus settings removed", "environment", env)
	}
	w.WriteHeader(http.StatusNoContent)
}

// testPrometheusSettings checks a connection before it is saved:
//
//	POST /api/v1/environments/{env}/settings/prometheus/test
func (h *Handler) testPrometheusSettings(w http.ResponseWriter, r *http.Request) {
	var in PrometheusSettingsInput
	if !httpjson.Decode(w, r, &in) {
		return
	}
	cfg, err := h.resolvePrometheusInput(r, in)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	version, n, err := h.prometheusClient(cfg).Check(r.Context())
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, PrometheusCheck{Version: version, Containers: n})
}

// containerStatsHistory returns the metrics history of a container from Prometheus (#176):
//
//	GET /api/v1/environments/{env}/containers/{id}/stats/history?namespace=&range=1h
//
// {id} is the exact container ID; the container may have been stopped or removed.
func (h *Handler) containerStatsHistory(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	rng, ok := prometheus.RangeByName(r.URL.Query().Get("range"))
	if !ok {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: range must be 1h, 6h, 24h or 7d", errdefs.ErrInvalidArgument))
		return
	}
	cfg, err := h.prometheusConfig(r.Context(), EnvFrom(r.Context()).ID)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if cfg == nil {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: metrics history is not configured for this environment", errdefs.ErrFailedPrecondition))
		return
	}
	hist, err := h.prometheusClient(*cfg).History(r.Context(), ns, chi.URLParam(r, "id"), rng, time.Now())
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, hist)
}

// metricsHistoryConfigured reports whether the environment has a Prometheus connection.
func (h *Handler) metricsHistoryConfigured(ctx context.Context, env string) bool {
	cfg, err := h.prometheusConfig(ctx, env)
	if err != nil {
		h.log().Warn("read prometheus settings", "environment", env, "error", err)
	}
	return cfg != nil
}
