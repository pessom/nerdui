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
)

// Action is a permission name in the form "<area>:<verb>".
type Action string

// Action catalog (ADR-0009). Add new actions here; the catalog doubles as the permission
// list for the future roles UI (#78).
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

	SystemRead  Action = "system:read" // info, events, df, runtime versions
	SystemPrune Action = "system:prune"

	RegistriesRead  Action = "registries:read"
	RegistriesWrite Action = "registries:write"
	UsersRead       Action = "users:read"
	UsersWrite      Action = "users:write"
	SettingsRead    Action = "settings:read"
	SettingsWrite   Action = "settings:write"
)

var catalog = []Action{
	SessionSelf, JobsRead,
	ContainersRead, ContainersOperate, ContainersWrite, ContainersExec,
	ImagesRead, ImagesWrite,
	VolumesRead, VolumesWrite, NetworksRead, NetworksWrite,
	StacksRead, StacksWrite, StacksDeploy,
	SystemRead, SystemPrune,
	RegistriesRead, RegistriesWrite, UsersRead, UsersWrite, SettingsRead, SettingsWrite,
}

// All returns every known action.
func All() []Action { return slices.Clone(catalog) }

// Known reports whether a is in the catalog.
func Known(a Action) bool { return slices.Contains(catalog, a) }

// RoleAdmin is the only role in MVP.
const RoleAdmin = "admin"

// Subject is the authenticated caller.
type Subject struct {
	UserID   int64
	Username string
	Role     string
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

// AdminOnly is the MVP authorizer: the admin role may do everything, nobody else anything.
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
