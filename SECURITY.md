# Security Policy

## Threat model

nerdui talks to the containerd socket. **Anyone who can use nerdui can effectively gain root on
the host** (for example, by running a privileged container that mounts `/`). Treat access to
nerdui exactly like root SSH access.

Recommendations:

- Bind to `127.0.0.1` and access through an SSH tunnel or an authenticating reverse proxy
  with TLS.
- Never expose nerdui directly to the internet.
- Restrict network access with a firewall.

## Supported versions

Only the latest release receives security fixes until 1.0.

## Reporting a vulnerability

Please **do not** open a public issue. Report privately via
[GitHub Security Advisories](https://github.com/pessom/nerdui/security/advisories/new).

Include affected versions, reproduction steps and impact. We aim to acknowledge reports within
3 business days.

## Verifying releases

Release checksums and container images are signed with [cosign](https://github.com/sigstore/cosign)
(keyless, GitHub OIDC). SBOMs (SPDX) are attached to every release.

```bash
# binaries
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/pessom/nerdui/\.github/workflows/release\.yml@' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
sha256sum --ignore-missing -c checksums.txt

# container image
cosign verify ghcr.io/pessom/nerdui:<version> \
  --certificate-identity-regexp '^https://github.com/pessom/nerdui/\.github/workflows/release\.yml@' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```
