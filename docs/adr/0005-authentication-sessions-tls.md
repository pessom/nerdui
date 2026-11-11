# 0005. Authentication, sessions and transport security

- **Status:** Accepted
- **Date:** 2026-11-11

## Context

nerdui grants root-equivalent access to the host. At home it is often opened by IP address on
the LAN, without a reverse proxy, over plain HTTP. MVP needs local users (#74); v2 adds MFA (#75),
OIDC (#76) and API keys (#80).

## Options considered

- **Server-side sessions + cookie** — revocable instantly, cookie invisible to JavaScript.
- **JWT** — stateless, but hard to revoke and usually stored where XSS can read it; no benefit
  for a single server.
- **Only an external auth proxy** (Authelia, Authentik, oauth2-proxy) — insecure by default when
  no proxy is configured. May be added later as an *optional* mode.

For transport: built-in TLS with a self-signed certificate by default, plain HTTP behind a
reverse proxy, or ACME (Let's Encrypt).

## Decision

### Authentication

- **Passwords:** argon2id (`golang.org/x/crypto/argon2`), per-user salt, parameters stored with
  the hash so they can be raised later. Minimum length 12.
- **Sessions:** random 256-bit token; only its SHA-256 is stored in SQLite with user, created,
  last-seen, expiry, client IP and user agent. Idle timeout 24 h, absolute lifetime 30 days
  (configurable). Logout and password change revoke sessions.
- **Cookie:** `nerdui_session`, `HttpOnly`, `Secure` (when TLS), `SameSite=Strict`, `Path=/`.
- **CSRF:** `SameSite=Strict` plus a required `X-CSRF-Token` header on every non-GET request
  (token bound to the session, returned by `GET /api/v1/auth/me`). WebSocket upgrades require a
  same-origin `Origin` header.
- **First run:** when no users exist, the UI shows a setup page to create the admin. To prevent a
  stranger on the LAN from claiming a fresh install, setup requires a one-time **setup token**
  printed to nerdui's log on startup.
- **Brute force:** login rate limit per IP and per username (e.g. 5 attempts / minute, then
  exponential backoff); failed logins are logged.
- **Recovery:** `nerdui admin reset-password <user>` CLI subcommand (requires host access).
- **Future:** API keys (#80) use `Authorization: Bearer` with hashed keys in the database;
  OIDC (#76) and MFA (#75) plug into the same session issuance.

### Transport security

- **MVP:** TLS is **on by default**. On first start nerdui generates a self-signed ECDSA P-256
  certificate (SANs: hostname, all local IPs, `localhost`; validity 825 days, regenerated when
  expiring) in `<data-dir>/tls/`. Users can provide their own (`--tls-cert`, `--tls-key`) or
  disable TLS (`--tls=false`) when running behind a reverse proxy.
- **v2:** first-class reverse-proxy mode (trusted proxy headers `X-Forwarded-*`, documented
  Caddy/Traefik/nginx examples) and automatic certificates via ACME (Let's Encrypt).

## Consequences

- Browsers show a warning for the self-signed certificate on first visit — documented, with
  instructions to trust it or use a proxy/ACME.
- All authentication state is in SQLite ([0004](0004-state-storage-sqlite.md)); sessions survive
  restarts.
- Every protected handler runs behind the session middleware and then the authorizer
  ([0009](0009-authorization.md)).
