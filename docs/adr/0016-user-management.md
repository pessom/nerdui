# 0016. User management: temporary passwords, disabling, last-admin guard

- **Status:** Accepted
- **Date:** 2026-11-12

## Context

ADR-0005 covers authentication of local users: the first admin from setup, argon2id
passwords, sessions, rate limiting and `nerdui admin reset-password`. #74 adds managing more
users from the UI. ADR-0009 has one built-in role, `admin`; roles come with RBAC (#78).

## Options considered

### Roles

- **Only `admin`** — every user is an administrator; the API returns `role` already.
- **`admin` + read-only `viewer`** — useful for sharing a dashboard, but needs a second
  authorizer, hiding actions in the UI and deciding what a viewer may see (environment
  variables in inspect may contain secrets).
- **Full RBAC** — #78.

### Removing access

- **Disable and delete** — disabling is reversible and keeps the account; deleting frees the
  username.
- **Disable only** — keeps every user forever for the audit log (#79).

### Passwords set by an administrator

- **Temporary password, changed at the next sign-in** — the administrator never knows the
  password the user ends up with.
- **Final password** — simpler, but the administrator knows it until the user changes it.

## Decision

Only `admin`; disable and delete; temporary passwords.

- `users` gains `disabled_at`, `must_change_password` and `last_login_at` (migration 00002).
- `GET|POST /users`, `GET|PATCH|DELETE /users/{id}` and `POST /users/{id}/password`
  (`users:read` / `users:write`). Without a password in the request, the server generates one
  (18 random bytes, 24 characters) and returns it once with `Cache-Control: no-store`.
- Created users and users whose password an administrator set have `mustChangePassword`.
  Their sessions may only use `GET /auth/me`, `POST /auth/logout` and `POST /auth/password`;
  everything else answers `403 password_change_required` (middleware after the session check).
  The UI sends them to a change-password page.
- `POST /auth/password` changes the caller's own password: it verifies the current one (rate
  limited per user), requires a different new one, clears the flag and revokes the caller's
  other sessions.
- Disabling, setting a password and deleting revoke the user's sessions. A disabled account
  cannot sign in; the error is returned only after a correct password, so it does not reveal
  which accounts exist. Sessions of disabled users are also rejected on every request.
- Nobody can disable, delete or set a temporary password for their own account through
  `/users`, and the last active administrator cannot be disabled or deleted (409). The check
  runs in the same write transaction as the change.
- `nerdui admin reset-password` also enables the account and does not set the flag: the
  operator on the host chose the password.

## Consequences

- With one role, every user can manage every other user and the whole host. The UI says so
  when adding a user. Who may see container environment variables (ADR-0013) is decided with
  roles in #78.
- Deleted users are gone; the audit log (#79) has to store the username, not only the ID.
- Usernames are compared case-insensitively (`COLLATE NOCASE`), as at setup.
