// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package autoheal reports containers that turn unhealthy and restarts those that opt in with
// the nerdui.auto-heal label (#22, ADR-0043).
package autoheal

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/notify"
	"github.com/pessom/nerdui/internal/runtime"
)

// Label opts a container in to restarts when it is unhealthy.
const Label = "nerdui.auto-heal"

const (
	// DefaultInterval is how often container health is read.
	DefaultInterval = 30 * time.Second
	// MaxRestarts within RestartWindow; beyond it a container is left unhealthy.
	MaxRestarts   = 3
	RestartWindow = time.Hour
)

// Enabled reports whether labels opt in to restarts when unhealthy.
func Enabled(labels map[string]string) bool {
	switch strings.ToLower(strings.TrimSpace(labels[Label])) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

// Environments lists environments; *environment.Registry implements it.
type Environments interface {
	List() []*environment.Environment
}

// Healer reads the health of running containers on every interval.
type Healer struct {
	Environments Environments
	Notify       func(notify.Event)
	Logger       *slog.Logger
	// Interval overrides DefaultInterval.
	Interval time.Duration
	// Now overrides time.Now (tests).
	Now func() time.Time

	mu      sync.Mutex
	tracked map[string]*tracked
}

// tracked is what the healer remembers about a container between checks.
type tracked struct {
	status string
	// restarted is set after a restart until the container is healthy again: nerdctl keeps the
	// failing streak across restarts, so another restart waits for Retries more failed checks
	// on top of streak.
	restarted bool
	streak    int
	restarts  []time.Time
	gaveUp    bool
}

func (h *Healer) log() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

func (h *Healer) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// Run checks until ctx ends.
func (h *Healer) Run(ctx context.Context) {
	interval := h.Interval
	if interval == 0 {
		interval = DefaultInterval
	}
	// The first check waits for an interval: right after start nerdui may still be connecting
	// to containerd, which does not follow ctx and would delay a shutdown.
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		h.Check(ctx)
	}
}

// Check reads the health of the containers of every environment once.
func (h *Healer) Check(ctx context.Context) {
	for _, env := range h.Environments.List() {
		if ctx.Err() != nil {
			return
		}
		if err := h.checkEnv(ctx, env); err != nil && ctx.Err() == nil {
			h.log().Debug("auto-heal: read containers", "environment", env.ID, "error", err)
		}
	}
}

func (h *Healer) checkEnv(ctx context.Context, env *environment.Environment) error {
	nss, err := env.Runtime.System().Namespaces(ctx)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, ns := range nss {
		list, err := env.Runtime.Containers().List(ctx, ns)
		if err != nil {
			return err
		}
		for _, ct := range list {
			key := env.ID + "\x00" + ns + "\x00" + ct.ID
			seen[key] = true
			h.observe(ctx, env, key, ct)
		}
	}
	// Forget containers that are gone.
	h.mu.Lock()
	for key := range h.tracked {
		if strings.HasPrefix(key, env.ID+"\x00") && !seen[key] {
			delete(h.tracked, key)
		}
	}
	h.mu.Unlock()
	return nil
}

func (h *Healer) observe(ctx context.Context, env *environment.Environment, key string, ct runtime.Container) {
	health, ok := runtime.ContainerHealth(ct.Labels)
	if !ok || ct.State != runtime.StateRunning {
		h.mu.Lock()
		delete(h.tracked, key)
		h.mu.Unlock()
		return
	}
	h.mu.Lock()
	if h.tracked == nil {
		h.tracked = map[string]*tracked{}
	}
	t := h.tracked[key]
	if t == nil {
		t = &tracked{}
		h.tracked[key] = t
	}
	prev := t.status
	t.status = health.Status
	if health.Status != "unhealthy" {
		t.restarted, t.gaveUp = false, false
		h.mu.Unlock()
		return
	}
	enabled := Enabled(ct.Labels)
	name := ct.Name
	if name == "" {
		name = ct.ID[:min(12, len(ct.ID))]
	}
	base := notify.Event{Environment: env.ID, Namespace: ct.Namespace, Object: name}
	if prev != "unhealthy" {
		ev := base
		ev.Kind, ev.Severity = notify.KindContainerUnhealthy, notify.SeverityWarning
		ev.Title = "Container " + name + " is unhealthy"
		ev.Message = fmt.Sprintf("%d failed health checks in a row", health.FailingStreak)
		if enabled {
			ev.Message += "; nerdui restarts it (" + Label + ")"
		}
		h.notify(ev)
	}
	if !enabled || (t.restarted && health.FailingStreak < t.streak+health.Retries) {
		h.mu.Unlock()
		return
	}
	now := h.now()
	recent := t.restarts[:0]
	for _, at := range t.restarts {
		if now.Sub(at) < RestartWindow {
			recent = append(recent, at)
		}
	}
	t.restarts = recent
	if len(t.restarts) >= MaxRestarts {
		if !t.gaveUp {
			t.gaveUp = true
			ev := base
			ev.Kind, ev.Severity = notify.KindContainerAutoHeal, notify.SeverityError
			ev.Title = "Container " + name + " is still unhealthy"
			ev.Message = fmt.Sprintf("Restarted %d times within %s; nerdui does not restart it again until it is healthy or an hour has passed", len(t.restarts), RestartWindow)
			h.notify(ev)
		}
		h.mu.Unlock()
		return
	}
	t.restarted, t.streak = true, health.FailingStreak
	t.restarts = append(t.restarts, now)
	count := len(t.restarts)
	h.mu.Unlock()

	h.log().Info("auto-heal: restarting unhealthy container", "environment", env.ID, "namespace", ct.Namespace, "container", name)
	ev := base
	ev.Kind = notify.KindContainerAutoHeal
	if err := env.Runtime.Containers().Do(ctx, ct.Namespace, ct.ID, runtime.ActionRestart, runtime.ActionOptions{}); err != nil {
		h.log().Warn("auto-heal: restart failed", "container", name, "error", err)
		ev.Severity = notify.SeverityError
		ev.Title = "Restart of unhealthy container " + name + " failed"
		ev.Message = err.Error()
	} else {
		ev.Severity = notify.SeverityWarning
		ev.Title = "Restarted unhealthy container " + name
		ev.Message = fmt.Sprintf("%d failed health checks in a row", health.FailingStreak)
	}
	// Each restart is its own event: the notifier drops repeats of an object within 15 minutes,
	// and a restart number repeats only after RestartWindow.
	ev.Object = fmt.Sprintf("%s (restart %d)", name, count)
	h.notify(ev)
}

func (h *Healer) notify(ev notify.Event) {
	if h.Notify != nil {
		ev.Time = h.now()
		h.Notify(ev)
	}
}
