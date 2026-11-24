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
  Radio,
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
import { Time } from "../../components/Time";

export type APIKey = components["schemas"]["APIKey"];
type Permission = components["schemas"]["Permission"];
type Access = "read" | "chosen" | "full";

/** Permissions a key can be limited to, by area (#80). */
const permissionAreas: { area: string; permissions: Permission[] }[] = [
  {
    area: "Containers",
    permissions: ["containers:read", "containers:operate", "containers:write", "containers:exec"],
  },
  { area: "Stacks", permissions: ["stacks:read", "stacks:deploy", "stacks:write"] },
  { area: "Images", permissions: ["images:read", "images:write"] },
  { area: "Volumes", permissions: ["volumes:read", "volumes:write"] },
  { area: "Networks", permissions: ["networks:read", "networks:write"] },
  { area: "Namespaces", permissions: ["namespaces:read", "namespaces:write"] },
  { area: "System", permissions: ["system:read", "system:prune", "jobs:read"] },
  { area: "Registries", permissions: ["registries:read", "registries:write"] },
  { area: "Settings", permissions: ["settings:read", "settings:write", "audit:read"] },
  { area: "Users", permissions: ["users:read", "users:write"] },
];

/** The access of a key in lists: read-only, full, or its permissions. */
function accessBadge(k: APIKey) {
  if (k.readOnly) return { color: "blue", label: "read-only", title: undefined };
  if (k.permissions)
    return {
      color: "grape",
      label: `${k.permissions.length} permission${k.permissions.length === 1 ? "" : "s"}`,
      title: k.permissions.join(", "),
    };
  return { color: "orange", label: "full", title: undefined };
}

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
          const access = accessBadge(k);
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
                <Badge size="sm" variant="light" color={access.color} title={access.title}>
                  {access.label}
                </Badge>
              </Table.Td>
              <Table.Td>
                <Time value={k.createdAt} />
              </Table.Td>
              <Table.Td>
                {k.lastUsedAt ? (
                  <Time value={k.lastUsedAt} />
                ) : (
                  <Text size="sm" c="dimmed">
                    never
                  </Text>
                )}
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
  const [access, setAccess] = useState<Access>("read");
  const [permissions, setPermissions] = useState<Permission[]>([]);
  const [expiry, setExpiry] = useState("90");
  const create = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/auth/api-keys", {
          body: {
            name: name.trim(),
            readOnly: access === "read",
            permissions: access === "chosen" ? permissions : undefined,
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
    setAccess("read");
    setPermissions([]);
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
            <Radio.Group
              label="Access"
              value={access}
              onChange={(v) => setAccess(v as Access)}
            >
              <Stack gap={6} mt={4}>
                <Radio
                  value="read"
                  label="Read-only"
                  description="Only reads: lists, inspect, logs, stats. Recommended for monitoring."
                />
                <Radio
                  value="chosen"
                  label="Chosen permissions"
                  description="Only what you tick below, e.g. stacks:deploy for a CI pipeline."
                />
                <Radio value="full" label="Full access" description="Everything you can do." />
              </Stack>
            </Radio.Group>
            {access === "chosen" && (
              <Checkbox.Group
                aria-label="Permissions"
                value={permissions}
                onChange={(v) => setPermissions(v as Permission[])}
              >
                <Table withRowBorders={false} verticalSpacing={4}>
                  <Table.Tbody>
                    {permissionAreas.map(({ area, permissions: list }) => (
                      <Table.Tr key={area}>
                        <Table.Td w={110}>
                          <Text size="sm">{area}</Text>
                        </Table.Td>
                        <Table.Td>
                          <Group gap="md">
                            {list.map((p) => (
                              <Checkbox key={p} value={p} label={p} size="xs" />
                            ))}
                          </Group>
                        </Table.Td>
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
              </Checkbox.Group>
            )}
            <Select
              label="Expires"
              data={expiries}
              value={expiry}
              onChange={(v) => v && setExpiry(v)}
              allowDeselect={false}
            />
            {access === "full" && (
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
              <Button
                type="submit"
                loading={create.isPending}
                disabled={!name.trim() || (access === "chosen" && permissions.length === 0)}
              >
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
