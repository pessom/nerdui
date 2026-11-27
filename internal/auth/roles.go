// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sync"
	"unicode/utf8"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

// Role errors (#78, ADR-0058).
var (
	ErrRoleNotFound    = errors.New("role not found")
	ErrRoleName        = errors.New("role name must be 1-32 characters: lowercase letters, digits and '-', starting with a letter")
	ErrRoleTaken       = errors.New("a role with this name already exists")
	ErrRoleBuiltin     = errors.New("built-in roles cannot be changed")
	ErrRoleInUse       = errors.New("the role is given to users; give them another role first")
	ErrRoleDescription = errors.New("role description must be at most 200 characters")
	// ErrRoleAbove: the caller lacks some of the actions of a role or of a user's role.
	ErrRoleAbove = errors.New("you may not grant or manage permissions you do not have")
)

var roleNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// Role is a built-in or custom role. Keep in sync with api/openapi.yaml.
type Role struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Builtin     bool           `json:"builtin"`
	Permissions []authz.Action `json:"permissions"`
	// Permissions are listed without session:self, which every role has.
	// Users is the number of users with the role.
	Users int64 `json:"users"`
}

var builtinDescriptions = map[string]string{
	authz.RoleAdmin:    "Every action, including users and settings",
	authz.RoleEditor:   "Manage containers, stacks, images, volumes and networks; read settings",
	authz.RoleOperator: "View everything but administration; start, stop, restart, open terminals and deploy stacks",
	authz.RoleViewer:   "View containers, stacks, images, volumes, networks and the host",
}

// roleCache holds the actions of custom roles; nil until loaded and after a change.
type roleCache struct {
	mu     sync.RWMutex
	custom map[string][]authz.Action
}

// RolePermissions returns the actions of a role (authz.RoleLookup); an unknown role has none.
func (s *Service) RolePermissions(ctx context.Context, role string) ([]authz.Action, error) {
	if perms, ok := authz.Builtin(role); ok {
		return perms, nil
	}
	s.roles.mu.RLock()
	custom := s.roles.custom
	s.roles.mu.RUnlock()
	if custom == nil {
		rows, err := s.store.Read().ListRoles(ctx)
		if err != nil {
			return nil, err
		}
		custom = make(map[string][]authz.Action, len(rows))
		for _, r := range rows {
			custom[r.Name] = decodePermissions(r.Permissions)
		}
		s.roles.mu.Lock()
		s.roles.custom = custom
		s.roles.mu.Unlock()
	}
	return custom[role], nil
}

// forgetRoles drops the cache after a change to custom roles.
func (s *Service) forgetRoles() {
	s.roles.mu.Lock()
	s.roles.custom = nil
	s.roles.mu.Unlock()
}

// listed drops session:self, which every role and key has, from a list shown in the API.
func listed(perms []authz.Action) []authz.Action {
	out := []authz.Action{}
	for _, a := range perms {
		if a != authz.SessionSelf {
			out = append(out, a)
		}
	}
	return out
}

func decodePermissions(raw string) []authz.Action {
	var list []authz.Action
	_ = json.Unmarshal([]byte(raw), &list) // unreadable data grants nothing
	return authz.Normalize(list)
}

// ListRoles returns the built-in roles, then the custom ones by name.
func (s *Service) ListRoles(ctx context.Context) ([]Role, error) {
	q := s.store.Read()
	counts, err := q.CountRoleUsers(ctx)
	if err != nil {
		return nil, err
	}
	users := map[string]int64{}
	for _, c := range counts {
		users[c.Role] = c.Users
	}
	var out []Role
	for _, name := range authz.BuiltinRoles() {
		perms, _ := authz.Builtin(name)
		out = append(out, Role{
			Name: name, Description: builtinDescriptions[name], Builtin: true, Permissions: listed(perms), Users: users[name],
		})
	}
	rows, err := q.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out = append(out, Role{
			Name: r.Name, Description: r.Description, Permissions: listed(decodePermissions(r.Permissions)), Users: users[r.Name],
		})
	}
	return out, nil
}

// roleExists reports whether a role is built-in or stored.
func (s *Service) roleExists(ctx context.Context, q *db.Queries, name string) (bool, error) {
	if _, ok := authz.Builtin(name); ok {
		return true, nil
	}
	_, err := q.GetRole(ctx, name)
	if store.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// covers reports whether the caller may perform every action of perms.
func covers(can func(authz.Action) bool, perms []authz.Action) bool {
	for _, a := range perms {
		if !can(a) {
			return false
		}
	}
	return true
}

func validRole(description string, perms []authz.Action) ([]authz.Action, error) {
	if utf8.RuneCountInString(description) > 200 {
		return nil, ErrRoleDescription
	}
	return authz.Normalize(perms), nil
}

// CreateRole adds a custom role. can tells which actions the caller may perform: the role
// may not have any other.
func (s *Service) CreateRole(ctx context.Context, can func(authz.Action) bool, name, description string, perms []authz.Action) (Role, error) {
	if !roleNameRe.MatchString(name) {
		return Role{}, ErrRoleName
	}
	if _, ok := authz.Builtin(name); ok {
		return Role{}, ErrRoleTaken
	}
	perms, err := validRole(description, perms)
	if err != nil {
		return Role{}, err
	}
	if !covers(can, perms) {
		return Role{}, ErrRoleAbove
	}
	raw, _ := json.Marshal(perms)
	_, err = s.store.Write().CreateRole(ctx, db.CreateRoleParams{
		Name: name, Description: description, Permissions: string(raw), Now: s.opts.Now().Unix(),
	})
	if store.IsUniqueViolation(err) {
		return Role{}, ErrRoleTaken
	}
	if err != nil {
		return Role{}, err
	}
	s.forgetRoles()
	s.opts.Logger.Info("role created", "role", name)
	return Role{Name: name, Description: description, Permissions: listed(perms)}, nil
}

// UpdateRole changes a custom role; the caller must have every action before and after.
func (s *Service) UpdateRole(ctx context.Context, can func(authz.Action) bool, name, description string, perms []authz.Action) (Role, error) {
	if _, ok := authz.Builtin(name); ok {
		return Role{}, ErrRoleBuiltin
	}
	perms, err := validRole(description, perms)
	if err != nil {
		return Role{}, err
	}
	var users int64
	err = s.store.Tx(ctx, func(q *db.Queries) error {
		old, err := q.GetRole(ctx, name)
		if store.IsNotFound(err) {
			return ErrRoleNotFound
		}
		if err != nil {
			return err
		}
		if !covers(can, decodePermissions(old.Permissions)) || !covers(can, perms) {
			return ErrRoleAbove
		}
		raw, _ := json.Marshal(perms)
		if _, err := q.UpdateRole(ctx, db.UpdateRoleParams{
			Name: name, Description: description, Permissions: string(raw), Now: s.opts.Now().Unix(),
		}); err != nil {
			return err
		}
		users, err = q.CountUsersWithRole(ctx, name)
		return err
	})
	if err != nil {
		return Role{}, err
	}
	s.forgetRoles()
	s.opts.Logger.Info("role updated", "role", name)
	return Role{Name: name, Description: description, Permissions: listed(perms), Users: users}, nil
}

// DeleteRole removes a custom role that no user has.
func (s *Service) DeleteRole(ctx context.Context, can func(authz.Action) bool, name string) error {
	if _, ok := authz.Builtin(name); ok {
		return ErrRoleBuiltin
	}
	err := s.store.Tx(ctx, func(q *db.Queries) error {
		old, err := q.GetRole(ctx, name)
		if store.IsNotFound(err) {
			return ErrRoleNotFound
		}
		if err != nil {
			return err
		}
		if !covers(can, decodePermissions(old.Permissions)) {
			return ErrRoleAbove
		}
		if n, err := q.CountUsersWithRole(ctx, name); err != nil {
			return err
		} else if n > 0 {
			return ErrRoleInUse
		}
		_, err = q.DeleteRole(ctx, name)
		return err
	})
	if err != nil {
		return err
	}
	s.forgetRoles()
	s.opts.Logger.Info("role deleted", "role", name)
	return nil
}

// mayManage checks that the caller has every action of u's role (ADR-0058).
func (s *Service) mayManage(ctx context.Context, can func(authz.Action) bool, u db.User) error {
	if can == nil {
		return nil // command line and tests: the operator of the host
	}
	perms, err := s.RolePermissions(ctx, u.Role)
	if err != nil {
		return err
	}
	if !covers(can, perms) {
		return ErrRoleAbove
	}
	return nil
}

// mayGrant checks that role exists and that the caller has every one of its actions.
func (s *Service) mayGrant(ctx context.Context, q *db.Queries, can func(authz.Action) bool, role string) error {
	ok, err := s.roleExists(ctx, q, role)
	if err != nil {
		return err
	}
	if !ok {
		return ErrRoleNotFound
	}
	if can == nil {
		return nil
	}
	perms, err := s.RolePermissions(ctx, role)
	if err != nil {
		return err
	}
	if !covers(can, perms) {
		return ErrRoleAbove
	}
	return nil
}

// SetRole gives another user a role. The caller must have every action of the user's
// current and new role; the last active administrator keeps the admin role.
func (s *Service) SetRole(ctx context.Context, actor int64, can func(authz.Action) bool, id int64, role string) (UserDetail, error) {
	if actor == id {
		return UserDetail{}, ErrOwnAccount
	}
	var u db.User
	err := s.store.Tx(ctx, func(q *db.Queries) error {
		var err error
		if u, err = s.lockTarget(ctx, q, id, role != authz.RoleAdmin); err != nil {
			return err
		}
		if err := s.mayManage(ctx, can, u); err != nil {
			return err
		}
		if err := s.mayGrant(ctx, q, can, role); err != nil {
			return err
		}
		if _, err := q.SetUserRole(ctx, db.SetUserRoleParams{ID: id, Role: role, Now: s.opts.Now().Unix()}); err != nil {
			return err
		}
		u.Role = role
		return nil
	})
	if err != nil {
		return UserDetail{}, err
	}
	s.opts.Logger.Info("user role changed", "username", u.Username, "role", role, "actor_id", actor)
	return toDetail(u), nil
}

// permissions are the actions of a role, limited by the API key that made the request,
// without session:self.
func (s *Service) permissions(ctx context.Context, role string) []authz.Action {
	perms, err := s.RolePermissions(ctx, role)
	if err != nil {
		s.opts.Logger.Error("role permissions", "role", role, "error", err)
		return []authz.Action{}
	}
	p, ok := PrincipalFrom(ctx)
	if !ok || p.APIKey == nil {
		return listed(perms)
	}
	sub := authz.Subject{APIKey: true, ReadOnly: p.APIKey.ReadOnly, Permissions: p.APIKey.Permissions}
	out := []authz.Action{}
	for _, a := range perms {
		if a != authz.SessionSelf && sub.KeyAllows(a) == nil {
			out = append(out, a)
		}
	}
	return out
}

var _ authz.RoleLookup = (*Service)(nil)
