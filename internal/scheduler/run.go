// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/backup"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
	"github.com/pessom/nerdui/internal/volbackup"
	"github.com/pessom/nerdui/internal/webhooks"
)

// Triggers of runs.
const (
	TriggerSchedule = "schedule"
	TriggerManual   = "manual"
)

func notFoundOr(err error, id int64) error {
	if errors.Is(err, sql.ErrNoRows) {
		return notFound(id)
	}
	return err
}

func conflict(err error, name string) error {
	if store.IsUniqueViolation(err) {
		return fmt.Errorf("%w: a schedule named %q exists", errdefs.ErrAlreadyExists, name)
	}
	return err
}

// Run starts due schedules until ctx ends. Runs left unfinished by a previous process are
// marked failed first.
func (s *Service) Run(ctx context.Context) {
	if err := s.Store.FailUnfinishedScheduleRuns(ctx, s.now().UnixMilli()); err != nil {
		s.log().Warn("scheduler: close unfinished runs", "error", err)
	}
	t := s.newTicker()
	defer t.stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.c:
		}
		s.Tick(ctx)
	}
}

// Tick starts the schedules that are due.
func (s *Service) Tick(ctx context.Context) {
	rows, err := s.Store.ListSchedules(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.log().Warn("scheduler: list schedules", "error", err)
		}
		return
	}
	now := s.now()
	for _, r := range rows {
		if n := s.next(r); n != nil && !n.After(now) {
			if _, err := s.start(ctx, r, TriggerSchedule, User); err != nil && ctx.Err() == nil {
				s.log().Warn("scheduler: start", "schedule", r.Name, "error", err)
			}
		}
	}
}

// RunNow starts a schedule at once, also a disabled one.
func (s *Service) RunNow(ctx context.Context, id int64, user string) (jobs.Job, error) {
	r, err := s.Store.GetSchedule(ctx, id)
	if err != nil {
		return jobs.Job{}, notFoundOr(err, id)
	}
	if user == "" {
		user = User
	}
	return s.start(ctx, r, TriggerManual, user)
}

func (s *Service) begin(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running == nil {
		s.running = map[int64]bool{}
	}
	if s.running[id] {
		return false
	}
	s.running[id] = true
	return true
}

func (s *Service) end(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.running, id)
}

// start records a run and starts its job. A schedule whose previous run has not finished is
// skipped; one that cannot start (a missing container, read-only mode) fails with the reason.
func (s *Service) start(ctx context.Context, r db.Schedule, trigger, user string) (jobs.Job, error) {
	started := s.now().UnixMilli()
	bg := context.WithoutCancel(ctx) // runs outlive the request that started them
	setLast := func(status, msg string) {
		_ = s.Store.SetScheduleLastRun(bg, db.SetScheduleLastRunParams{ID: r.ID, LastRunAt: started, LastStatus: status, LastMessage: msg})
	}
	insert := func(status, msg string) (int64, error) {
		id, err := s.Store.InsertScheduleRun(bg, db.InsertScheduleRunParams{
			ScheduleID: r.ID, Trigger: trigger, StartedAt: started, Status: status, Message: msg,
		})
		setLast(status, msg)
		_ = s.Store.PruneScheduleRuns(bg, db.PruneScheduleRunsParams{ScheduleID: r.ID, Keep: KeepRuns})
		return id, err
	}
	if !s.begin(r.ID) {
		_, _ = insert(StatusSkipped, "the previous run has not finished")
		return jobs.Job{}, fmt.Errorf("%w: the previous run of %q has not finished", errdefs.ErrConflict, r.Name)
	}
	runID, err := insert(StatusRunning, "")
	if err != nil {
		s.end(r.ID)
		return jobs.Job{}, fmt.Errorf("record the run: %w", err)
	}
	finish := func(err error) {
		defer s.end(r.ID)
		status, msg := StatusSucceeded, ""
		if err != nil {
			status, msg = StatusFailed, err.Error()
		}
		_ = s.Store.FinishScheduleRun(bg, db.FinishScheduleRunParams{ID: runID, FinishedAt: s.now().UnixMilli(), Status: status, Message: msg})
		setLast(status, msg)
	}
	job, err := s.startJob(ctx, r, user, func(fn jobs.Func) jobs.Func {
		return func(ctx context.Context, out *jobs.Output) error {
			err := fn(ctx, out)
			finish(err)
			return err
		}
	})
	if err != nil {
		finish(err)
		return jobs.Job{}, err
	}
	_ = s.Store.SetScheduleRunJob(bg, db.SetScheduleRunJobParams{ID: runID, JobID: job.ID})
	return job, nil
}

// startJob starts the job of a schedule; wrap is applied to the job function so the run is
// finished when it ends. Actions of webhooks run their own jobs: they are followed instead.
func (s *Service) startJob(ctx context.Context, r db.Schedule, user string, wrap func(jobs.Func) jobs.Func) (jobs.Job, error) {
	v := s.view(r)
	label := "schedule " + r.Name
	spec := jobs.Spec{Environment: r.Environment, Namespace: r.Namespace, User: user}
	switch r.Kind {
	case KindPrune:
		env, ok := s.Environments.Get(r.Environment)
		if !ok {
			return jobs.Job{}, fmt.Errorf("%w: environment %q is gone", errdefs.ErrFailedPrecondition, r.Environment)
		}
		if !env.Runtime.System().Nerdctl(ctx).Writable() {
			return jobs.Job{}, webhooks.ErrReadOnly
		}
		o := v.Options
		opts := runtime.PruneOptions{Containers: o.Containers, Images: o.Images, AllImages: o.AllImages, Networks: o.Networks, BuildCache: o.BuildCache}
		sys, ns := env.Runtime.System(), r.Namespace
		spec.Kind, spec.Title = "system.prune", "Prune "+ns+" ("+label+")"
		spec.Resource = fmt.Sprintf("env/%s/ns/%s/prune", env.ID, ns)
		spec.Action = string(authz.SystemPrune)
		return s.Jobs.Start(spec, wrap(func(ctx context.Context, out *jobs.Output) error {
			return sys.Prune(ctx, ns, opts, out)
		}))
	case KindBackup:
		if s.Backup == nil {
			return jobs.Job{}, fmt.Errorf("%w: backups are not available", errdefs.ErrNotImplemented)
		}
		src, dir, keep, password := *s.Backup, v.Options.Directory, v.Options.Keep, r.Secret
		spec.Kind, spec.Title = "backup", "Backup ("+label+")"
		spec.Resource = "backup/" + dir
		spec.Action = string(authz.SettingsWrite)
		return s.Jobs.Start(spec, wrap(func(ctx context.Context, out *jobs.Output) error {
			file, err := WriteBackup(ctx, src, password, dir, keep, s.now())
			if err != nil {
				return err
			}
			out.SetResult("file", file)
			_, _ = fmt.Fprintf(out, "Backup written to %s\r\n", file)
			return nil
		}))
	case KindVolumeBackup:
		env, ok := s.Environments.Get(r.Environment)
		if !ok {
			return jobs.Job{}, fmt.Errorf("%w: environment %q is gone", errdefs.ErrFailedPrecondition, r.Environment)
		}
		if s.Volumes == nil {
			return jobs.Job{}, fmt.Errorf("%w: volume backups are not available", errdefs.ErrNotImplemented)
		}
		vb, rt, ns, volume, o := s.Volumes, env.Runtime, r.Namespace, r.Target, v.Options
		spec.Kind, spec.Title = "volume.backup", "Back up volume "+volume+" ("+label+")"
		spec.Resource = fmt.Sprintf("env/%s/ns/%s/volume/%s", env.ID, ns, volume)
		spec.Action = string(authz.VolumesWrite)
		return s.Jobs.Start(spec, wrap(func(ctx context.Context, out *jobs.Output) error {
			key, err := vb.Backup(ctx, rt, o.TargetID, ns, volume, volbackup.Options{Keep: o.Keep, StopContainers: o.StopContainers}, out)
			if key != "" {
				out.SetResult("key", key)
			}
			return err
		}))
	case KindImageUpdates:
		env, ok := s.Environments.Get(r.Environment)
		if !ok {
			return jobs.Job{}, fmt.Errorf("%w: environment %q is gone", errdefs.ErrFailedPrecondition, r.Environment)
		}
		if s.CheckImages == nil {
			return jobs.Job{}, fmt.Errorf("%w: image update checks are not available", errdefs.ErrNotImplemented)
		}
		check := s.CheckImages
		spec.Kind, spec.Title = "image-updates.check", "Check for image updates ("+label+")"
		spec.Resource = fmt.Sprintf("env/%s/image-updates", env.ID)
		spec.Action = string(authz.ImagesRead)
		return s.Jobs.Start(spec, wrap(func(ctx context.Context, _ *jobs.Output) error {
			return check(ctx, env)
		}))
	case KindStackRedeploy, KindStackRestart, KindContainerRedeploy, KindContainerRestart:
		if s.Actions == nil {
			return jobs.Job{}, fmt.Errorf("%w: actions are not available", errdefs.ErrNotImplemented)
		}
		targetKind, action, _ := strings.Cut(r.Kind, ".")
		return s.followAction(ctx, webhooks.Action{
			Environment: r.Environment, Namespace: r.Namespace, TargetKind: targetKind, Target: r.Target,
			Action: action, User: user, Label: label,
		}, wrap)
	}
	return jobs.Job{}, fmt.Errorf("%w: unknown kind %q", errdefs.ErrFailedPrecondition, r.Kind)
}

// followAction starts an action's job (run by the webhooks service) and follows it, so the run
// finishes with the action.
func (s *Service) followAction(ctx context.Context, a webhooks.Action, wrap func(jobs.Func) jobs.Func) (jobs.Job, error) {
	job, err := s.Actions.StartAction(ctx, a)
	if err != nil {
		return jobs.Job{}, err
	}
	done := wrap(func(ctx context.Context, _ *jobs.Output) error {
		for {
			cur, ok := s.Jobs.Get(job.ID)
			switch {
			case !ok:
				return errors.New("the job was removed before it finished")
			case cur.Status == jobs.StatusSucceeded:
				return nil
			case cur.Done():
				if cur.Error != "" {
					return errors.New(cur.Error)
				}
				return fmt.Errorf("job %s", cur.Status)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(s.pollEvery()):
			}
		}
	})
	go func() { _ = done(context.WithoutCancel(ctx), nil) }()
	return job, nil
}

func (s *Service) pollEvery() time.Duration {
	if s.Poll > 0 {
		return s.Poll
	}
	return time.Second
}

type ticker struct {
	c    <-chan time.Time
	stop func()
}

func (s *Service) newTicker() ticker {
	every := s.Every
	if every <= 0 {
		every = tick
	}
	t := time.NewTicker(every)
	return ticker{c: t.C, stop: t.Stop}
}

// BackupPattern matches the archives WriteBackup writes.
const BackupPattern = "nerdui-backup-*.tar.gz.age"

// WriteBackup writes an archive into dir and keeps the newest keep archives there.
func WriteBackup(ctx context.Context, src backup.Source, password, dir string, keep int, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	file := filepath.Join(dir, backup.FileName(now))
	tmp, err := os.CreateTemp(dir, ".nerdui-backup-*.part")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // renamed on success
	if err := backup.Write(ctx, src, password, tmp); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), file); err != nil {
		return "", err
	}
	old, err := filepath.Glob(filepath.Join(dir, BackupPattern))
	if err != nil {
		return file, err
	}
	slices.Sort(old) // the names sort by time
	for len(old) > keep {
		if err := os.Remove(old[0]); err != nil {
			return file, fmt.Errorf("remove an old backup: %w", err)
		}
		old = old[1:]
	}
	return file, nil
}
