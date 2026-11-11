// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Group, Loader, Title } from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, getRouteApi } from "@tanstack/react-router";
import { containersQuery, envStatusQuery } from "../../../../api/queries";
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
      {list.data && <ContainersTable containers={list.data.containers} />}
    </>
  );
}
