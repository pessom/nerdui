// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/api/ws"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/runtime"
)

// maxStackTail bounds the history read per container: it is held in memory to be merged.
const maxStackTail = 5000

// StackLogLine is a log line of a stack's container (#30). Keep in sync with api/openapi.yaml.
type StackLogLine struct {
	Service     string `json:"service"`
	Container   string `json:"container,omitempty"`
	ContainerID string `json:"containerId"`
	runtime.LogLine
}

type stackContainer struct {
	id, name, service string
}

// stackLogsStream streams the merged logs of a stack's containers (#30, ADR-0007):
//
//	GET /api/v1/environments/{env}/stacks/{name}/logs/stream?namespace=&tail=200&since=&follow=true
//
// The history (the last tail lines of each container, at most 5000) is merged by time; with
// follow, new lines of every container follow as they come, until all containers stop. Data
// messages carry arrays of StackLogLine. Containers created after the stream opened are not
// included: the client reconnects (e.g. after a deploy).
func (h *Handler) stackLogsStream(w http.ResponseWriter, r *http.Request) {
	ns, opts, err := logParams(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if opts.Tail == 0 || opts.Tail > maxStackTail {
		opts.Tail = maxStackTail
	}
	env, name := EnvFrom(r.Context()), chi.URLParam(r, "name")
	list, err := env.Runtime.Containers().List(r.Context(), ns)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	var containers []stackContainer
	for _, c := range list {
		if c.Compose != nil && c.Compose.Project == name {
			containers = append(containers, stackContainer{id: c.ID, name: c.Name, service: c.Compose.Service})
		}
	}
	conn, err := ws.Accept(w, r, ws.Options{})
	if err != nil {
		return
	}
	b := &logBatcher[StackLogLine]{conn: conn}
	logs := env.Runtime.Containers()
	err = streamStackLogs(conn.Context(), containers, opts, func(ctx context.Context, id string, o runtime.LogOptions, emit runtime.LogEmitter) error {
		return logs.Logs(ctx, ns, id, o, emit)
	}, b.add)
	if !b.flush() || errors.Is(err, errClientGone) {
		return
	}
	if err != nil {
		_, code := httpjson.Status(err)
		h.logActionErr(r, err)
		conn.SendError(code, err.Error())
	}
	conn.End()
}

type logReader func(ctx context.Context, id string, opts runtime.LogOptions, emit runtime.LogEmitter) error

// streamStackLogs sends the merged history of the containers, then (with opts.Follow) their
// new lines. Each container is followed from its last history line; lines up to that time are
// skipped, as the log store filters by second.
func streamStackLogs(ctx context.Context, containers []stackContainer, opts runtime.LogOptions, read logReader, emit func(StackLogLine) error) error {
	history := opts
	history.Follow = false
	type result struct {
		lines []StackLogLine
		err   error
	}
	results := make([]result, len(containers))
	var wg sync.WaitGroup
	for i, c := range containers {
		wg.Go(func() {
			results[i].err = read(ctx, c.id, history, func(l runtime.LogLine) error {
				results[i].lines = append(results[i].lines, StackLogLine{Service: c.service, Container: c.name, ContainerID: c.id, LogLine: l})
				return nil
			})
		})
	}
	wg.Wait()
	var merged []StackLogLine
	last := make([]time.Time, len(containers))
	for i, r := range results {
		if r.err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return r.err
		}
		merged = append(merged, r.lines...)
		for _, l := range r.lines {
			if l.Time.After(last[i]) {
				last[i] = l.Time
			}
		}
	}
	// Lines without a time (journald) keep their container's order at the front.
	sort.SliceStable(merged, func(a, b int) bool { return merged[a].Time.Before(merged[b].Time) })
	for _, l := range merged {
		if err := emit(l); err != nil {
			return err
		}
	}
	if !opts.Follow {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make([]error, len(containers))
	for i, c := range containers {
		follow := opts
		follow.Tail = 0
		if !last[i].IsZero() {
			follow.Since = last[i].Format(time.RFC3339Nano)
		} else if follow.Since == "" {
			follow.Since = time.Now().UTC().Format(time.RFC3339Nano)
		}
		wg.Go(func() {
			errs[i] = read(ctx, c.id, follow, func(l runtime.LogLine) error {
				if !last[i].IsZero() && !l.Time.After(last[i]) {
					return nil // part of the history already sent
				}
				return emit(StackLogLine{Service: c.service, Container: c.name, ContainerID: c.id, LogLine: l})
			})
			if errs[i] != nil {
				cancel() // the client is gone or a reader failed: stop the others
			}
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	}
	return ctx.Err()
}
