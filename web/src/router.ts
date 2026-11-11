// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { QueryClient } from "@tanstack/react-query";
import { createRouter } from "@tanstack/react-router";
import { ApiError } from "./api/client";
import { routeTree } from "./routeTree.gen";

export interface RouterContext {
  queryClient: QueryClient;
}

export function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        // Live updates come from containerd events (ADR-0007), not from polling.
        staleTime: 30_000,
        refetchOnWindowFocus: true,
        retry: (count, err) =>
          !(err instanceof ApiError && err.status >= 400 && err.status < 500) && count < 2,
      },
    },
  });
}

export function createAppRouter(queryClient: QueryClient) {
  return createRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: "intent",
    defaultPreloadStaleTime: 0,
    scrollRestoration: true,
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}
