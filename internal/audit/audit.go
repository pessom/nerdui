// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package audit records who changed what (#79, ADR-0036): state-changing API requests, terminal
// sessions, authorization denials and authentication. Request bodies are never stored: they can
// hold passwords, tokens and environment values.
package audit

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/store/db"
)

// Results.
const (
	ResultOK       = "ok"
	ResultFailed   = "failed"
	ResultDenied   = "denied"
	ResultAccepted = "accepted" // a background job runs; set to ok or failed when it ends
)

// Authentication actions, recorded by the auth service.
const (
	ActionSetup  = "auth:setup"
	ActionLogin  = "auth:login"
	ActionLogout = "auth:logout"
)

// DefaultRetention keeps 90 days.
const DefaultRetention = 90 * 24 * time.Hour

const (
	maxMessage   = 500
	maxUserAgent = 300
	pruneEvery   = 6 * time.Hour
	// MaxRows bounds one page of the log.
	MaxRows = 500
)

// Store is the database of the log; *db.Queries of the write connection implements it.
type Store interface {
	InsertAudit(ctx context.Context, arg db.InsertAuditParams) error
	SetAuditJobResult(ctx context.Context, arg db.SetAuditJobResultParams) error
	ListAudit(ctx context.Context, arg db.ListAuditParams) ([]db.AuditLog, error)
	PruneAudit(ctx context.Context, before int64) (int64, error)
}

// Recorder writes the audit log. It implements authz.Auditor.
type Recorder struct {
	Store Store
	// Retention is how long entries are kept; 0 keeps them forever.
	Retention time.Duration
	Logger    *slog.Logger
	Now       func() time.Time

	// mu orders job entries and job results: a fast job can finish before the request that
	// started it is written.
	mu       sync.Mutex
	finished map[string]jobResult
	order    []string
}

type jobResult struct{ result, message string }

// rememberedJobs bounds the results kept for entries not written yet.
const rememberedJobs = 256

var _ authz.Auditor = (*Recorder)(nil)

// Entry is one log entry. Keep in sync with api/openapi.yaml.
type Entry struct {
	ID          int64     `json:"id"`
	At          time.Time `json:"at"`
	User        string    `json:"user,omitempty"`
	APIKey      bool      `json:"apiKey,omitempty"`
	Action      string    `json:"action"`
	Method      string    `json:"method,omitempty"`
	Path        string    `json:"path,omitempty"`
	Environment string    `json:"environment,omitempty"`
	Namespace   string    `json:"namespace,omitempty"`
	Object      string    `json:"object,omitempty"`
	Status      int       `json:"status,omitempty"`
	Result      string    `json:"result"`
	Message     string    `json:"message,omitempty"`
	JobID       string    `json:"jobId,omitempty"`
	ClientIP    string    `json:"clientIp,omitempty"`
	UserAgent   string    `json:"userAgent,omitempty"`
}

func (rec *Recorder) now() time.Time {
	if rec.Now != nil {
		return rec.Now()
	}
	return time.Now()
}

func (rec *Recorder) log() *slog.Logger {
	if rec.Logger != nil {
		return rec.Logger
	}
	return slog.Default()
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// ClientIP is the request's client address (behind a trusted proxy, already the real client).
func ClientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// object names what a request acted on: its route parameters other than the environment, e.g.
// a container ID, a stack name and an operation.
func object(r *http.Request) string {
	rc := chi.RouteContext(r.Context())
	if rc == nil {
		return ""
	}
	var parts []string
	for i, k := range rc.URLParams.Keys {
		if k == "env" || k == "*" || i >= len(rc.URLParams.Values) {
			continue
		}
		parts = append(parts, rc.URLParams.Values[i])
	}
	return strings.Join(parts, "/")
}

// fromResponse extracts the error message or the job ID from a JSON response.
func fromResponse(b []byte) (msg, jobID string) {
	var v struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	if json.Unmarshal(b, &v) != nil {
		return "", ""
	}
	return v.Error.Message, v.Job.ID
}

// Audit implements authz.Auditor.
func (rec *Recorder) Audit(r *http.Request, a authz.AuditRecord) {
	row := db.InsertAuditParams{
		At: rec.now().Unix(), Username: a.Subject.Username, ApiKey: boolInt(a.Subject.APIKey),
		Action: string(a.Action), Method: r.Method, Path: a.Route,
		Environment: a.Resource.Environment, Namespace: a.Resource.Namespace, Object: object(r),
		Status: int64(a.Status), ClientIp: ClientIP(r), UserAgent: clip(r.UserAgent(), maxUserAgent),
	}
	msg, jobID := fromResponse(a.Response)
	switch {
	case a.Denied:
		row.Result, row.Message = ResultDenied, "not allowed to "+string(a.Action)
	case a.Status >= 400:
		row.Result, row.Message = ResultFailed, msg
	case jobID != "":
		row.Result, row.JobID = ResultAccepted, jobID
	default:
		row.Result = ResultOK
	}
	row.Message = clip(row.Message, maxMessage)
	if row.JobID == "" {
		rec.insert(r.Context(), row)
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if res, ok := rec.finished[row.JobID]; ok {
		row.Result, row.Message = res.result, res.message
	}
	rec.insert(r.Context(), row)
}

// Auth records an authentication event; username is who tried, also when it failed.
func (rec *Recorder) Auth(r *http.Request, action, username string, ok bool, msg string) {
	row := db.InsertAuditParams{
		At: rec.now().Unix(), Username: username, Action: action, Method: r.Method, Path: r.URL.Path,
		Result: ResultOK, Message: clip(msg, maxMessage), ClientIp: ClientIP(r),
		UserAgent: clip(r.UserAgent(), maxUserAgent), Status: http.StatusOK,
	}
	if !ok {
		row.Result, row.Status = ResultFailed, http.StatusUnauthorized
	}
	rec.insert(r.Context(), row)
}

func (rec *Recorder) insert(ctx context.Context, row db.InsertAuditParams) {
	if err := rec.Store.InsertAudit(context.WithoutCancel(ctx), row); err != nil {
		rec.log().Error("write audit log", "action", row.Action, "user", row.Username, "error", err)
	}
}

// JobFinished completes the entry of a request that started a job; use as jobs.Manager.OnFinish.
func (rec *Recorder) JobFinished(j jobs.Job) {
	res := jobResult{result: ResultOK, message: clip(j.Error, maxMessage)}
	if j.Status != jobs.StatusSucceeded {
		res.result = ResultFailed
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.finished == nil {
		rec.finished = map[string]jobResult{}
	}
	rec.finished[j.ID] = res
	rec.order = append(rec.order, j.ID)
	if len(rec.order) > rememberedJobs {
		delete(rec.finished, rec.order[0])
		rec.order = rec.order[1:]
	}
	if err := rec.Store.SetAuditJobResult(context.Background(), db.SetAuditJobResultParams{
		JobID: j.ID, Result: res.result, Message: res.message,
	}); err != nil {
		rec.log().Error("update audit log", "job", j.ID, "error", err)
	}
}

// Run removes entries older than the retention until ctx ends.
func (rec *Recorder) Run(ctx context.Context) {
	if rec.Retention <= 0 {
		return
	}
	t := time.NewTicker(pruneEvery)
	defer t.Stop()
	for {
		if n, err := rec.Store.PruneAudit(ctx, rec.now().Add(-rec.Retention).Unix()); err != nil && ctx.Err() == nil {
			rec.log().Warn("prune audit log", "error", err)
		} else if n > 0 {
			rec.log().Debug("audit log pruned", "entries", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Filter selects log entries.
type Filter struct {
	User   string
	Result string
	Query  string
	Since  time.Time
	Until  time.Time
	Before int64 // entries with smaller IDs, for paging
	Limit  int
}

// List returns entries newest first.
func (rec *Recorder) List(ctx context.Context, f Filter) ([]Entry, error) {
	if f.Limit <= 0 || f.Limit > MaxRows {
		f.Limit = MaxRows
	}
	p := db.ListAuditParams{
		Before: f.Before, Username: f.User, Result: f.Result, Q: f.Query, MaxRows: int64(f.Limit),
	}
	if !f.Since.IsZero() {
		p.Since = f.Since.Unix()
	}
	if !f.Until.IsZero() {
		p.Until = f.Until.Unix()
	}
	rows, err := rec.Store.ListAudit(ctx, p)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		out = append(out, Entry{
			ID: r.ID, At: time.Unix(r.At, 0).UTC(), User: r.Username, APIKey: r.ApiKey != 0,
			Action: r.Action, Method: r.Method, Path: r.Path, Environment: r.Environment,
			Namespace: r.Namespace, Object: r.Object, Status: int(r.Status), Result: r.Result,
			Message: r.Message, JobID: r.JobID, ClientIP: r.ClientIp, UserAgent: r.UserAgent,
		})
	}
	return out, nil
}

// csvSafe keeps spreadsheet programs from running a cell as a formula.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// WriteCSV writes entries as CSV with a header row.
func WriteCSV(w io.Writer, entries []Entry) error {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"time", "user", "api_key", "action", "method", "path", "environment", "namespace",
		"object", "status", "result", "message", "job_id", "client_ip", "user_agent"})
	for _, e := range entries {
		_ = cw.Write([]string{
			e.At.Format(time.RFC3339), csvSafe(e.User), strconv.FormatBool(e.APIKey), e.Action, e.Method, e.Path,
			csvSafe(e.Environment), csvSafe(e.Namespace), csvSafe(e.Object), strconv.Itoa(e.Status), e.Result,
			csvSafe(e.Message), e.JobID, e.ClientIP, csvSafe(e.UserAgent),
		})
	}
	cw.Flush()
	return cw.Error()
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
