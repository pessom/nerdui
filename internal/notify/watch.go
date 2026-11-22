// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package notify

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/events"
	"github.com/pessom/nerdui/internal/runtime"
)

// explicitlyStopped is containerd's restart label, set by `nerdctl stop`/`kill` and by nerdui's
// own stop and kill: such exits were asked for.
const explicitlyStopped = "containerd.io/restart.explicitly-stopped"

const (
	// settleDelay lets `rm -f`, stop and recreate finish before an exit is judged.
	settleDelay  = 3 * time.Second
	maxInFlight  = 8
	watchBuffer  = 1024
	resubscribes = time.Second
)

// Watcher turns containerd events of an environment into notifications about containers that
// crashed or ran out of memory (#86).
type Watcher struct {
	Env      *environment.Environment
	Notifier *Service
	Logger   *slog.Logger
	// Delay overrides settleDelay (tests).
	Delay time.Duration
}

func (w *Watcher) log() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
}

// Run watches until ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	delay := w.Delay
	if delay == 0 {
		delay = settleDelay
	}
	sem := make(chan struct{}, maxInFlight)
	for ctx.Err() == nil {
		sub := w.Env.Events.Subscribe(events.Filter{Topics: []string{"/tasks/exit", "/tasks/oom"}}, watchBuffer)
		for ev := range sub.C {
			if ev.Kind == events.KindNerdui || ev.ID == "" || ev.Attributes["execId"] != "" {
				continue
			}
			if ev.Action == "exit" && (ev.Attributes["exitStatus"] == "" || ev.Attributes["exitStatus"] == "0") {
				continue
			}
			select {
			case sem <- struct{}{}:
			default:
				continue // a burst of crashes: the dedupe would drop most of them anyway
			}
			go func() {
				defer func() { <-sem }()
				w.handle(ctx, ev, delay)
			}()
		}
		sub.Close()
		select {
		case <-ctx.Done():
		case <-time.After(resubscribes):
		}
	}
}

func (w *Watcher) handle(ctx context.Context, ev runtime.Event, delay time.Duration) {
	if ev.Action == "exit" {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
	list, err := w.Env.Runtime.Containers().List(ctx, ev.Namespace)
	if err != nil {
		w.log().Debug("notification watcher: list containers", "error", err)
		return
	}
	var ct *runtime.Container
	for i := range list {
		if list[i].ID == ev.ID {
			ct = &list[i]
		}
	}
	if ct == nil {
		return // removed meanwhile (rm -f, recreate)
	}
	name := ct.Name
	if name == "" {
		name = ct.ID[:min(12, len(ct.ID))]
	}
	out := Event{
		Severity: SeverityError, Environment: w.Env.ID, Namespace: ev.Namespace, Object: name, Time: ev.Timestamp,
	}
	switch ev.Action {
	case "oom":
		out.Kind = KindContainerOOM
		out.Title = "Container " + name + " ran out of memory"
		out.Message = "Image " + ct.Image
	case "exit":
		project := ""
		if ct.Compose != nil {
			project = ct.Compose.Project
		}
		if ct.Labels[explicitlyStopped] == "true" || runtime.ExpectedExit(ev.Namespace, ct.ID, project, ev.Timestamp) {
			return
		}
		code, _ := strconv.Atoi(ev.Attributes["exitStatus"])
		out.Kind = KindContainerDied
		out.Title = fmt.Sprintf("Container %s exited with code %d", name, code)
		out.Message = "Image " + ct.Image
		if ct.State == runtime.StateRunning {
			out.Message += "; it was restarted by its restart policy"
		}
	default:
		return
	}
	w.Notifier.Notify(out)
}
