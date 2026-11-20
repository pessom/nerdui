# 0038. Certificates from Let's Encrypt (ACME)

- **Status:** Accepted
- **Date:** 2026-11-20

## Context

nerdui serves HTTPS with a self-signed certificate by default (ADR-0005), so browsers warn on
every new device. Users with a domain name want a trusted certificate without running certbot or
a reverse proxy (#113).

## Options considered

- **Library:** `golang.org/x/crypto/acme/autocert` (already a dependency through `x/crypto`)
  handles accounts, orders, TLS-ALPN-01 and HTTP-01 challenges, caching and renewal; lego or
  certmagic add DNS-01 providers but many dependencies.
- **Challenges:** TLS-ALPN-01 needs port 443 reachable; HTTP-01 needs port 80; DNS-01 needs a DNS
  provider API. The first two cover public servers; DNS-01 (homes without inbound ports) stays
  with a reverse proxy or own certificates.
- **Failure:** refuse to start, or keep serving the self-signed certificate and warn. Starting
  must not depend on the CA being reachable.

## Decision

- `--tls-acme-domains` (fully qualified names; no IP addresses or wildcards), optional
  `--tls-acme-email`, `--tls-acme-ca` (`production`, `staging` or a directory URL) and
  `--tls-acme-http` (address for HTTP-01 and HTTP→HTTPS redirects to the configured domains
  only). They cannot be combined with `--tls-cert` or `--tls=false`.
- TLS connections for the configured domains get the ACME certificate; challenge handshakes go
  to autocert; every other name (IP addresses, `localhost`, the health check) and every
  connection while no certificate is available get the self-signed certificate.
- Certificates are requested at start and retried every 10 minutes until obtained; autocert
  renews them 30 days before expiry. Account key and certificates are cached in
  `<data-dir>/tls/acme` (mode 0700).
- The status (domains, ready, expiry, newest error) is part of `GET /version` for callers with
  `system:read`; the UI warns while no certificate could be obtained.

## Consequences

- With a public domain, browsers trust nerdui without extra software.
- A misconfigured domain or closed port leaves the self-signed certificate in place and a
  warning, not a failed start; repeated failures count against Let's Encrypt rate limits, so the
  docs recommend `staging` first.
- Only the local environment's certificate is handled; DNS-01 is not supported.
