// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package authz implements per-action authorization (ADR-0009).
//
// Every API route declares the action it performs by wrapping its handler with
// Guard.Protect, or explicitly opts out with Public. Verify walks the router and rejects
// routes that did neither, so a forgotten check fails at startup instead of silently
// allowing access (default deny).
//
// The package does not depend on the authentication implementation: the guard obtains the
// caller through a SubjectFunc supplied by the server.
package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Action is a permission name in the form "<area>:<verb>".
type Action string

// Action catalog (ADR-0009). Add new actions here; the catalog is also the permission list
// of roles (ADR-0058) and API keys (ADR-0053). Check the built-in roles (Builtin) too.
const (
	SessionSelf Action = "session:self" // own session: me, logout

	// JobsRead lists background jobs (#126); each job is shown only to callers allowed the
	// action that started it, which is also required to cancel it.
	JobsRead Action = "jobs:read"

	ContainersRead    Action = "containers:read"    // list, inspect, logs, stats
	ContainersOperate Action = "containers:operate" // start, stop, restart, kill, pause
	ContainersWrite   Action = "containers:write"   // create, recreate, remove
	ContainersExec    Action = "containers:exec"    // web terminal (kept separate for #102)

	ImagesRead  Action = "images:read"
	ImagesWrite Action = "images:write" // pull, tag, push, remove

	VolumesRead   Action = "volumes:read"
	VolumesWrite  Action = "volumes:write"
	NetworksRead  Action = "networks:read"
	NetworksWrite Action = "networks:write"

	StacksRead   Action = "stacks:read"
	StacksWrite  Action = "stacks:write"  // edit files, adopt, remove
	StacksDeploy Action = "stacks:deploy" // up, down, restart, pull

	NamespacesRead  Action = "namespaces:read"
	NamespacesWrite Action = "namespaces:write" // create, remove (#195)

	SystemRead  Action = "system:read" // info, events, df, runtime versions
	SystemPrune Action = "system:prune"

	RegistriesRead  Action = "registries:read"
	RegistriesWrite Action = "registries:write"
	UsersRead       Action = "users:read"
	UsersWrite      Action = "users:write"
	SettingsRead    Action = "settings:read"
	SettingsWrite   Action = "settings:write"
	AuditRead       Action = "audit:read" // the audit log (#79)
)

var catalog = []Action{
	SessionSelf, JobsRead,
	ContainersRead, ContainersOperate, ContainersWrite, ContainersExec,
	ImagesRead, ImagesWrite,
	VolumesRead, VolumesWrite, NetworksRead, NetworksWrite,
	StacksRead, StacksWrite, StacksDeploy,
	NamespacesRead, NamespacesWrite,
	SystemRead, SystemPrune,
	RegistriesRead, RegistriesWrite, UsersRead, UsersWrite, SettingsRead, SettingsWrite, AuditRead,
}

// All returns every known action.
func All() []Action { return slices.Clone(catalog) }

// Known reports whether a is in the catalog.
func Known(a Action) bool { return slices.Contains(catalog, a) }

// Built-in roles (ADR-0058); custom roles are stored by the auth service.
const (
	RoleAdmin    = "admin"    // every action
	RoleEditor   = "editor"   // everything but users, settings:write and the audit log
	RoleOperator = "operator" // what a viewer reads; start, stop, restart, terminal, deploy
	RoleViewer   = "viewer"   // read actions, except users, settings and the audit log
)

// BuiltinRoles lists the built-in roles, most permissions first.
func BuiltinRoles() []string { return []string{RoleAdmin, RoleEditor, RoleOperator, RoleViewer} }

// Always are the actions every role has: the caller's own session and jobs (each job is
// further limited to callers allowed the action that started it).
var Always = []Action{SessionSelf, JobsRead}

// adminArea reports whether a belongs to administration: users, settings and the audit log.
func adminArea(a Action) bool {
	switch a {
	case UsersRead, UsersWrite, SettingsRead, SettingsWrite, AuditRead:
		return true
	}
	return false
}

// Builtin returns the actions of a built-in role, in catalog order.
func Builtin(role string) ([]Action, bool) {
	var keep func(Action) bool
	switch role {
	case RoleAdmin:
		keep = func(Action) bool { return true }
	case RoleEditor:
		keep = func(a Action) bool { return !adminArea(a) || a == SettingsRead }
	case RoleOperator:
		keep = func(a Action) bool {
			return (a.Read() && !adminArea(a)) || a == ContainersOperate || a == ContainersExec || a == StacksDeploy
		}
	case RoleViewer:
		keep = func(a Action) bool { return a.Read() && !adminArea(a) }
	default:
		return nil, false
	}
	var out []Action
	for _, a := range catalog {
		if keep(a) || slices.Contains(Always, a) {
			out = append(out, a)
		}
	}
	return out, true
}

// Normalize returns the known actions of list plus Always, deduplicated, in catalog order.
func Normalize(list []Action) []Action {
	var out []Action
	for _, a := range catalog {
		if slices.Contains(list, a) || slices.Contains(Always, a) {
			out = append(out, a)
		}
	}
	return out
}

// RoleLookup returns the actions of a role; an unknown role has none.
type RoleLookup interface {
	RolePermissions(ctx context.Context, role string) ([]Action, error)
}

// Roles authorizes by the actions of the subject's role (ADR-0058).
type Roles struct{ Lookup RoleLookup }

// Authorize implements Authorizer.
func (r Roles) Authorize(ctx context.Context, s Subject, a Action, _ Resource) error {
	if !Known(a) {
		return fmt.Errorf("%w: unknown action %q", ErrForbidden, a)
	}
	perms, err := r.Lookup.RolePermissions(ctx, s.Role)
	if err != nil {
		return err
	}
	if !slices.Contains(perms, a) {
		return fmt.Errorf("%w: role %q may not %s", ErrForbidden, s.Role, a)
	}
	return nil
}

// Subject is the authenticated caller.
type Subject struct {
	UserID   int64
	Username string
	Role     string
	// APIKey is set when the caller authenticated with an API key (#87); ReadOnly keys may
	// only perform read actions.
	APIKey   bool
	ReadOnly bool
	// Permissions limits an API key to these actions (#80); nil allows everything its user
	// may do. The user's own permissions apply as well.
	Permissions []Action
	// KeyName identifies the API key in the audit log: "<name> (<prefix>…)".
	KeyName string
}

// KeyAllows reports why the subject's API key may not perform a, or nil. Password sessions
// and unrestricted keys allow every action; the authorizer still decides for the user.
func (s Subject) KeyAllows(a Action) error {
	if !s.APIKey {
		return nil
	}
	if s.ReadOnly && !a.Read() {
		return fmt.Errorf("%w: a read-only API key may not %s", ErrForbidden, a)
	}
	if s.Permissions != nil && a != SessionSelf && !slices.Contains(s.Permissions, a) {
		return fmt.Errorf("%w: this API key may not %s", ErrForbidden, a)
	}
	return nil
}

// Read reports whether an action only reads: "<area>:read", or the caller's own session.
func (a Action) Read() bool {
	return strings.HasSuffix(string(a), ":read") || a == SessionSelf
}

// Resource identifies what an action applies to. Fields are empty when not applicable;
// they allow resource-level rules later (e.g. #97 is out of scope, but namespaces may be).
type Resource struct {
	Environment string
	Namespace   string
	ID          string
}

// String formats the resource for logs.
func (r Resource) String() string {
	return fmt.Sprintf("env=%q namespace=%q id=%q", r.Environment, r.Namespace, r.ID)
}

// ErrForbidden is returned when a subject may not perform an action.
var ErrForbidden = errors.New("forbidden")

// Authorizer decides whether a subject may perform an action on a resource.
type Authorizer interface {
	Authorize(ctx context.Context, s Subject, a Action, r Resource) error
}

// AdminOnly allows the admin role everything and nobody else anything; tests use it.
// Unknown actions are always denied.
type AdminOnly struct{}

// Authorize implements Authorizer.
func (AdminOnly) Authorize(_ context.Context, s Subject, a Action, _ Resource) error {
	if !Known(a) {
		return fmt.Errorf("%w: unknown action %q", ErrForbidden, a)
	}
	if s.Role != RoleAdmin {
		return fmt.Errorf("%w: role %q may not %s", ErrForbidden, s.Role, a)
	}
	return nil
}
