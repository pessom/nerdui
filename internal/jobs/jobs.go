// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package jobs runs long operations (pull, later prune and stack deploy) in the background
// (#126, ADR-0006, ADR-0018): the API answers 202 with a job, progress is terminal output
// streamed over WebSocket, jobs can be canceled, one job per resource runs at a time, and
// recent jobs are kept in memory.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/containerd/errdefs"
)

// Statuses.
const (
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCanceled  = "canceled"
)

// Defaults (ADR-0018).
const (
	DefaultMaxJobs   = 100
	DefaultRetention = time.Hour
	DefaultMaxOutput = 1 << 20
)

// Spec describes a job to start.
type Spec struct {
	// Kind identifies the operation, e.g. "image.pull".
	Kind string
	// Title is shown to users, e.g. "Pull docker.io/library/nginx:1.27".
	Title string
	// Environment and Namespace the job works on.
	Environment string
	Namespace   string
	// Resource is the lock key: a second job with the same key is rejected while one runs.
	Resource string
	// Action is the authorization action needed to see and cancel the job.
	Action string
	// User started the job.
	User string
}

// Job is a snapshot of a job. Keep in sync with api/openapi.yaml.
type Job struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind"`
	Title       string     `json:"title"`
	Environment string     `json:"environment"`
	Namespace   string     `json:"namespace,omitempty"`
	Resource    string     `json:"resource,omitempty"`
	Action      string     `json:"-"`
	User        string     `json:"user,omitempty"`
	Status      string     `json:"status"`
	Error       string     `json:"error,omitempty"`
	StartedAt   time.Time  `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
}

// Done reports whether the job has finished.
func (j Job) Done() bool { return j.Status != StatusRunning }

// Func is the work of a job. It writes terminal output to out and returns when done; ctx is
// canceled when the job is canceled or the server stops.
type Func func(ctx context.Context, out *Output) error

// Update is sent to subscribers: output bytes as written, or a new job state.
type Update struct {
	Output []byte
	Job    *Job
}

// Manager runs and keeps jobs. The zero value is not usable; use NewManager.
type Manager struct {
	MaxJobs   int
	Retention time.Duration
	MaxOutput int
	now       func() time.Time

	base   context.Context
	stop   context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
	jobs   map[string]*entry
	order  []string          // IDs, oldest first
	locks  map[string]string // resource -> job ID
	closed bool
}

type entry struct {
	job    Job
	out    *Output
	cancel context.CancelFunc
	subs   map[chan Update]struct{}
}

// subscriberBuffer bounds updates queued for one subscriber; a subscriber that falls behind
// is closed and resubscribes (it then gets the stored output).
const subscriberBuffer = 256

// NewManager creates a manager whose jobs run until Close.
func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		MaxJobs: DefaultMaxJobs, Retention: DefaultRetention, MaxOutput: DefaultMaxOutput,
		now:  time.Now,
		base: ctx, stop: cancel,
		jobs: map[string]*entry{}, locks: map[string]string{},
	}
}

// Start runs fn as a new job. It fails with a conflict error while another job holds
// spec.Resource.
func (m *Manager) Start(spec Spec, fn Func) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Job{}, fmt.Errorf("%w: shutting down", errdefs.ErrUnavailable)
	}
	if spec.Resource != "" {
		if id, busy := m.locks[spec.Resource]; busy {
			return Job{}, fmt.Errorf("%w: %s is busy with job %s (%s)", errdefs.ErrConflict,
				spec.Resource, id, m.jobs[id].job.Title)
		}
	}
	m.pruneLocked(m.MaxJobs - 1) // room for this one
	ctx, cancel := context.WithCancel(m.base)
	e := &entry{
		job: Job{
			ID: newID(), Kind: spec.Kind, Title: spec.Title, Environment: spec.Environment,
			Namespace: spec.Namespace, Resource: spec.Resource, Action: spec.Action, User: spec.User,
			Status: StatusRunning, StartedAt: m.now().UTC(),
		},
		out:    newOutput(m.MaxOutput),
		cancel: cancel,
		subs:   map[chan Update]struct{}{},
	}
	e.out.onWrite = func(p []byte) { m.broadcast(e, Update{Output: p}) }
	m.jobs[e.job.ID] = e
	m.order = append(m.order, e.job.ID)
	if spec.Resource != "" {
		m.locks[spec.Resource] = e.job.ID
	}
	m.wg.Go(func() {
		err := runSafely(ctx, fn, e.out)
		m.finish(ctx, e, err)
		cancel()
	})
	return e.job, nil
}

func runSafely(ctx context.Context, fn Func, out *Output) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("job panicked: %v", r)
		}
	}()
	return fn(ctx, out)
}

// finish records the result. Lock order everywhere: output, then manager (writes broadcast
// while holding the output lock).
func (m *Manager) finish(ctx context.Context, e *entry, err error) {
	e.out.mu.Lock()
	defer e.out.mu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	e.job.FinishedAt = &now
	switch {
	case err == nil:
		e.job.Status = StatusSucceeded
	case ctx.Err() != nil:
		e.job.Status = StatusCanceled
		e.job.Error = "canceled"
	default:
		e.job.Status = StatusFailed
		e.job.Error = err.Error()
	}
	if e.job.Resource != "" && m.locks[e.job.Resource] == e.job.ID {
		delete(m.locks, e.job.Resource)
	}
	job := e.job
	m.broadcastLocked(e, Update{Job: &job})
	for c := range e.subs {
		close(c)
	}
	e.subs = map[chan Update]struct{}{}
}

// broadcast is called by Output under its own lock, so updates keep the write order.
func (m *Manager) broadcast(e *entry, u Update) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.broadcastLocked(e, u)
}

// broadcastLocked needs m.mu and e.out.mu.
func (m *Manager) broadcastLocked(e *entry, u Update) {
	for c := range e.subs {
		select {
		case c <- u:
		default:
			delete(e.subs, c)
			close(c)
		}
	}
}

// Get returns a job.
func (m *Manager) Get(id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.jobs[id]
	if !ok {
		return Job{}, false
	}
	return e.job, true
}

// List returns recent jobs, newest first.
func (m *Manager) List() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(m.MaxJobs)
	out := make([]Job, 0, len(m.order))
	for _, id := range slices.Backward(m.order) {
		out = append(out, m.jobs[id].job)
	}
	return out
}

// Subscribe returns the job, its stored output and, while it runs, a channel of updates that
// is closed when the job finishes (after its final state) or the subscriber falls behind.
// For finished jobs the channel is nil. Call the returned function to unsubscribe.
func (m *Manager) Subscribe(id string) (Job, string, <-chan Update, func(), bool) {
	m.mu.Lock()
	e, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return Job{}, "", nil, func() {}, false
	}
	m.mu.Unlock()
	// Output lock first, then the manager lock: the same order as writes and finish.
	e.out.mu.Lock()
	defer e.out.mu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot := e.out.snapshotLocked()
	if e.job.Done() {
		return e.job, snapshot, nil, func() {}, true
	}
	c := make(chan Update, subscriberBuffer)
	e.subs[c] = struct{}{}
	unsubscribe := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if _, ok := e.subs[c]; ok {
			delete(e.subs, c)
			close(c)
		}
	}
	return e.job, snapshot, c, unsubscribe, true
}

// Cancel requests cancellation of a running job; it is a no-op for finished jobs.
func (m *Manager) Cancel(id string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.jobs[id]
	if !ok {
		return Job{}, fmt.Errorf("%w: job %s", errdefs.ErrNotFound, id)
	}
	e.cancel()
	return e.job, nil
}

// Close cancels running jobs and waits for them.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	m.stop()
	m.wg.Wait()
}

// pruneLocked drops finished jobs older than Retention and, above limit, the oldest finished
// ones. Running jobs are never dropped.
func (m *Manager) pruneLocked(limit int) {
	cutoff := m.now().Add(-m.Retention)
	keep := m.order[:0]
	finished := 0
	for _, id := range m.order {
		e := m.jobs[id]
		if e.job.FinishedAt != nil && e.job.FinishedAt.Before(cutoff) {
			delete(m.jobs, id)
			continue
		}
		if e.job.Done() {
			finished++
		}
		keep = append(keep, id)
	}
	m.order = keep
	for excess := len(m.order) - limit; excess > 0 && finished > 0; {
		i := slices.IndexFunc(m.order, func(id string) bool { return m.jobs[id].job.Done() })
		if i < 0 {
			break
		}
		delete(m.jobs, m.order[i])
		m.order = slices.Delete(m.order, i, i+1)
		excess--
		finished--
	}
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
