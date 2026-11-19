// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Badge,
  Button,
  Code,
  Group,
  List,
  Select,
  Stack,
  Table,
  Text,
  Title,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "../../api/client";
import { autoUpdatesQuery, type AutoUpdateEntry } from "../../api/queries";
import type { components } from "../../api/schema";
import { formatTime, usePreferences } from "../preferences/context";

const statusColor: Record<AutoUpdateEntry["status"], string> = {
  updated: "teal",
  failed: "red",
  skipped: "gray",
};

type AutoSettings = components["schemas"]["AutoUpdateSettings"];

const delays = [
  { value: "0", label: "At once" },
  { value: "3600", label: "1 hour" },
  { value: "21600", label: "6 hours" },
  { value: "86400", label: "1 day" },
  { value: "259200", label: "3 days" },
  { value: "604800", label: "7 days" },
];

const batches = [
  { value: "0", label: "Never" },
  ...[1, 3, 5, 10, 20].map((n) => ({
    value: String(n),
    label: `More than ${n} at once`,
  })),
];

/** Automatic updates of a namespace's opted-in containers and stacks (#54, #56, ADR-0032/0033). */
export function AutoUpdateLog({ env, namespace }: { env: string; namespace: string }) {
  const { prefs } = usePreferences();
  const queryClient = useQueryClient();
  const log = useQuery({ ...autoUpdatesQuery(env, namespace), refetchInterval: 30_000 });
  const settingsKey = ["env", env, "settings", "auto-updates"] as const;
  const settings = useQuery({
    queryKey: settingsKey,
    queryFn: () =>
      unwrap(
        api.GET("/api/v1/environments/{env}/settings/auto-updates", { params: { path: { env } } }),
      ),
  });
  const save = useMutation({
    mutationFn: (body: AutoSettings) =>
      unwrap(
        api.PUT("/api/v1/environments/{env}/settings/auto-updates", {
          params: { path: { env } },
          body,
        }),
      ),
    onSuccess: (data) => {
      queryClient.setQueryData(settingsKey, data);
      notifications.show({ message: "Automatic update settings saved" });
    },
    onError: (e) => notifications.show({ color: "red", title: "Cannot save", message: e.message }),
  });
  const approve = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/environments/{env}/auto-updates/approve", { params: { path: { env } } }),
      ),
    onSuccess: (data) => {
      notifications.show({ message: `${data.updates} automatic updates started` });
      void queryClient.invalidateQueries({ queryKey: ["env", env, "auto-updates"] });
    },
    onError: (e) =>
      notifications.show({ color: "red", title: "Cannot approve", message: e.message }),
  });
  const pending = log.data?.pending;
  return (
    <Stack gap="xs" component="section" aria-label="Automatic updates">
      <Title order={4}>Automatic updates</Title>
      <Text size="sm" c="dimmed">
        Containers and compose services labeled <Code>nerdui.auto-update=true</Code> are pulled and
        recreated after a scheduled check finds a newer image for their tag. Turn it on with
        &quot;Update automatically&quot; when creating or editing a container, or with the label in
        a compose file. Containers with settings recreating would lose are skipped.
      </Text>
      {settings.data && (
        <Group align="flex-end" gap="md" wrap="wrap">
          <Select
            label="Wait before applying a new image"
            description="Time since the check first saw it"
            w={240}
            data={delays}
            value={String(settings.data.delaySeconds)}
            onChange={(v) =>
              v !== null &&
              save.mutate({
                ...settings.data,
                delaySeconds: Number(v) as AutoSettings["delaySeconds"],
              })
            }
            allowDeselect={false}
            disabled={save.isPending}
          />
          <Select
            label="Ask for approval"
            description="When many updates are due"
            w={240}
            data={batches}
            value={String(settings.data.maxBatch)}
            onChange={(v) =>
              v !== null &&
              save.mutate({ ...settings.data, maxBatch: Number(v) as AutoSettings["maxBatch"] })
            }
            allowDeselect={false}
            disabled={save.isPending}
          />
        </Group>
      )}
      {pending && (
        <Alert color="yellow" title={`${pending.targets.length} updates wait for approval`}>
          <Stack gap="xs">
            <Text size="sm">
              A check found more updates than the approval threshold, in all namespaces. Nothing is
              updated until you approve; they are planned again at the next check.
            </Text>
            <List size="sm">
              {pending.targets.map((t) => (
                <List.Item key={`${t.namespace}/${t.kind}/${t.name}`}>
                  {t.kind === "stack" ? "Stack " : ""}
                  {t.name} ({t.namespace}): {t.refs.join(", ")}
                </List.Item>
              ))}
            </List>
            <Group>
              <Button size="xs" onClick={() => approve.mutate()} loading={approve.isPending}>
                Approve and update
              </Button>
            </Group>
          </Stack>
        </Alert>
      )}
      {log.isError && (
        <Alert color="red" title="Cannot read the log">
          {log.error.message}
        </Alert>
      )}
      {log.data && log.data.entries.length === 0 && (
        <Text size="sm" c="dimmed">
          No automatic updates in namespace {namespace} yet.
        </Text>
      )}
      {log.data && log.data.entries.length > 0 && (
        <Table.ScrollContainer minWidth={560}>
          <Table striped verticalSpacing={4} fz="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>When</Table.Th>
                <Table.Th>Container or stack</Table.Th>
                <Table.Th>Images</Table.Th>
                <Table.Th>Result</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {log.data.entries.map((e) => (
                <Table.Tr key={e.id}>
                  <Table.Td style={{ whiteSpace: "nowrap" }}>
                    {formatTime(e.startedAt, prefs.dateFormat).text}
                  </Table.Td>
                  <Table.Td>
                    {e.kind === "stack" ? "Stack " : ""}
                    {e.name}
                  </Table.Td>
                  <Table.Td style={{ wordBreak: "break-all" }}>{e.refs.join(", ")}</Table.Td>
                  <Table.Td>
                    <Badge size="sm" variant="light" color={statusColor[e.status]}>
                      {e.status}
                    </Badge>
                    {e.message && (
                      <Text size="xs" c="dimmed">
                        {e.message}
                      </Text>
                    )}
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      )}
    </Stack>
  );
}
