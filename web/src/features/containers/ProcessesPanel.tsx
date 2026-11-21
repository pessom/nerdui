// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Button, Group, Loader, Stack, Table, Text } from "@mantine/core";
import { IconRefresh } from "@tabler/icons-react";
import { useQuery } from "@tanstack/react-query";
import { api, unwrap } from "../../api/client";
import { formatBytes } from "../system/info";

/** Processes of a running container (#20), refreshed on demand. */
export function ProcessesPanel({
  env,
  id,
  namespace,
  running,
}: {
  env: string;
  id: string;
  namespace: string;
  running: boolean;
}) {
  const top = useQuery({
    queryKey: ["env", env, "ns", namespace, "containers", id, "top"],
    queryFn: () =>
      unwrap(
        api.GET("/api/v1/environments/{env}/containers/{id}/top", {
          params: { path: { env, id }, query: { namespace } },
        }),
      ),
    enabled: running,
    retry: false,
  });

  if (!running) {
    return (
      <Text c="dimmed" size="sm">
        The container is not running.
      </Text>
    );
  }
  return (
    <Stack gap="sm">
      <Group justify="space-between">
        <Text size="sm" c="dimmed">
          PIDs and users as the host sees them; CPU is the share of one CPU since the process
          started.
        </Text>
        <Button
          size="xs"
          variant="default"
          leftSection={<IconRefresh size={14} />}
          loading={top.isFetching}
          onClick={() => void top.refetch()}
        >
          Refresh
        </Button>
      </Group>
      {top.isPending && <Loader aria-label="Loading processes" />}
      {top.isError && (
        <Alert color="red" title="Cannot list processes">
          {top.error.message}
        </Alert>
      )}
      {top.data && (
        <Table.ScrollContainer minWidth={640}>
          <Table striped verticalSpacing={4} fz="sm" aria-label="Processes">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>PID</Table.Th>
                <Table.Th>PPID</Table.Th>
                <Table.Th>User</Table.Th>
                <Table.Th>State</Table.Th>
                <Table.Th ta="right">CPU %</Table.Th>
                <Table.Th ta="right">Memory</Table.Th>
                <Table.Th>Started</Table.Th>
                <Table.Th>Command</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {top.data.processes.map((p) => (
                <Table.Tr key={p.pid}>
                  <Table.Td>{p.pid}</Table.Td>
                  <Table.Td>{p.ppid}</Table.Td>
                  <Table.Td>{p.user || (p.uid >= 0 ? p.uid : "—")}</Table.Td>
                  <Table.Td>{p.state}</Table.Td>
                  <Table.Td ta="right">{p.cpu.toFixed(1)}</Table.Td>
                  <Table.Td ta="right">{formatBytes(p.rss)}</Table.Td>
                  <Table.Td>{new Date(p.started).toLocaleString()}</Table.Td>
                  <Table.Td>
                    <Text size="xs" ff="monospace" style={{ wordBreak: "break-all" }}>
                      {p.command}
                    </Text>
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
