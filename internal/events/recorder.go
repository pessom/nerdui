// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

// Recorder defaults (ADR-0021).
const (
	DefaultRetention = 7 * 24 * time.Hour
	DefaultMaxEvents = 100_000
	recordBatch      = 200
	recordInterval   = 500 * time.Millisecond
	cleanupEvery     = time.Hour
	nameCacheSize    = 10_000
)

// NameFunc resolves the name of a container; "" when unknown.
type NameFunc func(ctx context.Context, namespace, id string) string

// Recorder stores the events of one environment for the event feed (#65, ADR-0021). It
// subscribes to the environment's hub, writes events in batches with the container name at
// the time of the event, and deletes events older than Retention or beyond MaxEvents.
type Recorder struct {
	Environment string
	Hub         *Hub
	Store       *store.Store
	Names       NameFunc
	Retention   time.Duration
	MaxEvents   int
	Logger      *slog.Logger
	now         func() time.Time

	names map[string]string // namespace/id -> name, so deletes keep the name
}

// Run records until ctx ends.
func (r *Recorder) Run(ctx context.Context) {
	if r.Logger == nil {
		r.Logger = slog.Default()
	}
	if r.now == nil {
		r.now = time.Now
	}
	if r.MaxEvents <= 0 {
		r.MaxEvents = DefaultMaxEvents
	}
	r.names = map[string]string{}
	r.cleanup(ctx)
	cleanupTicker := time.NewTicker(cleanupEvery)
	defer cleanupTicker.Stop()

	for ctx.Err() == nil {
		sub := r.Hub.Subscribe(Filter{}, 4096)
		<-sub.C // the current state on subscribe is not a change
		r.consume(ctx, sub, cleanupTicker.C)
		sub.Close()
		if sub.Overflowed() {
			r.Logger.Warn("event recorder fell behind; some events were not recorded", "environment", r.Environment)
			continue
		}
		// Closed by the hub (it stopped): wait before subscribing again.
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

// consume records events of one subscription until it ends or ctx is done.
func (r *Recorder) consume(ctx context.Context, sub *Subscription, cleanup <-chan time.Time) {
	flushTicker := time.NewTicker(recordInterval)
	defer flushTicker.Stop()
	var batch []db.InsertEventParams
	flush := func() {
		if len(batch) == 0 {
			return
		}
		// The last batch is written also when ctx ends (shutdown).
		wctx := context.WithoutCancel(ctx)
		if err := r.Store.Tx(wctx, func(q *db.Queries) error {
			for _, e := range batch {
				if err := q.InsertEvent(wctx, e); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			r.Logger.Warn("record events", "environment", r.Environment, "count", len(batch), "error", err)
		}
		batch = batch[:0]
	}
	defer flush()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.C:
			if !ok {
				return
			}
			batch = append(batch, r.row(ctx, ev))
			if len(batch) >= recordBatch {
				flush()
			}
		case <-flushTicker.C:
			flush()
		case <-cleanup:
			flush()
			r.cleanup(ctx)
		}
	}
}

func (r *Recorder) row(ctx context.Context, ev runtime.Event) db.InsertEventParams {
	attrs := "{}"
	if len(ev.Attributes) > 0 {
		if b, err := json.Marshal(ev.Attributes); err == nil {
			attrs = string(b)
		}
	}
	return db.InsertEventParams{
		Environment: r.Environment,
		Ts:          ev.Timestamp.UnixMilli(),
		Namespace:   ev.Namespace,
		Topic:       ev.Topic,
		Kind:        ev.Kind,
		Action:      ev.Action,
		ObjectID:    ev.ID,
		ObjectName:  r.name(ctx, ev),
		Attributes:  attrs,
	}
}

// name returns the container name of container and task events, cached so that delete
// events, which come after the container is gone, keep it.
func (r *Recorder) name(ctx context.Context, ev runtime.Event) string {
	if ev.ID == "" || (ev.Kind != "container" && ev.Kind != "task") || r.Names == nil {
		return ""
	}
	key := ev.Namespace + "/" + ev.ID
	deleted := ev.Kind == "container" && ev.Action == "delete"
	if n, ok := r.names[key]; ok {
		if deleted {
			delete(r.names, key)
		}
		return n
	}
	lookup, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	n := r.Names(lookup, ev.Namespace, ev.ID)
	if n != "" && !deleted {
		if len(r.names) >= nameCacheSize {
			clear(r.names)
		}
		r.names[key] = n
	}
	return n
}

func (r *Recorder) cleanup(ctx context.Context) {
	if r.Retention > 0 {
		if _, err := r.Store.Write().DeleteEventsBefore(ctx, r.now().Add(-r.Retention).UnixMilli()); err != nil && ctx.Err() == nil {
			r.Logger.Warn("delete old events", "error", err)
		}
	}
	if _, err := r.Store.Write().DeleteEventsBeyond(ctx, int64(r.MaxEvents)); err != nil && ctx.Err() == nil {
		r.Logger.Warn("delete excess events", "error", err)
	}
}
