# 0056. Sign-in with an OpenID Connect provider

- **Status:** Accepted
- **Date:** 2026-11-25

## Context

Users sign in with a local password and optionally TOTP (ADR-0005, ADR-0037). Homelabs and
teams often run an identity provider (Keycloak, Authentik, Authelia, dex) or use Google, and
want one account everywhere (#76). nerdui has one role, `admin` (ADR-0009): whoever signs in
can do everything, so who may sign in through the provider matters most.

## Options considered

- **Only existing accounts**: an account at the provider is linked to the nerdui account with
  the same username. Safe, but every user has to be created by hand.
- **Linking plus a group**: as above, and when a group is configured, it is required, and its
  members without an account get one.
- **Anyone the provider signs in**: convenient, but with a public provider like Google it
  makes anyone an administrator.

## Decision

- Authorization code flow with PKCE (S256), a `state` kept in a SameSite=Lax cookie and
  checked against the server's list of sign-ins in progress (10 minutes), and a `nonce`
  checked in the ID token. The ID token is verified with the provider's keys
  (`github.com/coreos/go-oidc`); claims missing from it are read from the userinfo endpoint.
- **Linking plus a group**: a user linked to the provider's account (`users.oidc_identity`,
  "<issuer> <subject>") signs in. Otherwise the account whose username is the username claim
  (`preferred_username` by default) is linked. Otherwise, with a group configured and the user
  in it (`groups` claim), an account is created, without a usable password. With a group
  configured it is required in every case; without one, only existing accounts can sign in.
  Disabled accounts are refused, and an account linked to another identity is not relinked.
- Settings in the UI (Settings → Single sign-on): issuer, client ID, client secret (encrypted
  with the key of ADR-0049, write-only), scopes, claims, group, button label and an optional
  redirect URI; otherwise the redirect URI is derived from the request (with the trusted
  proxy's host, ADR-0035, and the base path, ADR-0046). Saving checks the discovery document.
- The callback answers with a page that navigates on to the UI: the session cookie is
  SameSite=Strict, and a redirect would carry on the provider's cross-site navigation.
- Sessions record how they signed in (`sessions.method`). The provider is responsible for the
  second factor: single sign-on sessions do not need local TOTP, also when it is required.
- Password sign-in stays available as a fallback. Administrators can unlink a user.
- Sign-ins through the provider, also failed ones, are in the audit log (`auth:login-sso`).

## Consequences

- Linking by username trusts the provider's username claim: users must not be able to choose
  it at the provider (in Keycloak, keep "Edit username" off). The docs say so.
- Accounts created by the provider sign in only through it, until an administrator sets a
  temporary password.
- Groups do not map to roles yet; with roles (#78) they can.
- One provider; logout does not end the provider's session.
