// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package hostmetrics samples the CPU, memory, disk and network usage of the host nerdui runs
// on, keeps the last day in memory and alerts when thresholds are exceeded (#64, ADR-0054).
package hostmetrics

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"runtime"
	"sync"
	"time"

	"github.com/pessom/nerdui/internal/notify"
)

// Defaults of the sampler.
const (
	DefaultInterval = 15 * time.Second
	// Retention is how long samples are kept, in memory.
	Retention = 24 * time.Hour
	// MaxPoints bounds a history response; longer ranges are averaged into buckets.
	MaxPoints = 360
)

// ErrUnavailable is returned before the first sample, or when the host cannot be read.
var ErrUnavailable = errors.New("host metrics are not available yet")

// Disk is the usage of a filesystem. Keep in sync with api/openapi.yaml.
type Disk struct {
	// Path is the first of Paths, which are the watched directories on this filesystem.
	Path  string   `json:"path"`
	Paths []string `json:"paths"`
	Total uint64   `json:"total"`
	Used  uint64   `json:"used"`
}

// Percent is the used share of the filesystem, as df shows it.
func (d Disk) Percent() float64 {
	if d.Total == 0 {
		return 0
	}
	return float64(d.Used) / float64(d.Total) * 100
}

// Sample is the state of the host at a time. Keep in sync with api/openapi.yaml.
type Sample struct {
	Time time.Time `json:"time"`
	// CPU is the busy share of all CPUs since the previous sample, 0 to 100.
	CPU         float64 `json:"cpu"`
	CPUs        int     `json:"cpus"`
	Load1       float64 `json:"load1"`
	MemoryUsed  uint64  `json:"memoryUsed"`
	MemoryTotal uint64  `json:"memoryTotal"`
	SwapUsed    uint64  `json:"swapUsed"`
	SwapTotal   uint64  `json:"swapTotal"`
	// NetRx and NetTx are bytes per second over the host's physical interfaces.
	NetRx float64 `json:"netRx"`
	NetTx float64 `json:"netTx"`
	Disks []Disk  `json:"disks"`
}

// MemoryPercent is the used share of memory: what is not available to new programs.
func (s Sample) MemoryPercent() float64 {
	if s.MemoryTotal == 0 {
		return 0
	}
	return float64(s.MemoryUsed) / float64(s.MemoryTotal) * 100
}

// Point is a sample in a history. Keep in sync with api/openapi.yaml.
type Point struct {
	// T is the time in Unix seconds.
	T      int64   `json:"t"`
	CPU    float64 `json:"cpu"`
	Memory float64 `json:"memory"`
	NetRx  float64 `json:"netRx"`
	NetTx  float64 `json:"netTx"`
	// Disks are the used percent of each filesystem by its path.
	Disks map[string]float64 `json:"disks"`
}

// Sampler reads the host every Interval.
type Sampler struct {
	// Proc is the procfs mount; default /proc.
	Proc string
	// Paths are the directories whose filesystems are watched, e.g. /, the data directory and
	// containerd's root; one entry per filesystem.
	Paths    []string
	Interval time.Duration
	// Environment names the environment in notifications.
	Environment string
	// Alerts returns the thresholds; nil or an error disables alerts.
	Alerts func(ctx context.Context) (Alerts, error)
	Notify func(notify.Event)
	Logger *slog.Logger

	now    func() time.Time
	statfs func(string) (string, uint64, uint64, error)

	defaultsOnce sync.Once
	mu           sync.Mutex
	current      *Sample
	points       []Point
	alerter      alerter

	prevCPU      cpuTimes
	prevRx       uint64
	prevTx       uint64
	prevAt       time.Time
	warnedFailed bool
}

func (s *Sampler) defaults() {
	if s.Proc == "" {
		s.Proc = "/proc"
	}
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.statfs == nil {
		s.statfs = statFS
	}
}

// Run samples until ctx ends.
func (s *Sampler) Run(ctx context.Context) {
	s.defaultsOnce.Do(s.defaults)
	s.Collect(ctx)
	t := time.NewTicker(s.Step())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Collect(ctx)
		}
	}
}

// Collect reads the host once. The first reading only sets the counters CPU and network
// rates are computed from.
func (s *Sampler) Collect(ctx context.Context) {
	s.defaultsOnce.Do(s.defaults)
	now := s.now()
	cpu, err := readCPU(s.Proc)
	if err == nil {
		var mem memory
		if mem, err = readMemory(s.Proc); err == nil {
			s.record(ctx, now, cpu, mem)
			return
		}
	}
	if !s.warnedFailed {
		s.Logger.Warn("host metrics: cannot read the host", "proc", s.Proc, "error", err)
		s.warnedFailed = true
	}
}

func (s *Sampler) record(ctx context.Context, now time.Time, cpu cpuTimes, mem memory) {
	load, _ := readLoad(s.Proc)
	rx, tx, netErr := readNetwork(s.Proc)
	smp := Sample{
		Time: now.UTC(), CPUs: runtime.NumCPU(), Load1: load,
		MemoryTotal: mem.total, MemoryUsed: mem.total - min(mem.available, mem.total),
		SwapTotal: mem.swapTotal, SwapUsed: mem.swapTotal - min(mem.swapFree, mem.swapTotal),
		Disks: s.disks(),
	}
	first := s.prevAt.IsZero()
	if !first {
		smp.CPU = cpuPercent(s.prevCPU, cpu)
		if secs := now.Sub(s.prevAt).Seconds(); secs > 0 && netErr == nil && rx >= s.prevRx && tx >= s.prevTx {
			smp.NetRx = float64(rx-s.prevRx) / secs
			smp.NetTx = float64(tx-s.prevTx) / secs
		}
	}
	s.prevCPU, s.prevRx, s.prevTx, s.prevAt = cpu, rx, tx, now
	if first {
		return
	}
	p := Point{T: now.Unix(), CPU: round1(smp.CPU), Memory: round1(smp.MemoryPercent()), NetRx: math.Round(smp.NetRx), NetTx: math.Round(smp.NetTx), Disks: map[string]float64{}}
	for _, d := range smp.Disks {
		p.Disks[d.Path] = round1(d.Percent())
	}
	s.mu.Lock()
	s.current = &smp
	s.points = append(s.points, p)
	cut := now.Add(-Retention).Unix()
	i := 0
	for i < len(s.points) && s.points[i].T < cut {
		i++
	}
	if i > 0 {
		s.points = append(s.points[:0], s.points[i:]...)
	}
	s.mu.Unlock()

	if s.Alerts != nil {
		a, err := s.Alerts(ctx)
		if err != nil {
			s.Logger.Warn("host metrics: read the alert thresholds", "error", err)
			return
		}
		for _, ev := range s.alerter.check(a, smp) {
			ev.Kind, ev.Environment = notify.KindHostAlert, s.Environment
			if s.Notify != nil {
				s.Notify(ev)
			}
		}
	}
}

// disks reads the filesystems of Paths, once per filesystem in the order of Paths.
func (s *Sampler) disks() []Disk {
	out := []Disk{}
	index := map[string]int{}
	for _, p := range s.Paths {
		id, total, free, err := s.statfs(p)
		if err != nil {
			continue
		}
		if i, ok := index[id]; ok {
			out[i].Paths = append(out[i].Paths, p)
			continue
		}
		index[id] = len(out)
		out = append(out, Disk{Path: p, Paths: []string{p}, Total: total, Used: total - min(free, total)})
	}
	return out
}

// Step is the time between samples.
func (s *Sampler) Step() time.Duration {
	if s.Interval > 0 {
		return s.Interval
	}
	return DefaultInterval
}

// Current returns the newest sample.
func (s *Sampler) Current() (Sample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return Sample{}, ErrUnavailable
	}
	return *s.current, nil
}

// History returns the samples of the last d, averaged into at most MaxPoints buckets. CPU,
// memory and network are averaged; disks take the last value of a bucket.
func (s *Sampler) History(d time.Duration) []Point {
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cut := now().Add(-d).Unix()
	var pts []Point
	for _, p := range s.points {
		if p.T >= cut {
			pts = append(pts, p)
		}
	}
	if len(pts) <= MaxPoints {
		return append([]Point{}, pts...)
	}
	size := (len(pts) + MaxPoints - 1) / MaxPoints
	out := make([]Point, 0, MaxPoints)
	for i := 0; i < len(pts); i += size {
		b := pts[i:min(i+size, len(pts))]
		last := b[len(b)-1]
		avg := Point{T: last.T, Disks: last.Disks}
		for _, p := range b {
			avg.CPU += p.CPU
			avg.Memory += p.Memory
			avg.NetRx += p.NetRx
			avg.NetTx += p.NetTx
		}
		n := float64(len(b))
		avg.CPU, avg.Memory = round1(avg.CPU/n), round1(avg.Memory/n)
		avg.NetRx, avg.NetTx = math.Round(avg.NetRx/n), math.Round(avg.NetTx/n)
		out = append(out, avg)
	}
	return out
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
