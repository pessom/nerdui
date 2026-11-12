// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package events fans containerd events out to subscribers (ADR-0007): one containerd
// subscription per environment, any number of in-process subscribers (WebSocket streams,
// later the scheduler and notifications).
//
// The hub reconnects with backoff when containerd restarts. Events missed during an outage
// cannot be replayed, so the hub emits synthetic "nerdui" events: "disconnected" when the
// subscription breaks and "connected" when it is (re-)established — clients refetch their
// data on "connected".
package events

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/pessom/nerdui/internal/runtime"
)

// Synthetic events.
const (
	KindNerdui         = "nerdui"
	ActionConnected    = "connected"
	ActionDisconnected = "disconnected"
	TopicConnected     = "/nerdui/connected"
	TopicDisconnected  = "/nerdui/disconnected"
)

// Filter selects events for a subscriber. Synthetic nerdui events always pass.
type Filter struct {
	// Namespace limits events to one containerd namespace; empty means all namespaces.
	Namespace string
	// Topics are topic prefixes such as "/containers/" or "/tasks/exit"; empty means all.
	Topics []string
}

// Match reports whether ev passes the filter.
func (f Filter) Match(ev runtime.Event) bool {
	if ev.Kind == KindNerdui {
		return true
	}
	if f.Namespace != "" && ev.Namespace != f.Namespace {
		return false
	}
	if len(f.Topics) == 0 {
		return true
	}
	for _, t := range f.Topics {
		if strings.HasPrefix(ev.Topic, t) {
			return true
		}
	}
	return false
}

// Subscription receives matching events on C. When the subscriber falls behind by more
// than its buffer, C is closed and Overflowed reports true: the subscriber must resync.
type Subscription struct {
	C <-chan runtime.Event

	c          chan runtime.Event
	filter     Filter
	hub        *Hub
	once       sync.Once
	overflowed bool
}

// Overflowed reports whether the subscription was dropped because it fell behind.
func (s *Subscription) Overflowed() bool {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	return s.overflowed
}

// Close unsubscribes. It is safe to call more than once.
func (s *Subscription) Close() {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	s.closeLocked()
}

func (s *Subscription) closeLocked() {
	s.once.Do(func() {
		delete(s.hub.subs, s)
		close(s.c)
	})
}

// Hub distributes the events of one environment.
type Hub struct {
	source runtime.EventSource
	logger *slog.Logger
	now    func() time.Time

	// Backoff between reconnect attempts.
	MinBackoff, MaxBackoff time.Duration

	mu        sync.Mutex
	subs      map[*Subscription]struct{}
	connected bool
}

// NewHub returns a hub for source. Call Run to start it.
func NewHub(source runtime.EventSource, logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		source: source, logger: logger, now: time.Now,
		MinBackoff: time.Second, MaxBackoff: 30 * time.Second,
		subs: map[*Subscription]struct{}{},
	}
}

// Connected reports whether the containerd subscription is currently established.
func (h *Hub) Connected() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.connected
}

// Subscribe registers a subscriber with a buffer of size buffer (default 256). The first
// event delivered is the current connection state ("connected" or "disconnected").
func (h *Hub) Subscribe(f Filter, buffer int) *Subscription {
	if buffer <= 0 {
		buffer = 256
	}
	c := make(chan runtime.Event, buffer)
	s := &Subscription{C: c, c: c, filter: f, hub: h}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subs[s] = struct{}{}
	c <- h.stateEvent(h.connected)
	return s
}

// Run maintains the containerd subscription until ctx ends, then closes all subscriptions.
func (h *Hub) Run(ctx context.Context) {
	defer h.closeAll()
	backoff := h.MinBackoff
	for ctx.Err() == nil {
		subCtx, cancel := context.WithCancel(ctx)
		evs, errs, err := h.source.Subscribe(subCtx)
		if err == nil {
			backoff = h.MinBackoff
			h.setConnected(true)
			err = h.pump(ctx, evs, errs)
		}
		cancel()
		if ctx.Err() != nil {
			return
		}
		h.setConnected(false)
		h.logger.Warn("containerd event subscription unavailable, retrying", "error", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, h.MaxBackoff)
	}
}

// pump forwards events until the subscription ends and returns the reason.
func (h *Hub) pump(ctx context.Context, evs <-chan runtime.Event, errs <-chan error) error {
	for {
		select {
		case ev, ok := <-evs:
			if !ok {
				return <-errs
			}
			h.publish(ev)
		case err := <-errs:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// setConnected changes the state and announces it under one lock, so a subscriber that
// registers concurrently sees the new state exactly once: either as its initial event or as
// the announcement, never both.
func (h *Hub) setConnected(v bool) {
	h.mu.Lock()
	changed := h.connected != v
	h.connected = v
	if changed {
		h.publishLocked(h.stateEvent(v))
	}
	h.mu.Unlock()
	if changed && v {
		h.logger.Info("containerd event subscription established")
	}
}

func (h *Hub) stateEvent(connected bool) runtime.Event {
	ev := runtime.Event{Timestamp: h.now().UTC(), Kind: KindNerdui, Topic: TopicDisconnected, Action: ActionDisconnected}
	if connected {
		ev.Topic, ev.Action = TopicConnected, ActionConnected
	}
	return ev
}

// publish delivers ev to matching subscribers without blocking; subscribers whose buffer
// is full are dropped (they resync after resubscribing).
func (h *Hub) publish(ev runtime.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.publishLocked(ev)
}

func (h *Hub) publishLocked(ev runtime.Event) {
	for s := range h.subs {
		if !s.filter.Match(ev) {
			continue
		}
		select {
		case s.c <- ev:
		default:
			s.overflowed = true
			s.closeLocked()
		}
	}
}

func (h *Hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		s.closeLocked()
	}
	h.connected = false
}
