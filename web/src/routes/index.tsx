// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { createFileRoute, redirect } from "@tanstack/react-router";

/** "/" goes to the local environment; the _app guard handles authentication. */
export const Route = createFileRoute("/")({
  beforeLoad: () => {
    throw redirect({ to: "/env/$env", params: { env: "local" } });
  },
});
