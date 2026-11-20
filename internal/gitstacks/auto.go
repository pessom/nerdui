// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package gitstacks

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/gitrepo"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/notify"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/stacks"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

// Automatic updates of Git stacks (#38, #39, ADR-0040): polling and a webhook find new commits,
// which are announced or pulled and redeployed.

// What happens on a new commit.
const (
	AutoOff    = "off"
	AutoNotify = "notify"
	AutoDeploy = "deploy"
)

// PollIntervals are the allowed polling intervals in seconds; 0 turns polling off.
var PollIntervals = []int64{0, 60, 300, 900, 3600, 6 * 3600, 24 * 3600}

// AutoUser is the user of jobs started by polling and webhooks.
const AutoUser = "nerdui (Git)"

// checkFailed prefixes errors of remote checks: a later successful check clears them, while
// errors of pulls stay until a pull succeeds.
const checkFailed = "Checking the repository failed: "

// SetAuto sets what happens on new commits and how often the remote is polled.
func (s *Service) SetAuto(ctx context.Context, name, mode string, interval int64) error {
	switch mode {
	case AutoOff, AutoNotify, AutoDeploy:
	default:
		return fmt.Errorf("%w: autoUpdate must be %s, %s or %s", errdefs.ErrInvalidArgument, AutoOff, AutoNotify, AutoDeploy)
	}
	if !slices.Contains(PollIntervals, interval) {
		return fmt.Errorf("%w: pollInterval must be one of %v seconds", errdefs.ErrInvalidArgument, PollIntervals)
	}
	n, err := s.Store.SetGitStackAuto(ctx, db.SetGitStackAutoParams{Name: name, AutoUpdate: mode, PollInterval: interval})
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: Git stack %q not found", errdefs.ErrNotFound, name)
	}
	return nil
}

// WebhookPath is the path of the webhook URL of a token.
func WebhookPath(token string) string { return "/api/v1/hooks/git/" + token }

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewWebhook creates (or replaces) the webhook token of a stack. Only its hash is stored: the
// token is returned once.
func (s *Service) NewWebhook(ctx context.Context, name string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	n, err := s.Store.SetGitStackWebhook(ctx, db.SetGitStackWebhookParams{Name: name, WebhookHash: tokenHash(token)})
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", fmt.Errorf("%w: Git stack %q not found", errdefs.ErrNotFound, name)
	}
	return token, nil
}

// DeleteWebhook removes the webhook of a stack.
func (s *Service) DeleteWebhook(ctx context.Context, name string) error {
	n, err := s.Store.SetGitStackWebhook(ctx, db.SetGitStackWebhookParams{Name: name})
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: Git stack %q not found", errdefs.ErrNotFound, name)
	}
	return nil
}

// ByWebhook returns the stack of a webhook token.
func (s *Service) ByWebhook(ctx context.Context, token string) (db.GitStack, error) {
	if token == "" || len(token) > 100 {
		return db.GitStack{}, fmt.Errorf("%w: unknown webhook", errdefs.ErrNotFound)
	}
	row, err := s.Store.GetGitStackByWebhook(ctx, tokenHash(token))
	if store.IsNotFound(err) {
		return row, fmt.Errorf("%w: unknown webhook", errdefs.ErrNotFound)
	}
	return row, err
}

// CheckRemote asks the remote for the newest commit of the stack's ref and records it. It
// returns the stack before and after; after.RemoteHash is set when a newer commit exists.
func (s *Service) CheckRemote(ctx context.Context, name string) (before, after db.GitStack, err error) {
	before, err = s.Get(ctx, name)
	if err != nil {
		return before, before, err
	}
	var cred *int64
	if before.CredentialID.Valid {
		cred = &before.CredentialID.Int64
	}
	o, err := s.options(ctx, before.Url, before.Ref, cred)
	var remote string
	if err == nil {
		remote, err = gitrepo.RemoteHead(ctx, before.FullRef, o)
	}
	if ctx.Err() != nil {
		return before, before, ctx.Err()
	}
	params := db.SetGitStackCheckedParams{Name: name, Now: s.now().UnixMilli(), RemoteHash: before.RemoteHash, Error: before.LastError}
	if err != nil {
		params.Error = checkFailed + err.Error()
	} else {
		params.RemoteHash = remote
		if strings.HasPrefix(before.LastError, checkFailed) {
			params.Error = ""
		}
	}
	if serr := s.Store.SetGitStackChecked(ctx, params); serr != nil {
		return before, before, serr
	}
	if err != nil {
		return before, before, err
	}
	after, err = s.Get(ctx, name)
	return before, after, err
}

// PullAndDeploy pulls a stack and, when redeploy is set and the commit changed (or always with
// force), calls deploy with the new definition. It writes progress to out and returns the
// checked-out commit.
func (s *Service) PullAndDeploy(ctx context.Context, out io.Writer, name string, redeploy, force bool,
	deploy func(context.Context, stacks.Definition) error,
) (gitrepo.Commit, error) {
	before, after, def, err := s.Pull(ctx, name)
	if err != nil {
		return after, err
	}
	changed := before.Hash != after.Hash
	if changed {
		_, _ = fmt.Fprintf(out, "Updated %s → %s %s\r\n", Short(before.Hash), Short(after.Hash), after.Message)
	} else {
		_, _ = fmt.Fprintf(out, "Already up to date at %s %s\r\n", Short(after.Hash), after.Message)
	}
	if !redeploy || !changed && !force {
		return after, nil
	}
	if def.File == "" {
		return after, fmt.Errorf("%w: no compose file in %s", errdefs.ErrFailedPrecondition, def.Dir)
	}
	return after, deploy(ctx, def)
}

// JobRunner starts jobs; *jobs.Manager implements it.
type JobRunner interface {
	Start(spec jobs.Spec, fn jobs.Func) (jobs.Job, error)
}

// Environments lists environments; *environment.Registry implements it.
type Environments interface {
	List() []*environment.Environment
}

// Auto polls the remotes of Git stacks and acts on new commits.
type Auto struct {
	Service      *Service
	Environments Environments
	Jobs         JobRunner
	// Notify, when set, receives new commits (notify mode) and done updates.
	Notify func(notify.Event)
	Logger *slog.Logger
	// Base is the context of checks started by webhooks, canceled on shutdown.
	Base context.Context //nolint:containedctx // background work outlives the webhook request
	// Tick is how often polling looks for due stacks; default 30 seconds.
	Tick time.Duration
}

func (a *Auto) log() *slog.Logger {
	if a.Logger == nil {
		return slog.Default()
	}
	return a.Logger
}

// Run polls due stacks until ctx ends.
func (a *Auto) Run(ctx context.Context) {
	tick := a.Tick
	if tick <= 0 {
		tick = 30 * time.Second
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.Poll(ctx)
		}
	}
}

// Poll checks the stacks whose polling interval elapsed.
func (a *Auto) Poll(ctx context.Context) {
	rows, err := a.Service.Store.ListGitStacks(ctx)
	if err != nil {
		a.log().Warn("git stacks: list", "error", err)
		return
	}
	now := a.Service.now()
	for _, r := range rows {
		if r.AutoUpdate == AutoOff || r.PollInterval <= 0 {
			continue
		}
		if now.Sub(time.UnixMilli(r.CheckedAt)) < time.Duration(r.PollInterval)*time.Second {
			continue
		}
		a.Check(ctx, r.Name)
		if ctx.Err() != nil {
			return
		}
	}
}

// Trigger checks a stack in the background (webhooks).
func (a *Auto) Trigger(name string) {
	ctx := a.Base
	if ctx == nil {
		ctx = context.Background()
	}
	go a.Check(ctx, name)
}

// Check looks for a new commit of a stack and announces or deploys it.
func (a *Auto) Check(ctx context.Context, name string) {
	before, after, err := a.Service.CheckRemote(ctx, name)
	if err != nil {
		if ctx.Err() == nil {
			a.log().Warn("git stack check failed", "stack", name, "error", err)
		}
		return
	}
	if after.RemoteHash == "" {
		return
	}
	switch after.AutoUpdate {
	case AutoNotify:
		if before.RemoteHash != after.RemoteHash && a.Notify != nil {
			a.Notify(notify.Event{
				Kind: notify.KindGitUpdate, Severity: notify.SeverityInfo,
				Title:   "New commit for stack " + name,
				Message: fmt.Sprintf("%s has %s; %s is deployed. Pull it in nerdui.", after.Url, Short(after.RemoteHash), Short(after.CommitHash)),
				Object:  "stack/" + name,
			})
		}
	case AutoDeploy:
		if _, err := a.Deploy(ctx, name); err != nil {
			a.log().Warn("git stack update not started", "stack", name, "error", err)
		}
	}
}

// target is a namespace where a stack's project has containers.
type target struct {
	env *environment.Environment
	ns  string
}

// targets finds the writable environments and namespaces where the project has containers:
// they are redeployed. A stack without containers is only pulled.
func (a *Auto) targets(ctx context.Context, name string) []target {
	if a.Environments == nil {
		return nil
	}
	var out []target
	for _, env := range a.Environments.List() {
		if !env.Runtime.System().Nerdctl(ctx).Writable() {
			continue
		}
		namespaces, err := env.Runtime.System().Namespaces(ctx)
		if err != nil {
			a.log().Warn("git stack update: namespaces", "environment", env.ID, "error", err)
			continue
		}
		for _, ns := range namespaces {
			list, err := env.Runtime.Containers().List(ctx, ns)
			if err != nil {
				continue
			}
			if slices.ContainsFunc(list, func(c runtime.Container) bool { return c.Compose != nil && c.Compose.Project == name }) {
				out = append(out, target{env, ns})
			}
		}
	}
	return out
}

// Deploy pulls a stack as a job and redeploys it where it runs when the commit changed.
func (a *Auto) Deploy(ctx context.Context, name string) (jobs.Job, error) {
	targets := a.targets(ctx, name)
	spec := jobs.Spec{Kind: "stack.git-pull", Title: "Auto-update stack " + name + " from Git", User: AutoUser, Action: string(authz.StacksDeploy)}
	if len(targets) > 0 {
		spec.Environment, spec.Namespace = targets[0].env.ID, targets[0].ns
		spec.Resource = fmt.Sprintf("env/%s/ns/%s/stack/%s", spec.Environment, spec.Namespace, name)
	} else {
		spec.Environment = environment.LocalID
		spec.Resource = "git/stack/" + name
	}
	svc, notifyFn := a.Service, a.Notify
	return a.Jobs.Start(spec, func(ctx context.Context, out *jobs.Output) error {
		if len(targets) == 0 {
			_, _ = fmt.Fprintf(out, "The stack has no containers: pulling only\r\n")
		}
		commit, err := svc.PullAndDeploy(ctx, out, name, len(targets) > 0, false, func(ctx context.Context, def stacks.Definition) error {
			for _, t := range targets {
				_, _ = fmt.Fprintf(out, "\r\nDeploying in %s/%s\r\n", t.env.ID, t.ns)
				if err := t.env.Runtime.Compose().Run(ctx, t.ns, name, def.Dir, runtime.ComposeUp, runtime.ComposeOptions{RemoveOrphans: true}, out); err != nil {
					return err
				}
			}
			return nil
		})
		out.SetResult("commit", commit.Hash)
		if err == nil && notifyFn != nil {
			notifyFn(notify.Event{
				Kind: notify.KindGitUpdate, Severity: notify.SeverityInfo, Environment: spec.Environment, Namespace: spec.Namespace,
				Title:   "Stack " + name + " updated from Git",
				Message: Short(commit.Hash) + " " + commit.Message,
				Object:  "stack/" + name,
			})
		}
		return err
	})
}
