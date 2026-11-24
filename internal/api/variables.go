// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/variables"
)

// Variables of stacks (#35, ADR-0049). Global variables are a setting of all stacks; a stack's
// own variables are edited like its files. Secret values are never returned.
func (h *Handler) variableRoutes(r chi.Router, g *authz.Guard) {
	if h.Variables == nil {
		return
	}
	r.Method(http.MethodGet, "/variables", g.Protect(authz.SettingsRead, h.listVariables(false)))
	r.Method(http.MethodPut, "/variables/{var}", g.Protect(authz.SettingsWrite, h.putVariable(false)))
	r.Method(http.MethodDelete, "/variables/{var}", g.Protect(authz.SettingsWrite, h.deleteVariable(false)))
}

// stackVariableRoutes are under /environments/{env}.
func (h *Handler) stackVariableRoutes(r chi.Router, g *authz.Guard) {
	if h.Variables == nil {
		return
	}
	r.Method(http.MethodGet, "/stacks/{name}/variables", g.Protect(authz.StacksRead, h.listVariables(true)))
	r.Method(http.MethodPut, "/stacks/{name}/variables/{var}", g.Protect(authz.StacksWrite, h.putVariable(true)))
	r.Method(http.MethodDelete, "/stacks/{name}/variables/{var}", g.Protect(authz.StacksWrite, h.deleteVariable(true)))
}

// variableScope is the stack of the request (stack) or the global scope.
func (h *Handler) variableScope(r *http.Request, stack bool) (string, error) {
	if !stack {
		return variables.Global, nil
	}
	def, err := h.findStack(r)
	if err != nil {
		return "", err
	}
	return def.Name, nil
}

// GET /api/v1/variables, GET /api/v1/environments/{env}/stacks/{name}/variables
func (h *Handler) listVariables(stack bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, err := h.variableScope(r, stack)
		if err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}
		list, err := h.Variables.List(r.Context(), scope)
		if err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}
		httpjson.Write(w, http.StatusOK, map[string]any{"variables": list})
	}
}

// PUT …/variables/{var}  {"value", "secret"}; a missing value keeps the stored one.
func (h *Handler) putVariable(stack bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, err := h.variableScope(r, stack)
		if err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}
		var in variables.Input
		if !httpjson.Decode(w, r, &in) {
			return
		}
		v, err := h.Variables.Put(r.Context(), scope, chi.URLParam(r, "var"), in, principalName(r))
		if err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}
		h.log().Info("variable saved", "stack", scope, "variable", v.Name, "secret", v.Secret, "user", principalName(r))
		httpjson.Write(w, http.StatusOK, v)
	}
}

// DELETE …/variables/{var}
func (h *Handler) deleteVariable(stack bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, err := h.variableScope(r, stack)
		if err == nil {
			err = h.Variables.Delete(r.Context(), scope, chi.URLParam(r, "var"))
		}
		if err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// forgetVariables removes the variables of a stack nerdui no longer knows (#35), so a later
// stack of the same name does not get them.
func (h *Handler) forgetVariables(ctx context.Context, stack string) {
	if h.Variables == nil {
		return
	}
	if err := h.Variables.DeleteStack(ctx, stack); err != nil {
		h.log().Warn("remove the variables of a forgotten stack", "stack", stack, "error", err)
	}
}
