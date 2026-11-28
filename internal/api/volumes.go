// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/runtime"
)

// listVolumes returns the volumes of a namespace with the containers that mount them (#57):
//
//	GET /api/v1/environments/{env}/volumes?namespace=&size=true
//
// size=true walks every volume on disk; the UI asks for it on demand. namespace "*" lists every
// namespace (#276).
func (h *Handler) listVolumes(w http.ResponseWriter, r *http.Request) {
	size, err := boolParam(r.URL.Query().Get("size"))
	if err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument, "size: "+err.Error())
		return
	}
	volumes := EnvFrom(r.Context()).Runtime.Volumes()
	ns, list, err := listNamespaces(r,
		func(ctx context.Context, ns string) ([]runtime.Volume, error) { return volumes.List(ctx, ns, size) },
		func(v *runtime.Volume, ns string) { v.Namespace = ns })
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"namespace": ns, "volumes": list})
}

// createVolume creates a named volume (#57):
//
//	POST /api/v1/environments/{env}/volumes?namespace=  {"name": "pgdata", "labels": {"app": "db"}}
func (h *Handler) createVolume(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	var req struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	ctx, cancel := runtime.ActionContext(r.Context())
	defer cancel()
	v, err := EnvFrom(r.Context()).Runtime.Volumes().Create(ctx, ns, req.Name, req.Labels)
	if err != nil {
		h.writeActionErr(w, r, err)
		return
	}
	v.Namespace = ns
	httpjson.Write(w, http.StatusCreated, v)
}

// removeVolume removes a volume no container mounts (#57); its data is deleted:
//
//	DELETE /api/v1/environments/{env}/volumes/{name}?namespace=
func (h *Handler) removeVolume(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	ctx, cancel := runtime.ActionContext(r.Context())
	defer cancel()
	name := chi.URLParam(r, "name")
	if err := EnvFrom(r.Context()).Runtime.Volumes().Remove(ctx, ns, name); err != nil {
		h.writeActionErr(w, r, err)
		return
	}
	h.log().Info("volume removed", "environment", EnvFrom(r.Context()).ID, "namespace", ns, "volume", name)
	w.WriteHeader(http.StatusNoContent)
}
