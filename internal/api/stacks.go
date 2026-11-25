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

	"github.com/pessom/nerdui/internal/auth"
	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
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

// stackCandidates lists compose projects in --stacks-scan-dirs that are not stacks yet (#25):
//
//	GET /api/v1/environments/{env}/stack-candidates
func (h *Handler) stackCandidates(w http.ResponseWriter, r *http.Request) {
	if h.Stacks == nil {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: stacks are not available", errdefs.ErrNotImplemented))
		return
	}
	list, err := h.Stacks.Discover(r.Context(), EnvFrom(r.Context()).Runtime.Compose())
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"scanDirs": h.Stacks.ScanDirs, "candidates": list})
}

// adoptStack makes an existing compose project a stack, in place or by copying its files (#25):
//
//	POST /api/v1/environments/{env}/stacks/adopt?namespace=  {"name", "path", "mode": "in-place"|"copy"}
func (h *Handler) adoptStack(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if h.Stacks == nil {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: stacks are not available", errdefs.ErrNotImplemented))
		return
	}
	var req stacks.AdoptRequest
	if !httpjson.Decode(w, r, &req) {
		return
	}
	env := EnvFrom(r.Context())
	def, err := h.Stacks.Adopt(r.Context(), env.Runtime.Compose(), req)
	if err != nil {
		h.writeActionErr(w, r, err)
		return
	}
	h.log().Info("stack adopted", "environment", env.ID, "stack", def.Name, "path", def.Dir, "mode", req.Mode)
	list, err := h.stackList(r.Context(), env, ns)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	for _, s := range list {
		if s.Name == def.Name {
			httpjson.Write(w, http.StatusCreated, s)
			return
		}
	}
	httpjson.Write(w, http.StatusCreated, stacks.Summarize(def, nil, nil, nil))
}

// forgetStack removes the registration of an external stack; files are kept (#25):
//
//	DELETE /api/v1/environments/{env}/stacks/{name}/registration
func (h *Handler) forgetStack(w http.ResponseWriter, r *http.Request) {
	if h.Stacks == nil {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: stacks are not available", errdefs.ErrNotImplemented))
		return
	}
	name := chi.URLParam(r, "name")
	if err := h.Stacks.Forget(r.Context(), name); err != nil {
		h.writeActionErr(w, r, err)
		return
	}
	h.forgetVariables(r.Context(), name)
	h.log().Info("stack registration removed", "environment", EnvFrom(r.Context()).ID, "stack", name)
	w.WriteHeader(http.StatusNoContent)
}

// deleteStack takes a stack created in nerdui down and removes its files and variables, as a
// job (#248, ADR-0052): the directory of a managed stack, or the clone and registration of a Git
// stack.
//
//	DELETE /api/v1/environments/{env}/stacks/{name}?namespace=&confirm=<name>&volumes=true
//
// volumes also removes the project's named volumes. Adopted stacks are forgotten instead.
func (h *Handler) deleteStack(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if h.Stacks == nil {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: stacks are not available", errdefs.ErrNotImplemented))
		return
	}
	name, q := chi.URLParam(r, "name"), r.URL.Query()
	if q.Get("confirm") != name {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: deleting removes the stack's files; confirm with the stack name", errdefs.ErrInvalidArgument))
		return
	}
	volumes := q.Get("volumes") == "true"
	def, err := h.Stacks.Find(r.Context(), name)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	// Stacks created in nerdui: from the stacks directory, or cloned from Git (#248).
	var remove func(ctx context.Context, name string) error
	switch {
	case def.Mode == stacks.ModeManaged:
		remove = h.Stacks.Remove
	case def.Mode == stacks.ModeGit && h.GitStacks != nil:
		remove = h.GitStacks.Remove
	default:
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: %q was not created in nerdui; forget it instead", errdefs.ErrFailedPrecondition, name))
		return
	}
	env := EnvFrom(r.Context())
	if err := h.stackBusy(env.ID, name); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	opts := runtime.ComposeOptions{RemoveOrphans: true, Volumes: volumes}
	if _, err := runtime.ComposeArgs(name, def.Dir, runtime.ComposeDown, opts); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	user := principalName(r)
	title := "Delete stack " + name
	if volumes {
		title += " with its volumes"
	}
	compose := env.Runtime.Compose()
	job, err := h.Jobs.Start(jobs.Spec{
		Kind: "stack.delete", Title: title, Environment: env.ID, Namespace: ns,
		Resource: fmt.Sprintf("env/%s/ns/%s/stack/%s", env.ID, ns, name),
		Action:   string(authz.StacksWrite), User: user,
	}, func(ctx context.Context, out *jobs.Output) error {
		if def.File != "" {
			if err := compose.Run(ctx, ns, name, def.Dir, runtime.ComposeDown, opts, out); err != nil {
				return fmt.Errorf("take the stack down: %w", err)
			}
		}
		if err := remove(ctx, name); err != nil {
			return err
		}
		h.forgetVariables(ctx, name)
		fmt.Fprintf(out, "Removed %s\n", def.Dir)
		return nil
	})
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("stack deletion started", "environment", env.ID, "namespace", ns, "stack", name, "volumes", volumes, "user", user)
	httpjson.Write(w, http.StatusAccepted, map[string]any{"job": job})
}

// StackOperationRequest modifies a stack operation. Confirm must repeat the stack name when
// Volumes is set: `down -v` deletes the project's volumes.
type StackOperationRequest struct {
	runtime.ComposeOptions
	Confirm string `json:"confirm,omitempty"`
}

var stackOpTitles = map[string]string{
	runtime.ComposeUp: "Deploy", runtime.ComposeDown: "Down", runtime.ComposeStart: "Start",
	runtime.ComposeStop: "Stop", runtime.ComposeRestart: "Restart", runtime.ComposePull: "Pull images of",
}

// stackOperation runs `nerdctl compose <op>` for a stack as a job (#28, #31, ADR-0026):
//
//	POST /api/v1/environments/{env}/stacks/{name}/{op}?namespace=  {"volumes", "removeOrphans", "pull", "confirm"}
//
// op is up, down, start, stop, restart or pull. One job per stack runs at a time (409).
func (h *Handler) stackOperation(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if h.Stacks == nil {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: stacks are not available", errdefs.ErrNotImplemented))
		return
	}
	name, op := chi.URLParam(r, "name"), chi.URLParam(r, "op")
	title, ok := stackOpTitles[op]
	if !ok {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: unknown stack operation %q", errdefs.ErrNotFound, op))
		return
	}
	var req StackOperationRequest
	if r.ContentLength != 0 && !httpjson.Decode(w, r, &req) {
		return
	}
	if req.Volumes && req.Confirm != name {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: removing volumes deletes their data; confirm with the stack name", errdefs.ErrInvalidArgument))
		return
	}
	defs, err := h.Stacks.Definitions(r.Context())
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	var def *stacks.Definition
	for i := range defs {
		if defs[i].Name == name {
			def = &defs[i]
		}
	}
	switch {
	case def == nil:
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: stack %q has no known compose file; adopt it first", errdefs.ErrNotFound, name))
		return
	case def.File == "":
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: no compose file in %s", errdefs.ErrFailedPrecondition, def.Dir))
		return
	}
	// Validate before starting a job, so mistakes come back as 400.
	if _, err := runtime.ComposeArgs(name, def.Dir, op, req.ComposeOptions); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	env := EnvFrom(r.Context())
	user := ""
	if p, ok := auth.PrincipalFrom(r.Context()); ok {
		user = p.User.Username
	}
	if req.Volumes {
		title = "Down (removing volumes)"
	}
	rt, compose, dir, opts := env.Runtime, env.Runtime.Compose(), def.Dir, req.ComposeOptions
	job, err := h.Jobs.Start(jobs.Spec{
		Kind: "stack." + op, Title: title + " stack " + name, Environment: env.ID, Namespace: ns,
		Resource: fmt.Sprintf("env/%s/ns/%s/stack/%s", env.ID, ns, name),
		Action:   string(authz.StacksDeploy), User: user,
	}, func(ctx context.Context, out *jobs.Output) error {
		if op == runtime.ComposeUp && opts.Pull {
			// compose up --pull alone keeps containers whose tag moved to a new image (ADR-0042).
			return runtime.Redeploy(ctx, rt, ns, name, dir, runtime.RedeployOptions{Services: opts.Services, RemoveOrphans: opts.RemoveOrphans}, out)
		}
		return compose.Run(ctx, ns, name, dir, op, opts, out)
	})
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("stack operation started", "environment", env.ID, "namespace", ns, "stack", name, "op", op,
		"volumes", opts.Volumes, "user", user)
	httpjson.Write(w, http.StatusAccepted, map[string]any{"job": job})
}
