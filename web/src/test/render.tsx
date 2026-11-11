// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { createMemoryHistory, createRouter } from "@tanstack/react-router";
import { render } from "@testing-library/react";
import { App } from "../App";
import { createQueryClient } from "../router";
import { routeTree } from "../routeTree.gen";
import { envStatus, json, session, version } from "./server";

/** Renders the whole app (router, query client, Mantine) at path. */
export function renderApp(path: string) {
  const queryClient = createQueryClient();
  queryClient.setDefaultOptions({ queries: { retry: false } });
  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  render(<App queryClient={queryClient} router={router} />);
  return { router, queryClient };
}

/** API handlers for a signed-in admin on the "local" environment. */
export const signedIn = {
  "GET /api/v1/auth/me": () => json(200, session),
  "GET /api/v1/setup": () => json(200, { required: false }),
  "GET /api/v1/version": () => json(200, version),
  "GET /api/v1/environments": () =>
    json(200, {
      environments: [
        { id: "local", name: "Local host", kind: "local", defaultNamespace: "default" },
      ],
    }),
  "GET /api/v1/environments/local": () => json(200, envStatus),
  "GET /api/v1/environments/local/namespaces": () =>
    json(200, { namespaces: ["default", "k8s.io"], default: "default" }),
};
