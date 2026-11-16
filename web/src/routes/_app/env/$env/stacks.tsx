// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Button,
  Code,
  Group,
  Loader,
  Stack as MStack,
  Table,
  Text,
  Title,
} from "@mantine/core";
import { useDisclosure } from "@mantine/hooks";
import { IconFolderPlus } from "@tabler/icons-react";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, getRouteApi } from "@tanstack/react-router";
import { envStatusQuery, stacksQuery } from "../../../../api/queries";
import { RouterAnchor } from "../../../../components/RouterAnchor";
import { AdoptStackDialog } from "../../../../features/stacks/AdoptStackDialog";
import { StackMode, StackStatus } from "../../../../features/stacks/StackStatus";

const envRoute = getRouteApi("/_app/env/$env");

/** Compose stacks of a namespace with their status (#24, ADR-0008). */
export const Route = createFileRoute("/_app/env/$env/stacks")({
  component: StacksPage,
});

function StacksPage() {
  const { env } = Route.useParams();
  const { ns } = envRoute.useSearch();
  const status = useQuery(envStatusQuery(env));
  const namespace = ns ?? status.data?.defaultNamespace ?? "default";
  const list = useQuery(stacksQuery(env, namespace));
  const [adopting, { open, close }] = useDisclosure();

  return (
    <MStack>
      <Group justify="space-between">
        <Group gap="xs">
          <Title order={2}>Stacks</Title>
          {list.isFetching && !list.isPending && <Loader size="xs" aria-label="Refreshing" />}
        </Group>
        <Button variant="default" leftSection={<IconFolderPlus size={16} />} onClick={open}>
          Adopt existing
        </Button>
      </Group>
      <AdoptStackDialog env={env} namespace={namespace} opened={adopting} onClose={close} />
      <Text size="sm" c="dimmed">
        Compose projects: directories with a compose file in nerdui&apos;s stacks directory, and
        projects started with <Code>nerdctl compose</Code> elsewhere.
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
              <Table.Th>Status</Table.Th>
              <Table.Th>Services</Table.Th>
              <Table.Th>Containers</Table.Th>
              <Table.Th>Path</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {list.data.stacks.length === 0 && (
              <Table.Tr>
                <Table.Td colSpan={5}>
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
                <Table.Tr key={s.name}>
                  <Table.Td>
                    <Group gap={6} wrap="nowrap">
                      <RouterAnchor
                        to="/env/$env/stacks/$name"
                        params={{ env, name: s.name }}
                        search={{ ns: namespace }}
                        fw={500}
                        size="sm"
                      >
                        {s.name}
                      </RouterAnchor>
                      <StackMode mode={s.mode} />
                    </Group>
                  </Table.Td>
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
