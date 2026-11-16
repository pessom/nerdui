// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

// Preferences are a user's interface settings (#90), kept on the server so they follow the
// user across browsers. Keep in sync with api/openapi.yaml.
type Preferences struct {
	// Theme is auto (the system's), light or dark.
	Theme string `json:"theme"`
	// DateFormat is relative ("5 min ago"), local (the browser's locale) or iso.
	DateFormat string `json:"dateFormat"`
	// RefreshSeconds polls lists in addition to live events; 0 turns it off.
	RefreshSeconds int `json:"refreshSeconds"`
	// HiddenColumns lists hidden columns per table.
	HiddenColumns map[string][]string `json:"hiddenColumns"`
}

// DefaultPreferences apply until a user saves their own.
func DefaultPreferences() Preferences {
	return Preferences{Theme: "auto", DateFormat: "relative", HiddenColumns: map[string][]string{}}
}

var (
	// ErrInvalidPreferences is returned for values outside the allowed ones.
	ErrInvalidPreferences = errors.New("invalid preferences")

	prefIdentifier = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]{0,39}$`)
	refreshChoices = []int{0, 5, 10, 30, 60}
)

const (
	maxPrefTables  = 20
	maxPrefColumns = 30
)

// Validate checks every field against the allowed values.
func (p *Preferences) Validate() error {
	if !slices.Contains([]string{"auto", "light", "dark"}, p.Theme) {
		return fmt.Errorf("%w: theme must be auto, light or dark", ErrInvalidPreferences)
	}
	if !slices.Contains([]string{"relative", "local", "iso"}, p.DateFormat) {
		return fmt.Errorf("%w: dateFormat must be relative, local or iso", ErrInvalidPreferences)
	}
	if !slices.Contains(refreshChoices, p.RefreshSeconds) {
		return fmt.Errorf("%w: refreshSeconds must be one of %v", ErrInvalidPreferences, refreshChoices)
	}
	if p.HiddenColumns == nil {
		p.HiddenColumns = map[string][]string{}
	}
	if len(p.HiddenColumns) > maxPrefTables {
		return fmt.Errorf("%w: too many tables", ErrInvalidPreferences)
	}
	for table, cols := range p.HiddenColumns {
		if !prefIdentifier.MatchString(table) || len(cols) > maxPrefColumns {
			return fmt.Errorf("%w: hidden columns of %q", ErrInvalidPreferences, table)
		}
		for _, c := range cols {
			if !prefIdentifier.MatchString(c) {
				return fmt.Errorf("%w: column %q", ErrInvalidPreferences, c)
			}
		}
	}
	return nil
}

// GetPreferences returns a user's preferences, or the defaults.
func (s *Service) GetPreferences(ctx context.Context, userID int64) (Preferences, error) {
	raw, err := s.store.Read().GetUserPreferences(ctx, userID)
	if store.IsNotFound(err) {
		return DefaultPreferences(), nil
	}
	if err != nil {
		return Preferences{}, err
	}
	p := DefaultPreferences()
	if err := json.Unmarshal([]byte(raw), &p); err != nil || p.Validate() != nil {
		return DefaultPreferences(), nil // an older or damaged value: start over
	}
	return p, nil
}

// SetPreferences validates and saves a user's preferences.
func (s *Service) SetPreferences(ctx context.Context, userID int64, p Preferences) (Preferences, error) {
	if err := p.Validate(); err != nil {
		return Preferences{}, err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return Preferences{}, err
	}
	if err := s.store.Write().SetUserPreferences(ctx, db.SetUserPreferencesParams{
		UserID: userID, Data: string(b), Now: s.opts.Now().Unix(),
	}); err != nil {
		return Preferences{}, err
	}
	return p, nil
}

func (s *Service) preferenceRoutes(r interface {
	Method(method, pattern string, h http.Handler)
}, g *authz.Guard) {
	r.Method(http.MethodGet, "/auth/preferences", g.Protect(authz.SessionSelf, s.handleGetPreferences))
	r.Method(http.MethodPut, "/auth/preferences", g.Protect(authz.SessionSelf, s.handlePutPreferences))
}

// GET /api/v1/auth/preferences
func (s *Service) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	prefs, err := s.GetPreferences(r.Context(), p.User.ID)
	if err != nil {
		s.opts.Logger.Error("read preferences", "error", err)
		httpjson.WriteError(w, http.StatusInternalServerError, httpjson.CodeInternal, "internal error")
		return
	}
	httpjson.Write(w, http.StatusOK, prefs)
}

// PUT /api/v1/auth/preferences  Preferences
func (s *Service) handlePutPreferences(w http.ResponseWriter, r *http.Request) {
	p, ok := passwordSession(w, r)
	if !ok {
		return
	}
	var req Preferences
	if !httpjson.Decode(w, r, &req) {
		return
	}
	saved, err := s.SetPreferences(r.Context(), p.User.ID, req)
	if errors.Is(err, ErrInvalidPreferences) {
		httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument, err.Error())
		return
	}
	if err != nil {
		s.opts.Logger.Error("save preferences", "error", err)
		httpjson.WriteError(w, http.StatusInternalServerError, httpjson.CodeInternal, "internal error")
		return
	}
	httpjson.Write(w, http.StatusOK, saved)
}
