# 0028. API keys

- **Status:** Accepted
- **Date:** 2026-11-16

## Context

The REST API (#87) is used by the UI with session cookies and a CSRF token (ADR-0005), which
scripts and integrations (Home Assistant, cron, CI) cannot use comfortably. Roles do not exist
yet (#78): every user is an administrator.

## Options considered

- **Per-user API keys with the owner's rights, optionally read-only** — simple, useful for
  monitoring with least privilege; finer rights come with roles.
- **Keys with a chosen set of actions** — flexible, but overlaps with roles and is harder to use.
- **Long-lived sessions** — no separate revocation or naming; cookies and CSRF remain.

## Decision

- A key is `nerdui_` + 32 random bytes (base64url), sent as `Authorization: Bearer <key>`. Only
  its SHA-256 is stored (migration 4, table `api_keys`) with a name, a display prefix, the
  read-only flag, an optional expiry and the time of last use (updated at most once a minute).
  The key is shown once, when it is created.
- A key acts as its user. A read-only key may only perform `*:read` actions (and read its own
  session); the check is in the authorization guard, before the authorizer, so it also holds
  with roles later. Bearer requests need no CSRF token: browsers never send the header by
  themselves.
- Keys cannot log out, change the password or manage keys: those need a password session.
- Keys of disabled users are rejected; deleting a user deletes the keys; at most 50 keys per user.
- Endpoints: `GET/POST /auth/api-keys`, `DELETE /auth/api-keys/{key}` (own keys);
  `GET /users/{id}/api-keys`, `DELETE /users/{id}/api-keys/{key}` (`users:read`/`users:write`).
- WebSocket endpoints accept the same header (non-browser clients).

## Consequences

- A leaked key gives its user's rights until revoked or expired; read-only keys and expiries
  limit the damage. Keys are not shown again: a lost key is replaced, not recovered.
- Request logs never contain keys; the prefix identifies a key in the UI.
