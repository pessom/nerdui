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
  Tabs,
  Text,
  Title,
} from "@mantine/core";
import { useDisclosure } from "@mantine/hooks";
import { notifications } from "@mantine/notifications";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, getRouteApi, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { IconTemplate, IconTrash } from "@tabler/icons-react";
import { api, unwrap, unwrapEmpty } from "../../../../api/client";
import { TemplateDialog, type TemplateDraft } from "../../../../features/templates/TemplateDialog";
import { exampleFromEnv } from "../../../../features/templates/templates";
import { envStatusQuery, keys, stackQuery } from "../../../../api/queries";
import { RouterAnchor } from "../../../../components/RouterAnchor";
import { AdoptStackDialog } from "../../../../features/stacks/AdoptStackDialog";
import { DeleteStackDialog } from "../../../../features/stacks/DeleteStackDialog";
import { GitStackPanel } from "../../../../features/stacks/GitStackPanel";
import { ServiceActions } from "../../../../features/stacks/ServiceActions";
import { StackActions } from "../../../../features/stacks/StackActions";
import { StackFilePanel } from "../../../../features/stacks/StackFilePanel";
import { StackLogsPanel } from "../../../../features/stacks/StackLogsPanel";
import { StackMode, StackStatus } from "../../../../features/stacks/StackStatus";
import { VariablesEditor } from "../../../../features/variables/VariablesEditor";

const envRoute = getRouteApi("/_app/env/$env");

type Tab = "services" | "logs" | "compose" | "env" | "variables";
const isTab = (t: unknown): t is Tab =>
  t === "services" || t === "logs" || t === "compose" || t === "env" || t === "variables";

/**
 * A stack: operations (#28), services and containers (#24), its compose and .env files (#26),
 * its variables (#35).
 */
export const Route = createFileRoute("/_app/env/$env/stacks_/$name")({
  validateSearch: (search: Record<string, unknown>): { tab?: Tab } => ({
    tab: isTab(search.tab) && search.tab !== "services" ? search.tab : undefined,
  }),
  component: StackPage,
});

function StackPage() {
  const { env, name } = Route.useParams();
  const { tab = "services" } = Route.useSearch();
  const { ns } = envRoute.useSearch();
  const status = useQuery(envStatusQuery(env));
  const namespace = ns ?? status.data?.defaultNamespace ?? "default";
  const nerdctl = status.data?.runtime?.nerdctl;
  const readOnly = !!nerdctl && !nerdctl.compatible && !nerdctl.checkSkipped;
  const stack = useQuery(stackQuery(env, namespace, name));
  const [adopting, adoptDialog] = useDisclosure();
  const [deleting, deleteDialog] = useDisclosure();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [templateDraft, setTemplateDraft] = useState<TemplateDraft | null>(null);
  // Save as template (#34): the compose file and the .env with secret-looking values emptied.
  const saveTemplate = useMutation({
    mutationFn: async () => {
      const file = (kind: "compose" | "env") =>
        unwrap(
          api.GET("/api/v1/environments/{env}/stacks/{name}/files/{file}", {
            params: { path: { env, name, file: kind } },
          }),
        );
      const [compose, dotenv] = await Promise.all([file("compose"), file("env")]);
      return { compose: compose.content, env: exampleFromEnv(dotenv.content) };
    },
    onSuccess: (f) => setTemplateDraft({ name, description: "", ...f }),
    onError: (e) =>
      notifications.show({
        color: "red",
        title: "Cannot read the stack files",
        message: e.message,
      }),
  });
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
        <Group gap="xs">
          {stack.data?.mode === "unmanaged" && <Button onClick={adoptDialog.open}>Adopt</Button>}
          {stack.data && stack.data.mode !== "unmanaged" && stack.data.composeFile && (
            <Button
              variant="default"
              leftSection={<IconTemplate size={16} />}
              loading={saveTemplate.isPending}
              onClick={() => saveTemplate.mutate()}
            >
              Save as template
            </Button>
          )}
          {stack.data?.mode === "external" && (
            <Button variant="default" loading={forget.isPending} onClick={() => forget.mutate()}>
              Forget
            </Button>
          )}
          {stack.data?.mode === "managed" && (
            <Button
              variant="light"
              color="red"
              leftSection={<IconTrash size={16} />}
              disabled={readOnly}
              onClick={deleteDialog.open}
            >
              Delete
            </Button>
          )}
        </Group>
      </Group>
      <TemplateDialog draft={templateDraft} onClose={() => setTemplateDraft(null)} />
      <DeleteStackDialog
        env={env}
        namespace={namespace}
        name={name}
        ns={ns}
        opened={deleting}
        onClose={deleteDialog.close}
      />
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
          {stack.data.git && (
            <GitStackPanel
              env={env}
              namespace={namespace}
              name={name}
              git={stack.data.git}
              containers={stack.data.containers}
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
          {(() => {
            const editable = stack.data.mode !== "unmanaged" && !!stack.data.composeFile;
            const shown =
              !editable && (tab === "compose" || tab === "env" || tab === "variables")
                ? "services"
                : tab;
            const ids = stack.data.services
              .flatMap((s) => s.containers.map((c) => c.id))
              .sort()
              .join(",");
            return (
              <Tabs
                value={shown}
                onChange={(t) =>
                  void navigate({
                    to: ".",
                    search: (s) => ({ ...s, tab: isTab(t) && t !== "services" ? t : undefined }),
                  })
                }
              >
                <Tabs.List mb="md">
                  <Tabs.Tab value="services">Services</Tabs.Tab>
                  <Tabs.Tab value="logs">Logs</Tabs.Tab>
                  {editable && <Tabs.Tab value="compose">{stack.data.composeFile}</Tabs.Tab>}
                  {editable && <Tabs.Tab value="env">.env</Tabs.Tab>}
                  {editable && <Tabs.Tab value="variables">Variables</Tabs.Tab>}
                </Tabs.List>
                <Tabs.Panel value="services">
                  <MStack>{services(stack.data.services)}</MStack>
                </Tabs.Panel>
                <Tabs.Panel value="logs">
                  {shown === "logs" && (
                    <StackLogsPanel
                      env={env}
                      name={name}
                      namespace={namespace}
                      services={stack.data.services.map((s) => s.name)}
                      containerIds={ids}
                    />
                  )}
                </Tabs.Panel>
                <Tabs.Panel value="compose">
                  {shown === "compose" && (
                    <StackFilePanel
                      env={env}
                      namespace={namespace}
                      name={name}
                      kind="compose"
                      readOnly={readOnly}
                      fromGit={stack.data.mode === "git"}
                    />
                  )}
                </Tabs.Panel>
                <Tabs.Panel value="env">
                  {shown === "env" && (
                    <StackFilePanel
                      env={env}
                      namespace={namespace}
                      name={name}
                      kind="env"
                      readOnly={readOnly}
                    />
                  )}
                </Tabs.Panel>
                <Tabs.Panel value="variables">
                  {shown === "variables" && (
                    <MStack gap="xs">
                      <Text size="sm" c="dimmed">
                        Passed to <Code>nerdctl compose</Code> of this stack as environment
                        variables: use them as <Code>{"${NAME}"}</Code>. They win over global
                        variables and <Code>.env</Code>, and apply on the next deploy.
                      </Text>
                      <VariablesEditor scope={{ kind: "stack", env, name }} />
                    </MStack>
                  )}
                </Tabs.Panel>
              </Tabs>
            );
          })()}
        </>
      )}
    </MStack>
  );

  function services(list: NonNullable<typeof stack.data>["services"]) {
    const manageable = !readOnly && stack.data?.mode !== "unmanaged";
    return list.map((svc) => (
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
          {manageable && (
            <ServiceActions
              env={env}
              namespace={namespace}
              stack={name}
              service={svc.name}
              running={svc.running > 0}
              containers={svc.containers.length > 0}
              composeFile={svc.defined && !!stack.data?.composeFile}
            />
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
    ));
  }
}
