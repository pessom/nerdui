// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Badge,
  Button,
  Code,
  Group,
  Loader,
  Paper,
  Stack as MStack,
  Table,
  Text,
  Title,
} from "@mantine/core";
import { useDisclosure } from "@mantine/hooks";
import { notifications } from "@mantine/notifications";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, getRouteApi, useNavigate } from "@tanstack/react-router";
import { api, unwrapEmpty } from "../../../../api/client";
import { envStatusQuery, keys, stackQuery } from "../../../../api/queries";
import { RouterAnchor } from "../../../../components/RouterAnchor";
import { AdoptStackDialog } from "../../../../features/stacks/AdoptStackDialog";
import { StackActions } from "../../../../features/stacks/StackActions";
import { StackMode, StackStatus } from "../../../../features/stacks/StackStatus";

const envRoute = getRouteApi("/_app/env/$env");

/** A stack: operations (#28), services and their containers (#24). */
export const Route = createFileRoute("/_app/env/$env/stacks_/$name")({
  component: StackPage,
});

function StackPage() {
  const { env, name } = Route.useParams();
  const { ns } = envRoute.useSearch();
  const status = useQuery(envStatusQuery(env));
  const namespace = ns ?? status.data?.defaultNamespace ?? "default";
  const nerdctl = status.data?.runtime?.nerdctl;
  const readOnly = !!nerdctl && !nerdctl.compatible && !nerdctl.checkSkipped;
  const stack = useQuery(stackQuery(env, namespace, name));
  const [adopting, adoptDialog] = useDisclosure();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const forget = useMutation({
    mutationFn: () =>
      unwrapEmpty(
        api.DELETE("/api/v1/environments/{env}/stacks/{name}/registration", {
          params: { path: { env, name } },
        }),
      ),
    onSuccess: () => {
      notifications.show({ message: `Stack ${name} forgotten; its files and containers stay` });
      void queryClient.invalidateQueries({ queryKey: keys.stacks(env, namespace) });
      void navigate({ to: "/env/$env/stacks", params: { env }, search: { ns } });
    },
    onError: (e) =>
      notifications.show({ color: "red", title: "Cannot forget the stack", message: e.message }),
  });

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
      <Group justify="space-between">
        <Group gap="xs">
          <Title order={2}>{name}</Title>
          {stack.data && <StackStatus stack={stack.data} />}
          {stack.data && <StackMode mode={stack.data.mode} />}
        </Group>
        {stack.data?.mode === "unmanaged" && <Button onClick={adoptDialog.open}>Adopt</Button>}
        {stack.data?.mode === "external" && (
          <Button variant="default" loading={forget.isPending} onClick={() => forget.mutate()}>
            Forget
          </Button>
        )}
      </Group>
      <AdoptStackDialog
        env={env}
        namespace={namespace}
        opened={adopting}
        onClose={adoptDialog.close}
        initialName={name}
      />
      {stack.isPending && <Loader aria-label="Loading" />}
      {stack.isError && (
        <Alert color="red" title="Cannot read the stack">
          {stack.error.message}
        </Alert>
      )}
      {stack.data && (
        <>
          {stack.data.mode !== "unmanaged" && stack.data.composeFile && (
            <StackActions
              env={env}
              namespace={namespace}
              name={name}
              running={stack.data.running > 0}
              readOnly={readOnly}
            />
          )}
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
              Adopt it to manage it here: give the directory with its compose file.
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
