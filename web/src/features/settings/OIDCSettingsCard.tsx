// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Button,
  Code,
  CopyButton,
  Group,
  Paper,
  PasswordInput,
  SimpleGrid,
  Stack,
  Switch,
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap } from "../../api/client";
import type { components } from "../../api/schema";

type Settings = components["schemas"]["OIDCSettings"];

const oidcKey = ["settings", "oidc"] as const;

/**
 * Sign-in with an OpenID Connect provider (#76, ADR-0056): Keycloak, Authentik, Authelia,
 * Google… The client secret is write-only.
 */
export function OIDCSettingsCard() {
  const settings = useQuery({
    queryKey: oidcKey,
    queryFn: () => unwrap(api.GET("/api/v1/settings/oidc")),
  });
  return (
    <Paper withBorder p="md" component="section" aria-label="Single sign-on">
      <Stack gap="sm">
        <Title order={3}>Single sign-on (OIDC)</Title>
        {settings.isError && <Alert color="red">{settings.error.message}</Alert>}
        {settings.data && <OIDCForm key={JSON.stringify(settings.data)} saved={settings.data} />}
      </Stack>
    </Paper>
  );
}

function OIDCForm({ saved }: { saved: Settings }) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState({
    enabled: saved.enabled ?? false,
    issuer: saved.issuer ?? "",
    clientId: saved.clientId ?? "",
    scopes: (saved.scopes ?? []).join(" "),
    usernameClaim: saved.usernameClaim ?? "",
    groupsClaim: saved.groupsClaim ?? "",
    group: saved.group ?? "",
    buttonLabel: saved.buttonLabel ?? "",
    redirectUrl: saved.redirectUrl ?? "",
  });
  // undefined keeps the stored secret.
  const [secret, setSecret] = useState<string>();
  const set = (k: keyof typeof form) => (e: { currentTarget: { value: string } }) => {
    const value = e.currentTarget.value;
    setForm((f) => ({ ...f, [k]: value }));
  };

  const save = useMutation({
    mutationFn: () =>
      unwrap(
        api.PUT("/api/v1/settings/oidc", {
          body: {
            ...form,
            scopes: form.scopes.split(/\s+/).filter(Boolean),
            ...(secret !== undefined ? { clientSecret: secret } : {}),
          },
        }),
      ),
    onSuccess: (data) => {
      queryClient.setQueryData(oidcKey, data);
      void queryClient.invalidateQueries({ queryKey: ["auth", "oidc"] });
      notifications.show({ message: "Single sign-on settings saved" });
    },
  });

  const callback = form.redirectUrl || saved.callbackUrl || "";
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <Stack gap="sm">
        <Switch
          label="Sign in with an OpenID Connect provider"
          description="The sign-in page shows a button for it; passwords keep working. The provider checks the second factor of these sign-ins."
          checked={form.enabled}
          onChange={(e) => {
            const enabled = e.currentTarget.checked;
            setForm((f) => ({ ...f, enabled }));
          }}
        />
        <Text size="sm">
          Register this redirect URI at the provider:{" "}
          <Code style={{ wordBreak: "break-all" }}>{callback}</Code>{" "}
          <CopyButton value={callback}>
            {({ copied, copy }) => (
              <Button size="compact-xs" variant="subtle" onClick={copy}>
                {copied ? "Copied" : "Copy"}
              </Button>
            )}
          </CopyButton>
        </Text>
        <SimpleGrid cols={{ base: 1, sm: 2 }}>
          <TextInput
            label="Issuer URL"
            placeholder="https://keycloak.example.com/realms/main"
            required={form.enabled}
            value={form.issuer}
            onChange={set("issuer")}
          />
          <TextInput
            label="Client ID"
            required={form.enabled}
            value={form.clientId}
            onChange={set("clientId")}
          />
          <PasswordInput
            label="Client secret"
            description={
              saved.hasClientSecret ? "Stored; leave empty to keep it" : "Empty for a public client"
            }
            autoComplete="new-password"
            value={secret ?? ""}
            onChange={(e) => setSecret(e.currentTarget.value || undefined)}
          />
          <TextInput
            label="Group"
            description="Required to sign in; members without an account get one. Empty: only existing accounts can sign in."
            placeholder="nerdui-admins"
            value={form.group}
            onChange={set("group")}
          />
          <TextInput
            label="Username claim"
            description="Matched with nerdui usernames"
            placeholder="preferred_username"
            value={form.usernameClaim}
            onChange={set("usernameClaim")}
          />
          <TextInput
            label="Groups claim"
            placeholder="groups"
            value={form.groupsClaim}
            onChange={set("groupsClaim")}
          />
          <TextInput
            label="Scopes"
            description="Besides openid, separated by spaces"
            placeholder={form.group ? "profile email groups" : "profile email"}
            value={form.scopes}
            onChange={set("scopes")}
          />
          <TextInput
            label="Button label"
            placeholder="Sign in with SSO"
            value={form.buttonLabel}
            onChange={set("buttonLabel")}
          />
          <TextInput
            label="Redirect URI"
            description="Only when nerdui cannot derive it, e.g. behind a proxy without X-Forwarded-Host"
            placeholder={saved.callbackUrl}
            value={form.redirectUrl}
            onChange={set("redirectUrl")}
          />
        </SimpleGrid>
        {secret === "" && (
          <Text size="xs" c="red">
            The stored client secret is removed when you save.
          </Text>
        )}
        {save.isError && (
          <Alert color="red" title="Cannot save">
            {save.error.message}
          </Alert>
        )}
        <Text size="xs" c="dimmed">
          The first sign-in links an account at the provider to the nerdui account with the same
          username; make sure users cannot choose that claim at the provider. Every user is an
          administrator.
        </Text>
        <Group justify="flex-end">
          {saved.hasClientSecret && (
            <Button variant="subtle" color="red" onClick={() => setSecret("")}>
              Remove the stored secret
            </Button>
          )}
          <Button type="submit" loading={save.isPending}>
            Save
          </Button>
        </Group>
      </Stack>
    </form>
  );
}
