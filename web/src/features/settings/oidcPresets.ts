// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

/** Fields of the single sign-on form that a provider preset fills. */
export interface OIDCPresetFields {
  issuer: string;
  scopes: string;
  buttonLabel: string;
  usernameClaim?: string;
  groupsClaim?: string;
}

export interface OIDCPreset {
  id: string;
  label: string;
  /** Values for the form; `<…>` in the issuer marks a part the administrator replaces. */
  fields: OIDCPresetFields;
  /** Where to create the client and what to watch for. */
  hint: string;
  docs: string;
}

/**
 * Settings of popular OpenID Connect providers (#99). nerdui speaks OpenID Connect only, so
 * GitHub, which signs users in with plain OAuth 2.0, is not here: front it with dex.
 */
export const oidcPresets: OIDCPreset[] = [
  {
    id: "google",
    label: "Google",
    fields: {
      issuer: "https://accounts.google.com",
      scopes: "profile email",
      buttonLabel: "Sign in with Google",
      usernameClaim: "email",
    },
    hint: "Google Cloud console → APIs & Services → Credentials → Create credentials → OAuth client ID of type Web application. Google sends no usernames and no groups; nerdui usernames cannot contain @, so put dex or Authentik in front of Google to sign in with usernames.",
    docs: "https://developers.google.com/identity/openid-connect/openid-connect",
  },
  {
    id: "entra",
    label: "Microsoft Entra ID",
    fields: {
      issuer: "https://login.microsoftonline.com/<tenant-id>/v2.0",
      scopes: "profile email",
      buttonLabel: "Sign in with Microsoft",
    },
    hint: "Microsoft Entra admin center → App registrations → New registration, platform Web; add a client secret under Certificates & secrets. Replace <tenant-id> with the Directory (tenant) ID from the app's Overview. preferred_username is the user principal name (user@domain); the groups claim lists group object IDs once you add it under Token configuration.",
    docs: "https://learn.microsoft.com/en-us/entra/identity-platform/v2-protocols-oidc",
  },
  {
    id: "gitlab",
    label: "GitLab.com",
    fields: {
      issuer: "https://gitlab.com",
      scopes: "profile email",
      buttonLabel: "Sign in with GitLab",
    },
    hint: "User, group or admin settings → Applications → Add new application, confidential, with the scopes openid, profile and email. The groups claim (from userinfo) lists the full paths of the user's groups.",
    docs: "https://docs.gitlab.com/integration/openid_connect_provider/",
  },
  {
    id: "gitlab-self",
    label: "GitLab (self-managed)",
    fields: {
      issuer: "https://<gitlab-host>",
      scopes: "profile email",
      buttonLabel: "Sign in with GitLab",
    },
    hint: "Replace <gitlab-host> with your GitLab host. Admin area or user settings → Applications → Add new application, confidential, with the scopes openid, profile and email.",
    docs: "https://docs.gitlab.com/integration/openid_connect_provider/",
  },
  {
    id: "keycloak",
    label: "Keycloak",
    fields: {
      issuer: "https://<keycloak-host>/realms/<realm>",
      scopes: "profile email",
      buttonLabel: "Sign in with Keycloak",
    },
    hint: "Clients → Create client, OpenID Connect, Client authentication on, Standard flow on. For groups, add a Group Membership mapper with the claim name groups and Full group path off.",
    docs: "https://www.keycloak.org/docs/latest/server_admin/#_oidc_clients",
  },
  {
    id: "authentik",
    label: "Authentik",
    fields: {
      issuer: "https://<authentik-host>/application/o/<application-slug>/",
      scopes: "profile email",
      buttonLabel: "Sign in with Authentik",
    },
    hint: "Applications → Create with provider: an OAuth2/OpenID provider, confidential client. The groups claim comes with the profile scope.",
    docs: "https://docs.goauthentik.io/add-secure-apps/providers/oauth2/",
  },
  {
    id: "authelia",
    label: "Authelia",
    fields: {
      issuer: "https://<authelia-host>",
      scopes: "profile email groups",
      buttonLabel: "Sign in with Authelia",
    },
    hint: "Add a client under identity_providers.oidc.clients in Authelia's configuration with the scopes openid, profile, email and groups and the redirect URI above; Authelia stores a hash of the client secret.",
    docs: "https://www.authelia.com/configuration/identity-providers/openid-connect/clients/",
  },
  {
    id: "okta",
    label: "Okta",
    fields: {
      issuer: "https://<okta-domain>",
      scopes: "profile email",
      buttonLabel: "Sign in with Okta",
    },
    hint: "Admin console → Applications → Create App Integration → OIDC, Web Application. Replace <okta-domain> with your Okta domain (for a custom authorization server, add /oauth2/<server-id>). preferred_username is the Okta login, often an email address; for groups, add a groups claim to the ID token.",
    docs: "https://developer.okta.com/docs/guides/sign-into-web-app-redirect/go/main/",
  },
  {
    id: "auth0",
    label: "Auth0",
    fields: {
      issuer: "https://<tenant>.<region>.auth0.com/",
      scopes: "profile email",
      buttonLabel: "Sign in with Auth0",
      usernameClaim: "nickname",
    },
    hint: "Applications → Create Application → Regular Web Application; the issuer is your Auth0 domain with a trailing slash (or your custom domain). Auth0 sends no preferred_username and no groups claim without an Action.",
    docs: "https://auth0.com/docs/get-started/applications/application-settings",
  },
  {
    id: "zitadel",
    label: "Zitadel",
    fields: {
      issuer: "https://<instance-domain>",
      scopes: "profile email",
      buttonLabel: "Sign in with Zitadel",
    },
    hint: "Project → New application → Web, authentication method Basic Auth (client secret). Zitadel sends roles, not a groups list, so leave Group empty and create the users in nerdui.",
    docs: "https://zitadel.com/docs/guides/integrate/login/oidc/login-users",
  },
  {
    id: "dex",
    label: "Dex",
    fields: {
      issuer: "https://<dex-host>/dex",
      scopes: "profile email groups",
      buttonLabel: "Sign in with Dex",
    },
    hint: "Add a static client with the redirect URI above to Dex's configuration; the issuer is the issuer value there. Dex also brings sign-in with GitHub (its GitHub connector), which nerdui cannot do directly.",
    docs: "https://dexidp.io/docs/guides/using-dex/",
  },
];

/** The first `<…>` part of an issuer template, which the administrator replaces. */
export function issuerPlaceholder(issuer: string): { start: number; end: number } | undefined {
  const m = /<[^<>]+>/.exec(issuer);
  return m ? { start: m.index, end: m.index + m[0].length } : undefined;
}

/** Labels of the preset fields. */
export const presetFieldLabels: Record<keyof OIDCPresetFields, string> = {
  issuer: "Issuer URL",
  scopes: "Scopes",
  buttonLabel: "Button label",
  usernameClaim: "Username claim",
  groupsClaim: "Groups claim",
};

/**
 * Applies a preset to the form: empty fields get the preset's values; fields with other values
 * are kept and returned as conflicts, which the administrator may replace explicitly.
 */
export function applyPreset<F extends OIDCPresetFields>(
  form: F,
  preset: OIDCPreset,
): { form: F; conflicts: (keyof OIDCPresetFields)[] } {
  const next: F = { ...form };
  const fields: OIDCPresetFields = next;
  const conflicts: (keyof OIDCPresetFields)[] = [];
  for (const [k, value] of Object.entries(preset.fields) as [keyof OIDCPresetFields, string][]) {
    const current = (form[k] ?? "").trim();
    if (current === "") fields[k] = value;
    else if (current !== value) conflicts.push(k);
  }
  return { form: next, conflicts };
}
