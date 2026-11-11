# 0009. Authorization layer with per-action permissions

- **Status:** Accepted
- **Date:** 2026-11-11

## Context

MVP has a single admin role, but RBAC (#78), API keys limited to the creator's permissions (#80)
and a role without console access (#102) are planned. Retrofitting permission checks into every
handler later is error-prone.

## Decision

- Every API route declares the **action** it performs, e.g.:

  | Action | Examples |
  | --- | --- |
  | `containers:read` | list, inspect, logs, stats |
  | `containers:operate` | start, stop, restart, kill, pause |
  | `containers:write` | create, recreate, remove |
  | `containers:exec` | web terminal (kept separate for #102) |
  | `images:read` / `images:write` | list / pull, tag, push, remove |
  | `volumes:*`, `networks:*` | read / write |
  | `stacks:read` / `stacks:write` / `stacks:deploy` | view / edit files / up, down |
  | `system:read` / `system:prune` | info, events, df / prune |
  | `registries:*`, `users:*`, `settings:*` | administration |

- Checks happen in middleware via an `authz.Authorizer` interface:
  `Authorize(ctx, principal, action, resource) error`, where `resource` carries environment,
  namespace and object identifiers (needed for future resource-level rules).
- **Default deny:** a route without a declared action fails at startup (router test).
- **MVP implementation:** one built-in role `admin` that allows every action. The `users` table
  already has a `role` column.
- Denied checks return `403 forbidden` and are logged (and later written to the audit log #79).

## Consequences

- RBAC, API-key scopes and restricted roles become new `Authorizer` implementations and data,
  not handler changes.
- The action catalog doubles as the permission list for the future roles UI.
