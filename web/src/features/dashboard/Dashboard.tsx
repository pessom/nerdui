// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Badge,
  Button,
  Group,
  Loader,
  Paper,
  Progress,
  SimpleGrid,
  Stack,
  Table,
  Text,
  Tooltip,
} from "@mantine/core";
import { IconRuler } from "@tabler/icons-react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { api, unwrap, type components } from "../../api/client";
import { keys } from "../../api/queries";
import { RouterAnchor } from "../../components/RouterAnchor";
import { Time } from "../../components/Time";
import { eventColor, eventLabel, eventObject } from "../events/format";
import { formatBytes } from "../system/info";
import { useTopStats } from "./topStats";

type Summary = components["schemas"]["Summary"];
type TopContainer = components["schemas"]["TopContainer"];

const summaryQuery = (env: string, volumeSizes: boolean) => ({
  // Under env: container events refresh the counters.
  queryKey: [...keys.env(env), "summary", { volumeSizes }] as const,
  queryFn: () =>
    unwrap(
      api.GET("/api/v1/environments/{env}/summary", {
        params: { path: { env }, query: { volumeSizes } },
      }),
    ),
  // Volume sizes walk every volume: never polled (#90).
  ...(volumeSizes && { refetchInterval: false as const }),
});

function Counter({
  env,
  label,
  value,
  to,
  hint,
  children,
}: {
  env: string;
  label: string;
  value: number;
  to:
    | "/env/$env/containers"
    | "/env/$env/stacks"
    | "/env/$env/images"
    | "/env/$env/volumes"
    | "/env/$env/networks";
  hint?: string;
  children?: ReactNode;
}) {
  const card = (
    <Paper withBorder p="sm" component="section" aria-label={label}>
      <Text size="xs" c="dimmed">
        {label}
      </Text>
      <Text fz={28} fw={700} lh={1.2}>
        {value}
      </Text>
      {children}
    </Paper>
  );
  return (
    <RouterAnchor to={to} params={{ env }} underline="never" c="inherit">
      {hint ? (
        <Tooltip label={hint} multiline w={260}>
          {card}
        </Tooltip>
      ) : (
        card
      )}
    </RouterAnchor>
  );
}

function perNamespace(
  s: Summary,
  pick: (n: Summary["namespaces"][number]) => number,
): string | undefined {
  const parts = s.namespaces.filter((n) => pick(n) > 0).map((n) => `${n.name}: ${pick(n)}`);
  return s.namespaces.length > 1 && parts.length ? parts.join(", ") : undefined;
}

function TopList({
  title,
  items,
  value,
  env,
}: {
  title: string;
  items: TopContainer[] | undefined;
  value: (t: TopContainer) => string;
  env: string;
}) {
  return (
    <Paper withBorder p="sm" component="section" aria-label={title}>
      <Text size="sm" fw={500} mb={4}>
        {title}
      </Text>
      {!items && <Loader size="xs" aria-label={`Sampling ${title}`} />}
      {items?.length === 0 && (
        <Text size="sm" c="dimmed">
          No running containers.
        </Text>
      )}
      {items && items.length > 0 && (
        <Table verticalSpacing={2}>
          <Table.Tbody>
            {items.map((t) => (
              <Table.Tr key={`${t.namespace}/${t.id}`}>
                <Table.Td>
                  <RouterAnchor
                    to="/env/$env/containers/$id"
                    params={{ env, id: t.id }}
                    search={{ ns: t.namespace }}
                    size="sm"
                    title={t.id}
                  >
                    {t.name || t.id.slice(0, 12)}
                  </RouterAnchor>
                </Table.Td>
                <Table.Td ta="right">
                  <Text size="sm" ff="monospace">
                    {value(t)}
                  </Text>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
    </Paper>
  );
}

/**
 * Dashboard of a host on the Overview page (#63): resource counters across namespaces, disk
 * usage, containers using most CPU and memory, recent events.
 */
export function Dashboard({ env }: { env: string }) {
  const [volumeSizes, setVolumeSizes] = useState(false);
  // Asking for volume sizes keeps the counters on screen while they are computed.
  const summary = useQuery({
    ...summaryQuery(env, volumeSizes),
    placeholderData: keepPreviousData,
  });
  const top = useTopStats(env);
  const events = useQuery({
    queryKey: [...keys.env(env), "recent-events"] as const,
    queryFn: () =>
      unwrap(
        api.GET("/api/v1/environments/{env}/events", {
          params: { path: { env }, query: { namespace: "*", limit: 8 } },
        }),
      ),
  });

  if (summary.isPending && !summary.data) return <Loader aria-label="Loading summary" />;
  if (summary.isError)
    return (
      <Alert color="red" title="Cannot read the summary">
        {summary.error.message}
      </Alert>
    );
  const s = summary.data;
  const c = s.containers;

  return (
    <Stack gap="sm">
      <SimpleGrid cols={{ base: 2, sm: 3, lg: 5 }} spacing="sm">
        <Counter
          env={env}
          label="Containers"
          value={c.total}
          to="/env/$env/containers"
          hint={perNamespace(s, (n) => n.containers.total)}
        >
          <Group gap={4} mt={4}>
            <Badge size="xs" color="teal" variant="light">
              {c.running} running
            </Badge>
            {c.paused > 0 && (
              <Badge size="xs" color="yellow" variant="light">
                {c.paused} paused
              </Badge>
            )}
            <Badge size="xs" color="gray" variant="light">
              {c.stopped} stopped
            </Badge>
            {c.unhealthy > 0 && (
              <Badge size="xs" color="red" variant="light">
                {c.unhealthy} unhealthy
              </Badge>
            )}
          </Group>
        </Counter>
        <Counter env={env} label="Stacks" value={s.stacks} to="/env/$env/stacks" />
        <Counter
          env={env}
          label="Images"
          value={s.images}
          to="/env/$env/images"
          hint={perNamespace(s, (n) => n.images)}
        />
        <Counter
          env={env}
          label="Volumes"
          value={s.volumes}
          to="/env/$env/volumes"
          hint={perNamespace(s, (n) => n.volumes)}
        />
        <Counter
          env={env}
          label="Networks"
          value={s.networks}
          to="/env/$env/networks"
          hint={perNamespace(s, (n) => n.networks)}
        />
      </SimpleGrid>

      <SimpleGrid cols={{ base: 1, md: 3 }} spacing="sm">
        <Paper withBorder p="sm" component="section" aria-label="Disk">
          <Group justify="space-between" mb={4}>
            <Text size="sm" fw={500}>
              Disk
            </Text>
            {!volumeSizes && (
              <Button
                size="compact-xs"
                variant="default"
                leftSection={<IconRuler size={12} />}
                onClick={() => setVolumeSizes(true)}
              >
                Volume sizes
              </Button>
            )}
          </Group>
          <Stack gap={6}>
            <Group justify="space-between">
              <Text size="sm">Images</Text>
              <Text size="sm" ff="monospace">
                {formatBytes(s.imagesSize)}
              </Text>
            </Group>
            <Group justify="space-between">
              <Text size="sm">Volumes</Text>
              <Text size="sm" ff="monospace">
                {s.volumesSize != null ? (
                  formatBytes(s.volumesSize)
                ) : volumeSizes ? (
                  <Loader size={10} />
                ) : (
                  "—"
                )}
              </Text>
            </Group>
            {s.filesystems.map((f) => {
              const used = f.total - f.free;
              const pct = f.total ? (used / f.total) * 100 : 0;
              return (
                <div key={f.paths.join(",")}>
                  <Group justify="space-between">
                    <Text size="xs" c="dimmed" title={f.paths.join("\n")} truncate="end" maw="60%">
                      {f.paths.join(", ")}
                    </Text>
                    <Text size="xs" ff="monospace">
                      {formatBytes(f.free)} free of {formatBytes(f.total)}
                    </Text>
                  </Group>
                  <Progress
                    value={pct}
                    color={pct > 90 ? "red" : pct > 75 ? "yellow" : "teal"}
                    aria-label={`Used ${Math.round(pct)}%`}
                  />
                </div>
              );
            })}
          </Stack>
        </Paper>
        <TopList
          title="Top CPU"
          env={env}
          items={top?.cpu}
          value={(t) => `${t.cpuPercent.toFixed(1)} %`}
        />
        <TopList
          title="Top memory"
          env={env}
          items={top?.memory}
          value={(t) => formatBytes(t.memoryUsage)}
        />
      </SimpleGrid>

      <Paper withBorder p="sm" component="section" aria-label="Recent events">
        <Group justify="space-between" mb={4}>
          <Text size="sm" fw={500}>
            Recent events
          </Text>
          <RouterAnchor to="/env/$env/events" params={{ env }} size="xs">
            All events
          </RouterAnchor>
        </Group>
        {events.isPending && <Loader size="xs" aria-label="Loading events" />}
        {events.data && !events.data.recording && (
          <Text size="sm" c="dimmed">
            Event history is turned off (--events-retention=0).
          </Text>
        )}
        {events.data?.recording && events.data.events.length === 0 && (
          <Text size="sm" c="dimmed">
            No events yet.
          </Text>
        )}
        {events.data && events.data.events.length > 0 && (
          <Table verticalSpacing={2}>
            <Table.Tbody>
              {events.data.events.map((e) => (
                <Table.Tr key={e.id}>
                  <Table.Td w={110}>
                    <Time value={e.ts} size="xs" c="dimmed" />
                  </Table.Td>
                  <Table.Td w={140}>
                    <Badge size="xs" variant="light" color={eventColor(e)}>
                      {eventLabel(e)}
                    </Badge>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm" truncate="end">
                      {eventObject(e)}
                    </Text>
                  </Table.Td>
                  <Table.Td w={90}>
                    <Text size="xs" c="dimmed">
                      {e.namespace}
                    </Text>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        )}
      </Paper>
    </Stack>
  );
}
