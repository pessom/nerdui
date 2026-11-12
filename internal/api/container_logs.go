// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/api/ws"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/runtime"
)

// Batching of log lines into WebSocket frames: a frame is sent when it holds
// logBatchLines lines or logBatchDelay after its first line.
const (
	logBatchLines = 500
	logBatchDelay = 50 * time.Millisecond
)

// errClientGone stops reading logs once the WebSocket has closed.
var errClientGone = errors.New("client disconnected")

// containerLogsStream streams log lines over WebSocket (#14, ADR-0007, ADR-0014):
//
//	GET /api/v1/environments/{env}/containers/{id}/logs/stream?follow=true&tail=500&since=…
//
// Data messages carry arrays of {stream, ts, line}. The stream ends with "end" when the
// logs are exhausted (or, with follow, when the container stops); errors arrive as an
// "error" message followed by "end", so clients do not reconnect.
func (h *Handler) containerLogsStream(w http.ResponseWriter, r *http.Request) {
	ns, opts, err := logParams(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	conn, err := ws.Accept(w, r, ws.Options{})
	if err != nil {
		return // Accept already answered
	}
	b := &logBatcher{conn: conn}
	err = EnvFrom(r.Context()).Runtime.Containers().Logs(conn.Context(), ns, chi.URLParam(r, "id"), opts, b.add)
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

// logBatcher groups lines into frames. SendDataWait paces the log reader to the client.
type logBatcher struct {
	conn  *ws.Conn
	mu    sync.Mutex
	lines []runtime.LogLine
	timer *time.Timer
	gone  bool
}

func (b *logBatcher) add(l runtime.LogLine) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.gone {
		return errClientGone
	}
	b.lines = append(b.lines, l)
	switch {
	case len(b.lines) >= logBatchLines:
		b.sendLocked()
	case b.timer == nil:
		b.timer = time.AfterFunc(logBatchDelay, func() { b.flush() })
	}
	if b.gone {
		return errClientGone
	}
	return nil
}

// flush sends pending lines; false when the client has gone.
func (b *logBatcher) flush() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sendLocked()
	return !b.gone
}

func (b *logBatcher) sendLocked() {
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	if len(b.lines) == 0 || b.gone {
		return
	}
	if !b.conn.SendDataWait(b.lines) {
		b.gone = true
	}
	b.lines = nil
}

// containerLogsDownload returns the logs as plain text, like `nerdctl logs` (#14):
//
//	GET /api/v1/environments/{env}/containers/{id}/logs?tail=&since=&until=&timestamps=true
func (h *Handler) containerLogsDownload(w http.ResponseWriter, r *http.Request) {
	ns, opts, err := logParams(r)
	if err == nil && opts.Follow {
		err = fmt.Errorf("%w: follow is only supported by logs/stream", errdefs.ErrInvalidArgument)
	}
	var timestamps bool
	if err == nil {
		timestamps, err = boolParam(r.URL.Query().Get("timestamps"))
		if err != nil {
			err = fmt.Errorf("%w: timestamps: %w", errdefs.ErrInvalidArgument, err)
		}
	}
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	id := chi.URLParam(r, "id")
	var out *bufio.Writer
	err = EnvFrom(r.Context()).Runtime.Containers().Logs(r.Context(), ns, id, opts, func(l runtime.LogLine) error {
		if out == nil {
			short := id
			if len(short) > 12 {
				short = short[:12]
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-logs.txt"`, short))
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			out = bufio.NewWriterSize(w, 32<<10)
		}
		// bufio.Writer keeps the first write error and returns it from later calls.
		if timestamps && !l.Time.IsZero() {
			_, _ = out.WriteString(l.Time.Format(time.RFC3339Nano))
			_ = out.WriteByte(' ')
		}
		_, _ = out.WriteString(l.Line)
		return out.WriteByte('\n')
	})
	switch {
	case out != nil:
		if ferr := out.Flush(); err == nil {
			err = ferr
		}
		if err != nil && r.Context().Err() == nil {
			h.logActionErr(r, err) // the body has started: the download ends early
		}
	case err != nil:
		h.writeActionErr(w, r, err)
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="logs.txt"`)
		w.WriteHeader(http.StatusOK)
	}
}

func logParams(r *http.Request) (string, runtime.LogOptions, error) {
	var opts runtime.LogOptions
	ns, err := Namespace(r)
	if err != nil {
		return "", opts, err
	}
	q := r.URL.Query()
	if opts.Follow, err = boolParam(q.Get("follow")); err != nil {
		return "", opts, fmt.Errorf("%w: follow: %w", errdefs.ErrInvalidArgument, err)
	}
	if v := q.Get("tail"); v != "" && v != "all" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return "", opts, fmt.Errorf("%w: tail must be a number or \"all\"", errdefs.ErrInvalidArgument)
		}
		opts.Tail = uint(n)
	}
	opts.Since, opts.Until = q.Get("since"), q.Get("until")
	return ns, opts, nil
}
