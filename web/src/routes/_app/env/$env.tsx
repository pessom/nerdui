// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Badge, Group, Select, Tooltip } from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { Outlet, createFileRoute, useNavigate } from "@tanstack/react-router";
import { envStatusQuery, environmentsQuery, namespacesQuery } from "../../../api/queries";
import { RuntimeBanners } from "../../../components/RuntimeBanners";
import { useEnvEvents } from "../../../hooks/useEnvEvents";

/**
 * Environment scope (ADR-0006): /env/$env?ns=<namespace>. Hosts the environment and
 * namespace selectors, the live event subscription and runtime banners.
 */
export const Route = createFileRoute("/_app/env/$env")({
  validateSearch: (search: Record<string, unknown>): { ns?: string } => ({
    ns: typeof search.ns === "string" && search.ns !== "" ? search.ns : undefined,
  }),
  component: EnvLayout,
});

function EnvLayout() {
  const { env } = Route.useParams();
  const { ns } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });

  const envs = useQuery(environmentsQuery);
  const status = useQuery(envStatusQuery(env));
  const namespaces = useQuery(namespacesQuery(env));
  const namespace = ns ?? status.data?.defaultNamespace ?? "default";
  const events = useEnvEvents(env, namespace);

  const nsOptions = Array.from(new Set([...(namespaces.data?.namespaces ?? []), namespace]));
  const containerdDown =
    events.containerd === "disconnected" || status.data?.status === "unavailable";

  return (
    <>
      <Group mb="md" gap="sm" wrap="wrap">
        <Select
          aria-label="Environment"
          w={200}
          allowDeselect={false}
          value={env}
          data={(envs.data ?? [{ id: env, name: env }]).map((e) => ({
            value: e.id,
            label: e.name,
          }))}
          onChange={(v) => v && navigate({ to: "/env/$env", params: { env: v }, search: {} })}
        />
        <Select
          aria-label="Namespace"
          w={200}
          allowDeselect={false}
          searchable
          value={namespace}
          data={nsOptions}
          disabled={namespaces.isError}
          onChange={(v) => v && navigate({ search: { ns: v } })}
        />
        <Tooltip label={`Live updates: ${events.stream}`}>
          <Badge variant="dot" color={events.stream === "open" ? "teal" : "gray"}>
            {events.stream === "open" ? "live" : events.stream}
          </Badge>
        </Tooltip>
      </Group>
      <RuntimeBanners runtime={status.data?.runtime} containerdDown={containerdDown} />
      <Outlet />
    </>
  );
}
