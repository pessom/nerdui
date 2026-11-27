# 0058. Roles: built-in and custom

- **Status:** Accepted
- **Date:** 2026-11-27

## Context

Every user has been an administrator (ADR-0009, ADR-0016): anyone who may sign in may remove
containers, open terminals, change settings and manage other users. Teams want people who
only watch, restart services or deploy stacks (#78), and a role without the web terminal
(#102). Every route already declares its action (ADR-0009), and API keys can be limited to a
list of those actions (ADR-0053).

## Options considered

- **Fixed levels** (viewer / operator / admin): simple, but each team draws the lines
  differently, and #102 needs one more level.
- **Built-in roles plus custom roles from the action catalog**: the built-in roles cover the
  common cases; a custom role is a named list of the same actions an API key uses.
- **Per-resource rules** (namespaces, stacks by name, environments): needed once there are
  several hosts (#69), but a separate step; the authorizer already receives the resource.
- **Where roles live**: built-in roles in code, so that a new action added to the catalog
  reaches them on upgrade; custom roles in the database.

## Decision

- A user has exactly one role. Built-in roles, defined in code and not editable:
  - `admin`: every action.
  - `editor`: every action except users, `settings:write` and `audit:read`.
  - `operator`: what a viewer reads, plus `containers:operate`, `containers:exec` and
    `stacks:deploy`: start, stop, restart, terminal, deploy.
  - `viewer`: read actions, except `users:read`, `settings:read` and `audit:read`.
  `session:self` and `jobs:read` belong to every role.
- Custom roles have a name (`[a-z][a-z0-9-]{0,31}`, not a built-in one), a description and a
  list of actions; they are stored in the `roles` table and managed with `users:write`.
  A role used by a user cannot be deleted. Changes apply to the next request of every user
  with the role, and to their API keys.
- The authorizer looks up the role's actions on every request (custom roles from a cache that
  is refreshed on every change). An unknown role allows nothing.
- No one gains permissions through user management:
  - giving a user a role, and creating or changing a custom role, requires the caller to have
    every action of that role (before and after a change);
  - changing another user (password, disable, delete, two-factor reset, single sign-on link,
    API keys) requires the caller to have every action of that user's role.
- At least one active user keeps the `admin` role: the last one cannot be disabled, deleted
  or given another role.
- New users get `viewer` unless a role is given. Accounts created at the first single sign-on
  get the role chosen in the provider settings (`admin` for settings saved before roles).
- `/auth/me` returns the caller's actions (`permissions`); the UI hides what the user may not
  do: menu sections, buttons and menu items of actions, the terminal tab; pages of settings the
  user may read but not change are shown with their controls disabled. The server still checks
  every request.

## Consequences

- `settings:write` remains powerful: schedules, webhooks, Git credentials and nerdui backups
  are settings. The built-in roles below `admin` do not have it.
- Per-resource rules (namespaces, stacks, environments) are left for multi-host (#69) and may
  extend roles later without changing the actions.
- Migration 22 replaces the `CHECK (role IN ('admin'))` of `users.role` and adds `roles`.
