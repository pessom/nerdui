# 0053. API keys limited to chosen permissions

- **Status:** Accepted
- **Date:** 2026-11-24

## Context

An API key (ADR-0028) acted as its user, either fully or read-only. A CI pipeline that only
redeploys stacks needed a full key, able to remove containers and volumes or to change users
(#80). The audit log only showed that a request used some key, not which one.

## Options considered

- **Scopes of our own** (e.g. "deploy", "monitor"): simple, but a second vocabulary next to
  the authorization actions, to keep in sync.
- **The authorization actions themselves** (ADR-0009): the same names as roles will use (#78),
  checked by the same guard.
- **Where the limit applies**: copy the user's permissions into the key when it is created,
  or check every request against both the key and its user.

## Decision

- A key may carry a list of authorization actions (`permissions`). Without it, it may do
  everything its user may, as before; `readOnly` keeps limiting a key to read actions.
- Each request is checked against the key (`Subject.KeyAllows`) and then by the authorizer
  for the user. A key can therefore never exceed its user, also after the user loses a
  permission.
- On creation, every chosen action must be one the creator may perform (403 otherwise);
  `session:self` is implied and not listed.
- Denials by a key's limits are written to the audit log like other denials. Every audit entry
  of a key request records `"<name> (<prefix>…)"` of the key, searchable and exported to CSV.

## Consequences

- Keys cannot be edited: a key with other permissions is a new key, and the old one is
  revoked.
- Permissions are stored as a JSON array in `api_keys.permissions` (migration 19); unknown
  actions in old data simply never match.
