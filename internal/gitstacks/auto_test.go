// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package gitstacks

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/notify"
)

// syncJobs runs jobs inline.
type syncJobs struct{ got []jobs.Spec }

func (j *syncJobs) Start(spec jobs.Spec, fn jobs.Func) (jobs.Job, error) {
	j.got = append(j.got, spec)
	return jobs.Job{}, fn(context.Background(), &jobs.Output{})
}

func TestAutoSettingsAndWebhook(t *testing.T) {
	ctx := context.Background()
	src := newSource(t)
	src.commit(map[string]string{"compose.yaml": "services: {}\n"}, "first")
	s := newService(t)
	if _, err := s.Create(ctx, CreateRequest{Name: "web", URL: "file://" + src.dir}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		mode     string
		interval int64
	}{{"sometimes", 0}, {AutoDeploy, 1}, {AutoNotify, -60}} {
		if err := s.SetAuto(ctx, "web", c.mode, c.interval); !errdefs.IsInvalidArgument(err) {
			t.Errorf("%+v: %v", c, err)
		}
	}
	if err := s.SetAuto(ctx, "ghost", AutoOff, 0); !errdefs.IsNotFound(err) {
		t.Errorf("unknown: %v", err)
	}
	token, err := s.NewWebhook(ctx, "web")
	if err != nil || len(token) < 40 {
		t.Fatalf("token %q %v", token, err)
	}
	if row, err := s.ByWebhook(ctx, token); err != nil || row.Name != "web" || strings.Contains(row.WebhookHash, token) {
		t.Errorf("by webhook %+v %v", row, err)
	}
	again, _ := s.NewWebhook(ctx, "web")
	if _, err := s.ByWebhook(ctx, token); !errdefs.IsNotFound(err) {
		t.Errorf("replaced token still valid: %v", err)
	}
	if err := s.DeleteWebhook(ctx, "web"); err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{again, ""} {
		if _, err := s.ByWebhook(ctx, tok); !errdefs.IsNotFound(err) {
			t.Errorf("%q after delete: %v", tok, err)
		}
	}
}

func TestAutoPoll(t *testing.T) {
	ctx := context.Background()
	src := newSource(t)
	src.commit(map[string]string{"compose.yaml": "services: {}\n"}, "first")
	s := newService(t)
	now := time.Date(2027, 3, 1, 10, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	if _, err := s.Create(ctx, CreateRequest{Name: "web", URL: "file://" + src.dir}); err != nil {
		t.Fatal(err)
	}
	var events []notify.Event
	runner := &syncJobs{}
	a := &Auto{Service: s, Jobs: runner, Notify: func(e notify.Event) { events = append(events, e) }}

	// Off: never checked.
	a.Poll(ctx)
	if row, _ := s.Get(ctx, "web"); row.CheckedAt != 0 {
		t.Fatalf("checked while off: %+v", row)
	}

	// Notify: a new commit is announced once.
	if err := s.SetAuto(ctx, "web", AutoNotify, 300); err != nil {
		t.Fatal(err)
	}
	a.Poll(ctx)
	if row, _ := s.Get(ctx, "web"); row.CheckedAt != now.UnixMilli() || row.RemoteHash != "" || len(events) != 0 {
		t.Fatalf("up to date: %+v %v", row, events)
	}
	second := src.commit(map[string]string{"compose.yaml": "services: {web: {image: nginx}}\n"}, "second")
	now = now.Add(time.Minute)
	a.Poll(ctx) // the interval has not elapsed
	if row, _ := s.Get(ctx, "web"); row.RemoteHash != "" {
		t.Fatalf("polled too early: %+v", row)
	}
	now = now.Add(5 * time.Minute)
	a.Poll(ctx)
	now = now.Add(5 * time.Minute)
	a.Poll(ctx)
	row, _ := s.Get(ctx, "web")
	if row.RemoteHash != second.String() || len(events) != 1 || events[0].Kind != notify.KindGitUpdate || len(runner.got) != 0 {
		t.Fatalf("notify: %+v %+v %v", row, events, runner.got)
	}

	// A failed check is recorded and cleared by the next good one.
	gitDir := src.dir + "/.git"
	if err := os.Rename(gitDir, gitDir+".off"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CheckRemote(ctx, "web"); err == nil {
		t.Fatal("check of a missing repository passed")
	}
	if row, _ := s.Get(ctx, "web"); !strings.HasPrefix(row.LastError, checkFailed) || row.RemoteHash != second.String() {
		t.Errorf("failed check: %+v", row)
	}
	if err := os.Rename(gitDir+".off", gitDir); err != nil {
		t.Fatal(err)
	}

	// Deploy: pulled as a job (no environments: nothing to redeploy).
	if err := s.SetAuto(ctx, "web", AutoDeploy, 300); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Minute)
	a.Poll(ctx)
	row, _ = s.Get(ctx, "web")
	if len(runner.got) != 1 || runner.got[0].User != AutoUser || row.CommitHash != second.String() || row.RemoteHash != "" || row.LastError != "" {
		t.Fatalf("deploy: %+v %+v", row, runner.got)
	}
	if len(events) != 2 || !strings.Contains(events[1].Title, "updated") {
		t.Errorf("events %+v", events)
	}
	// Events name the commit, so the notifier's deduplication keeps distinct commits apart.
	if events[0].Object != "stack/web@"+Short(second.String()) || events[1].Object != events[0].Object {
		t.Errorf("objects %q %q", events[0].Object, events[1].Object)
	}

	// The next commit is announced again, under another object.
	if err := s.SetAuto(ctx, "web", AutoNotify, 300); err != nil {
		t.Fatal(err)
	}
	third := src.commit(map[string]string{"compose.yaml": "services: {web: {image: nginx:alpine}}\n"}, "third")
	now = now.Add(10 * time.Minute)
	a.Poll(ctx)
	if len(events) != 3 || events[2].Object != "stack/web@"+Short(third.String()) {
		t.Errorf("third commit events %+v", events)
	}
}
