// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/hostmetrics"
	"github.com/pessom/nerdui/internal/httpjson"
)

// hostRanges are the history ranges of host metrics; the sampler keeps one day.
var hostRanges = map[string]time.Duration{"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour}

// Host metrics and their alert thresholds (#64, ADR-0054). The sampler reads the host nerdui
// runs on, which is the local environment's.
func (h *Handler) hostMetricRoutes(r chi.Router, g *authz.Guard) {
	r.Method(http.MethodGet, "/settings/host-alerts", g.Protect(authz.SettingsRead, h.getHostAlerts))
	r.Method(http.MethodPut, "/settings/host-alerts", g.Protect(authz.SettingsWrite, h.putHostAlerts))
}

// GET /api/v1/environments/{env}/host/metrics?range=1h
func (h *Handler) hostMetrics(w http.ResponseWriter, r *http.Request) {
	if h.HostMetrics == nil {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: host metrics are not available", errdefs.ErrNotImplemented))
		return
	}
	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = "1h"
	}
	d, ok := hostRanges[rng]
	if !ok {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: range must be 1h, 6h or 24h", errdefs.ErrInvalidArgument))
		return
	}
	cur, err := h.HostMetrics.Current()
	if errors.Is(err, hostmetrics.ErrUnavailable) {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: %w", errdefs.ErrUnavailable, err))
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{
		"current": cur, "points": h.HostMetrics.History(d), "range": rng,
		"intervalSeconds": int(h.HostMetrics.Step() / time.Second),
	})
}

// GET /api/v1/settings/host-alerts
func (h *Handler) getHostAlerts(w http.ResponseWriter, r *http.Request) {
	if h.Settings == nil {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: settings are not available", errdefs.ErrNotImplemented))
		return
	}
	a, err := hostmetrics.LoadAlerts(r.Context(), h.Settings)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, a)
}

// PUT /api/v1/settings/host-alerts  {"cpu", "memory", "disk"}
func (h *Handler) putHostAlerts(w http.ResponseWriter, r *http.Request) {
	if h.Settings == nil {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: settings are not available", errdefs.ErrNotImplemented))
		return
	}
	var a hostmetrics.Alerts
	if !httpjson.Decode(w, r, &a) {
		return
	}
	if err := hostmetrics.SaveAlerts(r.Context(), h.Settings, a); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("host alert thresholds changed", "user", principalName(r))
	httpjson.Write(w, http.StatusOK, a)
}
