// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Group,
  Loader,
  SegmentedControl,
  Stack,
  Table,
  Text,
  Title,
  Tooltip,
} from "@mantine/core";
import { useDisclosure } from "@mantine/hooks";
import { IconPlus, IconTrash } from "@tabler/icons-react";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, getRouteApi } from "@tanstack/react-router";
import { useState } from "react";
import { envStatusQuery, networksQuery, type Network } from "../../../../api/queries";
import { RouterAnchor } from "../../../../components/RouterAnchor";
import {
  CreateNetworkDialog,
  RemoveNetworkDialog,
} from "../../../../features/networks/NetworkDialogs";
import { NetworkTopology } from "../../../../features/networks/NetworkTopology";
import { useCan } from "../../../../lib/permissions";

const envRoute = getRouteApi("/_app/env/$env");

/** CNI networks (#60, ADR-0024): subnets, attached containers with addresses; topology (#61). */
export const Route = createFileRoute("/_app/env/$env/networks")({
  component: NetworksPage,
});

function removeBlocked(n: Network): string | undefined {
  if (n.default) return "The default network cannot be removed";
  if (!n.removable) return "Pre-defined or managed outside nerdctl";
  if (n.containers.some((c) => c.running)) return "Used by running containers";
  return undefined;
}

function NetworksPage() {
  const { env } = Route.useParams();
  const { ns } = envRoute.useSearch();
  const status = useQuery(envStatusQuery(env));
  const namespace = ns ?? status.data?.defaultNamespace ?? "default";
  const list = useQuery(networksQuery(env, namespace));
  const [creating, { open, close }] = useDisclosure();
  const [removing, setRemoving] = useState<Network>();
  const [view, setView] = useState<"table" | "topology">("table");
  const nerdctl = status.data?.runtime?.nerdctl;
  const readOnly = !!nerdctl && !nerdctl.compatible && !nerdctl.checkSkipped;
  const write = useCan()("networks:write");

  return (
    <Stack>
      <Group justify="space-between">
        <Group gap="xs">
          <Title order={2}>Networks</Title>
          {list.isFetching && !list.isPending && <Loader size="xs" aria-label="Refreshing" />}
        </Group>
        {write && (
          <Tooltip label="Read-only mode: host nerdctl is incompatible" disabled={!readOnly}>
            <Button leftSection={<IconPlus size={16} />} onClick={open} disabled={readOnly}>
              Create network
            </Button>
          </Tooltip>
        )}
      </Group>
      <SegmentedControl
        aria-label="View"
        w="fit-content"
        value={view}
        onChange={(v) => setView(v as typeof view)}
        data={[
          { value: "table", label: "Table" },
          { value: "topology", label: "Topology" },
        ]}
      />
      {list.isPending && <Loader aria-label="Loading" />}
      {list.isError && (
        <Alert color="red" title="Cannot read networks">
          {list.error.message}
        </Alert>
      )}
      {list.data && view === "topology" && (
        <NetworkTopology env={env} namespace={namespace} networks={list.data.networks} />
      )}
      {list.data && view === "table" && (
        <Table striped verticalSpacing="xs">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Name</Table.Th>
              <Table.Th>Driver</Table.Th>
              <Table.Th>Subnet</Table.Th>
              <Table.Th>Containers</Table.Th>
              <Table.Th w={48} />
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {list.data.networks.length === 0 && (
              <Table.Tr>
                <Table.Td colSpan={5}>
                  <Text c="dimmed" ta="center" py="md">
                    No networks yet. nerdctl creates the default bridge network with the first
                    container.
                  </Text>
                </Table.Td>
              </Table.Tr>
            )}
            {list.data.networks.map((n) => {
              const blocked = removeBlocked(n);
              return (
                <Table.Tr key={n.name}>
                  <Table.Td>
                    <Group gap={6} wrap="nowrap">
                      <Text size="sm" fw={500} title={n.file || undefined}>
                        {n.name}
                      </Text>
                      {n.default && (
                        <Badge size="xs" variant="light">
                          default
                        </Badge>
                      )}
                      {n.internal && (
                        <Badge size="xs" variant="outline" color="gray">
                          internal
                        </Badge>
                      )}
                    </Group>
                    {Object.keys(n.labels).length > 0 && (
                      <Text size="xs" c="dimmed">
                        {Object.entries(n.labels)
                          .map(([k, v]) => (v ? `${k}=${v}` : k))
                          .join(", ")}
                      </Text>
                    )}
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{n.driver || "—"}</Text>
                    {n.parent && (
                      <Text size="xs" c="dimmed">
                        parent {n.parent}
                      </Text>
                    )}
                  </Table.Td>
                  <Table.Td>
                    {n.ipam.length === 0 && <Text size="sm">—</Text>}
                    {n.ipam.map((r, i) => (
                      <div key={i}>
                        <Text size="sm" ff="monospace">
                          {r.subnet || "—"}
                        </Text>
                        {(r.gateway || r.rangeStart) && (
                          <Text size="xs" c="dimmed">
                            {r.gateway && `gateway ${r.gateway}`}
                            {r.gateway && r.rangeStart && " · "}
                            {r.rangeStart && `range ${r.rangeStart}–${r.rangeEnd ?? ""}`}
                          </Text>
                        )}
                      </div>
                    ))}
                  </Table.Td>
                  <Table.Td>
                    {n.containers.length === 0 && (
                      <Text size="sm" c="dimmed">
                        none
                      </Text>
                    )}
                    {n.containers.map((c) => (
                      <Group key={c.id} gap={6} wrap="nowrap">
                        <RouterAnchor
                          to="/env/$env/containers/$id"
                          params={{ env, id: c.id }}
                          search={{ ns: namespace }}
                          size="sm"
                          c={c.running ? undefined : "dimmed"}
                          title={c.id}
                        >
                          {c.name || c.id.slice(0, 12)}
                        </RouterAnchor>
                        <Text size="xs" c="dimmed" ff="monospace">
                          {c.running ? (c.ips?.join(", ") ?? "") : "stopped"}
                        </Text>
                      </Group>
                    ))}
                  </Table.Td>
                  <Table.Td>
                    {write && (
                      <Tooltip label={readOnly ? "Read-only mode" : (blocked ?? "Remove")}>
                        <ActionIcon
                          variant="subtle"
                          color="red"
                          aria-label={`Remove ${n.name}`}
                          disabled={!!blocked || readOnly}
                          onClick={() => setRemoving(n)}
                        >
                          <IconTrash size={16} />
                        </ActionIcon>
                      </Tooltip>
                    )}
                  </Table.Td>
                </Table.Tr>
              );
            })}
          </Table.Tbody>
        </Table>
      )}
      <CreateNetworkDialog env={env} namespace={namespace} opened={creating} onClose={close} />
      <RemoveNetworkDialog
        env={env}
        namespace={namespace}
        network={removing}
        onClose={() => setRemoving(undefined)}
      />
    </Stack>
  );
}
