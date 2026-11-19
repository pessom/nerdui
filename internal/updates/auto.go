// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/notify"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/stacks"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

// Label opts a container, or a compose service, in to automatic updates (#54, ADR-0032):
// nerdui.auto-update=true. Nothing is updated without it.
const Label = "nerdui.auto-update"

// Auto-update kinds and statuses, as stored in the log.
const (
	KindContainer = "container"
	KindStack     = "stack"

	StatusUpdated = "updated"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
)

// AutoUser is the job user of automatic updates.
const AutoUser = "nerdui (auto-update)"

const (
	logRetention = 90 * 24 * time.Hour
	maxLogRows   = 500
)

// AutoEnabled reports whether labels opt in to automatic updates.
func AutoEnabled(labels map[string]string) bool {
	switch strings.ToLower(strings.TrimSpace(labels[Label])) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

// AutoStore is the database used by the updater; *db.Queries of the write connection implements it.
type AutoStore interface {
	ListImageUpdates(ctx context.Context, environment string) ([]db.ImageUpdate, error)
	AddAutoUpdate(ctx context.Context, arg db.AddAutoUpdateParams) error
	LastAutoUpdate(ctx context.Context, arg db.LastAutoUpdateParams) (db.AutoUpdate, error)
	ListAutoUpdates(ctx context.Context, arg db.ListAutoUpdatesParams) ([]db.AutoUpdate, error)
	PruneAutoUpdates(ctx context.Context, arg db.PruneAutoUpdatesParams) error
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, arg db.SetSettingParams) error
}

// JobRunner starts jobs; *jobs.Manager implements it.
type JobRunner interface {
	Start(spec jobs.Spec, fn jobs.Func) (jobs.Job, error)
}

// StackDefinitions finds the compose files of stacks; *stacks.Service implements it.
type StackDefinitions interface {
	Definitions(ctx context.Context) ([]stacks.Definition, error)
}

// Updater pulls and recreates opted-in containers and stack services whose images have updates,
// after each scheduled check. Every update runs as a job and is written to the log.
type Updater struct {
	Store  AutoStore
	Jobs   JobRunner
	Stacks StackDefinitions // nil: stack services are skipped
	Now    func() time.Time
	// Notify, when set, receives events about updates done, failed and waiting (#86).
	Notify func(notify.Event)
	// Base is the context of approved updates, canceled on shutdown; nil: never canceled.
	Base context.Context //nolint:containedctx // background work outlives the approving request

	mu      sync.Mutex
	running map[string]bool
	pending map[string]Pending // by environment
}

// Delays are the allowed minimum ages of a new image before it is applied (#56).
var Delays = []time.Duration{0, time.Hour, 6 * time.Hour, 24 * time.Hour, 72 * time.Hour, 168 * time.Hour}

// MaxBatches are the allowed thresholds above which updates wait for approval; 0 never asks.
var MaxBatches = []int{0, 1, 3, 5, 10, 20}

// DefaultMaxBatch applies until an administrator chooses another.
const DefaultMaxBatch = 5

// ErrInvalidAutoSettings is returned for settings outside Delays and MaxBatches.
var ErrInvalidAutoSettings = errors.New("invalid automatic update settings")

// ErrNothingPending is returned when approving without waiting updates.
var ErrNothingPending = errors.New("no automatic updates wait for approval")

// AutoSettings guard automatic updates of an environment (#56, ADR-0033). Keep in sync with
// api/openapi.yaml.
type AutoSettings struct {
	// DelaySeconds is how long a new registry digest must have been seen before it is applied.
	DelaySeconds int64 `json:"delaySeconds"`
	// MaxBatch: when more updates than this are due at once, they wait for approval (0: never).
	MaxBatch int `json:"maxBatch"`
}

// Pending are updates waiting for approval because there were more than MaxBatch.
type Pending struct {
	Since   time.Time `json:"since"`
	Targets []Target  `json:"targets"`
}

func autoSettingKey(env string) string { return "environments/" + env + "/auto-updates" }

// Settings returns the environment's automatic update settings.
func (u *Updater) Settings(ctx context.Context, env string) (AutoSettings, error) {
	def := AutoSettings{MaxBatch: DefaultMaxBatch}
	v, err := u.Store.GetSetting(ctx, autoSettingKey(env))
	if store.IsNotFound(err) {
		return def, nil
	}
	if err != nil {
		return AutoSettings{}, err
	}
	var s AutoSettings
	if json.Unmarshal([]byte(v), &s) != nil || s.validate() != nil {
		return def, nil
	}
	return s, nil
}

func (s AutoSettings) validate() error {
	if !slices.Contains(Delays, time.Duration(s.DelaySeconds)*time.Second) || !slices.Contains(MaxBatches, s.MaxBatch) {
		return fmt.Errorf("%w: delay %ds, batch %d", ErrInvalidAutoSettings, s.DelaySeconds, s.MaxBatch)
	}
	return nil
}

// SetSettings stores the environment's automatic update settings.
func (u *Updater) SetSettings(ctx context.Context, env string, s AutoSettings) error {
	if err := s.validate(); err != nil {
		return err
	}
	b, _ := json.Marshal(s)
	return u.Store.SetSetting(ctx, db.SetSettingParams{Key: autoSettingKey(env), Value: string(b), Now: u.now().Unix()})
}

// PendingFor returns the updates of an environment waiting for approval, if any.
func (u *Updater) PendingFor(env string) (Pending, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	p, ok := u.pending[env]
	return p, ok
}

func (u *Updater) setPending(env string, p *Pending) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if p == nil {
		delete(u.pending, env)
		return
	}
	if u.pending == nil {
		u.pending = map[string]Pending{}
	}
	u.pending[env] = *p
}

func (u *Updater) begin(env string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.running == nil {
		u.running = map[string]bool{}
	}
	if u.running[env] {
		return false
	}
	u.running[env] = true
	return true
}

func (u *Updater) end(env string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.running, env)
}

// Target is one update: a standalone container, or the opted-in services of a stack.
type Target struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	// Name is the container name or the stack (compose project) name.
	Name string `json:"name"`
	// ContainerID is set for containers.
	ContainerID string `json:"-"`
	// Services are the stack's services to update.
	Services []string `json:"services,omitempty"`
	// Refs and Digests are the image references and the registry digests they update to.
	Refs    []string `json:"refs"`
	Digests []string `json:"-"`
}

// Entry is a row of the auto-update log. Keep in sync with api/openapi.yaml.
type Entry struct {
	ID         int64     `json:"id"`
	Namespace  string    `json:"namespace"`
	Kind       string    `json:"kind"`
	Name       string    `json:"name"`
	Refs       []string  `json:"refs"`
	Status     string    `json:"status"`
	Message    string    `json:"message,omitempty"`
	JobID      string    `json:"jobId,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
}

func (u *Updater) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

// Plan lists the updates due in an environment: opted-in containers whose image reference has a
// newer registry digest than the local image, grouped by stack for compose services.
//
// A registry digest first seen less than delay ago is not due yet (#56).
func (u *Updater) Plan(ctx context.Context, env *environment.Environment, delay time.Duration) ([]Target, error) {
	rows, err := u.Store.ListImageUpdates(ctx, env.ID)
	if err != nil {
		return nil, err
	}
	remote := map[string]string{}
	for _, r := range rows {
		if r.Error == "" && r.RemoteDigest != "" && u.now().Sub(time.Unix(r.SeenAt, 0)) >= delay {
			remote[r.Ref] = r.RemoteDigest
		}
	}
	if len(remote) == 0 {
		return nil, nil
	}
	nss, err := env.Runtime.System().Namespaces(ctx)
	if err != nil {
		return nil, err
	}
	var out []Target
	for _, ns := range nss {
		digests, _, err := usage(ctx, env, ns)
		if err != nil {
			return nil, err
		}
		list, err := env.Runtime.Containers().List(ctx, ns)
		if err != nil {
			return nil, err
		}
		stackTargets := map[string]*Target{}
		for _, ct := range list {
			d, ok := remote[ct.Image]
			if !ok || !AutoEnabled(ct.Labels) || digests[ct.Image] == "" || digests[ct.Image] == d {
				continue
			}
			if ct.Compose == nil {
				out = append(out, Target{
					Kind: KindContainer, Namespace: ns, Name: ct.Name, ContainerID: ct.ID,
					Refs: []string{ct.Image}, Digests: []string{d},
				})
				continue
			}
			t := stackTargets[ct.Compose.Project]
			if t == nil {
				t = &Target{Kind: KindStack, Namespace: ns, Name: ct.Compose.Project}
				stackTargets[ct.Compose.Project] = t
			}
			if ct.Compose.Service != "" && !slices.Contains(t.Services, ct.Compose.Service) {
				t.Services = append(t.Services, ct.Compose.Service)
			}
			if !slices.Contains(t.Refs, ct.Image) {
				t.Refs = append(t.Refs, ct.Image)
				t.Digests = append(t.Digests, d)
			}
		}
		names := make([]string, 0, len(stackTargets))
		for n := range stackTargets {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			t := stackTargets[n]
			sort.Strings(t.Services)
			out = append(out, *t)
		}
	}
	return out, nil
}

// ErrUpdating is returned while automatic updates of the environment run.
var ErrUpdating = errors.New("automatic updates are already running")

// Run applies the planned updates of an environment one at a time and logs each. When more are
// due than the environment's MaxBatch, nothing is applied and they wait for Approve (#56).
func (u *Updater) Run(ctx context.Context, env *environment.Environment) error {
	if !u.begin(env.ID) {
		return ErrUpdating
	}
	defer u.end(env.ID)
	if err := u.Store.PruneAutoUpdates(ctx, db.PruneAutoUpdatesParams{
		Environment: env.ID, Before: u.now().Add(-logRetention).Unix(),
	}); err != nil {
		return err
	}
	set, err := u.Settings(ctx, env.ID)
	if err != nil {
		return err
	}
	targets, err := u.Plan(ctx, env, time.Duration(set.DelaySeconds)*time.Second)
	if err != nil {
		return err
	}
	if set.MaxBatch > 0 && len(targets) > set.MaxBatch {
		since := u.now()
		p, waiting := u.PendingFor(env.ID)
		if waiting {
			since = p.Since
		}
		u.setPending(env.ID, &Pending{Since: since, Targets: targets})
		if !waiting {
			u.notify(notify.Event{
				Kind: notify.KindAutoUpdatePending, Severity: notify.SeverityWarning, Environment: env.ID,
				Title:   fmt.Sprintf("%d automatic updates wait for approval", len(targets)),
				Message: "Approve them under Settings → Image updates.",
				Object:  "pending",
			})
		}
		return nil
	}
	u.setPending(env.ID, nil)
	u.applyAll(ctx, env, targets)
	return nil
}

// Approve applies the updates waiting for approval, re-planned now so that updates done or
// withdrawn meanwhile are left out; it returns once they have started.
func (u *Updater) Approve(ctx context.Context, env *environment.Environment) (int, error) {
	if _, ok := u.PendingFor(env.ID); !ok {
		return 0, ErrNothingPending
	}
	if !u.begin(env.ID) {
		return 0, ErrUpdating
	}
	set, err := u.Settings(ctx, env.ID)
	if err != nil {
		u.end(env.ID)
		return 0, err
	}
	targets, err := u.Plan(ctx, env, time.Duration(set.DelaySeconds)*time.Second)
	if err != nil {
		u.end(env.ID)
		return 0, err
	}
	u.setPending(env.ID, nil)
	go func() {
		defer u.end(env.ID)
		base := u.Base
		if base == nil {
			base = context.WithoutCancel(ctx)
		}
		u.applyAll(base, env, targets)
	}()
	return len(targets), nil
}

func (u *Updater) applyAll(ctx context.Context, env *environment.Environment, targets []Target) {
	for _, t := range targets {
		if ctx.Err() != nil {
			return
		}
		u.apply(ctx, env, t)
	}
}

func (u *Updater) apply(ctx context.Context, env *environment.Environment, t Target) {
	started := u.now()
	if reason := u.skipReason(ctx, env, t); reason != "" {
		// Log a skip once per target and digest, not at every check.
		last, err := u.Store.LastAutoUpdate(ctx, db.LastAutoUpdateParams{
			Environment: env.ID, Namespace: t.Namespace, Kind: t.Kind, Name: t.Name,
		})
		if err == nil && last.Status == StatusSkipped && last.Digests == strings.Join(t.Digests, ",") && last.Message == reason {
			return
		}
		if err != nil && !store.IsNotFound(err) {
			return
		}
		u.record(ctx, env, t, StatusSkipped, reason, "", started)
		return
	}

	var fn jobs.Func
	spec := jobs.Spec{
		Environment: env.ID, Namespace: t.Namespace, User: AutoUser,
	}
	switch t.Kind {
	case KindContainer:
		containers, id := env.Runtime.Containers(), t.ContainerID
		spec.Kind, spec.Title = "container.recreate", "Auto-update container "+t.Name
		spec.Resource = fmt.Sprintf("env/%s/ns/%s/container/%s", env.ID, t.Namespace, t.Name)
		spec.Action = string(authz.ContainersWrite)
		fn = func(ctx context.Context, out *jobs.Output) error {
			cur, err := containers.Spec(ctx, t.Namespace, id)
			if err != nil {
				return err
			}
			newID, err := containers.Recreate(ctx, t.Namespace, id, cur.Spec, runtime.RecreateOptions{Pull: true}, out)
			if newID != "" {
				out.SetResult("containerId", newID)
			}
			return err
		}
	case KindStack:
		def, _ := u.stack(ctx, t.Name)
		compose, dir, services := env.Runtime.Compose(), def.Dir, t.Services
		spec.Kind, spec.Title = "stack.up", "Auto-update stack "+t.Name
		spec.Resource = fmt.Sprintf("env/%s/ns/%s/stack/%s", env.ID, t.Namespace, t.Name)
		spec.Action = string(authz.StacksDeploy)
		fn = func(ctx context.Context, out *jobs.Output) error {
			opts := runtime.ComposeOptions{Services: services}
			if err := compose.Run(ctx, t.Namespace, t.Name, dir, runtime.ComposePull, opts, out); err != nil {
				return err
			}
			return compose.Run(ctx, t.Namespace, t.Name, dir, runtime.ComposeUp, opts, out)
		}
	default:
		return
	}

	done := make(chan error, 1)
	job, err := u.Jobs.Start(spec, func(ctx context.Context, out *jobs.Output) (err error) {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic: %v", r)
				panic(r)
			}
			done <- err
		}()
		return fn(ctx, out)
	})
	if err != nil {
		if ctx.Err() != nil {
			return // shutting down
		}
		// Another job on the same resource is running: try again after the next check.
		u.record(ctx, env, t, StatusSkipped, err.Error(), "", started)
		return
	}
	var runErr error
	select {
	case runErr = <-done:
	case <-ctx.Done():
		runErr = ctx.Err()
	}
	if runErr != nil {
		u.record(ctx, env, t, StatusFailed, truncate(runErr.Error()), job.ID, started)
		return
	}
	u.record(ctx, env, t, StatusUpdated, "", job.ID, started)
}

// skipReason explains why a target cannot be updated automatically, or returns "".
func (u *Updater) skipReason(ctx context.Context, env *environment.Environment, t Target) string {
	switch t.Kind {
	case KindContainer:
		cur, err := env.Runtime.Containers().Spec(ctx, t.Namespace, t.ContainerID)
		switch {
		case err != nil:
			return "cannot read the container's settings: " + err.Error()
		case cur.Stack != "":
			return "the container belongs to stack " + cur.Stack
		case len(cur.Unsupported) > 0:
			return "recreating would lose settings: " + strings.Join(cur.Unsupported, "; ")
		}
	case KindStack:
		if len(t.Services) == 0 {
			return "the stack's containers have no compose service label"
		}
		def, err := u.stack(ctx, t.Name)
		switch {
		case err != nil:
			return err.Error()
		case def.File == "":
			return "no compose file in " + def.Dir
		}
	}
	return ""
}

var errUnknownStack = errors.New("nerdui does not know the stack's compose file; adopt the stack first")

func (u *Updater) stack(ctx context.Context, name string) (stacks.Definition, error) {
	if u.Stacks == nil {
		return stacks.Definition{}, errUnknownStack
	}
	defs, err := u.Stacks.Definitions(ctx)
	if err != nil {
		return stacks.Definition{}, err
	}
	for _, d := range defs {
		if d.Name == name {
			return d, nil
		}
	}
	return stacks.Definition{}, errUnknownStack
}

func (u *Updater) notify(ev notify.Event) {
	if u.Notify != nil {
		u.Notify(ev)
	}
}

func (u *Updater) record(ctx context.Context, env *environment.Environment, t Target, status, msg, jobID string, started time.Time) {
	what := "Container " + t.Name
	if t.Kind == KindStack {
		what = "Stack " + t.Name
	}
	switch status {
	case StatusUpdated:
		u.notify(notify.Event{
			Kind: notify.KindAutoUpdateDone, Severity: notify.SeverityInfo, Environment: env.ID, Namespace: t.Namespace,
			Title: what + " updated", Message: strings.Join(t.Refs, ", "), Object: t.Kind + "/" + t.Name,
		})
	case StatusFailed:
		u.notify(notify.Event{
			Kind: notify.KindAutoUpdateFailed, Severity: notify.SeverityError, Environment: env.ID, Namespace: t.Namespace,
			Title: what + " could not be updated", Message: msg, Object: t.Kind + "/" + t.Name,
		})
	}
	_ = u.Store.AddAutoUpdate(context.WithoutCancel(ctx), db.AddAutoUpdateParams{
		Environment: env.ID, Namespace: t.Namespace, Kind: t.Kind, Name: t.Name,
		Refs: strings.Join(t.Refs, ","), Digests: strings.Join(t.Digests, ","),
		Status: status, Message: msg, JobID: jobID, StartedAt: started.Unix(), FinishedAt: u.now().Unix(),
	})
}

// Log returns the newest entries of an environment's auto-update log, of one namespace or all.
func (u *Updater) Log(ctx context.Context, env, namespace string, limit int) ([]Entry, error) {
	if limit <= 0 || limit > maxLogRows {
		limit = maxLogRows
	}
	rows, err := u.Store.ListAutoUpdates(ctx, db.ListAutoUpdatesParams{Environment: env, Namespace: namespace, MaxRows: int64(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		refs := []string{}
		if r.Refs != "" {
			refs = strings.Split(r.Refs, ",")
		}
		out = append(out, Entry{
			ID: r.ID, Namespace: r.Namespace, Kind: r.Kind, Name: r.Name, Refs: refs,
			Status: r.Status, Message: r.Message, JobID: r.JobID,
			StartedAt: time.Unix(r.StartedAt, 0).UTC(), FinishedAt: time.Unix(r.FinishedAt, 0).UTC(),
		})
	}
	return out, nil
}
