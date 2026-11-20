// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/gitstacks"
	"github.com/pessom/nerdui/internal/httpjson"
)

// Automatic updates of Git stacks: polling (#39) and webhooks (#38, ADR-0040).

// gitAutoRoutes change only nerdui's database: they work in read-only mode too. Turning on
// automatic deploys is a deploy permission.
func (h *Handler) gitAutoRoutes(r chi.Router, g *authz.Guard) {
	r.Method(http.MethodPut, "/stacks/{name}/git/auto", g.Protect(authz.StacksDeploy, h.putGitAuto))
	r.Method(http.MethodPost, "/stacks/{name}/git/webhook", g.Protect(authz.StacksDeploy, h.createGitWebhook))
	r.Method(http.MethodDelete, "/stacks/{name}/git/webhook", g.Protect(authz.StacksDeploy, h.deleteGitWebhook))
}

// GitAutoRequest sets what happens on new commits. Keep in sync with api/openapi.yaml.
type GitAutoRequest struct {
	AutoUpdate   string `json:"autoUpdate"`
	PollInterval int64  `json:"pollInterval"`
}

// PUT /api/v1/environments/{env}/stacks/{name}/git/auto  {"autoUpdate", "pollInterval"}
func (h *Handler) putGitAuto(w http.ResponseWriter, r *http.Request) {
	if h.gitDisabled(w) {
		return
	}
	var req GitAutoRequest
	if !httpjson.Decode(w, r, &req) {
		return
	}
	name := chi.URLParam(r, "name")
	if err := h.GitStacks.SetAuto(r.Context(), name, req.AutoUpdate, req.PollInterval); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("git stack automatic updates set", "stack", name, "mode", req.AutoUpdate,
		"poll_interval", req.PollInterval, "user", principalName(r))
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/v1/environments/{env}/stacks/{name}/git/webhook: a new webhook token, shown once.
func (h *Handler) createGitWebhook(w http.ResponseWriter, r *http.Request) {
	if h.gitDisabled(w) {
		return
	}
	name := chi.URLParam(r, "name")
	token, err := h.GitStacks.NewWebhook(r.Context(), name)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("git stack webhook created", "stack", name, "user", principalName(r))
	httpjson.Write(w, http.StatusCreated, map[string]string{"path": gitstacks.WebhookPath(token)})
}

// DELETE /api/v1/environments/{env}/stacks/{name}/git/webhook
func (h *Handler) deleteGitWebhook(w http.ResponseWriter, r *http.Request) {
	if h.gitDisabled(w) {
		return
	}
	name := chi.URLParam(r, "name")
	if err := h.GitStacks.DeleteWebhook(r.Context(), name); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("git stack webhook removed", "stack", name, "user", principalName(r))
	w.WriteHeader(http.StatusNoContent)
}

// maxWebhookBody bounds the payload read for its ref; GitHub sends at most 25 MB.
const maxWebhookBody = 25 << 20

// GitWebhook handles push webhooks of Git stacks (#38): the secret token in the path selects
// the stack. A payload naming another ref than the stack's is ignored; otherwise the remote is
// checked in the background and a new commit is announced or deployed:
//
//	POST /api/v1/hooks/git/{token}
func GitWebhook(auto *gitstacks.Auto, logger *slog.Logger) http.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if auto == nil || auto.Service == nil {
			httpjson.WriteError(w, http.StatusNotImplemented, httpjson.CodeUnavailable, "Git stacks are not available")
			return
		}
		row, err := auto.Service.ByWebhook(r.Context(), chi.URLParam(r, "token"))
		if err != nil {
			httpjson.WriteErr(w, r, logger, err)
			return
		}
		// GitHub and Gitea name the event in a header; GitLab too ("Push Hook", "Tag Push Hook").
		for _, hdr := range []string{"X-GitHub-Event", "X-Gitea-Event", "X-Gogs-Event"} {
			switch ev := r.Header.Get(hdr); ev {
			case "", "push":
			case "ping":
				httpjson.Write(w, http.StatusOK, map[string]string{"status": "pong"})
				return
			default:
				httpjson.Write(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "event " + ev})
				return
			}
		}
		if ev := r.Header.Get("X-Gitlab-Event"); ev != "" && ev != "Push Hook" && ev != "Tag Push Hook" {
			httpjson.Write(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "event " + ev})
			return
		}
		var payload struct {
			Ref string `json:"ref"`
		}
		// Any body is accepted (generic callers send none); only a JSON "ref" is used.
		_ = json.NewDecoder(io.LimitReader(r.Body, maxWebhookBody)).Decode(&payload)
		if payload.Ref != "" && payload.Ref != row.FullRef {
			httpjson.Write(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "push to " + payload.Ref})
			return
		}
		if row.AutoUpdate == gitstacks.AutoOff {
			httpjson.WriteErr(w, r, logger, fmt.Errorf("%w: automatic updates of stack %q are off", errdefs.ErrFailedPrecondition, row.Name))
			return
		}
		logger.Info("git stack webhook", "stack", row.Name, "ref", payload.Ref)
		auto.Trigger(row.Name)
		httpjson.Write(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	}
}
