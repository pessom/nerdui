// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Button, Group, Loader, Title } from "@mantine/core";
import { IconPlus } from "@tabler/icons-react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { createFileRoute, getRouteApi, useNavigate } from "@tanstack/react-router";
import {
  containersQuery,
  envStatusQuery,
  imageUpdatesQuery,
  updatesByRef,
} from "../../../../api/queries";
import { useAllowedActions, useContainerActions } from "../../../../features/containers/actions";
import { ContainersTable } from "../../../../features/containers/ContainersTable";
import { ImageUpdatesContext } from "../../../../features/updates/context";
import { ALL_NAMESPACES } from "../../../../lib/namespace";
import { useCan } from "../../../../lib/permissions";

const envRoute = getRouteApi("/_app/env/$env");

/** Container list (#10): live via environment events, filters and grouping client-side. */
export const Route = createFileRoute("/_app/env/$env/containers")({
  component: ContainersPage,
});

function ContainersPage() {
  const { env } = Route.useParams();
  const { ns } = envRoute.useSearch();
  const navigate = useNavigate();
  const status = useQuery(envStatusQuery(env));
  const namespace = ns ?? status.data?.defaultNamespace ?? "default";
  const list = useQuery(containersQuery(env, namespace));
  // Image updates are per namespace: with all namespaces, those of each listed one (#276).
  const all = namespace === ALL_NAMESPACES;
  const updateNamespaces = all
    ? [...new Set((list.data?.containers ?? []).map((c) => c.namespace))].sort()
    : [namespace];
  const updates = useQueries({
    queries: updateNamespaces.map((n) => imageUpdatesQuery(env, n)),
    combine: (results) => new Map(results.flatMap((r) => [...updatesByRef(r.data)])),
  });
  const { run, busy } = useContainerActions(env, namespace);
  const can = useCan();
  const allowed = useAllowedActions();
  const nerdctl = status.data?.runtime?.nerdctl;
  // Read-only mode (ADR-0011): the server would answer 503 nerdctl_incompatible.
  const readOnly = !!nerdctl && !nerdctl.compatible && !nerdctl.checkSkipped;

  return (
    <>
      <Group justify="space-between" mb="md">
        <Group gap="xs">
          <Title order={2}>Containers</Title>
          {list.isFetching && !list.isPending && <Loader size="xs" aria-label="Refreshing" />}
        </Group>
        {can("containers:write") && (
          <Button
            onClick={() =>
              void navigate({
                to: "/env/$env/containers/new",
                params: { env },
                // A new container goes to one namespace: the default when all are listed.
                search: { ns: ns === ALL_NAMESPACES ? undefined : ns },
              })
            }
            leftSection={<IconPlus size={16} />}
            disabled={readOnly}
          >
            New container
          </Button>
        )}
      </Group>
      {list.isPending && <Loader aria-label="Loading" />}
      {list.isError && (
        <Alert color="red" title="Cannot list containers">
          {list.error.message}
        </Alert>
      )}
      {list.data && (
        <ImageUpdatesContext.Provider value={updates}>
          <ContainersTable
            containers={list.data.containers}
            allNamespaces={all}
            actions={
              allowed.length > 0
                ? {
                    run,
                    busy,
                    disabled: readOnly ? "Read-only mode: host nerdctl is incompatible" : undefined,
                  }
                : undefined
            }
          />
        </ImageUpdatesContext.Provider>
      )}
    </>
  );
}
