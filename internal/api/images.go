// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"

	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/runtime"
)

// listImages returns the images of a namespace (#42):
//
//	GET /api/v1/environments/{env}/images?namespace=default
func (h *Handler) listImages(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	list, err := EnvFrom(r.Context()).Runtime.Images().List(r.Context(), ns)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"namespace": ns, "images": list})
}

// tagImage adds a reference to an image (#44):
//
//	POST /api/v1/environments/{env}/images/tag?namespace=  {"source": "nginx:1.27", "target": "registry.local/nginx:1.27"}
func (h *Handler) tagImage(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	var req struct {
		Source string `json:"source"`
		Target string `json:"target"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	ctx, cancel := runtime.ActionContext(r.Context())
	defer cancel()
	if err := EnvFrom(r.Context()).Runtime.Images().Tag(ctx, ns, req.Source, req.Target); err != nil {
		h.writeActionErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// maxBulkImages bounds one removal request.
const maxBulkImages = 200

// RemoveImagesResult reports the removal of one reference. Keep in sync with api/openapi.yaml.
type RemoveImagesResult struct {
	Reference string                `json:"reference"`
	OK        bool                  `json:"ok"`
	Error     *httpjson.ErrorDetail `json:"error,omitempty"`
}

// removeImages removes image references one after another, like `nerdctl rmi a b c` (#44):
//
//	POST /api/v1/environments/{env}/images/remove?namespace=  {"references": [...], "force": false}
func (h *Handler) removeImages(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	var req struct {
		References []string `json:"references"`
		Force      bool     `json:"force"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	if len(req.References) == 0 || len(req.References) > maxBulkImages {
		httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument,
			fmt.Sprintf("references must contain 1 to %d images", maxBulkImages))
		return
	}
	ctx, cancel := runtime.ActionContext(r.Context())
	defer cancel()
	images := EnvFrom(r.Context()).Runtime.Images()
	results := make([]RemoveImagesResult, len(req.References))
	for i, ref := range req.References {
		results[i] = RemoveImagesResult{Reference: ref, OK: true}
		if err := images.Remove(ctx, ns, ref, req.Force); err != nil {
			_, code := httpjson.Status(err)
			h.logActionErr(r, err)
			results[i] = RemoveImagesResult{Reference: ref, Error: &httpjson.ErrorDetail{Code: code, Message: err.Error()}}
		}
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"results": results})
}
