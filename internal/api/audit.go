// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/audit"
	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
)

// AuditLog reads the audit log (#79); *audit.Recorder implements it.
type AuditLog interface {
	List(ctx context.Context, f audit.Filter) ([]audit.Entry, error)
}

// auditExportMax bounds a CSV export.
const auditExportMax = 100000

func (h *Handler) auditRoutes(r chi.Router, g *authz.Guard) {
	r.Method(http.MethodGet, "/audit", g.Protect(authz.AuditRead, h.listAudit))
	r.Method(http.MethodGet, "/audit/export", g.Protect(authz.AuditRead, h.exportAudit))
}

func auditFilter(r *http.Request) (audit.Filter, error) {
	q := r.URL.Query()
	f := audit.Filter{User: q.Get("user"), Result: q.Get("result"), Query: q.Get("q")}
	switch f.Result {
	case "", audit.ResultOK, audit.ResultFailed, audit.ResultDenied, audit.ResultAccepted:
	default:
		return f, fmt.Errorf("%w: result must be ok, failed, denied or accepted", errdefs.ErrInvalidArgument)
	}
	parseTime := func(name string, dst *time.Time) error {
		if v := q.Get(name); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return fmt.Errorf("%w: %s must be an RFC 3339 time", errdefs.ErrInvalidArgument, name)
			}
			*dst = t
		}
		return nil
	}
	if err := parseTime("since", &f.Since); err != nil {
		return f, err
	}
	if err := parseTime("until", &f.Until); err != nil {
		return f, err
	}
	if v := q.Get("before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return f, fmt.Errorf("%w: before must be an entry ID", errdefs.ErrInvalidArgument)
		}
		f.Before = n
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > audit.MaxRows {
			return f, fmt.Errorf("%w: limit must be 1 to %d", errdefs.ErrInvalidArgument, audit.MaxRows)
		}
		f.Limit = n
	}
	return f, nil
}

// GET /api/v1/audit?user=&result=&q=&since=&until=&before=&limit=
func (h *Handler) listAudit(w http.ResponseWriter, r *http.Request) {
	if h.Audit == nil {
		httpjson.WriteError(w, http.StatusNotImplemented, httpjson.CodeUnavailable, "the audit log is not available")
		return
	}
	f, err := auditFilter(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	entries, err := h.Audit.List(r.Context(), f)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"entries": entries})
}

// GET /api/v1/audit/export?<filters>  CSV of all matching entries, newest first.
func (h *Handler) exportAudit(w http.ResponseWriter, r *http.Request) {
	if h.Audit == nil {
		httpjson.WriteError(w, http.StatusNotImplemented, httpjson.CodeUnavailable, "the audit log is not available")
		return
	}
	f, err := auditFilter(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	var all []audit.Entry
	f.Limit = audit.MaxRows
	for len(all) < auditExportMax {
		page, err := h.Audit.List(r.Context(), f)
		if err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}
		all = append(all, page...)
		if len(page) < f.Limit {
			break
		}
		f.Before = page[len(page)-1].ID
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="nerdui-audit-`+time.Now().UTC().Format("20060102-150405")+`.csv"`)
	if err := audit.WriteCSV(w, all); err != nil {
		h.log().Warn("write audit export", "error", err)
	}
}
