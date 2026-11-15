// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Badge,
  Group,
  List,
  Loader,
  SegmentedControl,
  Stack,
  Table,
  Text,
  Title,
  Tooltip,
} from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { portsQuery, type PortEntry } from "../../../../api/queries";
import { RouterAnchor } from "../../../../components/RouterAnchor";

/** Ports of all containers of the host (#62): published, exposed, conflicts. */
export const Route = createFileRoute("/_app/env/$env/ports")({
  component: PortsPage,
});

type Filter = "all" | "published" | "conflicts";

function hostAddress(p: PortEntry): string {
  const ip = p.hostIp || "0.0.0.0";
  return `${ip.includes(":") ? `[${ip}]` : ip}:${p.hostPort}`;
}

function PortsPage() {
  const { env } = Route.useParams();
  const ports = useQuery(portsQuery(env));
  const [filter, setFilter] = useState<Filter>("all");
  const all = ports.data?.ports ?? [];
  const conflicts = all.filter((p) => (p.conflicts?.length ?? 0) > 0).length;
  const shown = all.filter((p) =>
    filter === "published"
      ? p.published
      : filter === "conflicts"
        ? (p.conflicts?.length ?? 0) > 0
        : true,
  );

  return (
    <Stack>
      <Group justify="space-between">
        <Group gap="xs">
          <Title order={2}>Ports</Title>
          {ports.isFetching && !ports.isPending && <Loader size="xs" aria-label="Refreshing" />}
        </Group>
        <SegmentedControl
          size="xs"
          aria-label="Filter"
          value={filter}
          onChange={(v) => setFilter(v as Filter)}
          data={[
            { value: "all", label: "All" },
            { value: "published", label: "Published" },
            { value: "conflicts", label: `Conflicts (${conflicts})` },
          ]}
        />
      </Group>
      <Text size="sm" c="dimmed">
        Containers of all namespaces. Exposed ports are declared by the image but not published on
        the host.
      </Text>
      {ports.data && !ports.data.hostChecked && (
        <Alert color="gray" title="Host processes were not checked">
          nerdui could not read the host&apos;s sockets (it runs in a container or not on Linux), so
          only conflicts between containers are shown.
        </Alert>
      )}
      {ports.isPending && <Loader aria-label="Loading" />}
      {ports.isError && (
        <Alert color="red" title="Cannot read ports">
          {ports.error.message}
        </Alert>
      )}
      {ports.data && (
        <Table striped verticalSpacing="xs">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Host</Table.Th>
              <Table.Th>Container port</Table.Th>
              <Table.Th>Container</Table.Th>
              <Table.Th>Namespace</Table.Th>
              <Table.Th>Conflicts</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {shown.length === 0 && (
              <Table.Tr>
                <Table.Td colSpan={5}>
                  <Text c="dimmed" ta="center" py="md">
                    {filter === "conflicts" ? "No conflicts." : "No ports."}
                  </Text>
                </Table.Td>
              </Table.Tr>
            )}
            {shown.map((p, i) => (
              <Table.Tr
                key={`${p.containerId}-${p.hostIp}-${p.hostPort}-${p.containerPort}-${p.protocol}-${i}`}
              >
                <Table.Td>
                  {p.published ? (
                    <Text size="sm" ff="monospace">
                      {hostAddress(p)}
                    </Text>
                  ) : (
                    <Badge size="xs" variant="outline" color="gray">
                      exposed
                    </Badge>
                  )}
                </Table.Td>
                <Table.Td>
                  <Text size="sm" ff="monospace">
                    {p.containerPort}/{p.protocol}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Group gap={6} wrap="nowrap">
                    <RouterAnchor
                      to="/env/$env/containers/$id"
                      params={{ env, id: p.containerId }}
                      search={{ ns: p.namespace }}
                      size="sm"
                      title={p.containerId}
                    >
                      {p.containerName || p.containerId.slice(0, 12)}
                    </RouterAnchor>
                    {p.state !== "running" && (
                      <Text size="xs" c="dimmed">
                        {p.state}
                      </Text>
                    )}
                  </Group>
                </Table.Td>
                <Table.Td>
                  <Text size="sm">{p.namespace}</Text>
                </Table.Td>
                <Table.Td>
                  {(p.conflicts?.length ?? 0) > 0 && (
                    <Tooltip
                      multiline
                      w={320}
                      label={
                        <List size="xs">
                          {p.conflicts?.map((c) => (
                            <List.Item key={c}>{c}</List.Item>
                          ))}
                        </List>
                      }
                    >
                      <Badge color="red" variant="light">
                        {p.conflicts?.length === 1
                          ? p.conflicts[0]
                          : `${p.conflicts?.length} conflicts`}
                      </Badge>
                    </Tooltip>
                  )}
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
    </Stack>
  );
}
