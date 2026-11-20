# 0035. Running behind a reverse proxy

- **Status:** Accepted
- **Date:** 2026-11-20

## Context

Many self-hosters put every service behind one reverse proxy (Caddy, Traefik, nginx, Nginx
Proxy Manager) that holds the public certificate (#112). nerdui already allowed `--tls=false`
(ADR-0005), but then saw the proxy as the client: login rate limiting counted all users as one
address, sessions recorded the proxy's IP, and session cookies lost `Secure` although the
browser used HTTPS.

## Options considered

- **Trust forwarding headers always:** any client could choose its own address and bypass rate
  limiting. Rejected.
- **A list of trusted proxies** (as Traefik, Gitea and Grafana do): headers only count from
  these addresses.
- **`Forwarded` (RFC 7239) as well:** common proxies all send `X-Forwarded-*`; one format keeps
  the parsing small.
- **Sub-path deployment** (`/nerdui`): the UI is built with absolute asset and API paths and a
  router at `/`; supporting a prefix touches the build, the router and every link. Deferred to
  its own issue (#222); a host name per service is the common setup.

## Decision

- `--trusted-proxies` (`NERDUI_TRUSTED_PROXIES`) takes IPs and CIDRs. For requests from these
  addresses only, as the first middleware:
  - the client address is the rightmost `X-Forwarded-For` entry that is not itself a trusted
    proxy (chains of proxies work; a forged leftmost entry is ignored);
  - `X-Forwarded-Proto: https` marks the request as HTTPS, so session cookies get `Secure` with
    `--tls=false`;
  - `X-Forwarded-Host` replaces the host that same-origin checks (CSRF, WebSockets) compare
    the browser's `Origin` with.
- `--tls=false` without trusted proxies logs a warning.
- Examples for Caddy, nginx, Nginx Proxy Manager and Traefik, with WebSocket settings, are in
  `docs/reverse-proxy.md`.

## Consequences

- Logins, rate limiting and session lists show the real client behind the proxy.
- A too-wide `--trusted-proxies` lets clients in that range spoof their address; the docs say
  to trust only the proxy.
- Sub-path deployment remains unsupported.
