// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Code,
  CopyButton,
  Group,
  Loader,
  Modal,
  Paper,
  SegmentedControl,
  Select,
  Stack,
  Switch,
  Table,
  Text,
  TextInput,
  Title,
  Tooltip,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { IconCheck, IconCopy, IconKey, IconPencil, IconTrash } from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import { containersQuery, namespacesQuery, stacksQuery } from "../../api/queries";
import type { components } from "../../api/schema";
import { formatTime, usePreferences } from "../preferences/context";

type Webhook = components["schemas"]["Webhook"];
type Input = components["schemas"]["WebhookInput"];

const webhooksKey = ["webhooks"] as const;

const actionLabels: Record<Input["action"], string> = {
  redeploy: "Pull images and redeploy",
  restart: "Restart",
};

/** Incoming webhooks that redeploy or restart a stack or container (#85, #91, ADR-0041). */
export function WebhooksSettingsCard({ env, namespace }: { env: string; namespace: string }) {
  const queryClient = useQueryClient();
  const { prefs } = usePreferences();
  const [editing, setEditing] = useState<Webhook | "new" | null>(null);
  const [removing, setRemoving] = useState<Webhook | null>(null);
  const [shown, setShown] = useState<{ name: string; url: string } | null>(null);
  const list = useQuery({
    queryKey: webhooksKey,
    queryFn: () => unwrap(api.GET("/api/v1/webhooks")),
  });
  const refresh = () => void queryClient.invalidateQueries({ queryKey: webhooksKey });
  const remove = useMutation({
    mutationFn: (id: number) =>
      unwrapEmpty(api.DELETE("/api/v1/webhooks/{id}", { params: { path: { id } } })),
    onSuccess: () => {
      setRemoving(null);
      refresh();
    },
    onError: (e) =>
      notifications.show({ color: "red", title: "Cannot delete", message: e.message }),
  });
  const newToken = useMutation({
    mutationFn: (w: Webhook) =>
      unwrap(api.POST("/api/v1/webhooks/{id}/token", { params: { path: { id: w.id } } })),
    onSuccess: ({ path }, w) => setShown({ name: w.name, url: window.location.origin + path }),
    onError: (e) =>
      notifications.show({ color: "red", title: "Cannot replace the URL", message: e.message }),
  });

  return (
    <Paper withBorder p="md" component="section" aria-label="Incoming webhooks">
      <Stack gap="sm">
        <Group justify="space-between" align="flex-start">
          <div>
            <Title order={3}>Incoming webhooks</Title>
            <Text size="sm" c="dimmed">
              Secret URLs that redeploy or restart a stack or container, e.g. from CI after an image
              was published: <Code>curl -X POST &lt;url&gt;</Code>.
            </Text>
          </div>
          <Button onClick={() => setEditing("new")}>Add webhook</Button>
        </Group>
        {list.isPending && <Loader aria-label="Loading webhooks" />}
        {list.isError && (
          <Alert color="red" title="Cannot read webhooks">
            {list.error.message}
          </Alert>
        )}
        {list.data && list.data.webhooks.length === 0 && (
          <Text size="sm" c="dimmed">
            No webhooks yet.
          </Text>
        )}
        {list.data && list.data.webhooks.length > 0 && (
          <Table.ScrollContainer minWidth={640}>
            <Table verticalSpacing={4} fz="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Name</Table.Th>
                  <Table.Th>Target</Table.Th>
                  <Table.Th>Action</Table.Th>
                  <Table.Th>Last call</Table.Th>
                  <Table.Th />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {list.data.webhooks.map((w) => (
                  <Table.Tr key={w.id}>
                    <Table.Td>
                      {w.name}{" "}
                      {!w.enabled && (
                        <Badge size="xs" color="gray" variant="light">
                          off
                        </Badge>
                      )}
                    </Table.Td>
                    <Table.Td>
                      {w.targetKind} <b>{w.target}</b>
                      <Text span size="xs" c="dimmed">
                        {" "}
                        {w.environment}/{w.namespace}
                      </Text>
                    </Table.Td>
                    <Table.Td>{actionLabels[w.action]}</Table.Td>
                    <Table.Td>
                      {w.lastError ? (
                        <Text size="xs" c="red">
                          {w.lastError}
                        </Text>
                      ) : w.lastCalledAt ? (
                        formatTime(w.lastCalledAt, prefs.dateFormat).text
                      ) : (
                        "—"
                      )}
                    </Table.Td>
                    <Table.Td>
                      <Group gap={4} justify="flex-end" wrap="nowrap">
                        <Tooltip label="New URL">
                          <ActionIcon
                            variant="subtle"
                            aria-label={`New URL for ${w.name}`}
                            loading={newToken.isPending && newToken.variables?.id === w.id}
                            onClick={() => newToken.mutate(w)}
                          >
                            <IconKey size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Edit">
                          <ActionIcon
                            variant="subtle"
                            aria-label={`Edit ${w.name}`}
                            onClick={() => setEditing(w)}
                          >
                            <IconPencil size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Delete">
                          <ActionIcon
                            variant="subtle"
                            color="red"
                            aria-label={`Delete ${w.name}`}
                            onClick={() => setRemoving(w)}
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

      {editing && (
        <WebhookDialog
          env={env}
          namespace={namespace}
          webhook={editing === "new" ? null : editing}
          onClose={() => setEditing(null)}
          onCreated={(name, url) => setShown({ name, url })}
        />
      )}
      <Modal opened={!!shown} onClose={() => setShown(null)} title="Webhook URL" size="lg">
        {shown && (
          <Stack gap="sm">
            <Text size="sm">
              The URL of <b>{shown.name}</b> is shown only now. Anyone who has it can run the
              webhook: keep it in your CI&apos;s secrets.
            </Text>
            <Group gap="xs" wrap="nowrap">
              <Code fz="xs" style={{ flex: 1, wordBreak: "break-all" }} aria-label="URL">
                {shown.url}
              </Code>
              <CopyButton value={shown.url}>
                {({ copied, copy }) => (
                  <Tooltip label={copied ? "Copied" : "Copy"}>
                    <ActionIcon
                      variant="subtle"
                      color={copied ? "teal" : "gray"}
                      onClick={copy}
                      aria-label="Copy the URL"
                    >
                      {copied ? <IconCheck size={16} /> : <IconCopy size={16} />}
                    </ActionIcon>
                  </Tooltip>
                )}
              </CopyButton>
            </Group>
            <Text size="xs" c="dimmed">
              Call it with <Code>curl -fsS -X POST &lt;url&gt;</Code>; the answer comes when the job
              has started. Its output is in the Jobs panel.
            </Text>
            <Group justify="flex-end">
              <Button onClick={() => setShown(null)}>Done</Button>
            </Group>
          </Stack>
        )}
      </Modal>
      <Modal opened={!!removing} onClose={() => setRemoving(null)} title="Delete webhook">
        {removing && (
          <Stack>
            <Text size="sm">
              Delete <b>{removing.name}</b>? Its URL stops working.
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
                Delete
              </Button>
            </Group>
          </Stack>
        )}
      </Modal>
    </Paper>
  );
}

function WebhookDialog({
  env,
  namespace,
  webhook,
  onClose,
  onCreated,
}: {
  env: string;
  namespace: string;
  webhook: Webhook | null;
  onClose: () => void;
  onCreated: (name: string, url: string) => void;
}) {
  const queryClient = useQueryClient();
  const [input, setInput] = useState<Input>(
    webhook
      ? {
          name: webhook.name,
          environment: webhook.environment,
          namespace: webhook.namespace,
          targetKind: webhook.targetKind,
          target: webhook.target,
          action: webhook.action,
          enabled: webhook.enabled,
        }
      : {
          name: "",
          environment: env,
          namespace,
          targetKind: "stack",
          target: "",
          action: "redeploy",
          enabled: true,
        },
  );
  const set = (patch: Partial<Input>) => setInput((i) => ({ ...i, ...patch }));
  const namespaces = useQuery(namespacesQuery(input.environment));
  const stacks = useQuery({
    ...stacksQuery(input.environment, input.namespace),
    enabled: input.targetKind === "stack",
  });
  const containers = useQuery({
    ...containersQuery(input.environment, input.namespace),
    enabled: input.targetKind === "container",
  });
  const targets =
    input.targetKind === "stack"
      ? (stacks.data?.stacks ?? []).filter((s) => s.mode !== "unmanaged").map((s) => s.name)
      : (containers.data?.containers ?? []).map((c) => c.name).filter((n) => n);
  const options = Array.from(new Set([...targets, ...(input.target ? [input.target] : [])]));

  const save = useMutation({
    mutationFn: async () => {
      const body = { ...input, name: input.name.trim() };
      if (webhook) {
        await unwrap(
          api.PUT("/api/v1/webhooks/{id}", { params: { path: { id: webhook.id } }, body }),
        );
        return null;
      }
      return unwrap(api.POST("/api/v1/webhooks", { body }));
    },
    onSuccess: (created) => {
      void queryClient.invalidateQueries({ queryKey: webhooksKey });
      onClose();
      if (created) onCreated(created.webhook.name, window.location.origin + created.path);
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };

  return (
    <Modal
      opened
      onClose={onClose}
      title={webhook ? `Edit ${webhook.name}` : "Add webhook"}
      size="lg"
    >
      <form onSubmit={submit}>
        <Stack gap="sm">
          <TextInput
            label="Name"
            required
            data-autofocus
            value={input.name}
            onChange={(e) => set({ name: e.currentTarget.value })}
          />
          <Select
            label="Namespace"
            data={Array.from(new Set([...(namespaces.data?.namespaces ?? []), input.namespace]))}
            value={input.namespace}
            allowDeselect={false}
            onChange={(v) => v && set({ namespace: v, target: "" })}
          />
          <SegmentedControl
            value={input.targetKind}
            onChange={(v) => set({ targetKind: v as Input["targetKind"], target: "" })}
            data={[
              { value: "stack", label: "Stack" },
              { value: "container", label: "Container" },
            ]}
          />
          <Select
            label={input.targetKind === "stack" ? "Stack" : "Container"}
            required
            searchable
            data={options}
            value={input.target || null}
            onChange={(v) => set({ target: v ?? "" })}
            nothingFoundMessage="Nothing in this namespace"
          />
          <Select
            label="Action"
            data={Object.entries(actionLabels).map(([value, label]) => ({ value, label }))}
            value={input.action}
            allowDeselect={false}
            onChange={(v) => v && set({ action: v as Input["action"] })}
            description={
              input.action === "redeploy"
                ? input.targetKind === "stack"
                  ? "compose pull, then compose up"
                  : "Pull the image and recreate the container with the same settings"
                : undefined
            }
          />
          <Switch
            label="Enabled"
            checked={input.enabled}
            onChange={(e) => set({ enabled: e.currentTarget.checked })}
          />
          {save.isError && (
            <Alert color="red" title="Cannot save the webhook">
              {save.error.message}
            </Alert>
          )}
          <Group justify="flex-end">
            <Button variant="default" onClick={onClose}>
              Cancel
            </Button>
            <Button
              type="submit"
              loading={save.isPending}
              disabled={!input.name.trim() || !input.target}
            >
              Save
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}
