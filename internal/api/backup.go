// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/backup"
	"github.com/pessom/nerdui/internal/httpjson"
)

func (h *Handler) backupRoutes(r chi.Router, g *authz.Guard) {
	if h.Backup == nil {
		return
	}
	// The archive holds every credential nerdui stores (#89, ADR-0047).
	r.Method(http.MethodPost, "/backup", g.Protect(authz.SettingsWrite, h.createBackup))
}

// POST /api/v1/backup  {"password": "..."} → encrypted archive (application/octet-stream)
func (h *Handler) createBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	if err := backup.ValidatePassword(req.Password); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	// Built in memory first, so a failure is an error response, not a truncated download.
	// The archive holds the database and stack files, not container data.
	var buf bytes.Buffer
	if err := backup.Write(r.Context(), *h.Backup, req.Password, &buf); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+backup.FileName(time.Now())+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = buf.WriteTo(w)
}
