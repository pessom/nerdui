# 0037. Two-factor authentication (TOTP)

- **Status:** Accepted
- **Date:** 2026-11-20

## Context

nerdui gives root-equivalent control of the host (SECURITY.md); a leaked or guessed password is
enough to take it over. Users ask for a second factor and for a way to require it (#75).
Dockhand offers TOTP; Arcane offers TOTP, passkeys and backup codes.

## Options considered

- **TOTP (RFC 6238)** works with every authenticator app and needs only HMAC-SHA1 from the
  standard library. **Passkeys (WebAuthn)** resist phishing but need a WebAuthn library,
  per-browser registration and an exact origin, which is awkward with self-signed certificates
  and IP addresses. TOTP first; passkeys get their own issue.
- **Recovery:** backup codes, an administrator reset, and a host command for the last
  administrator.
- **Second step:** a short-lived token after the password, or a session with a "half
  authenticated" flag. The token keeps sessions all-or-nothing: no session exists until both
  factors passed.

## Decision

- Each user can enable TOTP (SHA-1, 6 digits, 30 s, ±1 step for clock drift). Setup checks the
  password, shows the secret as a QR code and text, and is active only after a correct code.
  Enabling returns ten one-time backup codes (50 bits each, only their SHA-256 stored);
  replacing them or turning MFA off needs the password again. Migration 11.
- Login with MFA answers `mfaRequired` with a token (5 minutes, 5 attempts, in memory) instead
  of a session; `POST /auth/login/mfa` with a TOTP code or a backup code creates the session.
  A TOTP code is accepted once (the newest accepted step is stored), backup codes are deleted
  when used, and failures share the login rate limits.
- Administrators can require MFA for everyone (Settings → Sign-in security). Sessions of users
  without it then only reach their session and MFA endpoints (403 `mfa_setup_required`) and the
  UI leads them to the setup page. API keys are a separate credential and are not affected.
- Administrators reset another user's MFA on the Users page; `nerdui admin reset-mfa <user>` on
  the host recovers the last administrator and revokes their sessions.
- The TOTP secret is stored in nerdui's database like other credentials (mode 0600); encrypting
  secrets at rest is #35.

## Consequences

- A stolen password alone no longer signs in to accounts with MFA.
- Losing the phone and the backup codes needs another administrator or root on the host.
- In-memory login tokens are lost on restart: a login in progress starts again.
