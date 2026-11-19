// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package updates finds images whose tag points to a newer digest in the registry (#53,
// ADR-0030). Only the registry digest is stored; whether an update is available is decided when
// reading, against the local image at that moment, so a pull clears it at once.
package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

// Intervals are the allowed check intervals; 0 turns scheduled checks off.
var Intervals = []time.Duration{0, time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour}

// DefaultInterval applies until an administrator chooses another.
const DefaultInterval = 6 * time.Hour

const (
	concurrency   = 4
	refTimeout    = 20 * time.Second
	tick          = time.Minute
	firstDelay    = 2 * time.Minute // after start, let containerd and the UI settle
	maxErrorChars = 300
)

var (
	// ErrInvalidInterval is returned for intervals outside Intervals.
	ErrInvalidInterval = errors.New("invalid interval")
	// ErrBusy is returned while a check of the environment runs.
	ErrBusy = errors.New("a check is already running")
)

// Store is the database used by the checker; *db.Queries of the write connection implements it.
type Store interface {
	ListImageUpdates(ctx context.Context, environment string) ([]db.ImageUpdate, error)
	SetImageUpdate(ctx context.Context, arg db.SetImageUpdateParams) error
	LastImageUpdateCheck(ctx context.Context, environment string) (int64, error)
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, arg db.SetSettingParams) error
}

// Checker checks the images used by containers on a schedule and on request.
type Checker struct {
	Environments *environment.Registry
	Store        Store
	Logger       *slog.Logger
	Now          func() time.Time
	// AfterCheck runs after each successful scheduled check, e.g. Updater.Run (#54).
	AfterCheck func(ctx context.Context, env *environment.Environment) error

	mu      sync.Mutex
	running map[string]bool
	lastRun map[string]time.Time
}

// Status is the update state of one image reference in a namespace.
type Status struct {
	Ref          string    `json:"ref"`
	LocalDigest  string    `json:"localDigest"`
	RemoteDigest string    `json:"remoteDigest,omitempty"`
	CheckedAt    time.Time `json:"checkedAt"`
	Error        string    `json:"error,omitempty"`
	// UpdateAvailable: the registry's digest differs from the local one.
	UpdateAvailable bool `json:"updateAvailable"`
	// Containers counts the namespace's containers created from the reference.
	Containers int `json:"containers"`
}

// Overview is the state of an environment's update checks for one namespace.
type Overview struct {
	IntervalSeconds int64      `json:"intervalSeconds"`
	LastCheck       *time.Time `json:"lastCheck,omitempty"`
	NextCheck       *time.Time `json:"nextCheck,omitempty"`
	Running         bool       `json:"running"`
	Images          []Status   `json:"images"`
}

// Report summarizes a check.
type Report struct {
	Checked int `json:"checked"`
	Updates int `json:"updates"`
	Failed  int `json:"failed"`
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Checker) log() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}

func settingKey(env string) string { return "environments/" + env + "/image-updates" }

type settings struct {
	IntervalSeconds int64 `json:"intervalSeconds"`
}

// Interval returns the environment's check interval.
func (c *Checker) Interval(ctx context.Context, env string) (time.Duration, error) {
	v, err := c.Store.GetSetting(ctx, settingKey(env))
	if store.IsNotFound(err) {
		return DefaultInterval, nil
	}
	if err != nil {
		return 0, err
	}
	var s settings
	if err := json.Unmarshal([]byte(v), &s); err != nil {
		return DefaultInterval, nil //nolint:nilerr // a damaged value falls back to the default
	}
	d := time.Duration(s.IntervalSeconds) * time.Second
	if !slices.Contains(Intervals, d) {
		return DefaultInterval, nil
	}
	return d, nil
}

// SetInterval stores the environment's check interval.
func (c *Checker) SetInterval(ctx context.Context, env string, d time.Duration) error {
	if !slices.Contains(Intervals, d) {
		return fmt.Errorf("%w: %s", ErrInvalidInterval, d)
	}
	b, _ := json.Marshal(settings{IntervalSeconds: int64(d / time.Second)})
	return c.Store.SetSetting(ctx, db.SetSettingParams{Key: settingKey(env), Value: string(b), Now: c.now().Unix()})
}

// Run checks every environment when its interval has passed, until ctx ends.
func (c *Checker) Run(ctx context.Context) {
	timer := time.NewTimer(firstDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		for _, env := range c.Environments.List() {
			if !c.due(ctx, env.ID) {
				continue
			}
			if _, err := c.Check(ctx, env, ""); err != nil {
				if !errors.Is(err, ErrBusy) && ctx.Err() == nil {
					c.log().Warn("image update check failed", "environment", env.ID, "error", err)
				}
				continue
			}
			if c.AfterCheck != nil {
				if err := c.AfterCheck(ctx, env); err != nil && ctx.Err() == nil {
					c.log().Warn("automatic updates failed", "environment", env.ID, "error", err)
				}
			}
		}
		timer.Reset(tick)
	}
}

// due reports whether the environment's interval has passed since its last full check.
func (c *Checker) due(ctx context.Context, env string) bool {
	interval, err := c.Interval(ctx, env)
	if err != nil || interval == 0 {
		return false
	}
	last, ok := c.last(env)
	if !ok {
		// After a restart, continue from the newest stored check.
		if ts, err := c.Store.LastImageUpdateCheck(ctx, env); err == nil && ts > 0 {
			last = time.Unix(ts, 0)
			c.setLast(env, last)
		}
	}
	return c.now().Sub(last) >= interval
}

func (c *Checker) last(env string) (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.lastRun[env]
	return t, ok
}

func (c *Checker) setLast(env string, t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastRun == nil {
		c.lastRun = map[string]time.Time{}
	}
	c.lastRun[env] = t
}

func (c *Checker) begin(env string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running == nil {
		c.running = map[string]bool{}
	}
	if c.running[env] {
		return false
	}
	c.running[env] = true
	return true
}

func (c *Checker) end(env string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.running, env)
}

func (c *Checker) isRunning(env string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running[env]
}

// checkable reports whether a reference names a tag in a registry: digest-pinned references
// have nothing to compare.
func checkable(ref string) bool { return ref != "" && !strings.Contains(ref, "@") }

// usage maps the references of one namespace's images to their digests and counts the
// containers created from each reference.
func usage(ctx context.Context, env *environment.Environment, ns string) (digests map[string]string, containers map[string]int, err error) {
	imgs, err := env.Runtime.Images().List(ctx, ns)
	if err != nil {
		return nil, nil, err
	}
	digests = make(map[string]string, len(imgs))
	for _, img := range imgs {
		digests[img.Name] = img.Digest
	}
	list, err := env.Runtime.Containers().List(ctx, ns)
	if err != nil {
		return nil, nil, err
	}
	containers = map[string]int{}
	for _, ct := range list {
		containers[ct.Image]++
	}
	return digests, containers, nil
}

// Check resolves in the registry the references used by containers of every namespace, or only
// ref when given (used or not).
func (c *Checker) Check(ctx context.Context, env *environment.Environment, ref string) (Report, error) {
	if !c.begin(env.ID) {
		return Report{}, ErrBusy
	}
	defer c.end(env.ID)
	if ref != "" && !checkable(ref) {
		return Report{}, fmt.Errorf("%w: %s is pinned to a digest", errdefs.ErrInvalidArgument, ref)
	}

	refs := []string{ref}
	if ref == "" {
		nss, err := env.Runtime.System().Namespaces(ctx)
		if err != nil {
			return Report{}, err
		}
		seen := map[string]bool{}
		for _, ns := range nss {
			digests, containers, err := usage(ctx, env, ns)
			if err != nil {
				return Report{}, err
			}
			for r := range containers {
				if _, local := digests[r]; local && checkable(r) {
					seen[r] = true
				}
			}
		}
		refs = make([]string, 0, len(seen))
		for r := range seen {
			refs = append(refs, r)
		}
		sort.Strings(refs)
	}

	var (
		mu  sync.Mutex
		rep Report
		wg  sync.WaitGroup
		sem = make(chan struct{}, concurrency)
	)
	results := map[string]string{} // ref -> remote digest
	for _, r := range refs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			rctx, cancel := context.WithTimeout(ctx, refTimeout)
			d, err := env.Runtime.Images().RemoteDigest(rctx, r)
			cancel()
			if ctx.Err() != nil {
				return
			}
			row := db.SetImageUpdateParams{Environment: env.ID, Ref: r, RemoteDigest: d, CheckedAt: c.now().Unix()}
			if err != nil {
				row.Error = truncate(err.Error())
			}
			if serr := c.Store.SetImageUpdate(context.WithoutCancel(ctx), row); serr != nil {
				c.log().Error("store image update", "ref", r, "error", serr)
			}
			mu.Lock()
			defer mu.Unlock()
			rep.Checked++
			if err != nil {
				rep.Failed++
				return
			}
			results[r] = d
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return rep, err
	}
	if ref == "" {
		c.setLast(env.ID, c.now())
	}
	// Count updates against the local digests of every namespace using the references.
	if nss, err := env.Runtime.System().Namespaces(ctx); err == nil {
		updated := map[string]bool{}
		for _, ns := range nss {
			digests, _, err := usage(ctx, env, ns)
			if err != nil {
				continue
			}
			for r, remote := range results {
				if local, ok := digests[r]; ok && local != remote {
					updated[r] = true
				}
			}
		}
		rep.Updates = len(updated)
	}
	c.log().Info("image update check", "environment", env.ID, "ref", ref,
		"checked", rep.Checked, "updates", rep.Updates, "failed", rep.Failed)
	return rep, nil
}

func truncate(s string) string {
	if len(s) > maxErrorChars {
		return s[:maxErrorChars] + "…"
	}
	return s
}

// Overview reports the stored checks of the references present in a namespace: those used by
// containers, and any other image of the namespace that was checked.
func (c *Checker) Overview(ctx context.Context, env *environment.Environment, ns string) (Overview, error) {
	interval, err := c.Interval(ctx, env.ID)
	if err != nil {
		return Overview{}, err
	}
	out := Overview{IntervalSeconds: int64(interval / time.Second), Running: c.isRunning(env.ID), Images: []Status{}}
	rows, err := c.Store.ListImageUpdates(ctx, env.ID)
	if err != nil {
		return Overview{}, err
	}
	digests, containers, err := usage(ctx, env, ns)
	if err != nil {
		return Overview{}, err
	}
	var last time.Time
	for _, row := range rows {
		checked := time.Unix(row.CheckedAt, 0).UTC()
		if checked.After(last) {
			last = checked
		}
		local, ok := digests[row.Ref]
		if !ok {
			continue
		}
		out.Images = append(out.Images, Status{
			Ref: row.Ref, LocalDigest: local, RemoteDigest: row.RemoteDigest, CheckedAt: checked,
			Error: row.Error, Containers: containers[row.Ref],
			UpdateAvailable: row.Error == "" && row.RemoteDigest != "" && row.RemoteDigest != local,
		})
	}
	if t, ok := c.last(env.ID); ok && t.After(last) {
		last = t
	}
	if !last.IsZero() {
		out.LastCheck = &last
		if interval > 0 {
			next := last.Add(interval)
			out.NextCheck = &next
		}
	}
	return out, nil
}
