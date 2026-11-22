// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Group,
  Loader,
  Menu,
  Paper,
  Stack,
  Table,
  Text,
  Title,
  Tooltip,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import {
  IconDots,
  IconEdit,
  IconHistory,
  IconPlayerPlay,
  IconPlus,
  IconTrash,
} from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import { stacksQuery } from "../../api/queries";
import { RouterAnchor } from "../../components/RouterAnchor";
import { Time } from "../../components/Time";
import { useJobsPanel } from "../../features/jobs/JobsContext";
import {
  describeCron,
  describeTask,
  formatSeconds,
  kindLabels,
  schedulesKey,
  statusColor,
  type Schedule,
} from "../../features/schedules/kinds";
import { ScheduleForm } from "../../features/schedules/ScheduleForm";
import { ScheduleRuns } from "../../features/schedules/ScheduleRuns";

/** Scheduled jobs with their history (#82, ADR-0048). */
export const Route = createFileRoute("/_app/schedules")({
  component: SchedulesPage,
});

function SchedulesPage() {
  const queryClient = useQueryClient();
  const jobs = useJobsPanel();
  const list = useQuery({
    queryKey: schedulesKey,
    queryFn: () => unwrap(api.GET("/api/v1/schedules")),
    refetchInterval: 15_000,
  });
  const [editing, setEditing] = useState<Schedule | "new">();
  const [history, setHistory] = useState<Schedule>();
  const run = useMutation({
    mutationFn: (s: Schedule) =>
      unwrap(api.POST("/api/v1/schedules/{id}/run", { params: { path: { id: s.id } } })),
    onSuccess: ({ job }) => {
      void queryClient.invalidateQueries({ queryKey: schedulesKey });
      jobs.open(job.id);
    },
    onError: (e) => notifications.show({ color: "red", title: "Cannot run", message: e.message }),
  });
  const remove = useMutation({
    mutationFn: (s: Schedule) =>
      unwrapEmpty(api.DELETE("/api/v1/schedules/{id}", { params: { path: { id: s.id } } })),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: schedulesKey }),
    onError: (e) =>
      notifications.show({ color: "red", title: "Cannot delete", message: e.message }),
  });

  return (
    <Stack>
      <Group justify="space-between">
        <Title order={2}>Schedules</Title>
        <Button leftSection={<IconPlus size={16} />} onClick={() => setEditing("new")}>
          New schedule
        </Button>
      </Group>
      {list.isPending && <Loader aria-label="Loading" />}
      {list.isError && <Alert color="red">{list.error.message}</Alert>}
      {list.data && (
        <Table.ScrollContainer minWidth={800}>
          <Table striped verticalSpacing="xs" aria-label="Schedules">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Name</Table.Th>
                <Table.Th>Task</Table.Th>
                <Table.Th>When</Table.Th>
                <Table.Th>Next run</Table.Th>
                <Table.Th>Last run</Table.Th>
                <Table.Th w={48} />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {list.data.schedules.length === 0 && (
                <Table.Tr>
                  <Table.Td colSpan={6}>
                    <Text c="dimmed" ta="center" py="md">
                      No schedules yet.
                    </Text>
                  </Table.Td>
                </Table.Tr>
              )}
              {list.data.schedules.map((s) => (
                <Table.Tr key={s.id}>
                  <Table.Td>
                    <Group gap={6} wrap="nowrap">
                      <Text size="sm" fw={500}>
                        {s.name}
                      </Text>
                      {!s.enabled && (
                        <Badge size="xs" variant="outline" color="gray">
                          off
                        </Badge>
                      )}
                    </Group>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{kindLabels[s.kind]}</Text>
                    <Text size="xs" c="dimmed">
                      {describeTask(s)}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{describeCron(s.cron) ?? s.cron}</Text>
                    <Text size="xs" c="dimmed" ff="monospace">
                      {s.cron}
                    </Text>
                  </Table.Td>
                  <Table.Td>{s.nextRunAt ? <Time value={s.nextRunAt} /> : "—"}</Table.Td>
                  <Table.Td>
                    {s.lastRunAt ? (
                      <Tooltip label={s.lastMessage} disabled={!s.lastMessage} multiline maw={400}>
                        <Group gap={6} wrap="nowrap">
                          <Badge variant="light" color={statusColor[s.lastStatus ?? ""] ?? "gray"}>
                            {s.lastStatus}
                          </Badge>
                          <Time value={s.lastRunAt} />
                        </Group>
                      </Tooltip>
                    ) : (
                      "—"
                    )}
                  </Table.Td>
                  <Table.Td>
                    <Menu position="bottom-end" withArrow>
                      <Menu.Target>
                        <ActionIcon
                          variant="subtle"
                          color="gray"
                          aria-label={`Actions for ${s.name}`}
                        >
                          <IconDots size={16} />
                        </ActionIcon>
                      </Menu.Target>
                      <Menu.Dropdown>
                        <Menu.Item
                          leftSection={<IconPlayerPlay size={16} />}
                          onClick={() => run.mutate(s)}
                        >
                          Run now
                        </Menu.Item>
                        <Menu.Item
                          leftSection={<IconHistory size={16} />}
                          onClick={() => setHistory(s)}
                        >
                          History
                        </Menu.Item>
                        <Menu.Item
                          leftSection={<IconEdit size={16} />}
                          onClick={() => setEditing(s)}
                        >
                          Edit…
                        </Menu.Item>
                        <Menu.Divider />
                        <Menu.Item
                          color="red"
                          leftSection={<IconTrash size={16} />}
                          onClick={() => {
                            if (window.confirm(`Delete schedule ${s.name} and its history?`)) {
                              remove.mutate(s);
                            }
                          }}
                        >
                          Delete
                        </Menu.Item>
                      </Menu.Dropdown>
                    </Menu>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      )}
      <GitPolling />
      {editing && (
        <ScheduleForm
          key={editing === "new" ? "new" : editing.id}
          opened
          schedule={editing === "new" ? undefined : editing}
          onClose={() => setEditing(undefined)}
        />
      )}
      <ScheduleRuns schedule={history} onClose={() => setHistory(undefined)} />
    </Stack>
  );
}

/** Git stacks that poll their repository: set on each stack (#38, ADR-0040). */
function GitPolling() {
  const stacks = useQuery(stacksQuery("local", "default"));
  const polling = (stacks.data?.stacks ?? []).filter((s) => s.git && s.git.pollInterval > 0);
  return (
    <Paper withBorder p="md" component="section" aria-label="Git polling">
      <Stack gap="xs">
        <Title order={4}>Git polling</Title>
        <Text size="sm" c="dimmed">
          Git stacks check their repository for new commits on their own interval, set on the
          stack&apos;s page next to its webhook.
        </Text>
        {polling.length === 0 ? (
          <Text size="sm" c="dimmed">
            No Git stack polls its repository.
          </Text>
        ) : (
          <Table verticalSpacing={4} aria-label="Git polling">
            <Table.Tbody>
              {polling.map((s) => (
                <Table.Tr key={s.name}>
                  <Table.Td>
                    <RouterAnchor
                      to="/env/$env/stacks/$name"
                      params={{ env: "local", name: s.name }}
                      size="sm"
                    >
                      {s.name}
                    </RouterAnchor>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">every {formatSeconds(s.git?.pollInterval ?? 0)}</Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">
                      {s.git?.autoUpdate === "deploy" ? "pull and redeploy" : "notify"}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    {s.git?.checkedAt ? <Time value={s.git.checkedAt} /> : "not checked yet"}
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        )}
      </Stack>
    </Paper>
  );
}
