# 0036. Audit log

- **Status:** Accepted
- **Date:** 2026-11-20

## Context

With several users, API keys and automatic actions, administrators need to know who changed
what, from where and with which result (#79). Dockhand records user, action, resource,
environment, IP, user agent and outcome; Komodo records every change with its author.

## Options considered

- **Where to record:** in every handler (easy to forget a new one), or in one place that already
  sees every protected request: the authorization guard (ADR-0009), which knows the caller, the
  permission and the resource. Public endpoints (setup, login) are recorded by the auth service.
- **What to record:** request bodies would make entries self-explanatory but contain passwords,
  tokens, registry credentials and environment values. The route pattern, its parameters, the
  namespace, the HTTP status and the error message identify the action without them.
- **Results of background jobs:** the request only starts them (202). The entry keeps the job
  ID and is completed when the job ends.

## Decision

- Recorded (migration 10, `audit_log`): every state-changing request (unsafe method on a
  non-read permission, or on the caller's own session: logout, password, API keys,
  preferences), terminal sessions when they open, every authorization denial, and setup and
  login attempts, also failed ones with the username tried. Reads are not recorded.
- Each entry has the time, user, whether an API key was used, permission (or `auth:login` /
  `auth:setup`), method, route pattern, environment, namespace, object (route parameters),
  HTTP status, result (`ok`, `failed`, `denied`, or `accepted` until the job ends), error
  message, job ID, client address (the real client behind trusted proxies, ADR-0035) and user
  agent.
- `audit:read` (administrators) reads it: `GET /audit` with filters (user, result, text, time)
  and paging, and `GET /audit/export` as CSV (cells that a spreadsheet would run as formulas are
  escaped). The UI has an Audit log page.
- Entries are kept for `--audit-retention` (default 90 days; 0 keeps them).

## Consequences

- New endpoints are audited without extra code, as long as they use the guard.
- A request that changes nothing but uses a write permission (e.g. sending a test notification)
  is recorded too.
- A username typed into the login form is stored also when the login fails.
- The log is in nerdui's database: someone with root on the host can change it. It is a record
  of use, not tamper-proof evidence.
