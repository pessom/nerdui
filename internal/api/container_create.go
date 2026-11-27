// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/auth"
	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/templates"
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

// RecreateContainerRequest replaces a container (#18). Keep in sync with api/openapi.yaml.
type RecreateContainerRequest struct {
	runtime.CreateSpec
	// Pull pulls the image first: an update to the tag's newest image.
	Pull bool `json:"pull"`
}

// containerSpec returns the settings a container would be recreated with (#18):
//
//	GET /api/v1/environments/{env}/containers/{id}/spec?namespace=
func (h *Handler) containerSpec(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	spec, err := EnvFrom(r.Context()).Runtime.Containers().Spec(r.Context(), ns, chi.URLParam(r, "id"))
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if spec.Spec.Labels == nil {
		spec.Spec.Labels = map[string]string{}
	}
	if spec.Unsupported == nil {
		spec.Unsupported = []string{}
	}
	httpjson.Write(w, http.StatusOK, spec)
}

// containerTemplate turns a container's settings into a one-service compose file and an
// .env.example for a template (#19, ADR-0059); secrets become variables without a value.
//
//	GET /api/v1/environments/{env}/containers/{id}/template?namespace=
func (h *Handler) containerTemplate(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	spec, err := EnvFrom(r.Context()).Runtime.Containers().Spec(r.Context(), ns, chi.URLParam(r, "id"))
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, templates.FromContainer(spec.Spec, spec.Unsupported))
}

// recreateContainer replaces a container with one created from the edited settings as a job
// (#18, ADR-0031). Containers with settings the form cannot express, and stack containers, are
// refused:
//
//	POST /api/v1/environments/{env}/containers/{id}/recreate?namespace=  {spec, "pull": false}
func (h *Handler) recreateContainer(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	var req RecreateContainerRequest
	if !httpjson.Decode(w, r, &req) {
		return
	}
	if _, err := runtime.CreateArgs(req.CreateSpec); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	env := EnvFrom(r.Context())
	containers := env.Runtime.Containers()
	id := chi.URLParam(r, "id")
	cur, err := containers.Spec(r.Context(), ns, id)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	switch {
	case cur.Stack != "":
		httpjson.WriteErr(w, r, h.Logger, runtime.ErrStackContainer)
		return
	case len(cur.Unsupported) > 0:
		httpjson.WriteError(w, http.StatusConflict, httpjson.CodeConflict,
			"the container has settings that recreating would lose: "+strings.Join(cur.Unsupported, "; "))
		return
	}
	user := ""
	if p, ok := auth.PrincipalFrom(r.Context()); ok {
		user = p.User.Username
	}
	verb := "Recreate"
	if req.Pull {
		verb = "Update"
	}
	spec, opts := req.CreateSpec, runtime.RecreateOptions{Pull: req.Pull}
	job, err := h.Jobs.Start(jobs.Spec{
		Kind: "container.recreate", Title: verb + " container " + cur.Spec.Name, Environment: env.ID, Namespace: ns,
		Resource: fmt.Sprintf("env/%s/ns/%s/container/%s", env.ID, ns, cur.Spec.Name),
		Action:   string(authz.ContainersWrite), User: user,
	}, func(ctx context.Context, out *jobs.Output) error {
		newID, err := containers.Recreate(ctx, ns, cur.ID, spec, opts, out)
		if newID != "" {
			out.SetResult("containerId", newID)
		}
		return err
	})
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("container recreate started", "environment", env.ID, "namespace", ns, "container", cur.Spec.Name, "pull", req.Pull, "user", user)
	httpjson.Write(w, http.StatusAccepted, map[string]any{"job": job})
}
