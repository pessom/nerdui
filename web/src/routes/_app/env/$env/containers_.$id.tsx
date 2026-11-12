// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Badge,
  Breadcrumbs,
  Checkbox,
  Group,
  Loader,
  Stack,
  Tabs,
  Text,
  Title,
} from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, getRouteApi } from "@tanstack/react-router";
import { Suspense, lazy } from "react";
import { ApiError } from "../../../../api/client";
import { RouterAnchor } from "../../../../components/RouterAnchor";
import { containerInspectQuery, containersQuery, envStatusQuery } from "../../../../api/queries";
import { useContainerActions } from "../../../../features/containers/actions";
import { RowActions } from "../../../../features/containers/ContainerActions";
import { ContainerOverview } from "../../../../features/containers/ContainerOverview";
import { displayName, stateColor, statusText } from "../../../../features/containers/format";
import { inspectJSON } from "../../../../features/containers/inspect";
import { useActionRequests } from "../../../../features/containers/useActionRequests";

const JsonViewer = lazy(() => import("../../../../features/containers/JsonViewer"));

const envRoute = getRouteApi("/_app/env/$env");

type Tab = "overview" | "inspect";

/** Container page (#13): overview of key fields and the full inspect JSON; logs follow in #14. */
export const Route = createFileRoute("/_app/env/$env/containers_/$id")({
  validateSearch: (search: Record<string, unknown>): { tab?: Tab; size?: boolean } => ({
    tab: search.tab === "inspect" ? "inspect" : undefined,
    size: search.size === true || search.size === "true" ? true : undefined,
  }),
  component: ContainerPage,
});

function ContainerPage() {
  const { env, id } = Route.useParams();
  const { tab = "overview", size = false } = Route.useSearch();
  const navigate = Route.useNavigate();
  const { ns } = envRoute.useSearch();
  const status = useQuery(envStatusQuery(env));
  const namespace = ns ?? status.data?.defaultNamespace ?? "default";
  const inspect = useQuery(containerInspectQuery(env, namespace, id, size));
  const list = useQuery(containersQuery(env, namespace));
  const summary = list.data?.containers.find((c) => c.id === id);
  const actions = useContainerActions(env, namespace);
  const nerdctl = status.data?.runtime?.nerdctl;
  const readOnly = !!nerdctl && !nerdctl.compatible && !nerdctl.checkSkipped;
  const tableActions = {
    ...actions,
    disabled: readOnly ? "Read-only mode: host nerdctl is incompatible" : undefined,
  };
  const { request, dialog } = useActionRequests(tableActions);
  const gone = inspect.error instanceof ApiError && inspect.error.status === 404;
  const name = summary ? displayName(summary) : inspect.data?.Name || id.slice(0, 12);

  return (
    <Stack>
      <Breadcrumbs>
        <RouterAnchor to="/env/$env/containers" params={{ env }} search={{ ns }}>
          Containers
        </RouterAnchor>
        <Text>{name}</Text>
      </Breadcrumbs>
      <Group justify="space-between">
        <Group gap="sm">
          <Title order={2}>{name}</Title>
          {summary && (
            <Badge color={stateColor[summary.state]} variant="light">
              {statusText(summary)}
            </Badge>
          )}
          {inspect.isFetching && !inspect.isPending && <Loader size="xs" aria-label="Refreshing" />}
        </Group>
        {summary && <RowActions container={summary} actions={tableActions} request={request} />}
      </Group>
      {dialog}

      {inspect.isPending && <Loader aria-label="Loading" />}
      {gone && (
        <Alert color="gray" title="Container not found">
          It may have been removed.{" "}
          <RouterAnchor to="/env/$env/containers" params={{ env }} search={{ ns }}>
            Back to containers
          </RouterAnchor>
        </Alert>
      )}
      {inspect.isError && !gone && (
        <Alert color="red" title="Cannot inspect container">
          {inspect.error.message}
        </Alert>
      )}

      {inspect.data && (
        <Tabs
          value={tab}
          onChange={(t) =>
            void navigate({
              search: (s) => ({ ...s, tab: t === "inspect" ? "inspect" : undefined }),
              replace: true,
            })
          }
          keepMounted={false}
        >
          <Tabs.List mb="md">
            <Tabs.Tab value="overview">Overview</Tabs.Tab>
            <Tabs.Tab value="inspect">Inspect</Tabs.Tab>
          </Tabs.List>
          <Tabs.Panel value="overview">
            <ContainerOverview inspect={inspect.data} />
          </Tabs.Panel>
          <Tabs.Panel value="inspect">
            <Stack gap="xs">
              <Checkbox
                label="Include sizes (SizeRw, SizeRootFs; slow for large containers)"
                checked={size}
                onChange={(e) =>
                  void navigate({
                    search: (s) => ({ ...s, size: e.currentTarget.checked || undefined }),
                    replace: true,
                  })
                }
              />
              <Suspense fallback={<Loader aria-label="Loading viewer" />}>
                <JsonViewer value={inspectJSON(inspect.data)} label={`Inspect ${name}`} />
              </Suspense>
            </Stack>
          </Tabs.Panel>
        </Tabs>
      )}
    </Stack>
  );
}
