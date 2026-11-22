// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/updates"
)

// ImageUpdates checks images for newer registry digests (#53, ADR-0030); *updates.Checker
// implements it.
type ImageUpdates interface {
	Overview(ctx context.Context, env *environment.Environment, ns string) (updates.Overview, error)
	Check(ctx context.Context, env *environment.Environment, ref string) (updates.Report, error)
}

// checkTimeout bounds a check started from the UI.
const checkTimeout = 3 * time.Minute

// CheckImageUpdatesRequest checks one reference, or all used ones when empty.
type CheckImageUpdatesRequest struct {
	Ref string `json:"ref,omitempty"`
}

func (h *Handler) imageUpdatesDisabled(w http.ResponseWriter) bool {
	if h.ImageUpdates == nil {
		httpjson.WriteError(w, http.StatusNotImplemented, httpjson.CodeUnavailable, "image update checks are not available")
		return true
	}
	return false
}

// GET /api/v1/environments/{env}/image-updates?namespace=
func (h *Handler) listImageUpdates(w http.ResponseWriter, r *http.Request) {
	if h.imageUpdatesDisabled(w) {
		return
	}
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	out, err := h.ImageUpdates.Overview(r.Context(), EnvFrom(r.Context()), ns)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, out)
}

// POST /api/v1/environments/{env}/image-updates/check  CheckImageUpdatesRequest
func (h *Handler) checkImageUpdates(w http.ResponseWriter, r *http.Request) {
	if h.imageUpdatesDisabled(w) {
		return
	}
	var req CheckImageUpdatesRequest
	if !httpjson.Decode(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), checkTimeout)
	defer cancel()
	rep, err := h.ImageUpdates.Check(ctx, EnvFrom(r.Context()), req.Ref)
	if errors.Is(err, updates.ErrBusy) {
		httpjson.WriteError(w, http.StatusConflict, httpjson.CodeConflict, err.Error())
		return
	}
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, rep)
}

// AutoUpdates is the log of automatic updates (#54, ADR-0032); *updates.Updater implements it.
type AutoUpdates interface {
	Log(ctx context.Context, env, namespace string, limit int) ([]updates.Entry, error)
	PendingFor(env string) (updates.Pending, bool)
	Approve(ctx context.Context, env *environment.Environment) (int, error)
	Settings(ctx context.Context, env string) (updates.AutoSettings, error)
	SetSettings(ctx context.Context, env string, s updates.AutoSettings) error
}

// AutoUpdatesLog is the log and the updates waiting for approval. Keep in sync with
// api/openapi.yaml.
type AutoUpdatesLog struct {
	Entries []updates.Entry  `json:"entries"`
	Pending *updates.Pending `json:"pending,omitempty"`
}

func (h *Handler) autoUpdatesDisabled(w http.ResponseWriter) bool {
	if h.AutoUpdates == nil {
		httpjson.WriteError(w, http.StatusNotImplemented, httpjson.CodeUnavailable, "automatic updates are not available")
		return true
	}
	return false
}

// POST /api/v1/environments/{env}/auto-updates/approve
func (h *Handler) approveAutoUpdates(w http.ResponseWriter, r *http.Request) {
	if h.autoUpdatesDisabled(w) {
		return
	}
	n, err := h.AutoUpdates.Approve(r.Context(), EnvFrom(r.Context()))
	switch {
	case errors.Is(err, updates.ErrNothingPending), errors.Is(err, updates.ErrUpdating):
		httpjson.WriteError(w, http.StatusConflict, httpjson.CodeConflict, err.Error())
		return
	case err != nil:
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("automatic updates approved", "environment", EnvFrom(r.Context()).ID, "updates", n)
	httpjson.Write(w, http.StatusAccepted, map[string]int{"updates": n})
}

// GET /api/v1/environments/{env}/settings/auto-updates
func (h *Handler) getAutoUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if h.autoUpdatesDisabled(w) {
		return
	}
	s, err := h.AutoUpdates.Settings(r.Context(), EnvFrom(r.Context()).ID)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("automatic update settings saved", "environment", EnvFrom(r.Context()).ID,
		"delay", time.Duration(s.DelaySeconds)*time.Second, "max_batch", s.MaxBatch)
	httpjson.Write(w, http.StatusOK, s)
}

// PUT /api/v1/environments/{env}/settings/auto-updates  AutoSettings
func (h *Handler) putAutoUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if h.autoUpdatesDisabled(w) {
		return
	}
	var s updates.AutoSettings
	if !httpjson.Decode(w, r, &s) {
		return
	}
	if err := h.AutoUpdates.SetSettings(r.Context(), EnvFrom(r.Context()).ID, s); err != nil {
		if errors.Is(err, updates.ErrInvalidAutoSettings) {
			httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument,
				"delaySeconds must be 0, 3600, 21600, 86400, 259200 or 604800; maxBatch 0, 1, 3, 5, 10 or 20")
			return
		}
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("automatic update settings saved", "environment", EnvFrom(r.Context()).ID,
		"delay", time.Duration(s.DelaySeconds)*time.Second, "max_batch", s.MaxBatch)
	httpjson.Write(w, http.StatusOK, s)
}

// autoUpdatesLimit is the number of log entries returned.
const autoUpdatesLimit = 100

// GET /api/v1/environments/{env}/auto-updates?namespace=
func (h *Handler) listAutoUpdates(w http.ResponseWriter, r *http.Request) {
	if h.autoUpdatesDisabled(w) {
		return
	}
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	entries, err := h.AutoUpdates.Log(r.Context(), EnvFrom(r.Context()).ID, ns, autoUpdatesLimit)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	out := AutoUpdatesLog{Entries: entries}
	if p, ok := h.AutoUpdates.PendingFor(EnvFrom(r.Context()).ID); ok {
		out.Pending = &p
	}
	httpjson.Write(w, http.StatusOK, out)
}
