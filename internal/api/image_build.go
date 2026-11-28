// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/auth"
	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/buildctx"
	"github.com/pessom/nerdui/internal/gitrepo"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
)

// Sources of an image build.
const (
	BuildFromDockerfile = "dockerfile"
	BuildFromGit        = "git"
	BuildFromArchive    = "archive"
)

// BuildRequest builds an image (#45, ADR-0061). Keep in sync with api/openapi.yaml.
type BuildRequest struct {
	// Source is dockerfile, git or archive.
	Source string `json:"source"`
	// Dockerfile is the text of the Dockerfile (dockerfile); it builds with an empty context.
	Dockerfile string `json:"dockerfile,omitempty"`
	// URL, Ref and CredentialID select a Git repository (git), as for Git stacks.
	URL          string `json:"url,omitempty"`
	Ref          string `json:"ref,omitempty"`
	CredentialID *int64 `json:"credentialId,omitempty"`
	// Context is the directory of the context inside the repository (git); empty is its root.
	Context string `json:"context,omitempty"`
	// File is the Dockerfile's path inside the context (git, archive); empty is "Dockerfile".
	File     string            `json:"file,omitempty"`
	Tags     []string          `json:"tags"`
	Args     map[string]string `json:"args,omitempty"`
	Target   string            `json:"target,omitempty"`
	Platform string            `json:"platform,omitempty"`
	NoCache  bool              `json:"noCache,omitempty"`
	Pull     bool              `json:"pull,omitempty"`
	// Push pushes every tag after the build.
	Push bool `json:"push,omitempty"`
}

func (b BuildRequest) options() runtime.BuildOptions {
	file := b.File
	if b.Source == BuildFromDockerfile {
		file = ""
	}
	return runtime.BuildOptions{
		Tags: b.Tags, Dockerfile: file, Target: b.Target, Args: b.Args,
		NoCache: b.NoCache, Pull: b.Pull, Platform: b.Platform,
	}
}

// validate checks the request before a job starts, so mistakes answer 400 at once.
func (b BuildRequest) validate(gitEnabled bool) error {
	if _, err := runtime.BuildArgs(string(filepath.Separator), b.options()); err != nil {
		return err
	}
	switch b.Source {
	case BuildFromDockerfile:
		if strings.TrimSpace(b.Dockerfile) == "" {
			return fmt.Errorf("%w: the Dockerfile is empty", errdefs.ErrInvalidArgument)
		}
		if len(b.Dockerfile) > buildctx.MaxDockerfile {
			return fmt.Errorf("%w: the Dockerfile is too large", errdefs.ErrInvalidArgument)
		}
	case BuildFromGit:
		if !gitEnabled {
			return fmt.Errorf("%w: Git is not available", errdefs.ErrNotImplemented)
		}
		if err := gitrepo.ValidateURL(b.URL, false); err != nil {
			return err
		}
		if err := gitrepo.ValidateRef(b.Ref); err != nil {
			return err
		}
		if b.Context != "" && !filepath.IsLocal(b.Context) {
			return fmt.Errorf("%w: the context must be a directory inside the repository", errdefs.ErrInvalidArgument)
		}
	case BuildFromArchive:
	default:
		return fmt.Errorf("%w: source must be dockerfile, git or archive", errdefs.ErrInvalidArgument)
	}
	return nil
}

// builderStatus tells whether images can be built (#45):
//
//	GET /api/v1/environments/{env}/images/builder?namespace=
func (h *Handler) builderStatus(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, EnvFrom(r.Context()).Runtime.Images().BuilderStatus(r.Context(), ns))
}

// buildImage starts an image build job (#45, ADR-0061):
//
//	POST /api/v1/environments/{env}/images/build?namespace=
//
// The body is a BuildRequest in JSON, or for an archive multipart/form-data with the request
// in the part "request" followed by the tar (or tar.gz) context in the part "archive".
func (h *Handler) buildImage(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	dir, err := h.buildDir()
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(dir)
		}
	}()

	var req BuildRequest
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct == "multipart/form-data" {
		if req, err = h.readArchiveBuild(r, dir); err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}
	} else {
		if !httpjson.Decode(w, r, &req) {
			return
		}
		if req.Source == BuildFromArchive {
			httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: an archive is sent as multipart/form-data", errdefs.ErrInvalidArgument))
			return
		}
		if err := req.validate(h.GitStacks != nil); err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}
	}

	env := EnvFrom(r.Context())
	user := ""
	if p, ok := auth.PrincipalFrom(r.Context()); ok {
		user = p.User.Username
	}
	images := env.Runtime.Images()
	job, err := h.Jobs.Start(jobs.Spec{
		Kind:        "image.build",
		Title:       "Build " + req.Tags[0],
		Environment: env.ID,
		Namespace:   ns,
		Resource:    fmt.Sprintf("env/%s/ns/%s/image/%s", env.ID, ns, req.Tags[0]),
		Action:      string(authz.ImagesWrite),
		User:        user,
	}, func(ctx context.Context, out *jobs.Output) error {
		defer func() { _ = os.RemoveAll(dir) }()
		return h.runBuild(ctx, images, ns, dir, req, out)
	})
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	keep = true // the job removes it
	httpjson.Write(w, http.StatusAccepted, map[string]any{"job": job})
}

// buildDir creates a new directory for a build context under BuildDir.
func (h *Handler) buildDir() (string, error) {
	base := h.BuildDir
	if base == "" {
		base = os.TempDir()
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(base, "build-")
}

// readArchiveBuild reads a multipart archive build: the request first, then the archive,
// which is extracted into dir as it arrives.
func (h *Handler) readArchiveBuild(r *http.Request, dir string) (BuildRequest, error) {
	var req BuildRequest
	mr, err := r.MultipartReader()
	if err != nil {
		return req, fmt.Errorf("%w: %w", errdefs.ErrInvalidArgument, err)
	}
	part, err := mr.NextPart()
	if err != nil || part.FormName() != "request" {
		return req, fmt.Errorf("%w: the first part must be \"request\"", errdefs.ErrInvalidArgument)
	}
	if err := json.NewDecoder(io.LimitReader(part, httpjson.MaxBodyBytes)).Decode(&req); err != nil {
		return req, fmt.Errorf("%w: request: %w", errdefs.ErrInvalidArgument, err)
	}
	if req.Source != BuildFromArchive {
		return req, fmt.Errorf("%w: a multipart request builds from an archive", errdefs.ErrInvalidArgument)
	}
	if err := req.validate(h.GitStacks != nil); err != nil {
		return req, err
	}
	part, err = mr.NextPart()
	if err != nil || part.FormName() != "archive" {
		return req, fmt.Errorf("%w: the second part must be \"archive\"", errdefs.ErrInvalidArgument)
	}
	return req, buildctx.Extract(dir, part, 0)
}

// runBuild prepares the context in dir, builds, and pushes the tags when asked.
func (h *Handler) runBuild(ctx context.Context, images runtime.Images, ns, dir string, req BuildRequest, out io.Writer) error {
	rel := "." // the context, relative to dir
	switch req.Source {
	case BuildFromDockerfile:
		if err := buildctx.WriteDockerfile(dir, req.Dockerfile); err != nil {
			return err
		}
	case BuildFromGit:
		_, _ = fmt.Fprintf(out, "\x1b[1mCloning %s\x1b[0m\r\n", req.URL)
		commit, err := h.GitStacks.Clone(ctx, filepath.Join(dir, "repo"), req.URL, req.Ref, req.CredentialID)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "Commit %s %s\r\n", commit.Hash[:min(len(commit.Hash), 12)], firstLine(commit.Message))
		rel = path.Join("repo", filepath.ToSlash(req.Context))
	}
	if err := insideContext(dir, rel, req.options().Dockerfile); err != nil {
		return err
	}
	if err := images.Build(ctx, ns, filepath.Join(dir, filepath.FromSlash(rel)), req.options(), out); err != nil {
		return err
	}
	if req.Push {
		for _, tag := range req.Tags {
			if err := images.Push(ctx, ns, tag, runtime.PushOptions{}, out); err != nil {
				return err
			}
		}
	}
	return nil
}

// insideContext checks, through an os.Root of base, that the context rel is a directory and
// the Dockerfile a regular file in it. The root follows symbolic links only within base, so a
// link cannot make the build read a file of the host.
func insideContext(base, rel, file string) error {
	root, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	fi, err := root.Stat(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: the context directory does not exist", errdefs.ErrInvalidArgument)
	}
	if err != nil || !fi.IsDir() {
		return fmt.Errorf("%w: the context must be a directory inside the source", errdefs.ErrInvalidArgument)
	}
	if file == "" {
		file = "Dockerfile"
	}
	fi, err = root.Stat(path.Join(rel, filepath.ToSlash(file)))
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: no %s in the context", errdefs.ErrInvalidArgument, file)
	}
	if err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: %s must be a file inside the context", errdefs.ErrInvalidArgument, file)
	}
	return nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
