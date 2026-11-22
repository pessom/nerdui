// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/pessom/nerdui/internal/httpjson"
)

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// archiveName is the download file name of an image archive, e.g. nginx_1.27.tar.
func archiveName(names []string) string {
	if len(names) != 1 {
		return "images.tar"
	}
	n := names[0]
	n = strings.TrimPrefix(n, "docker.io/")
	n = strings.TrimPrefix(n, "library/")
	if i := strings.LastIndex(n, "/"); i >= 0 {
		n = n[i+1:]
	}
	n = strings.Trim(unsafeFileChars.ReplaceAllString(n, "_"), "._")
	if n == "" || len(n) > 100 {
		return "image.tar"
	}
	return n + ".tar"
}

// GET /api/v1/environments/{env}/images/save?namespace=&ref=a&ref=b  (application/x-tar)
func (h *Handler) saveImages(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	names := r.URL.Query()["ref"]
	images := EnvFrom(r.Context()).Runtime.Images()
	if err := images.CheckSave(r.Context(), ns, names); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", `attachment; filename="`+archiveName(names)+`"`)
	w.Header().Set("Cache-Control", "no-store")
	if err := images.Save(r.Context(), ns, names, w); err != nil {
		h.log().Warn("save images", "images", names, "error", err)
		// The archive has started: abort the connection so the download fails instead of
		// ending with a truncated file.
		panic(http.ErrAbortHandler)
	}
}

// POST /api/v1/environments/{env}/images/load?namespace=  (body: tar archive)
func (h *Handler) loadImages(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	switch ct, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";"); strings.TrimSpace(ct) {
	case "application/x-tar", "application/tar", "application/gzip", "application/x-gzip", "application/octet-stream":
	default:
		httpjson.WriteError(w, http.StatusUnsupportedMediaType, httpjson.CodeUnsupportedType,
			"Content-Type must be application/x-tar, application/gzip or application/octet-stream")
		return
	}
	names, err := EnvFrom(r.Context()).Runtime.Images().Load(r.Context(), ns, r.Body)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if names == nil {
		names = []string{}
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"images": names})
}
