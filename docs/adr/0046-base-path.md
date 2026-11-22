# 0046. Serving nerdui under a path prefix

- **Status:** Accepted
- **Date:** 2026-11-22

## Context

Behind a reverse proxy nerdui had to own a host name (ADR-0035). Users who share one name
between services want `https://example.com/nerdui/` (#222). The UI was built with absolute
asset paths and used `/api/v1/…` and the page origin for API calls, WebSockets, downloads and
the webhook URLs it shows.

## Options considered

- **Build-time prefix** (Vite `base`, router `basepath`): one build per prefix; release
  binaries could not be used.
- **Runtime prefix**: a relative build, and the server tells the page its prefix.
- How the page learns the prefix: an API call before anything renders, a global script (blocked
  by the Content-Security-Policy without `unsafe-inline`), or `<base href>` written into
  `index.html` by the server.
- Proxies that strip the prefix (Caddy `handle_path`, Traefik `StripPrefix`) and proxies that
  pass it (nginx `location /nerdui/`).

## Decision

- `--base-path` (`NERDUI_BASE_PATH`), validated (segments of letters, digits, `.`, `_`, `~`,
  `-`; not `/api`) and normalized without a trailing slash.
- The server serves `<base>/…` without the prefix and redirects `<base>` to `<base>/`. Requests
  without the prefix are served as well, so both kinds of proxies work and local health checks
  (`/healthz`, `nerdui healthcheck`) keep their path.
- `index.html` carries `<base href="/">`, which the server replaces with `<base>/`. The UI is
  built with relative asset URLs (Vite `base: "./"`), reads the prefix from
  `document.baseURI`, and uses it for the router, the API client, WebSockets, downloads and the
  webhook URLs it shows. Server redirects (`/api/docs`) include it.
- The session cookie's `Path` is the prefix.

## Consequences

- One release build serves any prefix.
- New code must build API URLs with `withBase`/`absoluteURL` (web/src/lib/base.ts) or the
  generated API client, never from `/api/v1` or `location.origin` alone.
