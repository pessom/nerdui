// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/auth"
	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
)

// prunePreview estimates what a prune removes (#52, ADR-0020):
//
//	GET /api/v1/environments/{env}/system/prune?namespace=&allImages=true
func (h *Handler) prunePreview(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	all, err := boolParam(r.URL.Query().Get("allImages"))
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: allImages: %w", errdefs.ErrInvalidArgument, err))
		return
	}
	p, err := EnvFrom(r.Context()).Runtime.System().PrunePreview(r.Context(), ns, all)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, p)
}

// startPrune runs a prune as a job (#52):
//
//	POST /api/v1/environments/{env}/system/prune?namespace=  {"containers": true, "images": true, ...}
func (h *Handler) startPrune(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	var req struct {
		Containers bool `json:"containers"`
		Images     bool `json:"images"`
		AllImages  bool `json:"allImages"`
		Networks   bool `json:"networks"`
		BuildCache bool `json:"buildCache"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	if !req.Containers && !req.Images && !req.Networks && !req.BuildCache {
		httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument, "select at least one of containers, images, networks, buildCache")
		return
	}
	opts := runtime.PruneOptions{
		Containers: req.Containers, Images: req.Images, AllImages: req.AllImages,
		Networks: req.Networks, BuildCache: req.BuildCache,
	}
	env := EnvFrom(r.Context())
	user := ""
	if p, ok := auth.PrincipalFrom(r.Context()); ok {
		user = p.User.Username
	}
	sys := env.Runtime.System()
	job, err := h.Jobs.Start(jobs.Spec{
		Kind:        "system.prune",
		Title:       "Prune " + ns,
		Environment: env.ID,
		Namespace:   ns,
		Resource:    fmt.Sprintf("env/%s/ns/%s/prune", env.ID, ns),
		Action:      string(authz.SystemPrune),
		User:        user,
	}, func(ctx context.Context, out *jobs.Output) error {
		return sys.Prune(ctx, ns, opts, out)
	})
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusAccepted, map[string]any{"job": job})
}
