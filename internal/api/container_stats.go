// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/api/ws"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/runtime"
)

// defaultStatsInterval is used without ?interval=.
const defaultStatsInterval = 2 * time.Second

// containerStatsStream streams samples of a running container (#16, ADR-0007):
//
//	GET /api/v1/environments/{env}/containers/{id}/stats/stream?namespace=&interval=2s
//
// Data messages carry one StatsSample each. The stream ends with "end" when the container
// stops; a container that is not running gets an error (conflict) and "end".
func (h *Handler) containerStatsStream(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	interval := defaultStatsInterval
	if v := r.URL.Query().Get("interval"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < runtime.MinStatsInterval || d > runtime.MaxStatsInterval {
			httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: interval must be %s to %s",
				errdefs.ErrInvalidArgument, runtime.MinStatsInterval, runtime.MaxStatsInterval))
			return
		}
		interval = d
	}
	conn, err := ws.Accept(w, r, ws.Options{})
	if err != nil {
		return
	}
	err = EnvFrom(r.Context()).Runtime.Containers().Stats(conn.Context(), ns, chi.URLParam(r, "id"), interval,
		func(s runtime.StatsSample) error {
			if !conn.SendData(s) {
				return errClientGone
			}
			return nil
		})
	if errors.Is(err, errClientGone) {
		return
	}
	if err != nil {
		_, code := httpjson.Status(err)
		conn.SendError(code, err.Error())
	}
	conn.End()
}
