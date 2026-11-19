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
	Interval(ctx context.Context, env string) (time.Duration, error)
	SetInterval(ctx context.Context, env string, d time.Duration) error
}

// checkTimeout bounds a check started from the UI.
const checkTimeout = 3 * time.Minute

// ImageUpdateSettings is the schedule of an environment's checks.
type ImageUpdateSettings struct {
	// IntervalSeconds is 0 (off), 3600, 21600, 43200 or 86400.
	IntervalSeconds int64 `json:"intervalSeconds"`
}

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

// GET /api/v1/environments/{env}/settings/image-updates
func (h *Handler) getImageUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if h.imageUpdatesDisabled(w) {
		return
	}
	d, err := h.ImageUpdates.Interval(r.Context(), EnvFrom(r.Context()).ID)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, ImageUpdateSettings{IntervalSeconds: int64(d / time.Second)})
}

// PUT /api/v1/environments/{env}/settings/image-updates  ImageUpdateSettings
func (h *Handler) putImageUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if h.imageUpdatesDisabled(w) {
		return
	}
	var in ImageUpdateSettings
	if !httpjson.Decode(w, r, &in) {
		return
	}
	env := EnvFrom(r.Context()).ID
	err := h.ImageUpdates.SetInterval(r.Context(), env, time.Duration(in.IntervalSeconds)*time.Second)
	if errors.Is(err, updates.ErrInvalidInterval) {
		httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument,
			"intervalSeconds must be 0, 3600, 21600, 43200 or 86400")
		return
	}
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("image update settings saved", "environment", env, "interval", time.Duration(in.IntervalSeconds)*time.Second)
	httpjson.Write(w, http.StatusOK, in)
}

// AutoUpdates is the log of automatic updates (#54, ADR-0032); *updates.Updater implements it.
type AutoUpdates interface {
	Log(ctx context.Context, env, namespace string, limit int) ([]updates.Entry, error)
}

// autoUpdatesLimit is the number of log entries returned.
const autoUpdatesLimit = 100

// GET /api/v1/environments/{env}/auto-updates?namespace=
func (h *Handler) listAutoUpdates(w http.ResponseWriter, r *http.Request) {
	if h.AutoUpdates == nil {
		httpjson.WriteError(w, http.StatusNotImplemented, httpjson.CodeUnavailable, "automatic updates are not available")
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
	httpjson.Write(w, http.StatusOK, map[string]any{"entries": entries})
}
