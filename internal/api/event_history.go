// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/store/db"
)

// EventHistory reads recorded events (#65, ADR-0021); *db.Queries implements it.
type EventHistory interface {
	ListEvents(ctx context.Context, arg db.ListEventsParams) ([]db.Event, error)
}

// HistoryEvent is a recorded event. Keep in sync with api/openapi.yaml.
type HistoryEvent struct {
	ID         int64             `json:"id"`
	Timestamp  time.Time         `json:"ts"`
	Namespace  string            `json:"namespace,omitempty"`
	Topic      string            `json:"topic"`
	Kind       string            `json:"kind"`
	Action     string            `json:"action"`
	ObjectID   string            `json:"objectId,omitempty"`
	ObjectName string            `json:"objectName,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

const (
	defaultEventPage = 100
	maxEventPage     = 500
)

// listEvents returns recorded events, newest first (#65):
//
//	GET /api/v1/environments/{env}/events?namespace=&kind=&object=&before=&limit=
//
// namespace "*" means all namespaces; before is the id of the oldest event already shown.
func (h *Handler) listEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ns := q.Get("namespace")
	if ns != "*" {
		var err error
		if ns, err = Namespace(r); err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}
	} else {
		ns = ""
	}
	limit, before := int64(defaultEventPage), int64(0)
	var err error
	if v := q.Get("limit"); v != "" {
		if limit, err = strconv.ParseInt(v, 10, 64); err != nil || limit < 1 || limit > maxEventPage {
			httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: limit must be 1 to %d", errdefs.ErrInvalidArgument, maxEventPage))
			return
		}
	}
	if v := q.Get("before"); v != "" {
		if before, err = strconv.ParseInt(v, 10, 64); err != nil || before < 0 {
			httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: before must be an event id", errdefs.ErrInvalidArgument))
			return
		}
	}
	out := []HistoryEvent{}
	if h.EventHistory == nil {
		httpjson.Write(w, http.StatusOK, map[string]any{"events": out, "recording": false})
		return
	}
	// One more than requested tells whether older events exist.
	rows, err := h.EventHistory.ListEvents(r.Context(), db.ListEventsParams{
		Environment: EnvFrom(r.Context()).ID, Before: before, Namespace: ns,
		Kind: q.Get("kind"), Object: q.Get("object"), Lim: limit + 1,
	})
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	more := int64(len(rows)) > limit
	if more {
		rows = rows[:limit]
	}
	for _, row := range rows {
		e := HistoryEvent{
			ID: row.ID, Timestamp: time.UnixMilli(row.Ts).UTC(), Namespace: row.Namespace, Topic: row.Topic,
			Kind: row.Kind, Action: row.Action, ObjectID: row.ObjectID, ObjectName: row.ObjectName,
		}
		if row.Attributes != "" && row.Attributes != "{}" {
			_ = json.Unmarshal([]byte(row.Attributes), &e.Attributes)
		}
		out = append(out, e)
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"events": out, "more": more, "recording": true})
}
