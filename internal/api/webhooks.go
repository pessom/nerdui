// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/webhooks"
)

// Incoming webhooks (#85, #91, ADR-0041). A webhook lets anyone with its URL run its action,
// so managing them is an administrator's setting.
func (h *Handler) webhookRoutes(r chi.Router, g *authz.Guard) {
	r.Method(http.MethodGet, "/webhooks", g.Protect(authz.SettingsRead, h.listWebhooks))
	r.Method(http.MethodPost, "/webhooks", g.Protect(authz.SettingsWrite, h.createWebhook))
	r.Method(http.MethodPut, "/webhooks/{id}", g.Protect(authz.SettingsWrite, h.updateWebhook))
	r.Method(http.MethodDelete, "/webhooks/{id}", g.Protect(authz.SettingsWrite, h.deleteWebhook))
	r.Method(http.MethodPost, "/webhooks/{id}/token", g.Protect(authz.SettingsWrite, h.newWebhookToken))
}

func (h *Handler) webhooksDisabled(w http.ResponseWriter) bool {
	if h.Webhooks == nil {
		httpjson.WriteError(w, http.StatusNotImplemented, httpjson.CodeUnavailable, "webhooks are not available")
		return true
	}
	return false
}

func webhookID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: webhook %q not found", errdefs.ErrNotFound, chi.URLParam(r, "id"))
	}
	return id, nil
}

// GET /api/v1/webhooks
func (h *Handler) listWebhooks(w http.ResponseWriter, r *http.Request) {
	if h.webhooksDisabled(w) {
		return
	}
	list, err := h.Webhooks.List(r.Context())
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"webhooks": list})
}

// POST /api/v1/webhooks: the response has the URL path with the token, shown only once.
func (h *Handler) createWebhook(w http.ResponseWriter, r *http.Request) {
	if h.webhooksDisabled(w) {
		return
	}
	var in webhooks.Input
	if !httpjson.Decode(w, r, &in) {
		return
	}
	v, token, err := h.Webhooks.Create(r.Context(), in, principalName(r))
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("webhook created", "webhook", v.Name, "target", v.TargetKind+" "+v.Target, "action", v.Action, "user", principalName(r))
	httpjson.Write(w, http.StatusCreated, map[string]any{"webhook": v, "path": webhooks.Path(token)})
}

// PUT /api/v1/webhooks/{id}; the token stays.
func (h *Handler) updateWebhook(w http.ResponseWriter, r *http.Request) {
	if h.webhooksDisabled(w) {
		return
	}
	id, err := webhookID(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	var in webhooks.Input
	if !httpjson.Decode(w, r, &in) {
		return
	}
	v, err := h.Webhooks.Update(r.Context(), id, in)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, v)
}

// DELETE /api/v1/webhooks/{id}
func (h *Handler) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	if h.webhooksDisabled(w) {
		return
	}
	id, err := webhookID(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if err := h.Webhooks.Delete(r.Context(), id); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/v1/webhooks/{id}/token: a new URL; the previous one stops working.
func (h *Handler) newWebhookToken(w http.ResponseWriter, r *http.Request) {
	if h.webhooksDisabled(w) {
		return
	}
	id, err := webhookID(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	token, err := h.Webhooks.NewToken(r.Context(), id)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("webhook token replaced", "id", id, "user", principalName(r))
	httpjson.Write(w, http.StatusOK, map[string]string{"path": webhooks.Path(token)})
}

// IncomingWebhook runs the webhook of the secret token in the path as a job; the body is
// ignored (#85, #91):
//
//	POST /api/v1/hooks/{token}
func IncomingWebhook(svc *webhooks.Service, logger *slog.Logger) http.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			httpjson.WriteError(w, http.StatusNotImplemented, httpjson.CodeUnavailable, "webhooks are not available")
			return
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
		hook, job, err := svc.Call(r.Context(), chi.URLParam(r, "token"))
		if err != nil {
			if hook.Name != "" {
				logger.Warn("webhook not run", "webhook", hook.Name, "error", err)
			}
			httpjson.WriteErr(w, r, logger, err)
			return
		}
		logger.Info("webhook called", "webhook", hook.Name, "job", job.ID)
		httpjson.Write(w, http.StatusAccepted, map[string]string{"status": "accepted", "job": job.ID})
	}
}
