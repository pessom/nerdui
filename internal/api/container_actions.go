// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/runtime"
)

// operateActions are the single-container actions served as POST .../containers/{id}/{action}.
var operateActions = []runtime.Action{
	runtime.ActionStart, runtime.ActionStop, runtime.ActionRestart,
	runtime.ActionKill, runtime.ActionPause, runtime.ActionUnpause,
}

// maxBulkContainers bounds a bulk request; bulkConcurrency bounds parallel actions in it.
const (
	maxBulkContainers = 200
	bulkConcurrency   = 4
)

// containerActionRoutes registers lifecycle actions (#11, #12):
//
//	POST   /containers/{id}/start|stop|restart|kill|pause|unpause   → 204
//	DELETE /containers/{id}?force=&volumes=                           → 204
//	POST   /containers/actions  {action, ids, force, volumes}         → 200 per-item results
//
// All accept ?namespace=. Actions are synchronous: stop waits for the container to exit
// (ADR-0006 reserves jobs for pull, deploy and prune).
func (h *Handler) containerActionRoutes(r chi.Router, g *authz.Guard) {
	for _, a := range operateActions {
		r.Method(http.MethodPost, "/containers/{id}/"+string(a), g.Protect(authz.ContainersOperate, h.containerAction(a)))
	}
	r.Method(http.MethodDelete, "/containers/{id}", g.Protect(authz.ContainersWrite, h.removeContainer))
	r.Method(http.MethodPost, "/containers/actions", g.Protect(authz.ContainersOperate, h.bulkContainerAction(g)))
}

func (h *Handler) containerAction(a runtime.Action) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.runAction(w, r, a, runtime.ActionOptions{})
	}
}

func (h *Handler) removeContainer(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var opts runtime.ActionOptions
	var err error
	if opts.Force, err = boolParam(q.Get("force")); err == nil {
		opts.Volumes, err = boolParam(q.Get("volumes"))
	}
	if err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument, err.Error())
		return
	}
	h.runAction(w, r, runtime.ActionRemove, opts)
}

func (h *Handler) runAction(w http.ResponseWriter, r *http.Request, a runtime.Action, opts runtime.ActionOptions) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	ctx, cancel := runtime.ActionContext(r.Context())
	defer cancel()
	if err := EnvFrom(r.Context()).Runtime.Containers().Do(ctx, ns, chi.URLParam(r, "id"), a, opts); err != nil {
		h.writeActionErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// BulkActionRequest is the body of POST /containers/actions. Keep in sync with api/openapi.yaml.
type BulkActionRequest struct {
	Action  runtime.Action `json:"action"`
	IDs     []string       `json:"ids"`
	Force   bool           `json:"force,omitempty"`
	Volumes bool           `json:"volumes,omitempty"`
}

// BulkActionResult is the outcome for one container.
type BulkActionResult struct {
	ID    string                `json:"id"`
	OK    bool                  `json:"ok"`
	Error *httpjson.ErrorDetail `json:"error,omitempty"`
}

func (h *Handler) bulkContainerAction(g *authz.Guard) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req BulkActionRequest
		if !httpjson.Decode(w, r, &req) {
			return
		}
		switch {
		case !req.Action.Valid():
			httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument, fmt.Sprintf("unknown action %q", req.Action))
			return
		case len(req.IDs) == 0 || len(req.IDs) > maxBulkContainers:
			httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument,
				fmt.Sprintf("ids must contain 1 to %d containers", maxBulkContainers))
			return
		case req.Action != runtime.ActionRemove && (req.Force || req.Volumes):
			httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument, "force and volumes apply to remove only")
			return
		case req.Action == runtime.ActionRemove && !g.Allowed(r, authz.ContainersWrite):
			httpjson.WriteError(w, http.StatusForbidden, httpjson.CodeForbidden, "permission denied: "+string(authz.ContainersWrite))
			return
		}
		ns, err := Namespace(r)
		if err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}

		ctx, cancel := runtime.ActionContext(r.Context())
		defer cancel()
		containers := EnvFrom(r.Context()).Runtime.Containers()
		opts := runtime.ActionOptions{Force: req.Force, Volumes: req.Volumes}
		results := make([]BulkActionResult, len(req.IDs))
		sem := make(chan struct{}, bulkConcurrency)
		var wg sync.WaitGroup
		for i, id := range req.IDs {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				results[i] = BulkActionResult{ID: id, OK: true}
				if err := containers.Do(ctx, ns, id, req.Action, opts); err != nil {
					_, code := httpjson.Status(err)
					h.logActionErr(r, err)
					results[i] = BulkActionResult{ID: id, Error: &httpjson.ErrorDetail{Code: code, Message: err.Error()}}
				}
			}()
		}
		wg.Wait()
		httpjson.Write(w, http.StatusOK, map[string]any{"action": req.Action, "results": results})
	}
}

// writeActionErr answers with the error class from httpjson.Status. Unlike WriteErr it keeps
// the message of unclassified errors: the operator asked for the action and needs nerdctl's
// reason (#11). Such errors are also logged.
func (h *Handler) writeActionErr(w http.ResponseWriter, r *http.Request, err error) {
	status, code := httpjson.Status(err)
	h.logActionErr(r, err)
	httpjson.WriteError(w, status, code, err.Error())
}

func (h *Handler) logActionErr(r *http.Request, err error) {
	if status, _ := httpjson.Status(err); status == http.StatusInternalServerError && h.Logger != nil {
		h.Logger.Error("container action failed", "method", r.Method, "path", r.URL.Path, "error", err)
	}
}

func boolParam(v string) (bool, error) {
	if v == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid boolean %q", v)
	}
	return b, nil
}
