// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Group,
  Loader,
  Stack,
  Table,
  Text,
  Title,
  Tooltip,
} from "@mantine/core";
import { useDisclosure } from "@mantine/hooks";
import { IconPlus, IconTrash } from "@tabler/icons-react";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, getRouteApi, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { envStatusQuery, summaryQuery } from "../../../../api/queries";
import { RouterAnchor } from "../../../../components/RouterAnchor";
import {
  CreateNamespaceDialog,
  RemoveNamespaceDialog,
} from "../../../../features/namespaces/NamespaceDialogs";
import { holders, type NamespaceSummary } from "../../../../features/namespaces/namespaces";
import { useCan } from "../../../../lib/permissions";

const envRoute = getRouteApi("/_app/env/$env");

/** containerd namespaces with what they hold; create and remove empty ones (#195). */
export const Route = createFileRoute("/_app/env/$env/namespaces")({
  component: NamespacesPage,
});

function NamespacesPage() {
  const { env } = Route.useParams();
  const { ns } = envRoute.useSearch();
  const navigate = useNavigate();
  const status = useQuery(envStatusQuery(env));
  const summary = useQuery(summaryQuery(env));
  const [creating, { open, close }] = useDisclosure();
  const [removing, setRemoving] = useState<NamespaceSummary>();
  const nerdctl = status.data?.runtime?.nerdctl;
  const readOnly = !!nerdctl && !nerdctl.compatible && !nerdctl.checkSkipped;
  const write = useCan()("namespaces:write");
  const defaultNs = status.data?.defaultNamespace ?? "default";
  const list = summary.data?.namespaces ?? [];

  return (
    <Stack>
      <Group justify="space-between">
        <Group gap="xs">
          <Title order={2}>Namespaces</Title>
          {summary.isFetching && !summary.isPending && <Loader size="xs" aria-label="Refreshing" />}
        </Group>
        {write && (
          <Tooltip label="Read-only mode: host nerdctl is incompatible" disabled={!readOnly}>
            <Button leftSection={<IconPlus size={16} />} onClick={open} disabled={readOnly}>
              Create namespace
            </Button>
          </Tooltip>
        )}
      </Group>
      {summary.isPending && <Loader aria-label="Loading" />}
      {summary.isError && (
        <Alert color="red" title="Cannot read namespaces">
          {summary.error.message}
        </Alert>
      )}
      {summary.data && (
        <Table striped verticalSpacing="xs">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Name</Table.Th>
              <Table.Th>Containers</Table.Th>
              <Table.Th>Images</Table.Th>
              <Table.Th>Volumes</Table.Th>
              <Table.Th w={48} />
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {list.map((n) => {
              const held = holders(n);
              return (
                <Table.Tr key={n.name}>
                  <Table.Td>
                    <Group gap="xs">
                      <RouterAnchor
                        to="/env/$env/containers"
                        params={{ env }}
                        search={{ ns: n.name }}
                        size="sm"
                        fw={500}
                      >
                        {n.name}
                      </RouterAnchor>
                      {n.name === defaultNs && (
                        <Badge size="xs" variant="light">
                          default
                        </Badge>
                      )}
                      {n.name === ns && (
                        <Badge size="xs" variant="outline">
                          selected
                        </Badge>
                      )}
                    </Group>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">
                      {n.containers.running}/{n.containers.total} running
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <RouterAnchor
                      to="/env/$env/images"
                      params={{ env }}
                      search={{ ns: n.name }}
                      size="sm"
                    >
                      {n.images}
                    </RouterAnchor>
                  </Table.Td>
                  <Table.Td>
                    <RouterAnchor
                      to="/env/$env/volumes"
                      params={{ env }}
                      search={{ ns: n.name }}
                      size="sm"
                    >
                      {n.volumes}
                    </RouterAnchor>
                  </Table.Td>
                  <Table.Td>
                    {write && (
                      <Tooltip
                        label={
                          readOnly
                            ? "Read-only mode"
                            : held.length > 0
                              ? `Not empty: ${held.join(", ")}`
                              : "Remove"
                        }
                      >
                        <ActionIcon
                          variant="subtle"
                          color="red"
                          aria-label={`Remove ${n.name}`}
                          disabled={readOnly}
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
      <CreateNamespaceDialog
        env={env}
        opened={creating}
        existing={list.map((n) => n.name)}
        onClose={close}
      />
      <RemoveNamespaceDialog
        env={env}
        namespace={removing}
        isDefault={removing?.name === defaultNs}
        onClose={() => setRemoving(undefined)}
        onRemoved={(name) => {
          // Leave a removed namespace that was selected.
          if (name === ns)
            void navigate({ to: ".", search: (prev) => ({ ...prev, ns: undefined }) });
        }}
      />
    </Stack>
  );
}
