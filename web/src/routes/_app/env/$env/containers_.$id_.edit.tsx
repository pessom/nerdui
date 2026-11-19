// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Anchor, Breadcrumbs, List, Loader, Stack, Text, Title } from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, getRouteApi, useNavigate } from "@tanstack/react-router";
import { api, unwrap } from "../../../../api/client";
import { envStatusQuery } from "../../../../api/queries";
import { RouterAnchor } from "../../../../components/RouterAnchor";
import { CreateContainerForm } from "../../../../features/containers/CreateContainerForm";
import { fromRequest } from "../../../../features/containers/create";

const envRoute = getRouteApi("/_app/env/$env");

/** Edit a container's settings and recreate it under the same name (#18, ADR-0031). */
export const Route = createFileRoute("/_app/env/$env/containers_/$id_/edit")({
  component: EditContainerPage,
});

function EditContainerPage() {
  const { env, id } = Route.useParams();
  const { ns } = envRoute.useSearch();
  const navigate = useNavigate();
  const status = useQuery(envStatusQuery(env));
  const namespace = ns ?? status.data?.defaultNamespace ?? "default";
  const spec = useQuery({
    queryKey: ["container-spec", env, namespace, id],
    queryFn: () =>
      unwrap(
        api.GET("/api/v1/environments/{env}/containers/{id}/spec", {
          params: { path: { env, id }, query: { namespace } },
        }),
      ),
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });
  const nerdctl = status.data?.runtime?.nerdctl;
  const readOnly = !!nerdctl && !nerdctl.compatible && !nerdctl.checkSkipped;
  const name = spec.data?.spec.name || id.slice(0, 12);
  const blocked = !!spec.data && (!!spec.data.stack || spec.data.unsupported.length > 0);
  return (
    <Stack>
      <Breadcrumbs>
        <RouterAnchor to="/env/$env/containers" params={{ env }} search={{ ns }}>
          Containers
        </RouterAnchor>
        <RouterAnchor to="/env/$env/containers/$id" params={{ env, id }} search={{ ns }}>
          {name}
        </RouterAnchor>
        <Anchor component="span" c="dimmed">
          Edit
        </Anchor>
      </Breadcrumbs>
      <Title order={2}>Edit {name}</Title>
      {spec.isPending && <Loader aria-label="Loading settings" />}
      {spec.isError && (
        <Alert color="red" title="Cannot read the container's settings">
          {spec.error.message}
        </Alert>
      )}
      {spec.data?.stack && (
        <Alert color="blue" title="Managed by a stack">
          This container belongs to the compose stack <b>{spec.data.stack}</b>: edit the stack file
          and deploy it instead.
        </Alert>
      )}
      {spec.data && spec.data.unsupported.length > 0 && (
        <Alert color="orange" title="Cannot be recreated here">
          The container has settings the form cannot express; recreating would lose them:
          <List size="sm" mt={4}>
            {spec.data.unsupported.map((u) => (
              <List.Item key={u}>{u}</List.Item>
            ))}
          </List>
        </Alert>
      )}
      {spec.data && !blocked && (
        <>
          <Text size="sm" c="dimmed">
            Saving stops the container, creates a new one with these settings under the same name
            {spec.data.running ? " and starts it" : ""}, then removes the old one. If the new
            container cannot be created or started, the old one is restored.
          </Text>
          {spec.data.imageChanged && (
            <Alert color="yellow">
              The image tag now points to a newer image than this container was created from.
              Environment variables that matched the old image&apos;s defaults are listed
              explicitly; remove those you want the new image to set.
            </Alert>
          )}
          <CreateContainerForm
            env={env}
            namespace={namespace}
            readOnly={readOnly}
            recreate={{ id: spec.data.id, initial: fromRequest(spec.data.spec) }}
            onStarted={() =>
              void navigate({ to: "/env/$env/containers", params: { env }, search: { ns } })
            }
          />
        </>
      )}
    </Stack>
  );
}
