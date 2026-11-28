// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Badge,
  Button,
  Code,
  Group,
  Loader,
  Stack as MStack,
  Table,
  Text,
  Title,
  Tooltip,
} from "@mantine/core";
import { useDisclosure } from "@mantine/hooks";
import { IconBrandGit, IconFolderPlus, IconPlus, IconTemplate } from "@tabler/icons-react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { createFileRoute, getRouteApi, useNavigate } from "@tanstack/react-router";
import {
  containersQuery,
  envStatusQuery,
  imageUpdatesQuery,
  stacksQuery,
  updatesByRef,
} from "../../../../api/queries";
import { RouterAnchor } from "../../../../components/RouterAnchor";
import { AdoptStackDialog } from "../../../../features/stacks/AdoptStackDialog";
import { GitStackDialog } from "../../../../features/stacks/GitStackDialog";
import { NewStackDialog } from "../../../../features/stacks/NewStackDialog";
import { StackMode, StackStatus } from "../../../../features/stacks/StackStatus";
import { StacksUpdateBadge } from "../../../../features/updates/StacksUpdateBadge";
import { ALL_NAMESPACES, CHOOSE_NAMESPACE } from "../../../../lib/namespace";
import { useCan } from "../../../../lib/permissions";

const envRoute = getRouteApi("/_app/env/$env");

/** Compose stacks of a namespace with their status (#24, ADR-0008). */
export const Route = createFileRoute("/_app/env/$env/stacks")({
  component: StacksPage,
});

function StacksPage() {
  const { env } = Route.useParams();
  const { ns } = envRoute.useSearch();
  const navigate = useNavigate();
  const status = useQuery(envStatusQuery(env));
  const namespace = ns ?? status.data?.defaultNamespace ?? "default";
  const list = useQuery(stacksQuery(env, namespace));
  // Update badges (#53): a stack has an update when one of its containers' images has.
  // With all namespaces (#276), stacks of the same name in different namespaces are apart.
  const allNamespaces = namespace === ALL_NAMESPACES;
  const containers = useQuery(containersQuery(env, namespace));
  const updateNamespaces = allNamespaces
    ? [...new Set((list.data?.stacks ?? []).map((s) => s.namespace))].sort()
    : [namespace];
  const byRef = useQueries({
    queries: updateNamespaces.map((n) => imageUpdatesQuery(env, n)),
    combine: (results) => new Map(results.flatMap((r) => [...updatesByRef(r.data)])),
  });
  const stackUpdates = (name: string, ns: string) =>
    (containers.data?.containers ?? []).filter(
      (c) =>
        c.compose?.project === name &&
        (!allNamespaces || c.namespace === ns) &&
        byRef.get(c.image)?.updateAvailable,
    ).length;
  const [adopting, { open, close }] = useDisclosure();
  const [creating, newStack] = useDisclosure();
  const [cloning, gitStack] = useDisclosure();
  const can = useCan();
  // New stacks go to one namespace.
  const createBlocked = allNamespaces ? CHOOSE_NAMESPACE : undefined;

  return (
    <MStack>
      <Group justify="space-between">
        <Group gap="xs">
          <Title order={2}>Stacks</Title>
          {list.isFetching && !list.isPending && <Loader size="xs" aria-label="Refreshing" />}
        </Group>
        <Group gap="xs">
          {can("stacks:write") && (
            <>
              <Tooltip label={createBlocked} disabled={!createBlocked}>
                <Button
                  variant="default"
                  leftSection={<IconFolderPlus size={16} />}
                  onClick={open}
                  disabled={!!createBlocked}
                >
                  Adopt existing
                </Button>
              </Tooltip>
              <Tooltip label={createBlocked} disabled={!createBlocked}>
                <Button
                  variant="default"
                  leftSection={<IconBrandGit size={16} />}
                  onClick={gitStack.open}
                  disabled={!!createBlocked}
                >
                  From Git
                </Button>
              </Tooltip>
            </>
          )}
          {(can("stacks:write") || can("containers:write")) && (
            <Tooltip label={createBlocked} disabled={!createBlocked}>
              <Button
                variant="default"
                leftSection={<IconTemplate size={16} />}
                onClick={() =>
                  void navigate({ to: "/env/$env/templates", params: { env }, search: { ns } })
                }
                disabled={!!createBlocked}
              >
                From template
              </Button>
            </Tooltip>
          )}
          {can("stacks:write") && (
            <Tooltip label={createBlocked} disabled={!createBlocked}>
              <Button
                leftSection={<IconPlus size={16} />}
                onClick={newStack.open}
                disabled={!!createBlocked}
              >
                New stack
              </Button>
            </Tooltip>
          )}
        </Group>
      </Group>
      <NewStackDialog env={env} namespace={namespace} opened={creating} onClose={newStack.close} />
      <AdoptStackDialog env={env} namespace={namespace} opened={adopting} onClose={close} />
      <GitStackDialog env={env} namespace={namespace} opened={cloning} onClose={gitStack.close} />
      <Text size="sm" c="dimmed">
        Compose projects: directories with a compose file in nerdui&apos;s stacks directory, and
        projects started with <Code>nerdctl compose</Code> elsewhere, and stacks cloned from Git
        repositories.
      </Text>
      {list.isPending && <Loader aria-label="Loading" />}
      {list.isError && (
        <Alert color="red" title="Cannot read stacks">
          {list.error.message}
        </Alert>
      )}
      {list.data && (
        <Table striped verticalSpacing="xs">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Name</Table.Th>
              {allNamespaces && <Table.Th>Namespace</Table.Th>}
              <Table.Th>Status</Table.Th>
              <Table.Th>Services</Table.Th>
              <Table.Th>Containers</Table.Th>
              <Table.Th>Path</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {list.data.stacks.length === 0 && (
              <Table.Tr>
                <Table.Td colSpan={allNamespaces ? 6 : 5}>
                  <Text c="dimmed" ta="center" py="md">
                    No stacks. Put a directory with a compose.yaml into the stacks directory, or
                    start a project with nerdctl compose.
                  </Text>
                </Table.Td>
              </Table.Tr>
            )}
            {list.data.stacks.map((s) => {
              const defined = s.services.filter((x) => x.defined);
              const up = defined.filter((x) => x.running > 0).length;
              return (
                <Table.Tr key={`${s.namespace}/${s.name}`}>
                  <Table.Td>
                    <Group gap={6} wrap="nowrap">
                      <RouterAnchor
                        to="/env/$env/stacks/$name"
                        params={{ env, name: s.name }}
                        search={{ ns: s.namespace }}
                        fw={500}
                        size="sm"
                      >
                        {s.name}
                      </RouterAnchor>
                      <StackMode mode={s.mode} />
                      <StacksUpdateBadge count={stackUpdates(s.name, s.namespace)} />
                    </Group>
                  </Table.Td>
                  {allNamespaces && (
                    <Table.Td>
                      <Badge variant="outline" color="gray" size="sm">
                        {s.namespace}
                      </Badge>
                    </Table.Td>
                  )}
                  <Table.Td>
                    <StackStatus stack={s} />
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">
                      {s.mode === "unmanaged" || s.status === "unknown"
                        ? s.services.map((x) => x.name).join(", ") || "—"
                        : `${up} / ${defined.length} running`}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">
                      {s.running} / {s.containers} running
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="xs" c="dimmed" ff="monospace" style={{ wordBreak: "break-all" }}>
                      {s.path ? `${s.path}/${s.composeFile ?? ""}` : "—"}
                    </Text>
                  </Table.Td>
                </Table.Tr>
              );
            })}
          </Table.Tbody>
        </Table>
      )}
    </MStack>
  );
}
