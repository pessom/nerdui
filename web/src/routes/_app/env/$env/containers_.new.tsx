// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Anchor, Breadcrumbs, List, Stack, Text, Title } from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, getRouteApi, useNavigate } from "@tanstack/react-router";
import { envStatusQuery } from "../../../../api/queries";
import { RouterAnchor } from "../../../../components/RouterAnchor";
import { useState } from "react";
import { CreateContainerForm } from "../../../../features/containers/CreateContainerForm";
import { takeContainerDraft } from "../../../../features/containers/draft";

const envRoute = getRouteApi("/_app/env/$env");

/** Create a container from a form (#17), also filled in from a template (ADR-0059). */
export const Route = createFileRoute("/_app/env/$env/containers_/new")({
  component: NewContainerPage,
});

function NewContainerPage() {
  const { env } = Route.useParams();
  const { ns } = envRoute.useSearch();
  const navigate = useNavigate();
  const [draft] = useState(takeContainerDraft);
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
      {draft && (
        <Alert color="blue" title={`From ${draft.origin}`}>
          <Text size="sm">Review the settings, then create the container.</Text>
          {draft.unsupported.length > 0 && (
            <>
              <Text size="sm" mt="xs">
                These settings of the template are not in the form and will be missing:
              </Text>
              <List size="sm">
                {draft.unsupported.map((u) => (
                  <List.Item key={u}>{u}</List.Item>
                ))}
              </List>
            </>
          )}
        </Alert>
      )}
      <CreateContainerForm
        env={env}
        namespace={namespace}
        readOnly={readOnly}
        initial={draft?.form}
        onStarted={() =>
          void navigate({ to: "/env/$env/containers", params: { env }, search: { ns } })
        }
      />
    </Stack>
  );
}
