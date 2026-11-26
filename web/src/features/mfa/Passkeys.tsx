// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Button,
  Group,
  PasswordInput,
  Stack,
  Table,
  Text,
  TextInput,
  Tooltip,
} from "@mantine/core";
import { IconTrash } from "@tabler/icons-react";
import { useMutation } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "../../api/client";
import type { components } from "../../api/schema";
import { timeAgo } from "../../lib/time";
import { createPasskey, passkeyErrorText, passkeysSupported } from "../../lib/webauthn";
import { BackupCodes } from "./MFASetup";

type Passkey = components["schemas"]["Passkey"];
type Credential = components["schemas"]["PasskeyCredential"];

/**
 * Adds a passkey (#226, ADR-0057): the password, then the browser's prompt. The first second
 * factor of a user also gets backup codes.
 */
export function PasskeyAdd({ onDone }: { onDone: () => void }) {
  const [name, setName] = useState(defaultName());
  const [password, setPassword] = useState("");
  const [codes, setCodes] = useState<string[]>();
  const add = useMutation({
    mutationFn: async () => {
      const { token, options } = await unwrap(
        api.POST("/api/v1/auth/passkeys/options", { body: { password } }),
      );
      const credential = await createPasskey(options);
      return unwrap(
        api.POST("/api/v1/auth/passkeys", {
          body: { token: token ?? "", name, credential: credential as Credential },
        }),
      );
    },
    onSuccess: (r) => {
      if (r.backupCodes?.length) setCodes(r.backupCodes);
      else onDone();
    },
  });

  if (codes) return <BackupCodes codes={codes} onDone={onDone} />;
  if (!passkeysSupported()) {
    return (
      <Alert color="yellow" title="Passkeys are not available here">
        Browsers offer passkeys only when nerdui is opened by its host name over HTTPS (or on
        localhost), not by an IP address. Use an authenticator app, or open nerdui through a host
        name with a trusted certificate.
      </Alert>
    );
  }
  return (
    <form
      onSubmit={(e: FormEvent) => {
        e.preventDefault();
        add.mutate();
      }}
    >
      <Stack>
        <Text size="sm">
          Your browser, phone or security key keeps the passkey; it signs you in with a fingerprint,
          face or PIN, also without a password. The passkey works only at {window.location.host}.
        </Text>
        {add.error && <Alert color="red">{passkeyErrorText(add.error)}</Alert>}
        <TextInput
          label="Name"
          description="To recognise it in the list"
          maxLength={64}
          value={name}
          onChange={(e) => setName(e.currentTarget.value)}
        />
        <PasswordInput
          label="Current password"
          autoComplete="current-password"
          required
          autoFocus
          value={password}
          onChange={(e) => setPassword(e.currentTarget.value)}
        />
        <Group justify="flex-end">
          <Button type="submit" loading={add.isPending}>
            Add passkey
          </Button>
        </Group>
      </Stack>
    </form>
  );
}

function defaultName() {
  const ua = typeof navigator === "undefined" ? "" : navigator.userAgent;
  for (const [re, name] of [
    [/iPhone|iPad/, "iPhone"],
    [/Android/, "Android"],
    [/Mac OS X/, "Mac"],
    [/Windows/, "Windows"],
    [/Linux/, "Linux"],
  ] as const) {
    if (re.test(ua)) return name;
  }
  return "Passkey";
}

/** The user's passkeys with a remove action. */
export function PasskeyList({
  passkeys,
  onRemove,
  removeDisabled,
}: {
  passkeys: Passkey[];
  onRemove: (p: Passkey) => void;
  removeDisabled?: string;
}) {
  if (passkeys.length === 0) {
    return (
      <Text size="sm" c="dimmed">
        No passkeys yet.
      </Text>
    );
  }
  return (
    <Table aria-label="Passkeys">
      <Table.Thead>
        <Table.Tr>
          <Table.Th>Name</Table.Th>
          <Table.Th>Host</Table.Th>
          <Table.Th>Added</Table.Th>
          <Table.Th>Last used</Table.Th>
          <Table.Th />
        </Table.Tr>
      </Table.Thead>
      <Table.Tbody>
        {passkeys.map((p) => (
          <Table.Tr key={p.id}>
            <Table.Td>{p.name}</Table.Td>
            <Table.Td>{p.host}</Table.Td>
            <Table.Td>{timeAgo(p.createdAt)}</Table.Td>
            <Table.Td>{p.lastUsedAt ? timeAgo(p.lastUsedAt) : "never"}</Table.Td>
            <Table.Td>
              <Tooltip label={removeDisabled ?? `Remove ${p.name}`}>
                <ActionIcon
                  variant="subtle"
                  color="red"
                  aria-label={`Remove ${p.name}`}
                  disabled={!!removeDisabled}
                  onClick={() => onRemove(p)}
                >
                  <IconTrash size={16} />
                </ActionIcon>
              </Tooltip>
            </Table.Td>
          </Table.Tr>
        ))}
      </Table.Tbody>
    </Table>
  );
}
