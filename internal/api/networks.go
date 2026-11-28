// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/runtime"
)

// listNetworks returns the CNI networks of a namespace with their containers (#60):
//
//	GET /api/v1/environments/{env}/networks?namespace=
//
// namespace "*" lists every namespace (#276).
func (h *Handler) listNetworks(w http.ResponseWriter, r *http.Request) {
	ns, list, err := listNamespaces(r, EnvFrom(r.Context()).Runtime.Networks().List,
		func(n *runtime.Network, ns string) { n.Namespace = ns })
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"namespace": ns, "networks": list})
}

// createNetwork creates a bridge, macvlan or ipvlan network with the host nerdctl (#60):
//
//	POST /api/v1/environments/{env}/networks?namespace=  runtime.NetworkSpec
func (h *Handler) createNetwork(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	var spec runtime.NetworkSpec
	if !httpjson.Decode(w, r, &spec) {
		return
	}
	ctx, cancel := runtime.ActionContext(r.Context())
	defer cancel()
	n, err := EnvFrom(r.Context()).Runtime.Networks().Create(ctx, ns, spec)
	if err != nil {
		h.writeActionErr(w, r, err)
		return
	}
	n.Namespace = ns
	h.log().Info("network created", "environment", EnvFrom(r.Context()).ID, "namespace", ns, "network", n.Name, "driver", n.Driver)
	httpjson.Write(w, http.StatusCreated, n)
}

// removeNetwork removes a network no running container uses (#60):
//
//	DELETE /api/v1/environments/{env}/networks/{name}?namespace=
func (h *Handler) removeNetwork(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	ctx, cancel := runtime.ActionContext(r.Context())
	defer cancel()
	name := chi.URLParam(r, "name")
	if err := EnvFrom(r.Context()).Runtime.Networks().Remove(ctx, ns, name); err != nil {
		h.writeActionErr(w, r, err)
		return
	}
	h.log().Info("network removed", "environment", EnvFrom(r.Context()).ID, "namespace", ns, "network", name)
	w.WriteHeader(http.StatusNoContent)
}
