// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/templates"
)

// Stack templates (#33, #34, ADR-0051). Deploying one creates a stack (POST …/stacks with
// env and variables). The registries read over HTTP are a setting.
func (h *Handler) templateRoutes(r chi.Router, g *authz.Guard) {
	if h.Templates == nil {
		return
	}
	r.Method(http.MethodGet, "/templates", g.Protect(authz.StacksRead, h.listTemplates))
	r.Method(http.MethodGet, "/templates/detail", g.Protect(authz.StacksRead, h.getTemplate))
	r.Method(http.MethodPost, "/templates", g.Protect(authz.StacksWrite, h.createTemplate))
	r.Method(http.MethodPut, "/templates/{id}", g.Protect(authz.StacksWrite, h.updateTemplate))
	r.Method(http.MethodDelete, "/templates/{id}", g.Protect(authz.StacksWrite, h.deleteTemplate))
	r.Method(http.MethodGet, "/template-registries", g.Protect(authz.SettingsRead, h.getTemplateRegistries))
	r.Method(http.MethodPut, "/template-registries", g.Protect(authz.SettingsWrite, h.putTemplateRegistries))
}

// GET /api/v1/templates: built-in, custom and registry templates, and registries that
// could not be read.
func (h *Handler) listTemplates(w http.ResponseWriter, r *http.Request) {
	list, errs, err := h.Templates.List(r.Context())
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if errs == nil {
		errs = []templates.SourceError{}
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"templates": list, "errors": errs})
}

// GET /api/v1/templates/detail?source=&id=
func (h *Handler) getTemplate(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	t, err := h.Templates.Get(r.Context(), q.Get("source"), q.Get("id"))
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, t)
}

// POST /api/v1/templates
func (h *Handler) createTemplate(w http.ResponseWriter, r *http.Request) {
	var in templates.Input
	if !httpjson.Decode(w, r, &in) {
		return
	}
	t, err := h.Templates.Create(r.Context(), in, principalName(r))
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusCreated, t)
}

// PUT /api/v1/templates/{id}
func (h *Handler) updateTemplate(w http.ResponseWriter, r *http.Request) {
	var in templates.Input
	if !httpjson.Decode(w, r, &in) {
		return
	}
	t, err := h.Templates.Update(r.Context(), chi.URLParam(r, "id"), in)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, t)
}

// DELETE /api/v1/templates/{id}
func (h *Handler) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	if err := h.Templates.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/v1/template-registries
func (h *Handler) getTemplateRegistries(w http.ResponseWriter, r *http.Request) {
	urls, err := h.Templates.RegistryURLs(r.Context())
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"urls": urls})
}

// PUT /api/v1/template-registries  {"urls"}
func (h *Handler) putTemplateRegistries(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URLs []string `json:"urls"`
	}
	if !httpjson.Decode(w, r, &in) {
		return
	}
	urls, err := h.Templates.SetRegistryURLs(r.Context(), in.URLs)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"urls": urls})
}
