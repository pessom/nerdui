// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package autoheal

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/notify"
	"github.com/pessom/nerdui/internal/runtime"
)

type fakeRuntime struct {
	runtime.Runtime
	containers *fakeContainers
}

func (f fakeRuntime) System() runtime.System         { return fakeSystem{} }
func (f fakeRuntime) Containers() runtime.Containers { return f.containers }

type fakeSystem struct{ runtime.System }

func (fakeSystem) Namespaces(context.Context) ([]string, error) { return []string{"default"}, nil }

type fakeContainers struct {
	runtime.Containers
	list     []runtime.Container
	restarts []string
	err      error
}

func (f *fakeContainers) List(context.Context, string) ([]runtime.Container, error) {
	return f.list, nil
}

func (f *fakeContainers) Do(_ context.Context, _, id string, a runtime.Action, _ runtime.ActionOptions) error {
	if a != runtime.ActionRestart {
		return fmt.Errorf("unexpected action %s", a)
	}
	f.restarts = append(f.restarts, id)
	return f.err
}

func container(id string, healed bool, status string, streak int) runtime.Container {
	labels := map[string]string{
		"nerdctl/healthstate": fmt.Sprintf(`{"Status":%q,"FailingStreak":%d}`, status, streak),
		"nerdctl/healthcheck": `{"Test":["CMD","false"],"Retries":2}`,
	}
	if healed {
		labels[Label] = "true"
	}
	return runtime.Container{ID: id, Name: id, Namespace: "default", State: runtime.StateRunning, Labels: labels}
}

type harness struct {
	h      *Healer
	cs     *fakeContainers
	events []notify.Event
	now    time.Time
}

func newHarness() *harness {
	x := &harness{cs: &fakeContainers{}, now: time.Unix(1_800_000_000, 0)}
	env := &environment.Environment{ID: "local", Runtime: fakeRuntime{containers: x.cs}}
	x.h = &Healer{
		Environments: environment.NewRegistry(env),
		Notify:       func(ev notify.Event) { x.events = append(x.events, ev) },
		Now:          func() time.Time { return x.now },
	}
	return x
}

// step sets the containers, checks once and returns the new events.
func (x *harness) step(list ...runtime.Container) []notify.Event {
	x.cs.list = list
	before := len(x.events)
	x.h.Check(context.Background())
	return x.events[before:]
}

func kinds(evs []notify.Event) []string {
	var out []string
	for _, ev := range evs {
		out = append(out, ev.Kind+" "+ev.Object)
	}
	return out
}

func TestRestartsLabeledUnhealthyContainer(t *testing.T) {
	x := newHarness()
	check := func(name string, got []notify.Event, restarts int, want ...string) {
		t.Helper()
		if fmt.Sprint(kinds(got)) != fmt.Sprint(want) || len(x.cs.restarts) != restarts {
			t.Errorf("%s: events %q, restarts %d; want %q, %d", name, kinds(got), len(x.cs.restarts), want, restarts)
		}
	}
	check("healthy", x.step(container("web", true, "healthy", 0)), 0)
	check("unhealthy", x.step(container("web", true, "unhealthy", 2)), 1,
		"container.unhealthy web", "container.autoheal web (restart 1)")
	// nerdctl keeps the streak across restarts: wait for Retries (2) new failures.
	check("one more failure", x.step(container("web", true, "unhealthy", 3)), 1)
	check("retries more", x.step(container("web", true, "unhealthy", 4)), 2, "container.autoheal web (restart 2)")
	check("third", x.step(container("web", true, "unhealthy", 6)), 3, "container.autoheal web (restart 3)")
	check("give up", x.step(container("web", true, "unhealthy", 8)), 3, "container.autoheal web")
	if ev := x.events[len(x.events)-1]; ev.Severity != notify.SeverityError {
		t.Errorf("give up severity %s", ev.Severity)
	}
	check("given up", x.step(container("web", true, "unhealthy", 10)), 3)

	x.now = x.now.Add(RestartWindow)
	check("after an hour", x.step(container("web", true, "unhealthy", 12)), 4, "container.autoheal web (restart 1)")

	// Healthy again: the next failure is reported and restarted at once.
	check("recovered", x.step(container("web", true, "healthy", 0)), 4)
	check("unhealthy again", x.step(container("web", true, "unhealthy", 2)), 5,
		"container.unhealthy web", "container.autoheal web (restart 2)")
}

func TestReportsUnlabeledUnhealthyContainer(t *testing.T) {
	x := newHarness()
	stopped := container("old", false, "unhealthy", 5)
	stopped.State = runtime.StateExited
	got := x.step(container("db", false, "unhealthy", 3), stopped,
		runtime.Container{ID: "plain", State: runtime.StateRunning, Labels: map[string]string{}})
	if fmt.Sprint(kinds(got)) != "[container.unhealthy db]" || len(x.cs.restarts) != 0 {
		t.Errorf("events %q, restarts %v", kinds(got), x.cs.restarts)
	}
	if got := x.step(container("db", false, "unhealthy", 4)); len(got) != 0 {
		t.Errorf("repeated: %q", kinds(got))
	}
	// A container removed and created again under the same id is new.
	x.step()
	if got := x.step(container("db", false, "unhealthy", 1)); len(got) != 1 {
		t.Errorf("after removal: %q", kinds(got))
	}
}

func TestRestartFailure(t *testing.T) {
	x := newHarness()
	x.cs.err = errors.New("boom")
	got := x.step(container("web", true, "unhealthy", 2))
	if len(got) != 2 || got[1].Severity != notify.SeverityError || got[1].Message != "boom" {
		t.Errorf("events %+v", got)
	}
}

func TestEnabled(t *testing.T) {
	for v, want := range map[string]bool{"true": true, "YES": true, "1": true, "on": true, "false": false, "": false} {
		if got := Enabled(map[string]string{Label: v}); got != want {
			t.Errorf("%q: %v", v, got)
		}
	}
}
