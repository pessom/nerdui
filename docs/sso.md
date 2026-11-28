# Single sign-on (OpenID Connect)

nerdui can sign users in through an OpenID Connect provider: Keycloak, Authentik, Authelia,
dex, Google and others (#76, ADR-0056). Passwords keep working as a fallback.

## How accounts are matched

Decide who may sign in, and with which role (ADR-0058):

- **Without a group** (default): only existing nerdui accounts can sign in. The first time, the
  account at the provider is linked to the nerdui account whose username equals the username
  claim (`preferred_username` by default). Create the users in nerdui first.
- **With a group** (e.g. `nerdui-admins`): only members of the group can sign in, and members
  without a nerdui account get one at their first sign-in, with the role chosen under *Role of
  new accounts* (`viewer` by default; `admin` for settings saved before roles). The group comes
  from the groups claim (`groups` by default). Change a user's role later on the Users page.

Later sign-ins use the link (the provider's issuer and subject), not the username. An account
linked to another account at the provider is not relinked; administrators can unlink a user on
the Users page. Disabled accounts cannot sign in.

> **Important:** linking trusts the username claim. Make sure users cannot choose or change it
> at the provider, or they could take over a nerdui account with the same name. In Keycloak,
> keep *Realm settings → Login → Edit username* off.

The provider is responsible for the second factor: sessions from single sign-on do not need
nerdui's two-factor authentication, also when it is required for every user.

## Setting it up

1. In nerdui, open **Settings → Single sign-on (OIDC)** and copy the **redirect URI**, e.g.
   `https://nerdui.example.com/api/v1/auth/oidc/callback` (with `--base-path`, under it).
   Behind a reverse proxy, set `--trusted-proxies` so the URI has the public host and scheme,
   or enter it yourself.
2. At the provider, create a confidential client (web application) with that redirect URI,
   the authorization code flow and PKCE allowed.
3. In nerdui, enter the issuer URL, the client ID and the client secret, optionally the group,
   turn sign-in on and save. Saving checks that the issuer's discovery document
   (`<issuer>/.well-known/openid-configuration`) can be read from the nerdui host.

The sign-in page then shows a button for the provider.

## Provider presets

The **Provider** list at the top of the card has settings for popular providers (#99). Choosing
one fills the *empty* issuer URL, scopes, button label and, where the provider needs it, the
username claim; fields you already filled are kept, and the card offers to replace them with
the provider's values. The client ID and the client secret are never changed. A part of the
issuer in angle brackets, e.g. `<tenant-id>`, is selected and marked until you replace it.
*Custom* changes nothing. Each preset shows where to create the client and links to the
provider's documentation; register the redirect URI from the card in every case.

Every preset uses the scopes `openid` (always sent) plus the ones listed below.

### Google

- Issuer: `https://accounts.google.com`; scopes `profile email`.
- Client: Google Cloud console → *APIs & Services → Credentials → Create credentials → OAuth
  client ID*, type *Web application*, with the redirect URI as an authorized redirect URI.
- Google sends no `preferred_username` and no groups; the preset sets the username claim to
  `email`. nerdui usernames allow letters, digits, `.`, `_` and `-` only, so an address with
  `@` cannot match a nerdui account: front Google with dex or Authentik to sign in with
  usernames.

### Microsoft Entra ID

- Issuer: `https://login.microsoftonline.com/<tenant-id>/v2.0`, with the *Directory (tenant)
  ID* from the app registration's *Overview*; scopes `profile email`.
- Client: Entra admin center → *App registrations → New registration*, platform *Web* with the
  redirect URI; add a client secret under *Certificates & secrets*.
- `preferred_username` is the user principal name (`user@domain`), which cannot be a nerdui
  username; the `groups` claim (added under *Token configuration*) lists group object IDs, so
  enter the group's object ID as the group.

### GitLab (gitlab.com or self-managed)

- Issuer: `https://gitlab.com`, or your instance's URL, e.g. `https://gitlab.example.com`;
  scopes `profile email`.
- Client: *User settings*, a group's *Settings* or the *Admin area* → *Applications → Add new
  application*, confidential, with the scopes `openid`, `profile` and `email`.
- `preferred_username` is the GitLab username. `groups` (full group paths, e.g.
  `team/nerdui-admins`) comes from the userinfo endpoint, which nerdui asks when the ID token
  lacks it.

### Keycloak

- Issuer: `https://keycloak.example.com/realms/<realm>`; scopes `profile email`.
- Client: *Client authentication* on, *Standard flow* on, valid redirect URI as above.
- Groups: add a *Group Membership* mapper to the client's dedicated scope with token claim name
  `groups` and *Full group path* off, so the claim lists `nerdui-admins`, not
  `/nerdui-admins`.

### Authentik

- Create an OAuth2/OpenID provider and an application; issuer:
  `https://authentik.example.com/application/o/<application-slug>/` (with the trailing slash);
  scopes `profile email`.
- The `groups` claim is in the default `profile` scope.

### Authelia

- Issuer: Authelia's URL, e.g. `https://auth.example.com`; scopes `profile email groups`.
- Client: add it under `identity_providers.oidc.clients` in Authelia's configuration with the
  redirect URI and the scopes `openid`, `profile`, `email`, `groups`; the client secret is
  stored hashed there.

### Okta

- Issuer: the org authorization server `https://<okta-domain>` (e.g.
  `https://example.okta.com`), or a custom one, `https://<okta-domain>/oauth2/default`;
  scopes `profile email`.
- Client: Admin console → *Applications → Create App Integration*, *OIDC*, *Web Application*.
- `preferred_username` is the Okta login, often an email address. For groups, add a `groups`
  claim to the ID token (a groups claim filter of the app or of the authorization server).

### Auth0

- Issuer: your Auth0 domain with a trailing slash, e.g. `https://example.eu.auth0.com/`, or
  your custom domain; scopes `profile email`.
- Client: *Applications → Create Application → Regular Web Application*; add the redirect URI
  to *Allowed Callback URLs*.
- Auth0 sends no `preferred_username`: the preset sets the username claim to `nickname`.
  Groups need an Action that adds a claim; enter its name as the groups claim.

### Zitadel

- Issuer: your instance's domain, e.g. `https://example-abc123.zitadel.cloud` or your custom
  domain; scopes `profile email`.
- Client: in a project, *New → Web* application, authentication method *Basic Auth* (client secret)
  or *PKCE*, with the redirect URI.
- Zitadel sends project roles as an object, not a groups list: leave the group empty and create
  the users in nerdui.

### Dex

- Issuer: the `issuer` value of Dex's configuration, e.g. `https://dex.example.com/dex`;
  scopes `profile email groups`.
- Client: a `staticClients` entry with the redirect URI in `redirectURIs` and a secret.

### GitHub (not supported directly)

GitHub signs users in with plain OAuth 2.0, not OpenID Connect (no ID token and no discovery
document), so nerdui cannot use it directly and there is no preset. Put an OpenID Connect
bridge in front of it: dex with its GitHub connector (groups from GitHub organizations and
teams), Authentik, or Keycloak with GitHub as an identity provider, and choose that one.

## Troubleshooting

- The sign-in page shows the reason of a failed sign-in; details are in the server log
  (`single sign-on failed`) and the audit log (`auth:login-sso`).
- *the sign-in attempt expired or did not start here*: the state cookie was missing — the
  sign-in took more than 10 minutes, nerdui was restarted meanwhile, or the redirect URI points
  to another host than the one you opened nerdui on.
