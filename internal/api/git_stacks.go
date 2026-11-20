// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/auth"
	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/gitstacks"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/stacks"
)

// Git stacks (#37) and Git credentials (#93, ADR-0039).

// gitRoutes are the endpoints outside an environment: credentials and known SSH hosts.
func (h *Handler) gitRoutes(r chi.Router, g *authz.Guard) {
	// Stack authors pick a credential by name; secrets are never returned.
	r.Method(http.MethodGet, "/git/credentials", g.Protect(authz.StacksRead, h.listGitCredentials))
	r.Method(http.MethodPost, "/git/credentials", g.Protect(authz.SettingsWrite, h.createGitCredential))
	r.Method(http.MethodPut, "/git/credentials/{id}", g.Protect(authz.SettingsWrite, h.updateGitCredential))
	r.Method(http.MethodDelete, "/git/credentials/{id}", g.Protect(authz.SettingsWrite, h.deleteGitCredential))
	r.Method(http.MethodGet, "/git/known-hosts", g.Protect(authz.SettingsRead, h.listGitKnownHosts))
	r.Method(http.MethodDelete, "/git/known-hosts", g.Protect(authz.SettingsWrite, h.forgetGitKnownHost))
}

// gitStackRoutes are the environment endpoints of Git stacks; they deploy, so they need a
// writable environment.
func (h *Handler) gitStackRoutes(r chi.Router, g *authz.Guard) {
	r.Method(http.MethodPost, "/stacks/git", g.Protect(authz.StacksWrite, h.createGitStack(g)))
	r.Method(http.MethodPost, "/stacks/{name}/git/pull", g.Protect(authz.StacksDeploy, h.pullGitStack))
	r.Method(http.MethodDelete, "/stacks/{name}/git", g.Protect(authz.StacksWrite, h.removeGitStack))
}

func (h *Handler) gitDisabled(w http.ResponseWriter) bool {
	if h.GitStacks == nil {
		httpjson.WriteError(w, http.StatusNotImplemented, httpjson.CodeUnavailable, "Git stacks are not available")
		return true
	}
	return false
}

func principalName(r *http.Request) string {
	if p, ok := auth.PrincipalFrom(r.Context()); ok {
		return p.User.Username
	}
	return ""
}

// GET /api/v1/git/credentials
func (h *Handler) listGitCredentials(w http.ResponseWriter, r *http.Request) {
	if h.gitDisabled(w) {
		return
	}
	list, err := h.GitStacks.Credentials(r.Context())
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"credentials": list})
}

// POST /api/v1/git/credentials  {"name", "kind": "token"|"ssh", "username", "secret", "passphrase", "generate"}
func (h *Handler) createGitCredential(w http.ResponseWriter, r *http.Request) {
	if h.gitDisabled(w) {
		return
	}
	var in gitstacks.CredentialInput
	if !httpjson.Decode(w, r, &in) {
		return
	}
	v, err := h.GitStacks.CreateCredential(r.Context(), in)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("git credential created", "name", v.Name, "kind", v.Kind, "user", principalName(r))
	httpjson.Write(w, http.StatusCreated, v)
}

func gitCredentialID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: Git credential %q not found", errdefs.ErrNotFound, chi.URLParam(r, "id"))
	}
	return id, nil
}

// PUT /api/v1/git/credentials/{id}; an empty secret keeps the stored one.
func (h *Handler) updateGitCredential(w http.ResponseWriter, r *http.Request) {
	if h.gitDisabled(w) {
		return
	}
	id, err := gitCredentialID(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	var in gitstacks.CredentialInput
	if !httpjson.Decode(w, r, &in) {
		return
	}
	v, err := h.GitStacks.UpdateCredential(r.Context(), id, in)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("git credential updated", "name", v.Name, "kind", v.Kind, "user", principalName(r))
	httpjson.Write(w, http.StatusOK, v)
}

// DELETE /api/v1/git/credentials/{id}; refused while a stack uses it (409).
func (h *Handler) deleteGitCredential(w http.ResponseWriter, r *http.Request) {
	if h.gitDisabled(w) {
		return
	}
	id, err := gitCredentialID(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if err := h.GitStacks.DeleteCredential(r.Context(), id); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("git credential deleted", "id", id, "user", principalName(r))
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/v1/git/known-hosts
func (h *Handler) listGitKnownHosts(w http.ResponseWriter, r *http.Request) {
	if h.gitDisabled(w) {
		return
	}
	list, err := h.GitStacks.KnownHostList(r.Context())
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"hosts": list})
}

// DELETE /api/v1/git/known-hosts?host=github.com:22 — the next connection trusts the key it sees.
func (h *Handler) forgetGitKnownHost(w http.ResponseWriter, r *http.Request) {
	if h.gitDisabled(w) {
		return
	}
	host := r.URL.Query().Get("host")
	if err := h.GitStacks.ForgetHost(r.Context(), host); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("git host key forgotten", "host", host, "user", principalName(r))
	w.WriteHeader(http.StatusNoContent)
}

// GitStackRequest creates a Git stack; Deploy runs `compose up` after cloning and needs
// stacks:deploy. Keep in sync with api/openapi.yaml.
type GitStackRequest struct {
	gitstacks.CreateRequest
	Deploy bool `json:"deploy"`
}

// createGitStack clones a repository into a new stack as a job (#37):
//
//	POST /api/v1/environments/{env}/stacks/git?namespace=  {"name", "url", "ref", "composePath", "credentialId", "deploy"}
func (h *Handler) createGitStack(g *authz.Guard) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ns, err := Namespace(r)
		if err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}
		if h.gitDisabled(w) {
			return
		}
		var req GitStackRequest
		if !httpjson.Decode(w, r, &req) {
			return
		}
		if req.Deploy && !g.Allowed(r, authz.StacksDeploy) {
			httpjson.WriteError(w, http.StatusForbidden, httpjson.CodeForbidden, "deploying needs the stacks:deploy permission")
			return
		}
		create, err := h.GitStacks.Check(r.Context(), req.CreateRequest)
		if err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}
		env, user, deploy := EnvFrom(r.Context()), principalName(r), req.Deploy
		compose, svc := env.Runtime.Compose(), h.GitStacks
		job, err := h.Jobs.Start(jobs.Spec{
			Kind: "stack.git-clone", Title: "Create stack " + create.Name + " from Git", Environment: env.ID, Namespace: ns,
			Resource: fmt.Sprintf("env/%s/ns/%s/stack/%s", env.ID, ns, create.Name),
			Action:   string(authz.StacksWrite), User: user,
		}, func(ctx context.Context, out *jobs.Output) error {
			ref := create.Ref
			if ref == "" {
				ref = "default branch"
			}
			_, _ = fmt.Fprintf(out, "Cloning %s (%s)\r\n", create.URL, ref)
			def, err := svc.Create(ctx, create)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(out, "Checked out %s %s\r\nCompose file: %s\r\n",
				gitstacks.Short(def.Git.Commit.Hash), def.Git.Commit.Message, def.Git.ComposePath)
			out.SetResult("stack", def.Name)
			if !deploy {
				return nil
			}
			_, _ = fmt.Fprintf(out, "\r\nDeploying\r\n")
			return compose.Run(ctx, ns, def.Name, def.Dir, runtime.ComposeUp, runtime.ComposeOptions{}, out)
		})
		if err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}
		h.log().Info("git stack creation started", "environment", env.ID, "namespace", ns, "stack", create.Name,
			"url", create.URL, "ref", create.Ref, "deploy", deploy, "user", user)
		httpjson.Write(w, http.StatusAccepted, map[string]any{"job": job})
	}
}

// pullGitStack pulls the newest commit of a Git stack's ref as a job, then redeploys when
// asked and the commit changed (or always with force):
//
//	POST /api/v1/environments/{env}/stacks/{name}/git/pull?namespace=  {"redeploy", "force", "pull"}
func (h *Handler) pullGitStack(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if h.gitDisabled(w) {
		return
	}
	var req struct {
		Redeploy bool `json:"redeploy"`
		// Force redeploys even when nothing changed; Pull also pulls images.
		Force bool `json:"force"`
		Pull  bool `json:"pull"`
	}
	if r.ContentLength != 0 && !httpjson.Decode(w, r, &req) {
		return
	}
	name := chi.URLParam(r, "name")
	if _, err := h.GitStacks.Get(r.Context(), name); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	env := EnvFrom(r.Context())
	if err := h.stackBusy(env.ID, name); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	user, compose, svc := principalName(r), env.Runtime.Compose(), h.GitStacks
	title := "Pull stack " + name + " from Git"
	if req.Redeploy {
		title = "Pull and redeploy stack " + name
	}
	job, err := h.Jobs.Start(jobs.Spec{
		Kind: "stack.git-pull", Title: title, Environment: env.ID, Namespace: ns,
		Resource: fmt.Sprintf("env/%s/ns/%s/stack/%s", env.ID, ns, name),
		Action:   string(authz.StacksDeploy), User: user,
	}, func(ctx context.Context, out *jobs.Output) error {
		commit, err := svc.PullAndDeploy(ctx, out, name, req.Redeploy, req.Force, func(ctx context.Context, def stacks.Definition) error {
			_, _ = fmt.Fprintf(out, "\r\nDeploying\r\n")
			return compose.Run(ctx, ns, name, def.Dir, runtime.ComposeUp, runtime.ComposeOptions{Pull: req.Pull, RemoveOrphans: true}, out)
		})
		out.SetResult("commit", commit.Hash)
		return err
	})
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("git stack pull started", "environment", env.ID, "namespace", ns, "stack", name,
		"redeploy", req.Redeploy, "user", user)
	httpjson.Write(w, http.StatusAccepted, map[string]any{"job": job})
}

// removeGitStack forgets a Git stack and deletes its clone, .env included. It is refused while
// the stack has containers in the namespace: take it down first.
//
//	DELETE /api/v1/environments/{env}/stacks/{name}/git?namespace=&confirm=<name>
func (h *Handler) removeGitStack(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if h.gitDisabled(w) {
		return
	}
	name := chi.URLParam(r, "name")
	if r.URL.Query().Get("confirm") != name {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: removing deletes the clone and its .env; confirm with the stack name", errdefs.ErrInvalidArgument))
		return
	}
	if _, err := h.GitStacks.Get(r.Context(), name); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	env := EnvFrom(r.Context())
	if err := h.stackBusy(env.ID, name); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	containers, err := env.Runtime.Containers().List(r.Context(), ns)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	for _, c := range containers {
		if c.Compose != nil && c.Compose.Project == name {
			httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: stack %q still has containers; take it down first", errdefs.ErrFailedPrecondition, name))
			return
		}
	}
	if err := h.GitStacks.Remove(r.Context(), name); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("git stack removed", "environment", env.ID, "stack", name, "user", principalName(r))
	w.WriteHeader(http.StatusNoContent)
}
