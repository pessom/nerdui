// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/stacks"
)

// getStackTree lists the files of a stack's directory (#27, ADR-0055):
//
//	GET /api/v1/environments/{env}/stacks/{name}/tree
func (h *Handler) getStackTree(w http.ResponseWriter, r *http.Request) {
	def, err := h.findStack(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	t, err := stacks.ReadTree(def)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, t)
}

// getStackPath reads a file of a stack's directory by its path (#27):
//
//	GET /api/v1/environments/{env}/stacks/{name}/tree/file?path=
func (h *Handler) getStackPath(w http.ResponseWriter, r *http.Request) {
	def, err := h.findStack(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	f, err := stacks.ReadPath(def, r.URL.Query().Get("path"))
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, f)
}

// StackPathSaved is the result of saving a file by its path: the file, and whether the project
// still loads with it. Keep in sync with api/openapi.yaml.
type StackPathSaved struct {
	File       stacks.PathFile `json:"file"`
	Validation StackValidation `json:"validation"`
}

// putStackPath creates or replaces a text file of a stack's directory (#27). version is the
// version that was loaded, empty for a new file. The compose file and .env are checked before
// they are saved and keep a backup, as with PUT …/files/{file}; other files (override files,
// configs) are saved, then the project is checked with them.
//
//	PUT /api/v1/environments/{env}/stacks/{name}/tree/file?path=  {"content", "version"}
func (h *Handler) putStackPath(w http.ResponseWriter, r *http.Request) {
	def, err := h.findStack(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	p, err := stacks.CleanPath(r.URL.Query().Get("path"))
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	var req struct {
		Content string `json:"content"`
		Version string `json:"version"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	env := EnvFrom(r.Context())
	if err := h.stackBusy(env.ID, def.Name); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	kind := ""
	switch p {
	case def.File:
		kind = stacks.FileCompose
	case ".env":
		kind = stacks.FileEnv
	}
	var out StackPathSaved
	if kind != "" {
		var compose, dotenv []byte
		if kind == stacks.FileCompose {
			compose = []byte(req.Content)
		} else {
			dotenv = []byte(req.Content)
		}
		if def.File != "" {
			if v := h.validateContent(r.Context(), env, def, compose, dotenv); !v.Valid {
				httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: %s", errdefs.ErrInvalidArgument, v.Error))
				return
			}
		}
		f, err := stacks.WriteFile(def, kind, []byte(req.Content), req.Version)
		if err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}
		out.File = stacks.PathFile{Path: p, Content: f.Content, Version: f.Version, Exists: true, Size: int64(len(f.Content))}
	} else {
		f, err := stacks.WritePath(def, p, []byte(req.Content), req.Version)
		if err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}
		out.File = f
	}
	out.Validation = StackValidation{Valid: true, Services: []string{}}
	if def.File != "" {
		out.Validation = h.validateContent(r.Context(), env, def, nil, nil)
	}
	h.log().Info("stack file saved", "environment", env.ID, "stack", def.Name, "file", p)
	httpjson.Write(w, http.StatusOK, out)
}

// deleteStackPath removes a file, or an empty directory, of a stack's directory (#27). version
// is the version of the file that was loaded; the compose file cannot be removed.
//
//	DELETE /api/v1/environments/{env}/stacks/{name}/tree/file?path=&version=
func (h *Handler) deleteStackPath(w http.ResponseWriter, r *http.Request) {
	def, err := h.findStack(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	env := EnvFrom(r.Context())
	if err := h.stackBusy(env.ID, def.Name); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	q := r.URL.Query()
	if err := stacks.DeletePath(def, q.Get("path"), q.Get("version")); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("stack file deleted", "environment", env.ID, "stack", def.Name, "file", q.Get("path"))
	w.WriteHeader(http.StatusNoContent)
}
