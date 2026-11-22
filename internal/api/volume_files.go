// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/httpjson"
)

// GET /api/v1/environments/{env}/volumes/{name}/files?namespace=&path=/
func (h *Handler) listVolumeFiles(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	p := r.URL.Query().Get("path")
	entries, truncated, err := EnvFrom(r.Context()).Runtime.Volumes().ListFiles(ns, chi.URLParam(r, "name"), p)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"entries": entries, "truncated": truncated})
}

// GET /api/v1/environments/{env}/volumes/{name}/files/preview?namespace=&path=
func (h *Handler) previewVolumeFile(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	f, err := EnvFrom(r.Context()).Runtime.Volumes().PreviewFile(ns, chi.URLParam(r, "name"), r.URL.Query().Get("path"))
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, f)
}

// GET /api/v1/environments/{env}/volumes/{name}/files/download?namespace=&path=
// A file as is, a directory (or the volume, path=/) as a .tar.gz archive.
func (h *Handler) downloadVolumeFile(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	d, err := EnvFrom(r.Context()).Runtime.Volumes().Download(ns, chi.URLParam(r, "name"), r.URL.Query().Get("path"))
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	defer d.Close() //nolint:errcheck // read-only
	ct := "application/octet-stream"
	if d.Size < 0 {
		ct = "application/gzip"
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(d.Size, 10))
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": d.Name}))
	w.Header().Set("Cache-Control", "no-store")
	if err := d.Write(w); err != nil {
		h.log().Warn("download from volume", "volume", chi.URLParam(r, "name"), "error", fmt.Sprint(err))
		// The download has started: abort so the client sees a failure, not a short file.
		panic(http.ErrAbortHandler)
	}
}
