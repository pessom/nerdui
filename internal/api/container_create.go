// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/pessom/nerdui/internal/auth"
	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
)

// CreateContainerRequest is the body of POST /containers. Keep in sync with api/openapi.yaml.
type CreateContainerRequest struct {
	runtime.CreateSpec
	// Start runs the container after creating it (like `nerdctl run -d`).
	Start bool `json:"start"`
}

// containerCommand shows the `nerdctl run` equivalent of a spec (#17):
//
//	POST /api/v1/environments/{env}/containers/command  {spec}
func (h *Handler) containerCommand(w http.ResponseWriter, r *http.Request) {
	var req CreateContainerRequest
	if !httpjson.Decode(w, r, &req) {
		return
	}
	cmd, err := runtime.RunCommand(req.CreateSpec)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if !req.Start {
		cmd = "nerdctl create" + cmd[len("nerdctl run -d"):]
	}
	httpjson.Write(w, http.StatusOK, map[string]string{"command": cmd})
}

// createContainer pulls (when missing), creates and optionally starts a container as a job
// (#17, ADR-0003). The job's result has the new container's ID:
//
//	POST /api/v1/environments/{env}/containers?namespace=  {spec, "start": true}
func (h *Handler) createContainer(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	var req CreateContainerRequest
	if !httpjson.Decode(w, r, &req) {
		return
	}
	// Validate before starting a job, so mistakes come back as 400.
	if _, err := runtime.CreateArgs(req.CreateSpec); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	env := EnvFrom(r.Context())
	user := ""
	if p, ok := auth.PrincipalFrom(r.Context()); ok {
		user = p.User.Username
	}
	title, resource := "Create container from "+req.Image, ""
	if req.Name != "" {
		title = "Create container " + req.Name
		resource = fmt.Sprintf("env/%s/ns/%s/container/%s", env.ID, ns, req.Name)
	}
	containers := env.Runtime.Containers()
	spec, start := req.CreateSpec, req.Start
	job, err := h.Jobs.Start(jobs.Spec{
		Kind: "container.create", Title: title, Environment: env.ID, Namespace: ns,
		Resource: resource, Action: string(authz.ContainersWrite), User: user,
	}, func(ctx context.Context, out *jobs.Output) error {
		id, err := containers.Create(ctx, ns, spec, start, out)
		if id != "" {
			out.SetResult("containerId", id)
		}
		return err
	})
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusAccepted, map[string]any{"job": job})
}
