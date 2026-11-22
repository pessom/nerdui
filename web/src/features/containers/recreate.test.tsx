// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";
import { fromRequest, joinArgs, parseArgs, toRequest, type CreateRequest } from "./create";

const id = "a".repeat(64);
const spec: CreateRequest = {
  image: "docker.io/library/nginx:1.27",
  name: "web",
  command: ["sh", "-c", "echo 'hi there'"],
  ports: [{ hostPort: 8080, containerPort: 80, protocol: "tcp" }],
  env: [{ name: "TZ", value: "UTC" }],
  restart: "on-failure:3",
  capAdd: ["NET_ADMIN"],
  labels: { app: "web" },
};

describe("container settings round trip", () => {
  it("turns a container's settings into the form and back", () => {
    for (const args of [
      ["a", "b c", "it's", ""],
      ["sh", "-c", 'echo "$X"'],
    ]) {
      expect(parseArgs(joinArgs(args)).args).toEqual(args);
    }
    const { request } = toRequest(fromRequest(spec));
    expect(request).toEqual({ ...spec, start: true });
  });

  it("maps the auto-update label to the switch", () => {
    const auto = { ...spec, labels: { app: "web", "nerdui.auto-update": "true" } };
    const form = fromRequest(auto);
    expect(form.autoUpdate).toBe(true);
    expect(form.labels).toEqual([{ key: "app", value: "web" }]);
    expect(toRequest(form).request.labels).toEqual(auto.labels);
    expect(toRequest({ ...form, autoUpdate: false }).request.labels).toEqual({ app: "web" });
  });

  it("keeps the healthcheck and the auto-heal label", () => {
    const healed: CreateRequest = {
      ...spec,
      labels: { app: "web", "nerdui.auto-heal": "true" },
      healthcheck: { command: "test -f /ok", interval: "5s", retries: 2 },
    };
    const form = fromRequest(healed);
    expect(form).toMatchObject({ health: "custom", healthCmd: "test -f /ok", autoHeal: true });
    expect(form.labels).toEqual([{ key: "app", value: "web" }]);
    expect(toRequest(form).request).toEqual({ ...healed, start: true });

    const off = fromRequest({ ...spec, healthcheck: { disabled: true } });
    expect(off.health).toBe("none");
    // Auto-heal needs a healthcheck.
    expect(toRequest({ ...off, autoHeal: true }).request).toEqual({
      ...spec,
      start: true,
      healthcheck: { disabled: true },
    });
    expect(toRequest({ ...off, health: "image" }).request.healthcheck).toBeUndefined();
    expect(toRequest({ ...off, health: "custom" }).request.healthcheck).toBeUndefined();
  });
});

describe("edit container page", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("recreates the container with the edited settings", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      [`GET /api/v1/environments/local/containers/${id}/spec`]: () =>
        json(200, { id, spec, unsupported: [], running: true }),
      "POST /api/v1/environments/local/containers/command": () =>
        json(200, { command: "nerdctl run -d --name=web nginx" }),
      [`POST /api/v1/environments/local/containers/${id}/recreate`]: () =>
        json(202, {
          job: {
            id: "j1",
            kind: "container.recreate",
            title: "Recreate container web",
            environment: "local",
            status: "running",
            startedAt: "2027-03-01T10:00:00Z",
          },
        }),
      "GET /api/v1/jobs": () => json(200, { jobs: [] }),
      "GET /api/v1/environments/local/containers": () =>
        json(200, { namespace: "default", containers: [] }),
    });
    renderApp(`/env/local/containers/${id}/edit`);

    const image = await screen.findByDisplayValue("docker.io/library/nginx:1.27");
    expect(image).toBeEnabled();
    expect(screen.getAllByDisplayValue("web")[0]).toBeDisabled(); // the name; the label value follows
    fireEvent.change(screen.getByLabelText("Variable value 1"), {
      target: { value: "Europe/Berlin" },
    });
    fireEvent.click(screen.getByLabelText(/Pull the image first/));
    const submit = await screen.findByRole("button", { name: "Pull and recreate" });
    await waitFor(() => expect(submit).toBeEnabled());
    fireEvent.click(submit);
    await waitFor(() => expect(calls.some((c) => c.path.endsWith("/recreate"))).toBe(true));
    const body = calls.find((c) => c.path.endsWith("/recreate"))?.body as Record<string, unknown>;
    expect(body).toMatchObject({
      name: "web",
      pull: true,
      env: [{ name: "TZ", value: "Europe/Berlin" }],
    });
    expect(body).not.toHaveProperty("start");
  });

  it("explains why a container cannot be recreated", async () => {
    fakeApi({
      ...signedIn,
      [`GET /api/v1/environments/local/containers/${id}/spec`]: () =>
        json(200, { id, spec, unsupported: ["sysctls net.ipv4.ip_forward"], running: true }),
    });
    renderApp(`/env/local/containers/${id}/edit`);
    expect(await screen.findByText("Cannot be recreated here")).toBeInTheDocument();
    expect(screen.getByText("sysctls net.ipv4.ip_forward")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Recreate" })).not.toBeInTheDocument();
  });
});
