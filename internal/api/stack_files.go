// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	composeschema "github.com/compose-spec/compose-go/v2/schema"
	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/stacks"
)

// StackValidation is the result of checking compose and .env content (#26). Line is the line of
// the error in the compose file when the loader reports it. Keep in sync with api/openapi.yaml.
type StackValidation struct {
	Valid    bool     `json:"valid"`
	Error    string   `json:"error,omitempty"`
	Line     int      `json:"line,omitempty"`
	Services []string `json:"services"`
}

var errLine = regexp.MustCompile(`\bline (\d+)\b`)

// validateContent loads the stack with the given content (nil: the file on disk).
func (h *Handler) validateContent(ctx context.Context, env *environment.Environment, def stacks.Definition, compose, dotenv []byte) StackValidation {
	svcs, err := env.Runtime.Compose().Validate(ctx, def.Name, def.Dir, def.File, compose, dotenv)
	if err != nil {
		v := StackValidation{Error: strings.TrimPrefix(err.Error(), errdefs.ErrInvalidArgument.Error()+": "), Services: []string{}}
		if m := errLine.FindStringSubmatch(v.Error); m != nil {
			v.Line, _ = strconv.Atoi(m[1])
		}
		return v
	}
	if svcs == nil {
		svcs = []string{}
	}
	return StackValidation{Valid: true, Services: svcs}
}

// stackBusy rejects edits while an operation of the stack runs in any namespace (ADR-0008).
func (h *Handler) stackBusy(env, name string) error {
	prefix, suffix := "env/"+env+"/ns/", "/stack/"+name
	if j, ok := h.Jobs.Running(func(res string) bool {
		return strings.HasPrefix(res, prefix) && strings.HasSuffix(res, suffix)
	}); ok {
		return fmt.Errorf("%w: %q is running for this stack; save when it is done", errdefs.ErrConflict, j.Title)
	}
	return nil
}

func (h *Handler) findStack(r *http.Request) (stacks.Definition, error) {
	if h.Stacks == nil {
		return stacks.Definition{}, fmt.Errorf("%w: stacks are not available", errdefs.ErrNotImplemented)
	}
	return h.Stacks.Find(r.Context(), chi.URLParam(r, "name"))
}

// getStackFile returns the compose or .env file of a stack (#26):
//
//	GET /api/v1/environments/{env}/stacks/{name}/files/{file}   file: compose | env
func (h *Handler) getStackFile(w http.ResponseWriter, r *http.Request) {
	def, err := h.findStack(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	f, err := stacks.ReadFile(def, chi.URLParam(r, "file"))
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, f)
}

// putStackFile saves the compose or .env file of a stack after validating the project with it
// (#26). version is the version that was loaded (empty for a new .env); a different version on
// disk is a conflict. The previous content is kept as <file>.bak.
//
//	PUT /api/v1/environments/{env}/stacks/{name}/files/{file}  {"content", "version"}
func (h *Handler) putStackFile(w http.ResponseWriter, r *http.Request) {
	def, err := h.findStack(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	kind := chi.URLParam(r, "file")
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
	var compose, dotenv []byte
	switch kind {
	case stacks.FileCompose:
		compose = []byte(req.Content)
	case stacks.FileEnv:
		dotenv = []byte(req.Content)
	}
	if _, _, err := stacks.FilePath(def, kind); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if v := h.validateContent(r.Context(), env, def, compose, dotenv); !v.Valid {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: %s", errdefs.ErrInvalidArgument, v.Error))
		return
	}
	f, err := stacks.WriteFile(def, kind, []byte(req.Content), req.Version)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("stack file saved", "environment", env.ID, "stack", def.Name, "file", f.Name)
	httpjson.Write(w, http.StatusOK, f)
}

// validateStack checks compose and .env content against the stack's directory without saving
// (#26); content left out is read from disk:
//
//	POST /api/v1/environments/{env}/stacks/{name}/validate  {"compose"?, "env"?}
func (h *Handler) validateStack(w http.ResponseWriter, r *http.Request) {
	def, err := h.findStack(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if def.File == "" {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: no compose file in %s", errdefs.ErrFailedPrecondition, def.Dir))
		return
	}
	var req struct {
		Compose *string `json:"compose"`
		Env     *string `json:"env"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	var compose, dotenv []byte
	if req.Compose != nil {
		compose = []byte(*req.Compose)
	}
	if req.Env != nil {
		dotenv = []byte(*req.Env)
	}
	httpjson.Write(w, http.StatusOK, h.validateContent(r.Context(), EnvFrom(r.Context()), def, compose, dotenv))
}

// createStack creates a stack in the stacks directory with a compose file (#26); invalid
// content is refused and nothing is left behind:
//
//	POST /api/v1/environments/{env}/stacks?namespace=  {"name", "compose"}
func (h *Handler) createStack(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if h.Stacks == nil {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: stacks are not available", errdefs.ErrNotImplemented))
		return
	}
	var req struct {
		Name    string `json:"name"`
		Compose string `json:"compose"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Compose) == "" {
		req.Compose = stacks.DefaultCompose
	}
	env := EnvFrom(r.Context())
	def, err := h.Stacks.Create(r.Context(), req.Name, []byte(req.Compose))
	if err != nil {
		h.writeActionErr(w, r, err)
		return
	}
	if v := h.validateContent(r.Context(), env, def, nil, nil); !v.Valid {
		_ = removeNewStack(def)
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: %s", errdefs.ErrInvalidArgument, v.Error))
		return
	}
	h.log().Info("stack created", "environment", env.ID, "stack", def.Name, "path", def.Dir)
	list, err := h.stackList(r.Context(), env, ns)
	if err == nil {
		for _, s := range list {
			if s.Name == def.Name {
				httpjson.Write(w, http.StatusCreated, s)
				return
			}
		}
	}
	httpjson.Write(w, http.StatusCreated, stacks.Summarize(def, nil, nil, nil))
}

// removeNewStack removes a stack directory created by this request: only its compose file.
func removeNewStack(def stacks.Definition) error {
	if err := os.Remove(filepath.Join(def.Dir, def.File)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Remove(def.Dir)
}

// composeSchema serves the Compose JSON schema of the compose-go version this build uses, for
// the editor's completion and checks (#26):
//
//	GET /api/v1/compose-schema
func (h *Handler) composeSchema(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/schema+json")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write([]byte(composeschema.Schema))
}
