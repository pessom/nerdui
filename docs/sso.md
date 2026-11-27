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

### Keycloak

- Issuer: `https://keycloak.example.com/realms/<realm>`.
- Client: *Client authentication* on, *Standard flow* on, valid redirect URI as above.
- Groups: add a *Group Membership* mapper to the client's dedicated scope with token claim name
  `groups` and *Full group path* off, so the claim lists `nerdui-admins`, not
  `/nerdui-admins`.

### Authentik

- Create an OAuth2/OpenID provider and an application; issuer:
  `https://authentik.example.com/application/o/<application-slug>/`.
- The `groups` claim is in the default `profile` scope.

### Google

- Issuer: `https://accounts.google.com`; create an OAuth client ID of type *Web application*.
- Google sends no `preferred_username` and no groups: set the username claim to `email` and
  create nerdui users named after the email addresses (letters, digits, `.`, `_`, `-` only, so
  an address with `@` cannot be a nerdui username — use a provider that has usernames instead,
  or front Google with dex or Authentik).

## Troubleshooting

- The sign-in page shows the reason of a failed sign-in; details are in the server log
  (`single sign-on failed`) and the audit log (`auth:login-sso`).
- *the sign-in attempt expired or did not start here*: the state cookie was missing — the
  sign-in took more than 10 minutes, nerdui was restarted meanwhile, or the redirect URI points
  to another host than the one you opened nerdui on.
