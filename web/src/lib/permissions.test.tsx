// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Container } from "../features/containers/format";
import { renderApp, signedIn } from "../test/render";
import { SilentWebSocket, fakeApi, json, roles, viewerSession } from "../test/server";

const adhoc: Container = {
  id: "3".repeat(64),
  name: "adhoc",
  namespace: "default",
  image: "busybox",
  command: "sleep 300",
  state: "running",
  createdAt: "2027-03-01T11:30:00Z",
  ports: [],
  labels: {},
};

const operator = {
  ...viewerSession,
  user: { ...viewerSession.user, role: "operator" },
  permissions: roles.find((r) => r.name === "operator")!.permissions,
};

const containers = {
  "GET /api/v1/environments/local/containers": () =>
    json(200, { namespace: "default", containers: [adhoc] }),
};

// The UI hides what the role may not do (ADR-0058); the server checks every request anyway.
describe("permissions in the UI", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("shows a viewer containers without actions", async () => {
    fakeApi({ ...signedIn, ...containers, "GET /api/v1/auth/me": () => json(200, viewerSession) });
    renderApp("/env/local/containers");

    expect(await screen.findByText("adhoc")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "New container" })).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Actions for adhoc")).not.toBeInTheDocument();
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  });

  it("lets an operator stop a container but not remove it", async () => {
    fakeApi({ ...signedIn, ...containers, "GET /api/v1/auth/me": () => json(200, operator) });
    renderApp("/env/local/containers");

    fireEvent.click(await screen.findByLabelText("Actions for adhoc"));
    expect(await screen.findByRole("menuitem", { name: "Stop" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: /Remove/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "New container" })).not.toBeInTheDocument();
  });

  it("shows a viewer stacks without the buttons that create them", async () => {
    fakeApi({
      ...signedIn,
      "GET /api/v1/auth/me": () => json(200, viewerSession),
      "GET /api/v1/environments/local/stacks": () =>
        json(200, { namespace: "default", stacks: [] }),
    });
    renderApp("/env/local/stacks");

    expect(await screen.findByRole("heading", { name: "Stacks" })).toBeInTheDocument();
    for (const name of ["New stack", "From Git", "Adopt existing", "From template"]) {
      expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
    }
  });
});
