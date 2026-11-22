// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/scheduler"
)

// Scheduled jobs (#82, ADR-0048). A schedule acts without a user at the time it runs, so
// managing schedules is an administrator's setting.
func (h *Handler) scheduleRoutes(r chi.Router, g *authz.Guard) {
	if h.Schedules == nil {
		return
	}
	r.Method(http.MethodGet, "/schedules", g.Protect(authz.SettingsRead, h.listSchedules))
	r.Method(http.MethodPost, "/schedules", g.Protect(authz.SettingsWrite, h.createSchedule))
	r.Method(http.MethodPut, "/schedules/{id}", g.Protect(authz.SettingsWrite, h.updateSchedule))
	r.Method(http.MethodDelete, "/schedules/{id}", g.Protect(authz.SettingsWrite, h.deleteSchedule))
	r.Method(http.MethodPost, "/schedules/{id}/run", g.Protect(authz.SettingsWrite, h.runSchedule))
	r.Method(http.MethodGet, "/schedules/{id}/runs", g.Protect(authz.SettingsRead, h.listScheduleRuns))
}

func scheduleID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: schedule %q not found", errdefs.ErrNotFound, chi.URLParam(r, "id"))
	}
	return id, nil
}

// GET /api/v1/schedules
func (h *Handler) listSchedules(w http.ResponseWriter, r *http.Request) {
	list, err := h.Schedules.List(r.Context())
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"schedules": list})
}

// POST /api/v1/schedules
func (h *Handler) createSchedule(w http.ResponseWriter, r *http.Request) {
	var in scheduler.Input
	if !httpjson.Decode(w, r, &in) {
		return
	}
	v, err := h.Schedules.Create(r.Context(), in, principalName(r))
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("schedule created", "schedule", v.Name, "kind", v.Kind, "cron", v.Cron, "user", principalName(r))
	httpjson.Write(w, http.StatusCreated, v)
}

// PUT /api/v1/schedules/{id}; an empty password keeps the stored one.
func (h *Handler) updateSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := scheduleID(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	var in scheduler.Input
	if !httpjson.Decode(w, r, &in) {
		return
	}
	v, err := h.Schedules.Update(r.Context(), id, in)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, v)
}

// DELETE /api/v1/schedules/{id}
func (h *Handler) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := scheduleID(r)
	if err == nil {
		err = h.Schedules.Delete(r.Context(), id)
	}
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/v1/schedules/{id}/run starts a run now: 202 with the job.
func (h *Handler) runSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := scheduleID(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	job, err := h.Schedules.RunNow(r.Context(), id, principalName(r))
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusAccepted, map[string]any{"job": job})
}

// GET /api/v1/schedules/{id}/runs
func (h *Handler) listScheduleRuns(w http.ResponseWriter, r *http.Request) {
	id, err := scheduleID(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	runs, err := h.Schedules.Runs(r.Context(), id)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"runs": runs})
}
