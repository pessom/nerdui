// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package variables keeps global and per-stack variables (#35, ADR-0049). They are encrypted in
// the database and passed to `nerdctl compose` as environment variables, so compose files
// interpolate them as ${NAME} and they take precedence over the stack's .env file. Secret
// values are never returned by the API.
package variables

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/secretbox"
	"github.com/pessom/nerdui/internal/store/db"
)

// Global is the scope of variables shared by all stacks.
const Global = ""

const (
	// MaxValue is the largest value in bytes.
	MaxValue = 64 << 10
	maxName  = 128
)

var nameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Names that would change how nerdctl, compose or the system run rather than the stack.
var (
	reservedNames    = []string{"PATH", "HOME", "USER", "LOGNAME", "SHELL", "PWD", "TMPDIR", "IFS"}
	reservedPrefixes = []string{"LD_", "XDG_", "CONTAINERD_", "NERDCTL_", "DOCKER_", "COMPOSE_", "BUILDKIT_", "CNI_", "NERDUI_", "ROOTLESSKIT_"}
)

// ValidateName checks a variable name.
func ValidateName(name string) error {
	if len(name) > maxName || !nameRe.MatchString(name) {
		return fmt.Errorf("%w: variable name %q: use letters, digits and _, not starting with a digit", errdefs.ErrInvalidArgument, name)
	}
	up := strings.ToUpper(name)
	if slices.Contains(reservedNames, up) {
		return fmt.Errorf("%w: variable name %q is reserved", errdefs.ErrInvalidArgument, name)
	}
	for _, p := range reservedPrefixes {
		if strings.HasPrefix(up, p) {
			return fmt.Errorf("%w: variable names starting with %s are reserved", errdefs.ErrInvalidArgument, p)
		}
	}
	return nil
}

// Store is the database; *db.Queries implements it.
type Store interface {
	ListVariables(ctx context.Context, stack string) ([]db.Variable, error)
	GetVariable(ctx context.Context, arg db.GetVariableParams) (db.Variable, error)
	PutVariable(ctx context.Context, arg db.PutVariableParams) error
	DeleteVariable(ctx context.Context, arg db.DeleteVariableParams) (int64, error)
	DeleteStackVariables(ctx context.Context, stack string) error
}

// Service manages variables.
type Service struct {
	Store Store
	Box   *secretbox.Box
	now   func() time.Time
}

// Variable as the API shows it. Keep in sync with api/openapi.yaml.
type Variable struct {
	Name string `json:"name"`
	// Value is empty for secrets.
	Value     string    `json:"value"`
	Secret    bool      `json:"secret"`
	UpdatedBy string    `json:"updatedBy,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Input changes a variable. A nil Value keeps the stored one; it is required for a new
// variable and to make a secret visible.
type Input struct {
	Value  *string `json:"value"`
	Secret bool    `json:"secret"`
}

func aad(stack, name string) string { return "variable\x00" + stack + "\x00" + name }

func (s *Service) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// List returns the variables of a scope (Global or a stack name) without secret values.
func (s *Service) List(ctx context.Context, stack string) ([]Variable, error) {
	rows, err := s.Store.ListVariables(ctx, stack)
	if err != nil {
		return nil, err
	}
	out := make([]Variable, 0, len(rows))
	for _, r := range rows {
		v := Variable{Name: r.Name, Secret: r.Secret == 1, UpdatedBy: r.UpdatedBy, UpdatedAt: time.UnixMilli(r.UpdatedAt).UTC()}
		if !v.Secret {
			if v.Value, err = s.Box.Open(r.Value, aad(stack, r.Name)); err != nil {
				return nil, err
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// Put creates or changes a variable.
func (s *Service) Put(ctx context.Context, stack, name string, in Input, user string) (Variable, error) {
	if err := ValidateName(name); err != nil {
		return Variable{}, err
	}
	value := ""
	if in.Value != nil {
		value = *in.Value
		if len(value) > MaxValue || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return Variable{}, fmt.Errorf("%w: the value must be UTF-8 text up to %d KiB without NUL", errdefs.ErrInvalidArgument, MaxValue>>10)
		}
	} else {
		cur, err := s.Store.GetVariable(ctx, db.GetVariableParams{Stack: stack, Name: name})
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return Variable{}, fmt.Errorf("%w: a new variable needs a value", errdefs.ErrInvalidArgument)
		case err != nil:
			return Variable{}, err
		case cur.Secret == 1 && !in.Secret:
			return Variable{}, fmt.Errorf("%w: enter the value to make a secret visible", errdefs.ErrInvalidArgument)
		}
		if value, err = s.Box.Open(cur.Value, aad(stack, name)); err != nil {
			return Variable{}, err
		}
	}
	sealed, err := s.Box.Seal(value, aad(stack, name))
	if err != nil {
		return Variable{}, err
	}
	now := s.clock()
	secret := int64(0)
	if in.Secret {
		secret = 1
	}
	if err := s.Store.PutVariable(ctx, db.PutVariableParams{
		Stack: stack, Name: name, Value: sealed, Secret: secret, UpdatedBy: user, UpdatedAt: now.UnixMilli(),
	}); err != nil {
		return Variable{}, err
	}
	v := Variable{Name: name, Secret: in.Secret, UpdatedBy: user, UpdatedAt: time.UnixMilli(now.UnixMilli()).UTC()}
	if !in.Secret {
		v.Value = value
	}
	return v, nil
}

// Delete removes a variable.
func (s *Service) Delete(ctx context.Context, stack, name string) error {
	n, err := s.Store.DeleteVariable(ctx, db.DeleteVariableParams{Stack: stack, Name: name})
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: variable %q not found", errdefs.ErrNotFound, name)
	}
	return nil
}

// DeleteStack removes the variables of a stack that nerdui forgets, so a later stack of the
// same name does not get them.
func (s *Service) DeleteStack(ctx context.Context, stack string) error {
	if stack == Global {
		return nil
	}
	return s.Store.DeleteStackVariables(ctx, stack)
}

// Values returns the variables of a stack as NAME=value: the global ones, overridden by the
// stack's own.
func (s *Service) Values(ctx context.Context, stack string) (map[string]string, error) {
	out := map[string]string{}
	scopes := []string{Global}
	if stack != Global {
		scopes = append(scopes, stack)
	}
	for _, scope := range scopes {
		rows, err := s.Store.ListVariables(ctx, scope)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			v, err := s.Box.Open(r.Value, aad(scope, r.Name))
			if err != nil {
				return nil, fmt.Errorf("variable %s: %w", r.Name, err)
			}
			out[r.Name] = v
		}
	}
	return out, nil
}
