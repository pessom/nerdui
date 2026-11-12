// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Group, Loader, Title } from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, getRouteApi } from "@tanstack/react-router";
import { containersQuery, envStatusQuery } from "../../../../api/queries";
import { useContainerActions } from "../../../../features/containers/actions";
import { ContainersTable } from "../../../../features/containers/ContainersTable";

const envRoute = getRouteApi("/_app/env/$env");

/** Container list (#10): live via environment events, filters and grouping client-side. */
export const Route = createFileRoute("/_app/env/$env/containers")({
  component: ContainersPage,
});

function ContainersPage() {
  const { env } = Route.useParams();
  const { ns } = envRoute.useSearch();
  const status = useQuery(envStatusQuery(env));
  const namespace = ns ?? status.data?.defaultNamespace ?? "default";
  const list = useQuery(containersQuery(env, namespace));
  const { run, busy } = useContainerActions(env, namespace);
  const nerdctl = status.data?.runtime?.nerdctl;
  // Read-only mode (ADR-0011): the server would answer 503 nerdctl_incompatible.
  const readOnly = !!nerdctl && !nerdctl.compatible && !nerdctl.checkSkipped;

  return (
    <>
      <Group justify="space-between" mb="md">
        <Title order={2}>Containers</Title>
        {list.isFetching && !list.isPending && <Loader size="xs" aria-label="Refreshing" />}
      </Group>
      {list.isPending && <Loader aria-label="Loading" />}
      {list.isError && (
        <Alert color="red" title="Cannot list containers">
          {list.error.message}
        </Alert>
      )}
      {list.data && (
        <ContainersTable
          containers={list.data.containers}
          actions={{
            run,
            busy,
            disabled: readOnly ? "Read-only mode: host nerdctl is incompatible" : undefined,
          }}
        />
      )}
    </>
  );
}
