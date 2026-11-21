// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package webhooks runs actions on stacks and containers when a secret URL is called (#85, #91,
// ADR-0041), e.g. from CI after a new image was published.
package webhooks

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/containerd/containerd/v2/pkg/identifiers"
	"github.com/containerd/errdefs"
	nerdctlids "github.com/containerd/nerdctl/v2/pkg/identifiers"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/stacks"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

// Target kinds and actions.
const (
	KindStack     = "stack"
	KindContainer = "container"

	// ActionRedeploy pulls images and recreates: compose pull + up for a stack, pull and
	// recreate with the same settings for a container.
	ActionRedeploy = "redeploy"
	// ActionRestart restarts as is.
	ActionRestart = "restart"
)

// Store is the database of webhooks; *db.Queries of the write connection implements it.
type Store interface {
	ListWebhooks(ctx context.Context) ([]db.Webhook, error)
	GetWebhook(ctx context.Context, id int64) (db.Webhook, error)
	GetWebhookByToken(ctx context.Context, tokenHash string) (db.Webhook, error)
	CreateWebhook(ctx context.Context, arg db.CreateWebhookParams) (db.Webhook, error)
	UpdateWebhook(ctx context.Context, arg db.UpdateWebhookParams) (db.Webhook, error)
	SetWebhookToken(ctx context.Context, arg db.SetWebhookTokenParams) (int64, error)
	SetWebhookCalled(ctx context.Context, arg db.SetWebhookCalledParams) error
	DeleteWebhook(ctx context.Context, id int64) (int64, error)
}

// JobRunner starts jobs; *jobs.Manager implements it.
type JobRunner interface {
	Start(spec jobs.Spec, fn jobs.Func) (jobs.Job, error)
}

// Environments finds environments; *environment.Registry implements it.
type Environments interface {
	Get(id string) (*environment.Environment, bool)
}

// StackFinder finds the compose file of a stack; *stacks.Service implements it.
type StackFinder interface {
	Find(ctx context.Context, name string) (stacks.Definition, error)
}

// Service manages webhooks and runs them.
type Service struct {
	Store        Store
	Environments Environments
	Stacks       StackFinder // nil: stack webhooks fail
	Jobs         JobRunner
	Now          func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Webhook is a webhook without its token. Keep in sync with api/openapi.yaml.
type Webhook struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	Environment  string     `json:"environment"`
	Namespace    string     `json:"namespace"`
	TargetKind   string     `json:"targetKind"`
	Target       string     `json:"target"`
	Action       string     `json:"action"`
	Enabled      bool       `json:"enabled"`
	CreatedBy    string     `json:"createdBy,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	LastCalledAt *time.Time `json:"lastCalledAt,omitempty"`
	// LastJob is the job of the newest call; LastError why it could not start.
	LastJob   string `json:"lastJob,omitempty"`
	LastError string `json:"lastError,omitempty"`
}

func view(r db.Webhook) Webhook {
	w := Webhook{
		ID: r.ID, Name: r.Name, Environment: r.Environment, Namespace: r.Namespace, TargetKind: r.TargetKind,
		Target: r.Target, Action: r.Action, Enabled: r.Enabled != 0, CreatedBy: r.CreatedBy,
		CreatedAt: time.UnixMilli(r.CreatedAt).UTC(), LastJob: r.LastJob, LastError: r.LastError,
	}
	if r.LastCalledAt > 0 {
		t := time.UnixMilli(r.LastCalledAt).UTC()
		w.LastCalledAt = &t
	}
	return w
}

// Input creates or changes a webhook. Keep in sync with api/openapi.yaml.
type Input struct {
	Name        string `json:"name"`
	Environment string `json:"environment"`
	Namespace   string `json:"namespace"`
	TargetKind  string `json:"targetKind"`
	Target      string `json:"target"`
	Action      string `json:"action"`
	Enabled     bool   `json:"enabled"`
}

func bad(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errdefs.ErrInvalidArgument, fmt.Sprintf(format, a...))
}

func (s *Service) validate(in *Input) error {
	in.Name, in.Target = strings.TrimSpace(in.Name), strings.TrimSpace(in.Target)
	if in.Name == "" || len(in.Name) > 100 {
		return bad("name must be 1 to 100 characters")
	}
	if _, ok := s.Environments.Get(in.Environment); !ok {
		return bad("unknown environment %q", in.Environment)
	}
	if err := identifiers.Validate(in.Namespace); err != nil {
		return fmt.Errorf("namespace %q: %w", in.Namespace, err)
	}
	switch in.TargetKind {
	case KindStack:
		if err := nerdctlids.ValidateDockerCompat(in.Target); err != nil {
			return bad("stack name: %v", err)
		}
	case KindContainer:
		if err := nerdctlids.ValidateDockerCompat(in.Target); err != nil {
			return bad("container name: %v", err)
		}
	default:
		return bad("targetKind must be %s or %s", KindStack, KindContainer)
	}
	if in.Action != ActionRedeploy && in.Action != ActionRestart {
		return bad("action must be %s or %s", ActionRedeploy, ActionRestart)
	}
	return nil
}

// Path is the URL path of a token.
func Path(token string) string { return "/api/v1/hooks/" + token }

func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// List lists the webhooks.
func (s *Service) List(ctx context.Context) ([]Webhook, error) {
	rows, err := s.Store.ListWebhooks(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Webhook, 0, len(rows))
	for _, r := range rows {
		out = append(out, view(r))
	}
	return out, nil
}

func enabled(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// Create saves a webhook and returns it with its token, which is not stored.
func (s *Service) Create(ctx context.Context, in Input, user string) (Webhook, string, error) {
	if err := s.validate(&in); err != nil {
		return Webhook{}, "", err
	}
	token, err := newToken()
	if err != nil {
		return Webhook{}, "", err
	}
	row, err := s.Store.CreateWebhook(ctx, db.CreateWebhookParams{
		Name: in.Name, Environment: in.Environment, Namespace: in.Namespace, TargetKind: in.TargetKind,
		Target: in.Target, Action: in.Action, TokenHash: hash(token), Enabled: enabled(in.Enabled),
		CreatedBy: user, Now: s.now().UnixMilli(),
	})
	if store.IsUniqueViolation(err) {
		return Webhook{}, "", fmt.Errorf("%w: a webhook named %q exists", errdefs.ErrAlreadyExists, in.Name)
	}
	if err != nil {
		return Webhook{}, "", err
	}
	return view(row), token, nil
}

func notFound(id int64) error { return fmt.Errorf("%w: webhook %d not found", errdefs.ErrNotFound, id) }

// Update changes a webhook; its token stays.
func (s *Service) Update(ctx context.Context, id int64, in Input) (Webhook, error) {
	if err := s.validate(&in); err != nil {
		return Webhook{}, err
	}
	row, err := s.Store.UpdateWebhook(ctx, db.UpdateWebhookParams{
		ID: id, Name: in.Name, Environment: in.Environment, Namespace: in.Namespace, TargetKind: in.TargetKind,
		Target: in.Target, Action: in.Action, Enabled: enabled(in.Enabled), Now: s.now().UnixMilli(),
	})
	switch {
	case store.IsNotFound(err):
		return Webhook{}, notFound(id)
	case store.IsUniqueViolation(err):
		return Webhook{}, fmt.Errorf("%w: a webhook named %q exists", errdefs.ErrAlreadyExists, in.Name)
	case err != nil:
		return Webhook{}, err
	}
	return view(row), nil
}

// NewToken replaces the token of a webhook; the previous URL stops working.
func (s *Service) NewToken(ctx context.Context, id int64) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	n, err := s.Store.SetWebhookToken(ctx, db.SetWebhookTokenParams{ID: id, TokenHash: hash(token), Now: s.now().UnixMilli()})
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", notFound(id)
	}
	return token, nil
}

// Delete removes a webhook.
func (s *Service) Delete(ctx context.Context, id int64) error {
	n, err := s.Store.DeleteWebhook(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return notFound(id)
	}
	return nil
}

// ErrReadOnly is returned when the webhook's environment cannot be changed (ADR-0011).
var ErrReadOnly = fmt.Errorf("%w: the environment is in read-only mode", errdefs.ErrUnavailable)

// Call runs the webhook of a token as a job. Unknown and disabled tokens are not found.
func (s *Service) Call(ctx context.Context, token string) (Webhook, jobs.Job, error) {
	if token == "" || len(token) > 100 {
		return Webhook{}, jobs.Job{}, fmt.Errorf("%w: unknown webhook", errdefs.ErrNotFound)
	}
	row, err := s.Store.GetWebhookByToken(ctx, hash(token))
	if store.IsNotFound(err) || err == nil && row.Enabled == 0 {
		return Webhook{}, jobs.Job{}, fmt.Errorf("%w: unknown webhook", errdefs.ErrNotFound)
	}
	if err != nil {
		return Webhook{}, jobs.Job{}, err
	}
	job, err := s.start(ctx, row)
	called := db.SetWebhookCalledParams{ID: row.ID, Now: s.now().UnixMilli(), LastJob: job.ID}
	if err != nil {
		called.LastError = err.Error()
	}
	if serr := s.Store.SetWebhookCalled(context.WithoutCancel(ctx), called); serr != nil && err == nil {
		err = serr
	}
	return view(row), job, err
}

func (s *Service) start(ctx context.Context, w db.Webhook) (jobs.Job, error) {
	env, ok := s.Environments.Get(w.Environment)
	if !ok {
		return jobs.Job{}, fmt.Errorf("%w: environment %q is gone", errdefs.ErrFailedPrecondition, w.Environment)
	}
	if !env.Runtime.System().Nerdctl(ctx).Writable() {
		return jobs.Job{}, ErrReadOnly
	}
	ns, name := w.Namespace, w.Target
	spec := jobs.Spec{
		Environment: env.ID, Namespace: ns, User: "webhook " + w.Name,
		Resource: fmt.Sprintf("env/%s/ns/%s/%s/%s", env.ID, ns, w.TargetKind, name),
	}
	var fn jobs.Func
	switch w.TargetKind {
	case KindStack:
		if s.Stacks == nil {
			return jobs.Job{}, fmt.Errorf("%w: stacks are not available", errdefs.ErrNotImplemented)
		}
		def, err := s.Stacks.Find(ctx, name)
		if err != nil {
			return jobs.Job{}, err
		}
		if def.File == "" {
			return jobs.Job{}, fmt.Errorf("%w: stack %q has no compose file", errdefs.ErrFailedPrecondition, name)
		}
		rt, compose, dir := env.Runtime, env.Runtime.Compose(), def.Dir
		spec.Action = string(authz.StacksDeploy)
		if w.Action == ActionRestart {
			spec.Kind, spec.Title = "stack.restart", "Restart stack "+name+" (webhook "+w.Name+")"
			fn = func(ctx context.Context, out *jobs.Output) error {
				return compose.Run(ctx, ns, name, dir, runtime.ComposeRestart, runtime.ComposeOptions{}, out)
			}
		} else {
			spec.Kind, spec.Title = "stack.up", "Redeploy stack "+name+" (webhook "+w.Name+")"
			fn = func(ctx context.Context, out *jobs.Output) error {
				return runtime.Redeploy(ctx, rt, ns, name, dir, runtime.RedeployOptions{}, out)
			}
		}
	case KindContainer:
		containers := env.Runtime.Containers()
		id, err := containerID(ctx, containers, ns, name)
		if err != nil {
			return jobs.Job{}, err
		}
		spec.Action = string(authz.ContainersWrite)
		if w.Action == ActionRestart {
			spec.Kind, spec.Title = "container.restart", "Restart container "+name+" (webhook "+w.Name+")"
			fn = func(ctx context.Context, _ *jobs.Output) error {
				return containers.Do(ctx, ns, id, runtime.ActionRestart, runtime.ActionOptions{})
			}
		} else {
			spec.Kind, spec.Title = "container.recreate", "Redeploy container "+name+" (webhook "+w.Name+")"
			fn = func(ctx context.Context, out *jobs.Output) error {
				cur, err := containers.Spec(ctx, ns, id)
				if err != nil {
					return err
				}
				// Like automatic updates: never drop settings silently.
				if len(cur.Unsupported) > 0 {
					return fmt.Errorf("%w: recreating would lose settings: %s", errdefs.ErrFailedPrecondition, strings.Join(cur.Unsupported, ", "))
				}
				newID, err := containers.Recreate(ctx, ns, id, cur.Spec, runtime.RecreateOptions{Pull: true}, out)
				if newID != "" {
					out.SetResult("containerId", newID)
				}
				return err
			}
		}
	default:
		return jobs.Job{}, fmt.Errorf("%w: unknown target kind %q", errdefs.ErrFailedPrecondition, w.TargetKind)
	}
	return s.Jobs.Start(spec, fn)
}

// containerID finds a container by name: recreating changes its ID, never its name.
func containerID(ctx context.Context, c runtime.Containers, ns, name string) (string, error) {
	list, err := c.List(ctx, ns)
	if err != nil {
		return "", err
	}
	for _, ct := range list {
		if ct.Name == name {
			return ct.ID, nil
		}
	}
	return "", fmt.Errorf("%w: no container %q in namespace %s", errdefs.ErrFailedPrecondition, name, ns)
}
