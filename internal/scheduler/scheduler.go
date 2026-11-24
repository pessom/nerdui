// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package scheduler runs tasks on cron schedules with a history of runs (#82, ADR-0048).
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/containerd/errdefs"
	"github.com/robfig/cron/v3"

	"github.com/pessom/nerdui/internal/backup"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/store/db"
	"github.com/pessom/nerdui/internal/volbackup"
	"github.com/pessom/nerdui/internal/webhooks"
)

// Kinds of scheduled tasks.
const (
	KindPrune             = "prune"
	KindBackup            = "backup"
	KindStackRedeploy     = "stack.redeploy"
	KindStackRestart      = "stack.restart"
	KindContainerRedeploy = "container.redeploy"
	KindContainerRestart  = "container.restart"
	KindImageUpdates      = "image-updates.check"
	KindVolumeBackup      = "volume.backup"
)

// Kinds lists every kind.
var Kinds = []string{KindPrune, KindBackup, KindStackRedeploy, KindStackRestart, KindContainerRedeploy, KindContainerRestart, KindImageUpdates, KindVolumeBackup}

// Run statuses.
const (
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusSkipped   = "skipped"
)

const (
	// User is the job user of scheduled runs.
	User = "nerdui (scheduler)"
	// KeepRuns is how many runs of a schedule are kept.
	KeepRuns = 50
	// DefaultBackupKeep is how many scheduled backups are kept by default.
	DefaultBackupKeep = 7
	tick              = 15 * time.Second
)

// parser accepts five-field expressions and descriptors such as @daily or @every 6h.
var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// ParseCron validates a cron expression.
func ParseCron(expr string) (cron.Schedule, error) {
	s, err := parser.Parse(strings.TrimSpace(expr))
	if err != nil {
		return nil, fmt.Errorf("%w: cron: %s", errdefs.ErrInvalidArgument, err.Error())
	}
	return s, nil
}

// Options are kind-specific settings. Keep in sync with api/openapi.yaml.
type Options struct {
	// Prune: what to remove, as the Prune dialog.
	Containers bool `json:"containers,omitempty"`
	Images     bool `json:"images,omitempty"`
	AllImages  bool `json:"allImages,omitempty"`
	Networks   bool `json:"networks,omitempty"`
	BuildCache bool `json:"buildCache,omitempty"`
	// Backup: the directory on the host and how many archives to keep there.
	Directory string `json:"directory,omitempty"`
	Keep      int    `json:"keep,omitempty"`
	// Volume backup (#59): the backup target, and whether containers using the volume are
	// stopped while it is read. Keep is shared with backups.
	TargetID       int64 `json:"targetId,omitempty"`
	StopContainers bool  `json:"stopContainers,omitempty"`
}

// Schedule is a schedule without its secret. Keep in sync with api/openapi.yaml.
type Schedule struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	Environment string  `json:"environment,omitempty"`
	Namespace   string  `json:"namespace,omitempty"`
	Target      string  `json:"target,omitempty"`
	Options     Options `json:"options"`
	// HasPassword is set for backups with a stored password.
	HasPassword bool       `json:"hasPassword,omitempty"`
	Cron        string     `json:"cron"`
	Enabled     bool       `json:"enabled"`
	CreatedBy   string     `json:"createdBy,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	NextRunAt   *time.Time `json:"nextRunAt,omitempty"`
	LastRunAt   *time.Time `json:"lastRunAt,omitempty"`
	LastStatus  string     `json:"lastStatus,omitempty"`
	LastMessage string     `json:"lastMessage,omitempty"`
}

// Input creates or changes a schedule. Keep in sync with api/openapi.yaml.
type Input struct {
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	Environment string  `json:"environment"`
	Namespace   string  `json:"namespace"`
	Target      string  `json:"target"`
	Options     Options `json:"options"`
	// Password of backups; empty keeps the stored one.
	Password string `json:"password"`
	Cron     string `json:"cron"`
	Enabled  bool   `json:"enabled"`
}

// Run is a run of a schedule. Keep in sync with api/openapi.yaml.
type Run struct {
	ID         int64      `json:"id"`
	Trigger    string     `json:"trigger"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Status     string     `json:"status"`
	Message    string     `json:"message,omitempty"`
	JobID      string     `json:"jobId,omitempty"`
}

// Store is the database of schedules; *db.Queries of the write connection implements it.
type Store interface {
	ListSchedules(ctx context.Context) ([]db.Schedule, error)
	GetSchedule(ctx context.Context, id int64) (db.Schedule, error)
	CreateSchedule(ctx context.Context, arg db.CreateScheduleParams) (db.Schedule, error)
	UpdateSchedule(ctx context.Context, arg db.UpdateScheduleParams) (db.Schedule, error)
	DeleteSchedule(ctx context.Context, id int64) (int64, error)
	SetScheduleLastRun(ctx context.Context, arg db.SetScheduleLastRunParams) error
	InsertScheduleRun(ctx context.Context, arg db.InsertScheduleRunParams) (int64, error)
	FinishScheduleRun(ctx context.Context, arg db.FinishScheduleRunParams) error
	SetScheduleRunJob(ctx context.Context, arg db.SetScheduleRunJobParams) error
	ListScheduleRuns(ctx context.Context, arg db.ListScheduleRunsParams) ([]db.ScheduleRun, error)
	PruneScheduleRuns(ctx context.Context, arg db.PruneScheduleRunsParams) error
	FailUnfinishedScheduleRuns(ctx context.Context, now int64) error
}

// JobRunner starts and looks up jobs; *jobs.Manager implements it.
type JobRunner interface {
	Start(spec jobs.Spec, fn jobs.Func) (jobs.Job, error)
	Get(id string) (jobs.Job, bool)
}

// Environments finds environments; *environment.Registry implements it.
type Environments interface {
	Get(id string) (*environment.Environment, bool)
}

// ImageChecker checks an environment's images for updates; the updates package implements it.
type ImageChecker func(ctx context.Context, env *environment.Environment) error

// ActionStarter restarts or redeploys stacks and containers; *webhooks.Service implements it.
type ActionStarter interface {
	StartAction(ctx context.Context, a webhooks.Action) (jobs.Job, error)
}

// Service manages schedules and runs them.
type Service struct {
	Store        Store
	Jobs         JobRunner
	Environments Environments
	Actions      ActionStarter      // nil: stack and container kinds fail
	Backup       *backup.Source     // nil: backups fail
	Volumes      *volbackup.Service // nil: volume backups fail
	CheckImages  ImageChecker       // nil: image checks fail
	Logger       *slog.Logger
	Location     *time.Location // of cron expressions; nil is the server's local zone
	// Now, Every and Poll override the clock, the check interval and how often a followed job
	// is looked up (tests).
	Now   func() time.Time
	Every time.Duration
	Poll  time.Duration

	mu      sync.Mutex
	running map[int64]bool
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) loc() *time.Location {
	if s.Location != nil {
		return s.Location
	}
	return time.Local
}

func (s *Service) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func ms(t int64) *time.Time {
	if t <= 0 {
		return nil
	}
	v := time.UnixMilli(t).UTC()
	return &v
}

// next is when a schedule runs next: after its last run, or after it was last changed.
func (s *Service) next(r db.Schedule) *time.Time {
	sched, err := ParseCron(r.Cron)
	if err != nil || r.Enabled == 0 {
		return nil
	}
	from := max(r.LastRunAt, r.UpdatedAt)
	n := sched.Next(time.UnixMilli(from).In(s.loc()))
	if n.IsZero() {
		return nil
	}
	n = n.UTC()
	return &n
}

func (s *Service) view(r db.Schedule) Schedule {
	v := Schedule{
		ID: r.ID, Name: r.Name, Kind: r.Kind, Environment: r.Environment, Namespace: r.Namespace,
		Target: r.Target, HasPassword: r.Secret != "", Cron: r.Cron, Enabled: r.Enabled != 0,
		CreatedBy: r.CreatedBy, CreatedAt: time.UnixMilli(r.CreatedAt).UTC(), UpdatedAt: time.UnixMilli(r.UpdatedAt).UTC(),
		LastRunAt: ms(r.LastRunAt), LastStatus: r.LastStatus, LastMessage: r.LastMessage, NextRunAt: s.next(r),
	}
	_ = json.Unmarshal([]byte(r.Options), &v.Options)
	return v
}

func bad(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errdefs.ErrInvalidArgument, fmt.Sprintf(format, a...))
}

func notFound(id int64) error {
	return fmt.Errorf("%w: schedule %d not found", errdefs.ErrNotFound, id)
}

// validate checks an input and normalizes it; stored is the secret kept when the input has none.
func (s *Service) validate(in *Input, stored string) (secret string, err error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 100 {
		return "", bad("name must be 1 to 100 characters")
	}
	in.Cron = strings.TrimSpace(in.Cron)
	if _, err := ParseCron(in.Cron); err != nil {
		return "", err
	}
	o := in.Options
	switch in.Kind {
	case KindPrune, KindStackRedeploy, KindStackRestart, KindContainerRedeploy, KindContainerRestart, KindImageUpdates, KindVolumeBackup:
		if _, ok := s.Environments.Get(in.Environment); !ok {
			return "", bad("unknown environment %q", in.Environment)
		}
	case KindBackup:
		in.Environment, in.Namespace = "", ""
	default:
		return "", bad("kind must be one of %s", strings.Join(Kinds, ", "))
	}
	switch in.Kind {
	case KindPrune:
		if in.Namespace == "" {
			return "", bad("namespace is required")
		}
		if !o.Containers && !o.Images && !o.Networks && !o.BuildCache {
			return "", bad("select at least one of containers, images, networks, buildCache")
		}
		in.Target, in.Options = "", Options{Containers: o.Containers, Images: o.Images, AllImages: o.Images && o.AllImages, Networks: o.Networks, BuildCache: o.BuildCache}
	case KindStackRedeploy, KindStackRestart, KindContainerRedeploy, KindContainerRestart:
		if in.Namespace == "" || strings.TrimSpace(in.Target) == "" {
			return "", bad("namespace and target are required")
		}
		in.Target, in.Options = strings.TrimSpace(in.Target), Options{}
	case KindImageUpdates:
		in.Namespace, in.Target, in.Options = "", "", Options{}
	case KindVolumeBackup:
		if in.Namespace == "" || strings.TrimSpace(in.Target) == "" {
			return "", bad("namespace and volume are required")
		}
		if o.TargetID <= 0 {
			return "", bad("choose a backup target")
		}
		if s.Volumes != nil {
			if _, err := s.Volumes.Target(context.Background(), o.TargetID); err != nil {
				return "", err
			}
		}
		if o.Keep == 0 {
			o.Keep = volbackup.DefaultKeep
		}
		if o.Keep < 1 || o.Keep > volbackup.MaxKeep {
			return "", bad("keep must be 1 to %d", volbackup.MaxKeep)
		}
		in.Target = strings.TrimSpace(in.Target)
		in.Options = Options{TargetID: o.TargetID, Keep: o.Keep, StopContainers: o.StopContainers}
	case KindBackup:
		in.Target = ""
		if o.Directory == "" {
			o.Directory = filepath.Join(s.Backup.DataDir, "backups")
		}
		if !filepath.IsAbs(o.Directory) {
			return "", bad("the backup directory must be an absolute path")
		}
		if o.Keep == 0 {
			o.Keep = DefaultBackupKeep
		}
		if o.Keep < 1 || o.Keep > 1000 {
			return "", bad("keep must be 1 to 1000")
		}
		in.Options = Options{Directory: filepath.Clean(o.Directory), Keep: o.Keep}
		secret = stored
		if in.Password != "" {
			if err := backup.ValidatePassword(in.Password); err != nil {
				return "", err
			}
			secret = in.Password
		}
		if secret == "" {
			return "", bad("a backup password is required")
		}
		return secret, nil
	}
	return "", nil
}

func enabled(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// List returns every schedule.
func (s *Service) List(ctx context.Context) ([]Schedule, error) {
	rows, err := s.Store.ListSchedules(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Schedule, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.view(r))
	}
	return out, nil
}

// Create adds a schedule.
func (s *Service) Create(ctx context.Context, in Input, user string) (Schedule, error) {
	if in.Kind == KindBackup && s.Backup == nil {
		return Schedule{}, fmt.Errorf("%w: backups are not available", errdefs.ErrNotImplemented)
	}
	secret, err := s.validate(&in, "")
	if err != nil {
		return Schedule{}, err
	}
	opts, _ := json.Marshal(in.Options)
	r, err := s.Store.CreateSchedule(ctx, db.CreateScheduleParams{
		Name: in.Name, Kind: in.Kind, Environment: in.Environment, Namespace: in.Namespace, Target: in.Target,
		Options: string(opts), Secret: secret, Cron: in.Cron, Enabled: enabled(in.Enabled), CreatedBy: user,
		Now: s.now().UnixMilli(),
	})
	if err != nil {
		return Schedule{}, conflict(err, in.Name)
	}
	return s.view(r), nil
}

// Update changes a schedule.
func (s *Service) Update(ctx context.Context, id int64, in Input) (Schedule, error) {
	cur, err := s.Store.GetSchedule(ctx, id)
	if err != nil {
		return Schedule{}, notFoundOr(err, id)
	}
	stored := ""
	if in.Kind == cur.Kind {
		stored = cur.Secret
	}
	if in.Kind == KindBackup && s.Backup == nil {
		return Schedule{}, fmt.Errorf("%w: backups are not available", errdefs.ErrNotImplemented)
	}
	secret, err := s.validate(&in, stored)
	if err != nil {
		return Schedule{}, err
	}
	opts, _ := json.Marshal(in.Options)
	r, err := s.Store.UpdateSchedule(ctx, db.UpdateScheduleParams{
		ID: id, Name: in.Name, Kind: in.Kind, Environment: in.Environment, Namespace: in.Namespace, Target: in.Target,
		Options: string(opts), Secret: secret, Cron: in.Cron, Enabled: enabled(in.Enabled), Now: s.now().UnixMilli(),
	})
	if err != nil {
		return Schedule{}, conflict(notFoundOr(err, id), in.Name)
	}
	return s.view(r), nil
}

// Delete removes a schedule and its runs.
func (s *Service) Delete(ctx context.Context, id int64) error {
	n, err := s.Store.DeleteSchedule(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return notFound(id)
	}
	return nil
}

// Runs returns the newest runs of a schedule.
func (s *Service) Runs(ctx context.Context, id int64) ([]Run, error) {
	if _, err := s.Store.GetSchedule(ctx, id); err != nil {
		return nil, notFoundOr(err, id)
	}
	rows, err := s.Store.ListScheduleRuns(ctx, db.ListScheduleRunsParams{ScheduleID: id, Limit: KeepRuns})
	if err != nil {
		return nil, err
	}
	out := make([]Run, 0, len(rows))
	for _, r := range rows {
		out = append(out, Run{
			ID: r.ID, Trigger: r.Trigger, StartedAt: time.UnixMilli(r.StartedAt).UTC(), FinishedAt: ms(r.FinishedAt),
			Status: r.Status, Message: r.Message, JobID: r.JobID,
		})
	}
	return out, nil
}

// TargetUsers names the schedules that write to a volume backup target.
func (s *Service) TargetUsers(ctx context.Context, target int64) ([]string, error) {
	rows, err := s.Store.ListSchedules(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, r := range rows {
		if r.Kind == KindVolumeBackup && s.view(r).Options.TargetID == target {
			names = append(names, r.Name)
		}
	}
	return names, nil
}
