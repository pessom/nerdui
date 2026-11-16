// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Badge,
  Code,
  Group,
  Loader,
  Paper,
  Stack as MStack,
  Table,
  Text,
  Title,
} from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, getRouteApi } from "@tanstack/react-router";
import { envStatusQuery, stackQuery } from "../../../../api/queries";
import { RouterAnchor } from "../../../../components/RouterAnchor";
import { StackMode, StackStatus } from "../../../../features/stacks/StackStatus";

const envRoute = getRouteApi("/_app/env/$env");

/** A stack: services and their containers (#24); deploy and editing follow (#26, #28). */
export const Route = createFileRoute("/_app/env/$env/stacks_/$name")({
  component: StackPage,
});

function StackPage() {
  const { env, name } = Route.useParams();
  const { ns } = envRoute.useSearch();
  const status = useQuery(envStatusQuery(env));
  const namespace = ns ?? status.data?.defaultNamespace ?? "default";
  const stack = useQuery(stackQuery(env, namespace, name));

  return (
    <MStack>
      <Group gap="xs">
        <RouterAnchor size="sm" to="/env/$env/stacks" params={{ env }} search={{ ns }}>
          Stacks
        </RouterAnchor>
        <Text size="sm" c="dimmed">
          /
        </Text>
      </Group>
      <Group gap="xs">
        <Title order={2}>{name}</Title>
        {stack.data && <StackStatus stack={stack.data} />}
        {stack.data && <StackMode mode={stack.data.mode} />}
      </Group>
      {stack.isPending && <Loader aria-label="Loading" />}
      {stack.isError && (
        <Alert color="red" title="Cannot read the stack">
          {stack.error.message}
        </Alert>
      )}
      {stack.data && (
        <>
          {stack.data.path && (
            <Text size="sm">
              Compose file:{" "}
              <Code>
                {stack.data.path}/{stack.data.composeFile ?? ""}
              </Code>
            </Text>
          )}
          {stack.data.error && (
            <Alert color="red" title="The compose file cannot be read">
              {stack.data.error}
            </Alert>
          )}
          {stack.data.mode === "unmanaged" && (
            <Alert color="orange" title="Compose file unknown">
              This project was started with nerdctl compose outside nerdui&apos;s stacks directory.
              Adopting it (registering its directory) will be possible in the next version.
            </Alert>
          )}
          {stack.data.services.map((svc) => (
            <Paper key={svc.name} withBorder p="sm" component="section" aria-label={svc.name}>
              <Group gap="xs" mb={4}>
                <Text fw={600}>{svc.name}</Text>
                <Badge
                  size="sm"
                  variant="light"
                  color={svc.running > 0 ? "teal" : svc.containers.length > 0 ? "gray" : "yellow"}
                >
                  {svc.running > 0
                    ? `${svc.running} running`
                    : svc.containers.length > 0
                      ? "stopped"
                      : "not created"}
                </Badge>
                {!svc.defined && (
                  <Badge size="sm" variant="outline" color="orange">
                    not in compose file
                  </Badge>
                )}
              </Group>
              {svc.containers.length > 0 && (
                <Table verticalSpacing={4}>
                  <Table.Tbody>
                    {svc.containers.map((c) => (
                      <Table.Tr key={c.id}>
                        <Table.Td>
                          <RouterAnchor
                            to="/env/$env/containers/$id"
                            params={{ env, id: c.id }}
                            search={{ ns: namespace }}
                            size="sm"
                            title={c.id}
                          >
                            {c.name || c.id.slice(0, 12)}
                          </RouterAnchor>
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm">{c.state}</Text>
                        </Table.Td>
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
              )}
            </Paper>
          ))}
        </>
      )}
    </MStack>
  );
}
