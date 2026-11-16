// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/stacks"
)

// stackList builds the stacks of a namespace: known definitions with their status, then
// compose projects seen only in container labels.
func (h *Handler) stackList(ctx context.Context, env *environment.Environment, ns string) ([]stacks.Stack, error) {
	if h.Stacks == nil {
		return nil, fmt.Errorf("%w: stacks are not available", errdefs.ErrNotImplemented)
	}
	defs, err := h.Stacks.Definitions(ctx)
	if err != nil {
		return nil, err
	}
	containers, err := env.Runtime.Containers().List(ctx, ns)
	if err != nil {
		return nil, err
	}
	out := make([]stacks.Stack, 0, len(defs))
	for _, d := range defs {
		var services []string
		var loadErr error
		if d.File != "" {
			services, loadErr = env.Runtime.Compose().Services(ctx, ns, d.Name, d.Dir, filepath.Join(d.Dir, d.File))
		}
		out = append(out, stacks.Summarize(d, services, loadErr, containers))
	}
	for _, name := range stacks.Unmanaged(defs, containers) {
		out = append(out, stacks.Summarize(stacks.Definition{Name: name, Mode: stacks.ModeUnmanaged}, nil, nil, containers))
	}
	return out, nil
}

// listStacks returns the stacks of a namespace with their status (#24, ADR-0008):
//
//	GET /api/v1/environments/{env}/stacks?namespace=
func (h *Handler) listStacks(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	list, err := h.stackList(r.Context(), EnvFrom(r.Context()), ns)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"namespace": ns, "stacks": list})
}

// getStack returns one stack with its services and containers (#24):
//
//	GET /api/v1/environments/{env}/stacks/{name}?namespace=
func (h *Handler) getStack(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	list, err := h.stackList(r.Context(), EnvFrom(r.Context()), ns)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	name := chi.URLParam(r, "name")
	for _, s := range list {
		if s.Name == name {
			httpjson.Write(w, http.StatusOK, s)
			return
		}
	}
	httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: stack %q not found in namespace %s", errdefs.ErrNotFound, name, ns))
}
