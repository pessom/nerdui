// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/containerd/containerd/v2/pkg/identifiers"
	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/api/ws"
	"github.com/pessom/nerdui/internal/events"
	"github.com/pessom/nerdui/internal/httpjson"
)

// eventsStream streams containerd events over WebSocket (ADR-0007):
//
//	GET /api/v1/environments/{env}/events/stream?namespace=default&topics=/containers/,/tasks/
//
// namespace: empty = the environment's default, "*" = all namespaces. topics: comma-separated
// topic prefixes, empty = all. The first message is the connection state
// (kind "nerdui", action "connected"/"disconnected"); clients refetch data on "connected".
func (h *Handler) eventsStream(w http.ResponseWriter, r *http.Request) {
	filter, err := eventFilter(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	conn, err := ws.Accept(w, r, ws.Options{})
	if err != nil {
		return // Accept already answered (e.g. 403 for cross-origin requests)
	}
	sub := EnvFrom(r.Context()).Events.Subscribe(filter, 0)
	defer sub.Close()
	for {
		select {
		case <-conn.Done():
			return
		case ev, ok := <-sub.C:
			if !ok {
				// Hub shut down or this subscriber fell behind: the client reconnects and
				// resyncs on the initial "connected" event.
				conn.Close(ws.StatusTryAgainLater, "events lagged, reconnect")
				return
			}
			if !conn.SendData(ev) {
				return
			}
		}
	}
}

func eventFilter(r *http.Request) (events.Filter, error) {
	var f events.Filter
	switch ns := r.URL.Query().Get("namespace"); ns {
	case "*":
	case "":
		f.Namespace = EnvFrom(r.Context()).DefaultNamespace
	default:
		if err := identifiers.Validate(ns); err != nil {
			return f, fmt.Errorf("namespace %q: %w", ns, err)
		}
		f.Namespace = ns
	}
	if t := r.URL.Query().Get("topics"); t != "" {
		for _, p := range strings.Split(t, ",") {
			p = strings.TrimSpace(p)
			if !strings.HasPrefix(p, "/") {
				return f, fmt.Errorf("%w: topic %q must start with '/'", errdefs.ErrInvalidArgument, p)
			}
			f.Topics = append(f.Topics, p)
		}
	}
	return f, nil
}
