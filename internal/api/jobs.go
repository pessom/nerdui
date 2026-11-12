// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/api/ws"
	"github.com/pessom/nerdui/internal/auth"
	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
)

// jobRoutes registers background jobs (#126, ADR-0018). A job is visible only to callers
// allowed the action that started it.
func (h *Handler) jobRoutes(r chi.Router, g *authz.Guard) {
	r.Method(http.MethodGet, "/jobs", g.Protect(authz.JobsRead, h.listJobs(g)))
	r.Method(http.MethodGet, "/jobs/{id}", g.Protect(authz.JobsRead, h.getJob(g)))
	r.Method(http.MethodGet, "/jobs/{id}/stream", g.Protect(authz.JobsRead, h.streamJob(g)))
	r.Method(http.MethodPost, "/jobs/{id}/cancel", g.Protect(authz.JobsRead, h.cancelJob(g)))
}

// visibleJob returns the job when the caller may see it; otherwise it answers 404.
func (h *Handler) visibleJob(w http.ResponseWriter, r *http.Request, g *authz.Guard) (jobs.Job, bool) {
	j, ok := h.Jobs.Get(chi.URLParam(r, "id"))
	if !ok || !g.Allowed(r, authz.Action(j.Action)) {
		httpjson.WriteError(w, http.StatusNotFound, httpjson.CodeNotFound, "job not found")
		return jobs.Job{}, false
	}
	return j, true
}

func (h *Handler) listJobs(g *authz.Guard) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out := []jobs.Job{}
		for _, j := range h.Jobs.List() {
			if g.Allowed(r, authz.Action(j.Action)) {
				out = append(out, j)
			}
		}
		httpjson.Write(w, http.StatusOK, map[string]any{"jobs": out})
	}
}

func (h *Handler) getJob(g *authz.Guard) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if j, ok := h.visibleJob(w, r, g); ok {
			httpjson.Write(w, http.StatusOK, j)
		}
	}
}

func (h *Handler) cancelJob(g *authz.Guard) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		j, ok := h.visibleJob(w, r, g)
		if !ok {
			return
		}
		j, err := h.Jobs.Cancel(j.ID)
		if err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}
		httpjson.Write(w, http.StatusAccepted, j)
	}
}

// JobFrame is a data message of a job stream: the job state and/or terminal output. The
// first frame carries the job and the stored output with reset set.
type JobFrame struct {
	Job    *jobs.Job `json:"job,omitempty"`
	Output string    `json:"output,omitempty"`
	Reset  bool      `json:"reset,omitempty"`
}

// streamJob streams a job over WebSocket (ADR-0007):
//
//	GET /api/v1/jobs/{id}/stream
//
// The stream ends with "end" once the job has finished (after its final state).
func (h *Handler) streamJob(g *authz.Guard) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := h.visibleJob(w, r, g); !ok {
			return
		}
		job, output, updates, unsubscribe, ok := h.Jobs.Subscribe(chi.URLParam(r, "id"))
		if !ok {
			httpjson.WriteError(w, http.StatusNotFound, httpjson.CodeNotFound, "job not found")
			return
		}
		defer unsubscribe()
		conn, err := ws.Accept(w, r, ws.Options{})
		if err != nil {
			return
		}
		if !conn.SendDataWait(JobFrame{Job: &job, Output: output, Reset: true}) {
			return
		}
		for {
			select {
			case u, open := <-updates:
				if !open {
					// Finished, or this subscriber fell behind: the final state (or the client's
					// resubscription) follows from the job itself.
					if j, ok := h.Jobs.Get(job.ID); ok && j.Done() {
						if u.Job == nil {
							conn.SendDataWait(JobFrame{Job: &j})
						}
						conn.End()
						return
					}
					conn.SendError(httpjson.CodeUnavailable, "stream fell behind; reconnect")
					conn.End()
					return
				}
				if !conn.SendDataWait(JobFrame{Job: u.Job, Output: string(u.Output)}) {
					return
				}
			case <-conn.Done():
				return
			}
		}
	}
}

// imageJobRequest is the body of pull and push.
type imageJobRequest struct {
	Reference    string `json:"reference"`
	Platform     string `json:"platform"`
	AllPlatforms bool   `json:"allPlatforms"`
}

// pullImage starts a pull job (#43):
//
//	POST /api/v1/environments/{env}/images/pull?namespace=  {"reference": "nginx:1.27"}
func (h *Handler) pullImage(w http.ResponseWriter, r *http.Request) {
	h.startImageJob(w, r, "image.pull", "Pull ", func(ctx context.Context, img runtime.Images, ns string, req imageJobRequest, out *jobs.Output) error {
		return img.Pull(ctx, ns, req.Reference, runtime.PullOptions{Platform: req.Platform, AllPlatforms: req.AllPlatforms}, out)
	})
}

// pushImage starts a push job (#44):
//
//	POST /api/v1/environments/{env}/images/push?namespace=  {"reference": "registry.local/app:1"}
func (h *Handler) pushImage(w http.ResponseWriter, r *http.Request) {
	h.startImageJob(w, r, "image.push", "Push ", func(ctx context.Context, img runtime.Images, ns string, req imageJobRequest, out *jobs.Output) error {
		return img.Push(ctx, ns, req.Reference, runtime.PushOptions{Platform: req.Platform, AllPlatforms: req.AllPlatforms}, out)
	})
}

type imageJobFunc func(ctx context.Context, img runtime.Images, ns string, req imageJobRequest, out *jobs.Output) error

// startImageJob runs an operation on one image reference as a job. Pull and push of the same
// reference share the lock.
func (h *Handler) startImageJob(w http.ResponseWriter, r *http.Request, kind, verb string, run imageJobFunc) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	var req imageJobRequest
	if !httpjson.Decode(w, r, &req) {
		return
	}
	if req.Reference == "" {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: reference is required", errdefs.ErrInvalidArgument))
		return
	}
	env := EnvFrom(r.Context())
	user := ""
	if p, ok := auth.PrincipalFrom(r.Context()); ok {
		user = p.User.Username
	}
	images := env.Runtime.Images()
	job, err := h.Jobs.Start(jobs.Spec{
		Kind:        kind,
		Title:       verb + req.Reference,
		Environment: env.ID,
		Namespace:   ns,
		Resource:    fmt.Sprintf("env/%s/ns/%s/image/%s", env.ID, ns, req.Reference),
		Action:      string(authz.ImagesWrite),
		User:        user,
	}, func(ctx context.Context, out *jobs.Output) error {
		return run(ctx, images, ns, req, out)
	})
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusAccepted, map[string]any{"job": job})
}
