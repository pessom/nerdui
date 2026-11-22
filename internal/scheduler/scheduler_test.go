// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package scheduler

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/backup"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/webhooks"
)

type fakeRuntime struct {
	runtime.Runtime
	sys *fakeSystem
}

func (f fakeRuntime) System() runtime.System { return f.sys }

type fakeSystem struct {
	runtime.System
	mu     sync.Mutex
	prunes []runtime.PruneOptions
	block  chan struct{}
}

func (f *fakeSystem) Nerdctl(context.Context) runtime.NerdctlStatus {
	return runtime.NerdctlStatus{Compatible: true}
}

func (f *fakeSystem) Prune(ctx context.Context, _ string, opts runtime.PruneOptions, _ io.Writer) error {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prunes = append(f.prunes, opts)
	return nil
}

type fakeActions struct {
	jobs *jobs.Manager
	got  []webhooks.Action
	err  error
}

func (f *fakeActions) StartAction(_ context.Context, a webhooks.Action) (jobs.Job, error) {
	f.got = append(f.got, a)
	return f.jobs.Start(jobs.Spec{Kind: "x", Title: "x", User: a.User}, func(context.Context, *jobs.Output) error { return f.err })
}

type harness struct {
	s   *Service
	sys *fakeSystem
	act *fakeActions
	now time.Time
	st  *store.Store
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	data := t.TempDir()
	st, err := store.Open(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	m := jobs.NewManager()
	t.Cleanup(m.Close)
	h := &harness{sys: &fakeSystem{}, now: time.Date(2027, 3, 1, 10, 0, 0, 0, time.UTC), st: st}
	h.act = &fakeActions{jobs: m}
	h.s = &Service{
		Store: st.Write(), Jobs: m, Actions: h.act, Location: time.UTC, Poll: 5 * time.Millisecond,
		Environments: environment.NewRegistry(&environment.Environment{ID: "local", Runtime: fakeRuntime{sys: h.sys}}),
		Backup:       &backup.Source{Store: st, DataDir: data, StacksDir: filepath.Join(data, "stacks")},
		Now:          func() time.Time { return h.now },
	}
	return h
}

// waitRun waits until the newest run of a schedule is not running.
func (h *harness) waitRun(t *testing.T, id int64) Run {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := h.s.Runs(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) > 0 && runs[0].Status != StatusRunning {
			return runs[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run did not finish")
	return Run{}
}

func TestValidation(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for name, in := range map[string]Input{
		"bad cron":         {Name: "a", Kind: KindPrune, Environment: "local", Namespace: "default", Cron: "every day", Options: Options{Images: true}},
		"nothing to prune": {Name: "a", Kind: KindPrune, Environment: "local", Namespace: "default", Cron: "@daily"},
		"no env":           {Name: "a", Kind: KindPrune, Environment: "nas", Namespace: "default", Cron: "@daily", Options: Options{Images: true}},
		"no target":        {Name: "a", Kind: KindStackRedeploy, Environment: "local", Namespace: "default", Cron: "@daily"},
		"no password":      {Name: "a", Kind: KindBackup, Cron: "@daily"},
		"short password":   {Name: "a", Kind: KindBackup, Cron: "@daily", Password: "short"},
		"relative dir":     {Name: "a", Kind: KindBackup, Cron: "@daily", Password: "correct horse battery", Options: Options{Directory: "backups"}},
		"unknown kind":     {Name: "a", Kind: "reboot", Cron: "@daily"},
		"no name":          {Kind: KindImageUpdates, Environment: "local", Cron: "@daily"},
	} {
		if _, err := h.s.Create(ctx, in, "admin"); !errdefs.IsInvalidArgument(err) {
			t.Errorf("%s: %v", name, err)
		}
	}

	b, err := h.s.Create(ctx, Input{Name: "nightly", Kind: KindBackup, Cron: "0 3 * * *", Password: "correct horse battery", Enabled: true}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !b.HasPassword || b.Options.Keep != DefaultBackupKeep || b.Options.Directory != filepath.Join(h.s.Backup.DataDir, "backups") {
		t.Errorf("backup %+v", b)
	}
	if b.NextRunAt == nil || !b.NextRunAt.Equal(time.Date(2027, 3, 2, 3, 0, 0, 0, time.UTC)) {
		t.Errorf("next run %v", b.NextRunAt)
	}
	// Editing without a password keeps the stored one.
	b, err = h.s.Update(ctx, b.ID, Input{Name: "nightly", Kind: KindBackup, Cron: "@weekly", Enabled: false, Options: Options{Keep: 3}})
	if err != nil || !b.HasPassword || b.Options.Keep != 3 || b.NextRunAt != nil {
		t.Errorf("update %+v %v", b, err)
	}
	if _, err := h.s.Create(ctx, Input{Name: "nightly", Kind: KindImageUpdates, Environment: "local", Cron: "@daily"}, ""); !errdefs.IsAlreadyExists(err) {
		t.Errorf("duplicate name: %v", err)
	}
	if err := h.s.Delete(ctx, 999); !errdefs.IsNotFound(err) {
		t.Errorf("delete missing: %v", err)
	}
}

func TestTickRunsDueSchedules(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	p, err := h.s.Create(ctx, Input{
		Name: "prune", Kind: KindPrune, Environment: "local", Namespace: "default", Cron: "*/5 * * * *",
		Options: Options{Images: true, AllImages: true}, Enabled: true,
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	off, err := h.s.Create(ctx, Input{Name: "off", Kind: KindPrune, Environment: "local", Namespace: "default", Cron: "* * * * *", Options: Options{Containers: true}}, "admin")
	if err != nil {
		t.Fatal(err)
	}

	h.s.Tick(ctx) // 10:00 — created now, next is 10:05
	h.now = h.now.Add(5 * time.Minute)
	h.s.Tick(ctx)
	run := h.waitRun(t, p.ID)
	if run.Status != StatusSucceeded || run.Trigger != TriggerSchedule || run.JobID == "" {
		t.Errorf("run %+v", run)
	}
	if len(h.sys.prunes) != 1 || !h.sys.prunes[0].AllImages {
		t.Errorf("prunes %+v", h.sys.prunes)
	}
	// Not again until 10:10.
	h.s.Tick(ctx)
	if runs, _ := h.s.Runs(ctx, p.ID); len(runs) != 1 {
		t.Errorf("runs %d", len(runs))
	}
	if runs, _ := h.s.Runs(ctx, off.ID); len(runs) != 0 {
		t.Errorf("disabled schedule ran %d times", len(runs))
	}
	list, _ := h.s.List(ctx)
	for _, v := range list {
		if v.ID == p.ID && (v.LastStatus != StatusSucceeded || v.NextRunAt == nil || !v.NextRunAt.Equal(time.Date(2027, 3, 1, 10, 10, 0, 0, time.UTC))) {
			t.Errorf("after the run: %+v", v)
		}
	}

	// A disabled schedule runs on request.
	if _, err := h.s.RunNow(ctx, off.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if run := h.waitRun(t, off.ID); run.Trigger != TriggerManual || run.Status != StatusSucceeded {
		t.Errorf("manual run %+v", run)
	}
}

func TestSkipWhileRunning(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.sys.block = make(chan struct{})
	p, err := h.s.Create(ctx, Input{Name: "p", Kind: KindPrune, Environment: "local", Namespace: "default", Cron: "@daily", Options: Options{Containers: true}, Enabled: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.RunNow(ctx, p.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.RunNow(ctx, p.ID, ""); !errdefs.IsConflict(err) {
		t.Errorf("second run: %v", err)
	}
	close(h.sys.block)
	runs, _ := h.s.Runs(ctx, p.ID)
	if len(runs) != 2 || runs[0].Status != StatusSkipped {
		t.Errorf("runs %+v", runs)
	}
	h.waitRun(t, p.ID)
}

func TestActionRuns(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	s, err := h.s.Create(ctx, Input{Name: "nightly redeploy", Kind: KindStackRedeploy, Environment: "local", Namespace: "default", Target: "shop", Cron: "@daily", Enabled: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.RunNow(ctx, s.ID, ""); err != nil {
		t.Fatal(err)
	}
	if run := h.waitRun(t, s.ID); run.Status != StatusSucceeded {
		t.Errorf("run %+v", run)
	}
	a := h.act.got[0]
	if a.TargetKind != "stack" || a.Action != "redeploy" || a.Target != "shop" || a.User != User || a.Label != "schedule nightly redeploy" {
		t.Errorf("action %+v", a)
	}
	h.act.err = errors.New("pull failed")
	if _, err := h.s.RunNow(ctx, s.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if run := h.waitRun(t, s.ID); run.Status != StatusFailed || run.Message != "pull failed" {
		t.Errorf("failed run %+v", run)
	}
}

func TestScheduledBackups(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "backups")
	s, err := h.s.Create(ctx, Input{Name: "b", Kind: KindBackup, Cron: "@daily", Password: "correct horse battery", Options: Options{Directory: dir, Keep: 2}}, "")
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := h.s.RunNow(ctx, s.ID, ""); err != nil {
			t.Fatal(err)
		}
		if run := h.waitRun(t, s.ID); run.Status != StatusSucceeded {
			t.Fatalf("run %+v", run)
		}
		h.now = h.now.Add(time.Hour)
	}
	files, _ := filepath.Glob(filepath.Join(dir, BackupPattern))
	if len(files) != 2 || filepath.Base(files[0]) != backup.FileName(h.now.Add(-2*time.Hour)) {
		t.Errorf("files %v", files)
	}
	if fi, err := os.Stat(files[1]); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v %v", fi.Mode(), err)
	}
}

func TestMigrateImageChecks(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	settings := h.st.Write()
	interval := func(context.Context, string) (time.Duration, error) { return 12 * time.Hour, nil }
	for range 2 {
		if err := h.s.MigrateImageChecks(ctx, settings, []string{"local"}, interval); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := h.s.List(ctx)
	if len(list) != 1 || list[0].Kind != KindImageUpdates || list[0].Cron != "0 */12 * * *" || !list[0].Enabled {
		t.Fatalf("schedules %+v", list)
	}
	if n := h.s.NextOf(ctx, KindImageUpdates, "local"); n == nil || !n.Equal(time.Date(2027, 3, 1, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("next %v", n)
	}
	// Removed by the user: not created again.
	_ = h.s.Delete(ctx, list[0].ID)
	_ = h.s.MigrateImageChecks(ctx, settings, []string{"local"}, interval)
	if list, _ := h.s.List(ctx); len(list) != 0 {
		t.Errorf("recreated: %+v", list)
	}
	if expr, on := intervalCron(0); on || expr == "" {
		t.Errorf("off: %s %v", expr, on)
	}
}
