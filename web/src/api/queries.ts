// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { queryOptions } from "@tanstack/react-query";
import { api, setCsrfToken, unwrap, type components } from "./client";

export type SessionResponse = components["schemas"]["SessionResponse"];
export type EnvironmentStatus = components["schemas"]["EnvironmentStatus"];
export type Environment = components["schemas"]["Environment"];
export type VersionResponse = components["schemas"]["VersionResponse"];

/** Query keys. Everything of an environment lives under ["env", id] (invalidated by events). */
export const keys = {
  me: ["me"] as const,
  jobs: ["jobs"] as const,
  users: ["users"] as const,
  setup: ["setup"] as const,
  version: ["version"] as const,
  environments: ["environments"] as const,
  env: (env: string) => ["env", env] as const,
  envStatus: (env: string) => ["env", env, "status"] as const,
  envInfo: (env: string) => ["env", env, "info"] as const,
  namespaces: (env: string) => ["env", env, "namespaces"] as const,
  containers: (env: string, ns: string) => ["env", env, "containers", ns] as const,
  /** Under containers(env, ns), so container actions refresh it too. */
  images: (env: string, ns: string) => ["env", env, "images", ns] as const,
  containerInspect: (env: string, ns: string, id: string, size: boolean) =>
    ["env", env, "containers", ns, "inspect", id, size] as const,
};

export const meQuery = queryOptions({
  queryKey: keys.me,
  queryFn: async (): Promise<SessionResponse> => {
    const s = await unwrap(api.GET("/api/v1/auth/me"));
    setCsrfToken(s.csrfToken);
    return s;
  },
  retry: false,
  staleTime: 60_000,
});

export const setupQuery = queryOptions({
  queryKey: keys.setup,
  queryFn: () => unwrap(api.GET("/api/v1/setup")),
  retry: false,
});

export const versionQuery = queryOptions({
  queryKey: keys.version,
  queryFn: () => unwrap(api.GET("/api/v1/version")),
  staleTime: 5 * 60_000,
});

export const environmentsQuery = queryOptions({
  queryKey: keys.environments,
  queryFn: async () => (await unwrap(api.GET("/api/v1/environments"))).environments,
  staleTime: 5 * 60_000,
});

export const envStatusQuery = (env: string) =>
  queryOptions({
    queryKey: keys.envStatus(env),
    queryFn: () => unwrap(api.GET("/api/v1/environments/{env}", { params: { path: { env } } })),
  });

export type SystemInfo = components["schemas"]["SystemInfo"];

/** Host and runtime information (#66); changes rarely, so it is not refetched on focus. */
export const envInfoQuery = (env: string) =>
  queryOptions({
    queryKey: keys.envInfo(env),
    queryFn: () =>
      unwrap(api.GET("/api/v1/environments/{env}/info", { params: { path: { env } } })),
    staleTime: 60_000,
    retry: false,
  });

export type UserDetail = components["schemas"]["UserDetail"];

export const usersQuery = queryOptions({
  queryKey: keys.users,
  queryFn: async () => (await unwrap(api.GET("/api/v1/users"))).users,
});

export const namespacesQuery = (env: string) =>
  queryOptions({
    queryKey: keys.namespaces(env),
    queryFn: () =>
      unwrap(api.GET("/api/v1/environments/{env}/namespaces", { params: { path: { env } } })),
    retry: false,
  });

export type Image = components["schemas"]["Image"];
export type Job = components["schemas"]["Job"];
export type JobFrame = components["schemas"]["JobFrame"];

/** Background jobs (#126); polled while any job runs. */
export const jobsQuery = queryOptions({
  queryKey: keys.jobs,
  queryFn: async () => (await unwrap(api.GET("/api/v1/jobs"))).jobs,
  refetchInterval: (q) => (q.state.data?.some((j) => j.status === "running") ? 2000 : false),
});

/** Images of a namespace (#42); refetched on environment events like containers. */
export const imagesQuery = (env: string, namespace: string) =>
  queryOptions({
    queryKey: keys.images(env, namespace),
    queryFn: () =>
      unwrap(
        api.GET("/api/v1/environments/{env}/images", {
          params: { path: { env }, query: { namespace } },
        }),
      ),
  });

export const containersQuery = (env: string, namespace: string) =>
  queryOptions({
    queryKey: keys.containers(env, namespace),
    queryFn: () =>
      unwrap(
        api.GET("/api/v1/environments/{env}/containers", {
          params: { path: { env }, query: { namespace } },
        }),
      ),
  });

export type ContainerInspect = components["schemas"]["ContainerInspect"];

export const containerInspectQuery = (env: string, namespace: string, id: string, size = false) =>
  queryOptions({
    queryKey: keys.containerInspect(env, namespace, id, size),
    queryFn: () =>
      unwrap(
        api.GET("/api/v1/environments/{env}/containers/{id}/inspect", {
          params: { path: { env, id }, query: { namespace, size } },
        }),
      ),
  });
