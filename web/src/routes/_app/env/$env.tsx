// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Badge, Group, Select, Tooltip } from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { Outlet, createFileRoute, useLocation, useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";
import {
  envStatusQuery,
  environmentsQuery,
  namespacesQuery,
  versionQuery,
} from "../../../api/queries";
import { RuntimeBanners } from "../../../components/RuntimeBanners";
import { useEnvEvents } from "../../../hooks/useEnvEvents";
import { ALL_NAMESPACES, rememberNamespace, rememberedNamespace } from "../../../lib/namespace";

/**
 * Environment scope (ADR-0006): /env/$env?ns=<namespace>. Hosts the environment and
 * namespace selectors, the live event subscription and runtime banners.
 *
 * Choosing a namespace stays on the current page and only changes ?ns=. The choice is
 * remembered per environment in this browser and restored when ?ns= is absent.
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
  const navigate = useNavigate();
  const { pathname } = useLocation();
  // Host-wide pages (overview, registries, settings, ports) have no namespace selector.
  const envPath = `/env/${encodeURIComponent(env)}`;
  const page = pathname.replace(/\/$/, "");
  const overview =
    page === envPath ||
    page === `${envPath}/registries` ||
    page === `${envPath}/settings` ||
    page === `${envPath}/ports`;
  // Only the container list can show every namespace (#276).
  const allAllowed = page === `${envPath}/containers`;
  const leavingAll = ns === ALL_NAMESPACES && !allAllowed;

  const envs = useQuery(environmentsQuery);
  const status = useQuery(envStatusQuery(env));
  const version = useQuery(versionQuery);
  const namespaces = useQuery(namespacesQuery(env));
  const namespace = ns ?? status.data?.defaultNamespace ?? "default";
  const events = useEnvEvents(env, namespace);

  const nsOptions = [
    ...(allAllowed ? [{ value: ALL_NAMESPACES, label: "<All namespaces>" }] : []),
    ...Array.from(
      new Set(
        [...(namespaces.data?.namespaces ?? []), namespace].filter((n) => n !== ALL_NAMESPACES),
      ),
    ).map((n) => ({ value: n, label: n })),
  ];

  // Other pages show one namespace: leaving the list with "all" goes back to the remembered one.
  useEffect(() => {
    if (!leavingAll) return;
    void navigate({
      to: ".",
      search: (prev) => ({ ...prev, ns: rememberedNamespace(env) }),
      replace: true,
    });
  }, [leavingAll, env, navigate]);

  // Restore the remembered namespace when the URL has none and it still exists.
  const known = namespaces.data?.namespaces;
  useEffect(() => {
    if (ns || !known) return;
    const saved = rememberedNamespace(env);
    if (!saved) return;
    if (!known.includes(saved)) {
      rememberNamespace(env, undefined);
      return;
    }
    void navigate({ to: ".", search: (prev) => ({ ...prev, ns: saved }), replace: true });
  }, [env, ns, known, navigate]);

  const selectNamespace = (v: string) => {
    if (v !== ALL_NAMESPACES) rememberNamespace(env, v);
    void navigate({ to: ".", search: (prev) => ({ ...prev, ns: v }) });
  };
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
        {!overview && (
          <Select
            aria-label="Namespace"
            w={200}
            allowDeselect={false}
            searchable
            value={namespace}
            data={nsOptions}
            disabled={namespaces.isError}
            onChange={(v) => v && selectNamespace(v)}
          />
        )}
        <Tooltip label={`Live updates: ${events.stream}`}>
          <Badge variant="dot" color={events.stream === "open" ? "teal" : "gray"}>
            {events.stream === "open" ? "live" : events.stream}
          </Badge>
        </Tooltip>
      </Group>
      <RuntimeBanners
        runtime={status.data?.runtime}
        containerdDown={containerdDown}
        acme={version.data?.acme}
        warnings={status.data?.warnings}
      />
      {!leavingAll && <Outlet />}
    </>
  );
}
