// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/stacks"
)

// Operations on one service of a stack (#29, ADR-0042). `nerdctl compose <op> <service>` also
// acts on the service's dependencies, so start, stop and restart act on the service's
// containers directly, and a redeploy recreates only the service's containers.

var serviceOps = map[string]string{
	"start": "Start", "stop": "Stop", "restart": "Restart", "redeploy": "Redeploy",
}

// composeService matches compose service names (compose-go: [a-zA-Z0-9._-]).
var composeService = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// serviceOperation runs an operation on one service as a job:
//
//	POST /api/v1/environments/{env}/stacks/{name}/services/{service}/{op}?namespace=
//
// op is start, stop, restart or redeploy (pull the service's image and recreate its containers
// when the image changed).
func (h *Handler) serviceOperation(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if h.Stacks == nil {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: stacks are not available", errdefs.ErrNotImplemented))
		return
	}
	name, service, op := chi.URLParam(r, "name"), chi.URLParam(r, "service"), chi.URLParam(r, "op")
	title, ok := serviceOps[op]
	if !ok {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: unknown service operation %q", errdefs.ErrNotFound, op))
		return
	}
	if !composeService.MatchString(service) {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: invalid service name %q", errdefs.ErrInvalidArgument, service))
		return
	}
	def, err := h.Stacks.Find(r.Context(), name)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	env := EnvFrom(r.Context())
	rt := env.Runtime
	containers, err := serviceContainers(r.Context(), rt, ns, name, service)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	// A service without containers can only be created from the compose file.
	needsCompose := op == "redeploy" || op == "start" && len(containers) == 0
	if needsCompose && def.File == "" {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: no compose file in %s", errdefs.ErrFailedPrecondition, def.Dir))
		return
	}
	if !needsCompose && len(containers) == 0 {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: service %q of stack %q has no containers", errdefs.ErrFailedPrecondition, service, name))
		return
	}
	user := principalName(r)
	job, err := h.Jobs.Start(jobs.Spec{
		Kind: "stack.service-" + op, Title: title + " service " + service + " of stack " + name,
		Environment: env.ID, Namespace: ns,
		Resource: fmt.Sprintf("env/%s/ns/%s/stack/%s", env.ID, ns, name),
		Action:   string(authz.StacksDeploy), User: user,
	}, func(ctx context.Context, out *jobs.Output) error {
		return runServiceOp(ctx, rt, ns, name, service, op, def, out)
	})
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("stack service operation started", "environment", env.ID, "namespace", ns, "stack", name,
		"service", service, "op", op, "user", user)
	httpjson.Write(w, http.StatusAccepted, map[string]any{"job": job})
}

func serviceContainers(ctx context.Context, rt runtime.Runtime, ns, project, service string) ([]runtime.Container, error) {
	list, err := rt.Containers().List(ctx, ns)
	if err != nil {
		return nil, err
	}
	var out []runtime.Container
	for _, c := range list {
		if c.Compose != nil && c.Compose.Project == project && c.Compose.Service == service {
			out = append(out, c)
		}
	}
	return out, nil
}

func runServiceOp(ctx context.Context, rt runtime.Runtime, ns, project, service, op string, def stacks.Definition, out *jobs.Output) error {
	services := []string{service}
	if op == "redeploy" {
		return runtime.Redeploy(ctx, rt, ns, project, def.Dir, runtime.RedeployOptions{Services: services}, out)
	}
	containers, err := serviceContainers(ctx, rt, ns, project, service)
	if err != nil {
		return err
	}
	if op == "start" && len(containers) == 0 {
		return rt.Compose().Run(ctx, ns, project, def.Dir, runtime.ComposeUp, runtime.ComposeOptions{Services: services}, out)
	}
	action := map[string]runtime.Action{"start": runtime.ActionStart, "stop": runtime.ActionStop, "restart": runtime.ActionRestart}[op]
	for _, c := range containers {
		_, _ = fmt.Fprintf(out, "%s %s\r\n", op, c.Name)
		if err := rt.Containers().Do(ctx, ns, c.ID, action, runtime.ActionOptions{}); err != nil {
			return fmt.Errorf("%s %s: %w", op, c.Name, err)
		}
	}
	return nil
}
