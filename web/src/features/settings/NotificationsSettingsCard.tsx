// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Checkbox,
  Group,
  Loader,
  Modal,
  NumberInput,
  Paper,
  PasswordInput,
  Select,
  SimpleGrid,
  Stack,
  Switch,
  Table,
  Text,
  TextInput,
  Title,
  Tooltip,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { IconPencil, IconTrash } from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import type { components } from "../../api/schema";
import { formatTime, usePreferences } from "../preferences/context";

type Channel = components["schemas"]["NotificationChannel"];
type ChannelInput = components["schemas"]["NotificationChannelInput"];
type ChannelType = ChannelInput["type"];
type Kind = components["schemas"]["NotificationKind"];
type Config = components["schemas"]["NotificationConfig"];

const kindLabels: Record<Kind, string> = {
  "image.update": "A new image for a tag in use",
  "autoupdate.updated": "Automatic update done",
  "autoupdate.failed": "Automatic update failed",
  "autoupdate.pending": "Automatic updates wait for approval",
  "container.died": "Container exited with an error",
  "container.oom": "Container ran out of memory",
  "container.unhealthy": "Container became unhealthy",
  "container.autoheal": "Unhealthy container restarted, or left after repeated restarts",
  "job.failed": "Operation failed",
  "git.update": "New commit or update of a Git stack",
};

const typeLabels: Record<ChannelType, string> = {
  webhook: "Webhook",
  slack: "Slack",
  discord: "Discord",
  telegram: "Telegram",
  ntfy: "ntfy",
  gotify: "Gotify",
  email: "Email (SMTP)",
};

const channelsKey = ["notifications", "channels"] as const;

const emptyInput: ChannelInput = {
  name: "",
  type: "telegram",
  config: {},
  events: [
    "autoupdate.failed",
    "autoupdate.pending",
    "container.died",
    "container.oom",
    "container.unhealthy",
    "container.autoheal",
    "job.failed",
  ],
  enabled: true,
};

/** Notification channels and the events each receives (#86, ADR-0034). */
export function NotificationsSettingsCard() {
  const queryClient = useQueryClient();
  const { prefs } = usePreferences();
  const [editing, setEditing] = useState<Channel | "new" | null>(null);
  const channels = useQuery({
    queryKey: channelsKey,
    queryFn: () => unwrap(api.GET("/api/v1/notifications/channels")),
  });
  const remove = useMutation({
    mutationFn: (id: number) =>
      unwrapEmpty(api.DELETE("/api/v1/notifications/channels/{id}", { params: { path: { id } } })),
    onSuccess: () => {
      setRemoving(null);
      void queryClient.invalidateQueries({ queryKey: channelsKey });
    },
    onError: (e) =>
      notifications.show({ color: "red", title: "Cannot remove", message: e.message }),
  });
  const [removing, setRemoving] = useState<Channel | null>(null);

  return (
    <Paper withBorder p="md" component="section" aria-label="Notifications">
      <Stack gap="sm">
        <Group justify="space-between" align="flex-start">
          <div>
            <Title order={3}>Notifications</Title>
            <Text size="sm" c="dimmed">
              Send events of every environment to chat, push services, email or a webhook. The same
              event about the same object is sent at most once in 15 minutes.
            </Text>
          </div>
          <Button onClick={() => setEditing("new")}>Add channel</Button>
        </Group>
        {channels.isPending && <Loader aria-label="Loading channels" />}
        {channels.isError && (
          <Alert color="red" title="Cannot read channels">
            {channels.error.message}
          </Alert>
        )}
        {channels.data && channels.data.channels.length === 0 && (
          <Text size="sm" c="dimmed">
            No channels yet.
          </Text>
        )}
        {channels.data && channels.data.channels.length > 0 && (
          <Table.ScrollContainer minWidth={560}>
            <Table verticalSpacing={4} fz="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Name</Table.Th>
                  <Table.Th>Type</Table.Th>
                  <Table.Th>Events</Table.Th>
                  <Table.Th>Last delivery</Table.Th>
                  <Table.Th />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {channels.data.channels.map((ch) => (
                  <Table.Tr key={ch.id}>
                    <Table.Td>
                      {ch.name}{" "}
                      {!ch.enabled && (
                        <Badge size="xs" color="gray" variant="light">
                          off
                        </Badge>
                      )}
                    </Table.Td>
                    <Table.Td>{typeLabels[ch.type]}</Table.Td>
                    <Table.Td>{ch.events.length}</Table.Td>
                    <Table.Td>
                      {ch.lastError ? (
                        <Text size="xs" c="red">
                          {ch.lastError}
                        </Text>
                      ) : ch.lastSentAt ? (
                        formatTime(ch.lastSentAt, prefs.dateFormat).text
                      ) : (
                        "—"
                      )}
                    </Table.Td>
                    <Table.Td>
                      <Group gap={4} justify="flex-end" wrap="nowrap">
                        <Tooltip label="Edit">
                          <ActionIcon
                            variant="subtle"
                            aria-label={`Edit ${ch.name}`}
                            onClick={() => setEditing(ch)}
                          >
                            <IconPencil size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Remove">
                          <ActionIcon
                            variant="subtle"
                            color="red"
                            aria-label={`Remove ${ch.name}`}
                            onClick={() => setRemoving(ch)}
                          >
                            <IconTrash size={16} />
                          </ActionIcon>
                        </Tooltip>
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        )}
      </Stack>
      <Modal opened={!!removing} onClose={() => setRemoving(null)} title="Remove channel">
        {removing && (
          <Stack>
            <Text size="sm">
              Remove <b>{removing.name}</b>? Its events are no longer sent through it.
            </Text>
            <Group justify="flex-end">
              <Button variant="default" onClick={() => setRemoving(null)}>
                Cancel
              </Button>
              <Button
                color="red"
                loading={remove.isPending}
                onClick={() => remove.mutate(removing.id)}
              >
                Remove
              </Button>
            </Group>
          </Stack>
        )}
      </Modal>
      <Modal
        opened={editing !== null}
        onClose={() => setEditing(null)}
        title={editing === "new" ? "Add notification channel" : "Edit notification channel"}
        size="lg"
      >
        {editing !== null && (
          <ChannelForm
            key={editing === "new" ? "new" : editing.id}
            channel={editing === "new" ? undefined : editing}
            onDone={() => {
              setEditing(null);
              void queryClient.invalidateQueries({ queryKey: channelsKey });
            }}
          />
        )}
      </Modal>
    </Paper>
  );
}

function ChannelForm({ channel, onDone }: { channel?: Channel; onDone: () => void }) {
  const [input, setInput] = useState<ChannelInput>(
    channel
      ? {
          name: channel.name,
          type: channel.type,
          config: channel.config,
          events: channel.events,
          enabled: channel.enabled,
        }
      : emptyInput,
  );
  const set = (p: Partial<ChannelInput>) => setInput((i) => ({ ...i, ...p }));
  const cfg = (p: Partial<Config>) => setInput((i) => ({ ...i, config: { ...i.config, ...p } }));
  // Saved secrets apply while the type is unchanged.
  const saved = (f: string) =>
    !!channel && channel.type === input.type && channel.secrets.includes(f);
  const secretProps = (f: string, required: boolean) => ({
    placeholder: saved(f) ? "Saved; leave empty to keep" : undefined,
    required: required && !saved(f),
  });

  const save = useMutation({
    mutationFn: () =>
      channel
        ? unwrap(
            api.PUT("/api/v1/notifications/channels/{id}", {
              params: { path: { id: channel.id } },
              body: input,
            }),
          )
        : unwrap(api.POST("/api/v1/notifications/channels", { body: input })),
    onSuccess: () => {
      notifications.show({ message: `Channel ${input.name} saved` });
      onDone();
    },
    onError: (e) => notifications.show({ color: "red", title: "Cannot save", message: e.message }),
  });
  const test = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/notifications/test", {
          body: { ...input, id: channel && channel.type === input.type ? channel.id : undefined },
        }),
      ),
    onSuccess: () => notifications.show({ message: "Test message sent" }),
    onError: (e) =>
      notifications.show({ color: "red", title: "Test message not sent", message: e.message }),
  });

  const c = input.config;
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <Stack>
        <SimpleGrid cols={{ base: 1, sm: 2 }}>
          <TextInput
            label="Name"
            required
            maxLength={100}
            value={input.name}
            onChange={(e) => set({ name: e.currentTarget.value })}
          />
          <Select
            label="Type"
            data={Object.entries(typeLabels).map(([value, label]) => ({ value, label }))}
            value={input.type}
            onChange={(v) => v && set({ type: v as ChannelType })}
            allowDeselect={false}
          />
        </SimpleGrid>

        {input.type === "webhook" && (
          <>
            <TextInput
              label="URL"
              required
              placeholder="https://example.com/hooks/nerdui"
              description="Receives each event as JSON with a POST request"
              value={c.url ?? ""}
              onChange={(e) => cfg({ url: e.currentTarget.value })}
            />
            <PasswordInput
              label="Signing secret"
              description="Optional: adds X-Nerdui-Signature: sha256=HMAC of the body"
              value={c.secret ?? ""}
              onChange={(e) => cfg({ secret: e.currentTarget.value })}
              {...secretProps("secret", false)}
            />
          </>
        )}
        {(input.type === "slack" || input.type === "discord") && (
          <PasswordInput
            label="Webhook URL"
            description={
              input.type === "slack"
                ? "An incoming webhook of a Slack app"
                : "Channel settings → Integrations → Webhooks"
            }
            value={c.url ?? ""}
            onChange={(e) => cfg({ url: e.currentTarget.value })}
            {...secretProps("url", true)}
          />
        )}
        {input.type === "telegram" && (
          <SimpleGrid cols={{ base: 1, sm: 2 }}>
            <PasswordInput
              label="Bot token"
              description="From @BotFather"
              value={c.token ?? ""}
              onChange={(e) => cfg({ token: e.currentTarget.value })}
              {...secretProps("token", true)}
            />
            <TextInput
              label="Chat ID"
              required
              description="User, group (-100…) or @channel"
              value={c.chatId ?? ""}
              onChange={(e) => cfg({ chatId: e.currentTarget.value })}
            />
          </SimpleGrid>
        )}
        {(input.type === "ntfy" || input.type === "gotify") && (
          <SimpleGrid cols={{ base: 1, sm: 2 }}>
            <TextInput
              label={input.type === "ntfy" ? "Topic URL" : "Server URL"}
              required
              placeholder={
                input.type === "ntfy" ? "https://ntfy.sh/my-nerdui" : "https://gotify.example.com"
              }
              value={c.url ?? ""}
              onChange={(e) => cfg({ url: e.currentTarget.value })}
            />
            <PasswordInput
              label={input.type === "ntfy" ? "Access token" : "Application token"}
              description={input.type === "ntfy" ? "Optional, for protected topics" : undefined}
              value={c.token ?? ""}
              onChange={(e) => cfg({ token: e.currentTarget.value })}
              {...secretProps("token", input.type === "gotify")}
            />
          </SimpleGrid>
        )}
        {input.type === "email" && (
          <SimpleGrid cols={{ base: 1, sm: 2 }}>
            <TextInput
              label="SMTP host"
              required
              placeholder="smtp.example.com"
              value={c.host ?? ""}
              onChange={(e) => cfg({ host: e.currentTarget.value })}
            />
            <Group grow align="flex-start">
              <Select
                label="Encryption"
                data={[
                  { value: "starttls", label: "STARTTLS" },
                  { value: "tls", label: "TLS" },
                  { value: "none", label: "None" },
                ]}
                value={c.tls ?? "starttls"}
                onChange={(v) => v && cfg({ tls: v as Config["tls"] })}
                allowDeselect={false}
              />
              <NumberInput
                label="Port"
                placeholder={c.tls === "tls" ? "465" : "587"}
                min={1}
                max={65535}
                value={c.port ?? ""}
                onChange={(v) => cfg({ port: typeof v === "number" ? v : undefined })}
              />
            </Group>
            <TextInput
              label="Username"
              value={c.username ?? ""}
              onChange={(e) => cfg({ username: e.currentTarget.value })}
            />
            <PasswordInput
              label="Password"
              value={c.password ?? ""}
              onChange={(e) => cfg({ password: e.currentTarget.value })}
              {...secretProps("password", false)}
            />
            <TextInput
              label="From"
              required
              placeholder="nerdui <nerdui@example.com>"
              value={c.from ?? ""}
              onChange={(e) => cfg({ from: e.currentTarget.value })}
            />
            <TextInput
              label="To"
              required
              description="Comma-separated"
              value={c.to ?? ""}
              onChange={(e) => cfg({ to: e.currentTarget.value })}
            />
          </SimpleGrid>
        )}

        <Checkbox.Group
          label="Events"
          value={input.events}
          onChange={(v) => set({ events: v as Kind[] })}
        >
          <SimpleGrid cols={{ base: 1, sm: 2 }} mt="xs" spacing="xs">
            {Object.entries(kindLabels).map(([value, label]) => (
              <Checkbox key={value} value={value} label={label} />
            ))}
          </SimpleGrid>
        </Checkbox.Group>
        <Switch
          label="Enabled"
          checked={input.enabled}
          onChange={(e) => set({ enabled: e.currentTarget.checked })}
        />
        <Group justify="flex-end">
          <Button variant="default" onClick={() => test.mutate()} loading={test.isPending}>
            Send test
          </Button>
          <Button type="submit" loading={save.isPending}>
            Save
          </Button>
        </Group>
      </Stack>
    </form>
  );
}
