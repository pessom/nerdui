// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Container } from "../containers/format";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";

const kuma = {
  source: "builtin",
  id: "uptime-kuma",
  name: "Uptime Kuma",
  description: "Monitoring",
  categories: ["monitoring"],
};
const compose = "services:\n  kuma:\n    image: louislam/uptime-kuma:1\n";

describe("container templates (ADR-0059)", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("deploys a one-service template as a container through the form", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/templates": () => json(200, { templates: [kuma], errors: [] }),
      "GET /api/v1/templates/detail": () =>
        json(200, {
          ...kuma,
          compose,
          env: "PORT=3001\n",
          variables: [{ name: "PORT", default: "3001", secret: false }],
          services: ["kuma"],
        }),
      "POST /api/v1/templates/container": () =>
        json(200, {
          spec: {
            image: "louislam/uptime-kuma:1",
            name: "status",
            ports: [{ hostPort: 3002, containerPort: 3001 }],
            mounts: [{ type: "volume", source: "status-data", target: "/app/data" }],
            restart: "unless-stopped",
          },
          unsupported: ["sysctls"],
        }),
      "POST /api/v1/environments/local/containers/command": () =>
        json(200, { command: "nerdctl create …" }),
    });
    const { router } = renderApp("/env/local/templates");
    fireEvent.click(await screen.findByRole("button", { name: "Deploy Uptime Kuma" }));
    const dialog = await screen.findByRole("dialog", { name: "Deploy Uptime Kuma" });
    fireEvent.click(await within(dialog).findByText("Container"));
    const name = within(dialog).getByLabelText(/^Container name/);
    fireEvent.change(name, { target: { value: "status" } });
    fireEvent.change(within(dialog).getByLabelText("PORT"), { target: { value: "3002" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Continue" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/env/local/containers/new"));
    expect(calls.find((c) => c.path === "/api/v1/templates/container")?.body).toEqual({
      compose,
      values: { PORT: "3002" },
      name: "status",
    });
    expect(await screen.findByText("From template Uptime Kuma")).toBeInTheDocument();
    expect(screen.getByText("sysctls")).toBeInTheDocument();
    expect(screen.getByLabelText(/^Image/)).toHaveValue("louislam/uptime-kuma:1");
    expect(router.state.location.href).not.toContain("3002");
  });

  it("saves a container as a template", async () => {
    const id = "c".repeat(64);
    const base = "/api/v1/environments/local/containers";
    const summary: Container = {
      id,
      name: "web",
      namespace: "default",
      image: "nginx:1",
      command: "nginx",
      state: "running",
      createdAt: "2027-03-01T10:00:00Z",
      ports: [],
      labels: {},
    };
    const { calls } = fakeApi({
      ...signedIn,
      [`GET ${base}`]: () => json(200, { namespace: "default", containers: [summary] }),
      [`GET ${base}/${id}/inspect`]: () => json(200, { Id: id, Name: "web" }),
      [`GET ${base}/${id}/template`]: () =>
        json(200, {
          compose: "services:\n  web:\n    image: nginx:1\n",
          env: "# TOKEN of the container (secret)\nTOKEN=\n",
          unsupported: ["static IP address 10.4.0.9"],
        }),
      "POST /api/v1/templates": (c) =>
        json(201, { ...(c.body as object), source: "custom", id: "1" }),
    });
    renderApp(`/env/local/containers/${id}?ns=default`);
    fireEvent.click(await screen.findByRole("button", { name: "Save as template" }));
    const dialog = await screen.findByRole("dialog", { name: "Save as template" });
    expect(within(dialog).getByText(/static IP address/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: /Save/ }));
    await waitFor(() =>
      expect(
        calls.find((c) => c.method === "POST" && c.path === "/api/v1/templates")?.body,
      ).toEqual({
        name: "web",
        description: "",
        compose: "services:\n  web:\n    image: nginx:1\n",
        env: "# TOKEN of the container (secret)\nTOKEN=\n",
      }),
    );
  });
});
