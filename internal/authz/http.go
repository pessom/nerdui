// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package authz

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/pessom/nerdui/internal/httpjson"
)

// SubjectFunc returns the authenticated caller of a request.
type SubjectFunc func(*http.Request) (Subject, bool)

// Guard wraps handlers with authorization checks.
type Guard struct {
	authorizer Authorizer
	subject    SubjectFunc
	logger     *slog.Logger
	// Auditor, when set, receives state-changing requests, terminal sessions and denials (#79).
	Auditor Auditor
}

// AuditRecord describes one audited request.
type AuditRecord struct {
	Subject  Subject
	Action   Action
	Resource Resource
	// Route is the matched route pattern, e.g. /api/v1/environments/{env}/containers/{id}.
	Route  string
	Status int
	Denied bool
	// Response is the start of the response body, for error messages and job IDs.
	Response []byte
}

// Auditor records audited requests; it must not block for long.
type Auditor interface {
	Audit(r *http.Request, rec AuditRecord)
}

// audited reports whether a request changes state: an unsafe method on a non-read action (or
// on the caller's own session, e.g. logout, password, API keys), or a terminal session.
func audited(r *http.Request, a Action) bool {
	if a == ContainersExec {
		return true
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return !a.Read() || a == SessionSelf
}

// auditBodyLimit bounds the captured response body.
const auditBodyLimit = 2048

type limitedBuffer struct{ b []byte }

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := auditBodyLimit - len(l.b); room > 0 {
		l.b = append(l.b, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		return rc.RoutePattern()
	}
	return r.URL.Path
}

// NewGuard returns a guard using authorizer and subject.
func NewGuard(authorizer Authorizer, subject SubjectFunc, logger *slog.Logger) *Guard {
	if logger == nil {
		logger = slog.Default()
	}
	return &Guard{authorizer: authorizer, subject: subject, logger: logger}
}

// Allowed reports whether the request's subject may perform action. Use it inside public
// handlers that reveal extra data to authorized callers.
func (g *Guard) Allowed(r *http.Request, action Action) bool {
	s, ok := g.subject(r)
	return ok && g.authorizer.Authorize(r.Context(), s, action, ResourceFromRequest(r)) == nil
}

// Protected is a handler with a declared action.
type Protected struct {
	Action Action
	guard  *Guard
	next   http.Handler
}

// Protect returns a handler that requires an authenticated subject allowed to perform
// action. It panics for actions missing from the catalog (programming error).
func (g *Guard) Protect(action Action, h http.HandlerFunc) *Protected {
	if !Known(action) {
		panic(fmt.Sprintf("authz: action %q is not in the catalog", action))
	}
	return &Protected{Action: action, guard: g, next: h}
}

// ServeHTTP implements http.Handler.
func (p *Protected) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s, ok := p.guard.subject(r)
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, httpjson.CodeUnauthenticated, "authentication required")
		return
	}
	res := ResourceFromRequest(r)
	if s.ReadOnly && !p.Action.Read() {
		p.guard.logger.Warn("authorization denied: read-only API key",
			"user", s.Username, "action", string(p.Action), "method", r.Method, "path", r.URL.Path)
		httpjson.WriteError(w, http.StatusForbidden, httpjson.CodeForbidden,
			fmt.Sprintf("a read-only API key may not %s", p.Action))
		return
	}
	if err := p.guard.authorizer.Authorize(r.Context(), s, p.Action, res); err != nil {
		if !errors.Is(err, ErrForbidden) {
			p.guard.logger.Error("authorization error", "error", err)
			httpjson.WriteError(w, http.StatusInternalServerError, httpjson.CodeInternal, "authorization failed")
			return
		}
		p.guard.logger.Warn("authorization denied",
			"user", s.Username, "role", s.Role, "action", string(p.Action), "resource", res.String(),
			"method", r.Method, "path", r.URL.Path)
		httpjson.WriteError(w, http.StatusForbidden, httpjson.CodeForbidden,
			fmt.Sprintf("you are not allowed to %s", p.Action))
		if p.guard.Auditor != nil {
			p.guard.Auditor.Audit(r, AuditRecord{
				Subject: s, Action: p.Action, Resource: res, Route: routePattern(r),
				Status: http.StatusForbidden, Denied: true,
			})
		}
		return
	}
	if p.guard.Auditor == nil || !audited(r, p.Action) {
		p.next.ServeHTTP(w, r)
		return
	}
	if p.Action == ContainersExec {
		// A terminal session lasts long: record that it was opened, before it runs.
		p.guard.Auditor.Audit(r, AuditRecord{
			Subject: s, Action: p.Action, Resource: res, Route: routePattern(r), Status: http.StatusSwitchingProtocols,
		})
		p.next.ServeHTTP(w, r)
		return
	}
	ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
	body := &limitedBuffer{}
	ww.Tee(body)
	p.next.ServeHTTP(ww, r)
	status := ww.Status()
	if status == 0 {
		status = http.StatusOK
	}
	p.guard.Auditor.Audit(r, AuditRecord{
		Subject: s, Action: p.Action, Resource: res, Route: routePattern(r), Status: status, Response: body.b,
	})
}

// PublicHandler is a handler explicitly marked as not requiring authorization.
type PublicHandler struct{ next http.Handler }

// Public marks h as reachable without authorization (setup, login, version, health).
func Public(h http.HandlerFunc) *PublicHandler { return &PublicHandler{next: h} }

// ServeHTTP implements http.Handler.
func (p *PublicHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) { p.next.ServeHTTP(w, r) }

// ResourceFromRequest builds the resource from the conventional route parameters
// ({env}, {id}) and the namespace query parameter (ADR-0006).
func ResourceFromRequest(r *http.Request) Resource {
	return Resource{
		Environment: chi.URLParam(r, "env"),
		Namespace:   r.URL.Query().Get("namespace"),
		ID:          chi.URLParam(r, "id"),
	}
}

// Route describes a registered route and its declared access.
type Route struct {
	Method string
	Path   string
	Action Action // empty for public routes
	Public bool
}

// Inspect lists routes whose path starts with prefix together with their declared access.
// It returns an error listing every route that declares neither an action nor Public.
func Inspect(routes chi.Routes, prefix string) ([]Route, error) {
	var (
		out        []Route
		undeclared []string
	)
	err := chi.Walk(routes, func(method, route string, h http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, prefix) {
			return nil
		}
		for {
			ch, ok := h.(*chi.ChainHandler)
			if !ok {
				break
			}
			h = ch.Endpoint
		}
		switch v := h.(type) {
		case *Protected:
			out = append(out, Route{Method: method, Path: route, Action: v.Action})
		case *PublicHandler:
			out = append(out, Route{Method: method, Path: route, Public: true})
		default:
			undeclared = append(undeclared, method+" "+route)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	if len(undeclared) > 0 {
		sort.Strings(undeclared)
		return out, fmt.Errorf("authz: routes without a declared action (wrap with Guard.Protect or authz.Public): %s",
			strings.Join(undeclared, ", "))
	}
	return out, nil
}
