// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Anchor, Breadcrumbs, Stack, Title } from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, getRouteApi, useNavigate } from "@tanstack/react-router";
import { envStatusQuery } from "../../../../api/queries";
import { RouterAnchor } from "../../../../components/RouterAnchor";
import { CreateContainerForm } from "../../../../features/containers/CreateContainerForm";

const envRoute = getRouteApi("/_app/env/$env");

/** Create a container from a form (#17). */
export const Route = createFileRoute("/_app/env/$env/containers_/new")({
  component: NewContainerPage,
});

function NewContainerPage() {
  const { env } = Route.useParams();
  const { ns } = envRoute.useSearch();
  const navigate = useNavigate();
  const status = useQuery(envStatusQuery(env));
  const namespace = ns ?? status.data?.defaultNamespace ?? "default";
  const nerdctl = status.data?.runtime?.nerdctl;
  const readOnly = !!nerdctl && !nerdctl.compatible && !nerdctl.checkSkipped;
  return (
    <Stack>
      <Breadcrumbs>
        <RouterAnchor to="/env/$env/containers" params={{ env }} search={{ ns }}>
          Containers
        </RouterAnchor>
        <Anchor component="span" c="dimmed">
          New
        </Anchor>
      </Breadcrumbs>
      <Title order={2}>New container in {namespace}</Title>
      <CreateContainerForm
        env={env}
        namespace={namespace}
        readOnly={readOnly}
        onStarted={() =>
          void navigate({ to: "/env/$env/containers", params: { env }, search: { ns } })
        }
      />
    </Stack>
  );
}
