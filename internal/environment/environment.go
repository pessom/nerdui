// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package environment keeps the registry of managed containerd hosts (ADR-0006).
// MVP registers a single "local" environment backed by the containerd socket; remote
// agent-backed environments (#69, #70) implement the same runtime.Runtime interface.
package environment

import (
	"errors"
	"fmt"
	"sync"

	"github.com/pessom/nerdui/internal/events"
	"github.com/pessom/nerdui/internal/runtime"
)

// LocalID is the ID of the environment for the host nerdui runs on.
const LocalID = "local"

// Kinds of environments.
const (
	KindLocal = "local"
)

// Environment is a managed containerd host.
type Environment struct {
	ID               string
	Name             string
	Kind             string
	DefaultNamespace string
	Runtime          runtime.Runtime
	// Events fans out the environment's containerd events; Run it for the server lifetime.
	Events *events.Hub
}

// Local returns the environment for the local containerd socket.
func Local(rt runtime.Runtime, defaultNamespace string) *Environment {
	return &Environment{
		ID:               LocalID,
		Name:             "Local host",
		Kind:             KindLocal,
		DefaultNamespace: defaultNamespace,
		Runtime:          rt,
		Events:           events.NewHub(rt.Events(), nil),
	}
}

// Registry is a concurrency-safe, ordered set of environments.
type Registry struct {
	mu    sync.RWMutex
	byID  map[string]*Environment
	order []string
}

// NewRegistry returns a registry with envs in the given order. It panics on duplicate IDs.
func NewRegistry(envs ...*Environment) *Registry {
	r := &Registry{byID: map[string]*Environment{}}
	for _, e := range envs {
		if err := r.Add(e); err != nil {
			panic(err)
		}
	}
	return r
}

// ErrDuplicate is returned when an environment ID is already registered.
var ErrDuplicate = errors.New("environment already registered")

// Add registers e.
func (r *Registry) Add(e *Environment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[e.ID]; ok {
		return fmt.Errorf("%w: %q", ErrDuplicate, e.ID)
	}
	r.byID[e.ID] = e
	r.order = append(r.order, e.ID)
	return nil
}

// Get returns the environment with id.
func (r *Registry) Get(id string) (*Environment, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.byID[id]
	return e, ok
}

// List returns environments in registration order.
func (r *Registry) List() []*Environment {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Environment, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.byID[id])
	}
	return out
}

// Close closes every environment's runtime.
func (r *Registry) Close() error {
	var errs []error
	for _, e := range r.List() {
		errs = append(errs, e.Runtime.Close())
	}
	return errors.Join(errs...)
}
