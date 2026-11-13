// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
)

func TestOutputAppliesProgressRedraws(t *testing.T) {
	o := newOutput(1 << 20)
	_, _ = o.Write([]byte("resolving\n"))
	// Two progress frames of two lines, redrawn like containerd's progress writer.
	_, _ = o.Write([]byte("layer1 10%\nlayer2  0%\n"))
	for range 2 {
		_, _ = o.Write(clearLine)
	}
	_, _ = o.Write([]byte("layer1 100%\nlayer2  50%\n"))
	_, _ = o.Write([]byte("done"))
	if got, want := o.String(), "resolving\nlayer1 100%\nlayer2  50%\ndone"; got != want {
		t.Errorf("screen %q, want %q", got, want)
	}

	// Sequences inside one write, and erasing with a partial line.
	o = newOutput(1 << 20)
	_, _ = o.Write([]byte("a\nb\npartial" + string(clearLine) + "c\n"))
	if got := o.String(); got != "a\nc\n" {
		t.Errorf("screen %q", got)
	}
}

func TestOutputCap(t *testing.T) {
	o := newOutput(20)
	for i := range 10 {
		_, _ = fmt.Fprintf(o, "line %d\n", i)
	}
	got := o.String()
	if !strings.Contains(got, "earlier output dropped") || !strings.HasSuffix(got, "line 9\n") || strings.Contains(got, "line 0") {
		t.Errorf("capped screen %q", got)
	}
}

func wait(t *testing.T, m *Manager, id string) Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if j, _ := m.Get(id); j.Done() {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish", id)
	return Job{}
}

func TestJobLifecycle(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)

	ok, err := m.Start(Spec{Kind: "test", Title: "ok"}, func(_ context.Context, out *Output) error {
		_, _ = out.Write([]byte("hello\n"))
		out.SetResult("containerId", "abc")
		return nil
	})
	if err != nil || ok.Status != StatusRunning || ok.ID == "" {
		t.Fatalf("start: %+v %v", ok, err)
	}
	if j := wait(t, m, ok.ID); j.Status != StatusSucceeded || j.FinishedAt == nil || j.Result["containerId"] != "abc" {
		t.Errorf("succeeded job %+v", j)
	}
	_, out, ch, _, _ := m.Subscribe(ok.ID)
	if out != "hello\n" || ch != nil {
		t.Errorf("finished job output %q, channel %v", out, ch)
	}

	failed, _ := m.Start(Spec{Title: "fail"}, func(context.Context, *Output) error { return errors.New("boom") })
	if j := wait(t, m, failed.ID); j.Status != StatusFailed || j.Error != "boom" {
		t.Errorf("failed job %+v", j)
	}
	panicked, _ := m.Start(Spec{Title: "panic"}, func(context.Context, *Output) error { panic("oops") })
	if j := wait(t, m, panicked.ID); j.Status != StatusFailed || !strings.Contains(j.Error, "oops") {
		t.Errorf("panicked job %+v", j)
	}

	list := m.List()
	if len(list) != 3 || list[0].ID != panicked.ID {
		t.Errorf("list newest first: %+v", list)
	}
}

func TestResourceLockAndCancel(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)
	started := make(chan struct{})
	j, err := m.Start(Spec{Resource: "image:nginx"}, func(ctx context.Context, _ *Output) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := m.Start(Spec{Resource: "image:nginx"}, func(context.Context, *Output) error { return nil }); !errdefs.IsConflict(err) {
		t.Errorf("second job on the same resource: %v", err)
	}
	if _, err := m.Cancel("missing"); !errdefs.IsNotFound(err) {
		t.Errorf("cancel unknown: %v", err)
	}
	if _, err := m.Cancel(j.ID); err != nil {
		t.Fatal(err)
	}
	if done := wait(t, m, j.ID); done.Status != StatusCanceled {
		t.Errorf("canceled job %+v", done)
	}
	// The lock is released.
	next, err := m.Start(Spec{Resource: "image:nginx"}, func(context.Context, *Output) error { return nil })
	if err != nil {
		t.Fatalf("after cancel: %v", err)
	}
	wait(t, m, next.ID)
}

func TestSubscribeStreamsInOrder(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)
	proceed := make(chan struct{})
	j, _ := m.Start(Spec{}, func(_ context.Context, out *Output) error {
		_, _ = out.Write([]byte("before\n"))
		<-proceed
		for i := range 50 {
			_, _ = fmt.Fprintf(out, "%d\n", i)
		}
		return nil
	})
	var snap string
	var ch <-chan Update
	deadline := time.Now().Add(5 * time.Second)
	for snap == "" && time.Now().Before(deadline) { // until "before" is written
		_, snap, ch, _, _ = m.Subscribe(j.ID)
	}
	if snap != "before\n" {
		t.Fatalf("snapshot %q", snap)
	}
	close(proceed)
	var got strings.Builder
	var final *Job
	for u := range ch {
		got.Write(u.Output)
		if u.Job != nil {
			final = u.Job
		}
	}
	var want strings.Builder
	for i := range 50 {
		fmt.Fprintf(&want, "%d\n", i)
	}
	if got.String() != want.String() {
		t.Errorf("streamed %q", got.String())
	}
	if final == nil || final.Status != StatusSucceeded {
		t.Errorf("final update %+v", final)
	}
}

func TestPrune(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m.MaxJobs = 3

	block := make(chan struct{})
	running, _ := m.Start(Spec{Title: "running"}, func(context.Context, *Output) error { <-block; return nil })
	t.Cleanup(func() { close(block) })
	var finished []string
	for i := range 4 {
		j, _ := m.Start(Spec{Title: fmt.Sprint(i)}, func(context.Context, *Output) error { return nil })
		wait(t, m, j.ID)
		finished = append(finished, j.ID)
	}
	ids := func() []string {
		var out []string
		for _, j := range m.List() {
			out = append(out, j.ID)
		}
		return out
	}
	// MaxJobs keeps room for the next job; the running one stays.
	if got := ids(); len(got) != 3 || got[len(got)-1] != running.ID {
		t.Errorf("after max: %v", got)
	}
	now = now.Add(2 * time.Hour)
	if got := ids(); len(got) != 1 || got[0] != running.ID {
		t.Errorf("after retention: %v (finished %v)", got, finished)
	}
}
