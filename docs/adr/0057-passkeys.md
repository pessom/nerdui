# 0057. Passkeys (WebAuthn)

- **Status:** Accepted
- **Date:** 2026-11-26

## Context

TOTP (ADR-0037) protects against a leaked password but not against phishing: a code typed into
a fake page works on the real one. Passkeys (WebAuthn) are bound to the site and resist that;
they also sign in without a password (#226). Browsers allow WebAuthn only in a secure context
(HTTPS or `localhost`) and on a host name, never on an IP address. nerdui is often opened by an
IP address with a self-signed certificate.

## Options considered

- **Library:** go-webauthn (BSD-3-Clause, the standard Go implementation, verifies attestation
  and assertions) or an own implementation of the subset (ES256, "none" attestation). Signature
  checks are security-critical: the library.
- **Relying party ID:** a setting with the public host name, or the host the browser opened
  nerdui by (the `Host` header, or `X-Forwarded-Host` from a trusted proxy, #112). A setting
  breaks when it is wrong and has to be documented; the request host is what the browser checks
  anyway. Each passkey records its host, so a passkey added at another name is ignored there.
- **Role:** a second factor only, or also sign-in without a password. A discoverable passkey
  with user verification (PIN, biometrics) is itself two factors.
- **Backup codes** for users whose only second factor is a passkey: without them, losing the
  device needs an administrator.

## Decision

- Users add passkeys on the Account security page after entering their password (at most 20,
  named, `residentKey: preferred`, `userVerification: preferred`, no attestation). Each user
  gets a random 32-byte WebAuthn user handle; credentials are stored as the library's JSON with
  their ID and host. Migration 21.
- A passkey is a second factor: login with a password then offers "Use a passkey" next to the
  code (`POST /auth/login/mfa/passkey` for the options, the answer to `/auth/login/mfa`). It
  counts for "require two-factor authentication", and the required setup page offers it.
- "Sign in with a passkey" signs in without a username or password (discoverable credentials,
  `userVerification: required`); the session's method is `passkey`.
- The first second factor of a user (an app or a passkey) brings ten backup codes; they are
  removed with the last one. Turning off the authenticator app keeps passkeys and codes.
- Passkeys are refused with a clear error on an IP address or plain HTTP (except `localhost`).
  Pending requests live in memory for 5 minutes and are answered once. A signature counter that
  goes back (a cloned authenticator) refuses the sign-in and is logged.
- The administrator's reset and `nerdui admin reset-mfa` remove passkeys too.

## Consequences

- Phishing-resistant sign-in, also without a password.
- Installations reached only by an IP address cannot use passkeys; they need a host name with a
  trusted certificate (reverse proxy, #112, or ACME, #113). The UI says so.
- Changing the public host name makes existing passkeys unusable; users add them again.
- Several dependencies come with the library (CBOR, TPM attestation, JWT for metadata), all
  under permissive licenses.
