// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Checkbox,
  Code,
  CopyButton,
  Group,
  Loader,
  Modal,
  Select,
  Stack,
  Table,
  Text,
  TextInput,
  Tooltip,
} from "@mantine/core";
import { IconCheck, IconCopy, IconTrash } from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, unwrap, type components } from "../../api/client";
import { timeAgo } from "../../lib/time";

export type APIKey = components["schemas"]["APIKey"];

const expiries = [
  { value: "30", label: "30 days" },
  { value: "90", label: "90 days" },
  { value: "365", label: "1 year" },
  { value: "never", label: "Never" },
];

/** Confirms revoking a key: requests with it fail at once. */
export function RevokeKeyModal({
  apiKey,
  loading,
  onClose,
  onConfirm,
}: {
  apiKey?: APIKey;
  loading: boolean;
  onClose: () => void;
  onConfirm: (k: APIKey) => void;
}) {
  return (
    <Modal opened={!!apiKey} onClose={onClose} title="Revoke API key">
      {apiKey && (
        <Stack gap="sm">
          <Text size="sm">
            Revoke <b>{apiKey.name}</b> (<Code>{apiKey.prefix}…</Code>)? Everything using it stops
            working at once.
          </Text>
          <Group justify="flex-end">
            <Button variant="default" onClick={onClose}>
              Cancel
            </Button>
            <Button color="red" loading={loading} onClick={() => onConfirm(apiKey)}>
              Revoke
            </Button>
          </Group>
        </Stack>
      )}
    </Modal>
  );
}

/** A table of API keys with revoke buttons (#87). */
export function APIKeyTable({ keys, onRevoke }: { keys: APIKey[]; onRevoke: (k: APIKey) => void }) {
  // Expiry is judged when the table appears; the list refreshes after changes.
  const [now] = useState(() => Date.now());
  if (keys.length === 0)
    return (
      <Text size="sm" c="dimmed">
        No API keys.
      </Text>
    );
  return (
    <Table striped verticalSpacing="xs">
      <Table.Thead>
        <Table.Tr>
          <Table.Th>Name</Table.Th>
          <Table.Th>Key</Table.Th>
          <Table.Th>Access</Table.Th>
          <Table.Th>Created</Table.Th>
          <Table.Th>Last used</Table.Th>
          <Table.Th>Expires</Table.Th>
          <Table.Th w={48} />
        </Table.Tr>
      </Table.Thead>
      <Table.Tbody>
        {keys.map((k) => {
          const expired = !!k.expiresAt && Date.parse(k.expiresAt) <= now;
          return (
            <Table.Tr key={k.id}>
              <Table.Td>
                <Text size="sm" fw={500}>
                  {k.name}
                </Text>
              </Table.Td>
              <Table.Td>
                <Code>{k.prefix}…</Code>
              </Table.Td>
              <Table.Td>
                <Badge size="sm" variant="light" color={k.readOnly ? "blue" : "orange"}>
                  {k.readOnly ? "read-only" : "full"}
                </Badge>
              </Table.Td>
              <Table.Td>
                <Text size="sm" title={k.createdAt}>
                  {timeAgo(k.createdAt)}
                </Text>
              </Table.Td>
              <Table.Td>
                <Text size="sm" c={k.lastUsedAt ? undefined : "dimmed"} title={k.lastUsedAt}>
                  {k.lastUsedAt ? timeAgo(k.lastUsedAt) : "never"}
                </Text>
              </Table.Td>
              <Table.Td>
                {k.expiresAt ? (
                  <Text size="sm" c={expired ? "red" : undefined} title={k.expiresAt}>
                    {expired ? "expired" : new Date(k.expiresAt).toLocaleDateString()}
                  </Text>
                ) : (
                  <Text size="sm" c="dimmed">
                    never
                  </Text>
                )}
              </Table.Td>
              <Table.Td>
                <Tooltip label="Revoke">
                  <ActionIcon
                    variant="subtle"
                    color="red"
                    aria-label={`Revoke ${k.name}`}
                    onClick={() => onRevoke(k)}
                  >
                    <IconTrash size={16} />
                  </ActionIcon>
                </Tooltip>
              </Table.Td>
            </Table.Tr>
          );
        })}
      </Table.Tbody>
    </Table>
  );
}

/** Creates an API key for the signed-in user and shows it once (#87). */
export function CreateAPIKeyModal({ opened, onClose }: { opened: boolean; onClose: () => void }) {
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [readOnly, setReadOnly] = useState(true);
  const [expiry, setExpiry] = useState("90");
  const create = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/auth/api-keys", {
          body: {
            name: name.trim(),
            readOnly,
            expiresAt:
              expiry === "never"
                ? undefined
                : new Date(Date.now() + Number(expiry) * 86_400_000).toISOString(),
          },
        }),
      ),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["api-keys"] }),
  });
  const close = () => {
    setName("");
    setReadOnly(true);
    setExpiry("90");
    create.reset();
    onClose();
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate();
  };
  const created = create.data;
  return (
    <Modal opened={opened} onClose={close} title="Create API key">
      {created ? (
        <Stack gap="sm">
          <Text size="sm">
            The key <b>{created.name}</b> is shown only once: store it now, e.g. in a password
            manager or your integration&apos;s secrets.
          </Text>
          <Group gap="xs" wrap="nowrap">
            <Code fz="sm" style={{ flex: 1, wordBreak: "break-all" }} aria-label="New API key">
              {created.key}
            </Code>
            <CopyButton value={created.key}>
              {({ copied, copy }) => (
                <Tooltip label={copied ? "Copied" : "Copy"}>
                  <ActionIcon
                    variant="subtle"
                    color={copied ? "teal" : "gray"}
                    onClick={copy}
                    aria-label="Copy key"
                  >
                    {copied ? <IconCheck size={16} /> : <IconCopy size={16} />}
                  </ActionIcon>
                </Tooltip>
              )}
            </CopyButton>
          </Group>
          <Text size="xs" c="dimmed">
            Use it as <Code>Authorization: Bearer {created.prefix}…</Code>
          </Text>
          <Group justify="flex-end">
            <Button onClick={close}>Done</Button>
          </Group>
        </Stack>
      ) : (
        <form onSubmit={submit}>
          <Stack gap="sm">
            <TextInput
              label="Name"
              description="What uses the key, e.g. home-assistant"
              required
              maxLength={64}
              data-autofocus
              value={name}
              onChange={(e) => setName(e.currentTarget.value)}
            />
            <Checkbox
              label="Read-only"
              description="Only reads: lists, inspect, logs, stats. Recommended for monitoring."
              checked={readOnly}
              onChange={(e) => setReadOnly(e.currentTarget.checked)}
            />
            <Select
              label="Expires"
              data={expiries}
              value={expiry}
              onChange={(v) => v && setExpiry(v)}
              allowDeselect={false}
            />
            {!readOnly && (
              <Alert color="orange">
                A full key can do everything you can, including removing containers and volumes.
              </Alert>
            )}
            {create.isError && (
              <Alert color="red" title="Cannot create the key">
                {create.error.message}
              </Alert>
            )}
            <Group justify="flex-end">
              <Button variant="default" onClick={close}>
                Cancel
              </Button>
              <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
                Create
              </Button>
            </Group>
          </Stack>
        </form>
      )}
    </Modal>
  );
}

export function KeysLoading({ pending, error }: { pending: boolean; error?: Error | null }) {
  if (pending) return <Loader aria-label="Loading keys" />;
  if (error)
    return (
      <Alert color="red" title="Cannot read API keys">
        {error.message}
      </Alert>
    );
  return null;
}
