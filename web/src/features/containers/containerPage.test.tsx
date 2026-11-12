// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json, type Call } from "../../test/server";
import {
  commandLine,
  envPairs,
  portBindings,
  restartPolicy,
  type ContainerInspect,
} from "./inspect";
import type { Container } from "./format";

const id = "a".repeat(64);
const base = "/api/v1/environments/local/containers";

const summary: Container = {
  id,
  name: "web",
  namespace: "default",
  image: "nginx:1",
  command: "nginx -g 'daemon off;'",
  state: "running",
  createdAt: "2027-03-01T10:00:00Z",
  startedAt: "2027-03-01T10:00:01Z",
  ports: [],
  labels: {},
};

const inspect: ContainerInspect = {
  Id: id,
  Name: "web",
  Created: "2027-03-01T10:00:00Z",
  Path: "nginx",
  Args: ["-g", "daemon off;"],
  Image: "docker.io/library/nginx:1",
  RestartCount: 0,
  Platform: "linux",
  State: { Status: "running", Pid: 42, StartedAt: "2027-03-01T10:00:01Z" },
  HostConfig: {
    NetworkMode: "bridge",
    RestartPolicy: { Name: "on-failure", MaximumRetryCount: 3 },
  },
  Mounts: [
    { Type: "volume", Name: "webdata", Source: "/var/lib/x", Destination: "/data", RW: true },
    { Type: "bind", Source: "/etc/web.conf", Destination: "/etc/nginx/nginx.conf", RW: false },
  ],
  Config: {
    Env: ["PATH=/usr/bin", "TOKEN=a=b", "EMPTY"],
    Labels: { "com.example": "yes" },
  },
  NetworkSettings: {
    Ports: { "80/tcp": [{ HostIp: "0.0.0.0", HostPort: "8080" }], "443/tcp": null },
    Networks: { "unknown-eth0": { IPAddress: "10.4.0.5", IPPrefixLen: 24, MacAddress: "aa:bb" } },
  },
};

function setup(extra: Record<string, (call: Call) => Response> = {}) {
  return fakeApi({
    ...signedIn,
    [`GET ${base}`]: () => json(200, { namespace: "default", containers: [summary] }),
    [`GET ${base}/${id}/inspect`]: () => json(200, inspect),
    ...extra,
  });
}

describe("inspect helpers", () => {
  it("formats key fields", () => {
    expect(envPairs(inspect.Config?.Env)).toEqual([
      ["PATH", "/usr/bin"],
      ["TOKEN", "a=b"],
      ["EMPTY", ""],
    ]);
    expect(commandLine(inspect)).toBe("nginx -g 'daemon off;'");
    expect(restartPolicy(inspect)).toBe("on-failure:3");
    expect(restartPolicy({ Id: "x", Name: "" })).toBe("no");
    expect(portBindings(inspect)).toEqual([{ container: "80/tcp", host: "0.0.0.0:8080" }]);
    expect(
      portBindings({
        Id: "x",
        Name: "",
        NetworkSettings: { Ports: { "53/udp": [{ HostIp: "::", HostPort: "53" }] } },
      }),
    ).toEqual([{ container: "53/udp", host: "[::]:53" }]);
  });
});

describe("container page", () => {
  beforeEach(() => {
    vi.stubGlobal("WebSocket", SilentWebSocket);
    localStorage.clear();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("opens from the container list and shows key fields", async () => {
    setup();
    const { router } = renderApp("/env/local/containers?ns=default");

    fireEvent.click(await screen.findByRole("link", { name: "web" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/env/local/containers/${id}`));
    expect(router.state.location.search).toEqual({ ns: "default" });

    expect(await screen.findByRole("heading", { name: "web" })).toBeInTheDocument();
    const general = await screen.findByRole("region", { name: "General" });
    expect(within(general).getByText("nginx -g 'daemon off;'")).toBeInTheDocument();
    const state = screen.getByRole("region", { name: "State" });
    expect(within(state).getByText("on-failure:3")).toBeInTheDocument();

    const env = screen.getByRole("region", { name: "Environment" });
    expect(within(env).getByText("a=b")).toBeInTheDocument();
    const mounts = screen.getByRole("region", { name: "Mounts" });
    expect(within(mounts).getByText("webdata")).toBeInTheDocument();
    expect(within(mounts).getByText("ro")).toBeInTheDocument();
    const ports = screen.getByRole("region", { name: "Ports" });
    expect(within(ports).getByText("0.0.0.0:8080")).toBeInTheDocument();
    const nets = screen.getByRole("region", { name: "Networks" });
    expect(within(nets).getByText("10.4.0.5/24")).toBeInTheDocument();
    expect(
      within(screen.getByRole("region", { name: "Labels" })).getByText("com.example"),
    ).toBeInTheDocument();

    // Actions from the list are available on the page too.
    expect(screen.getByRole("button", { name: "Actions for web" })).toBeEnabled();
  });

  it("shows the full JSON and requests sizes on demand", async () => {
    const { calls } = setup();
    renderApp(`/env/local/containers/${id}?tab=inspect`);

    const editor = await screen.findByRole("textbox", { name: "Inspect web" });
    await waitFor(() => expect(editor.textContent).toContain('"Name": "web"'));
    expect(screen.getByRole("button", { name: "Copy JSON" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Search" })).toBeInTheDocument();

    fireEvent.click(screen.getByLabelText(/Include sizes/));
    await waitFor(() =>
      expect(
        calls.some((c) => c.path === `${base}/${id}/inspect` && c.query.get("size") === "true"),
      ).toBe(true),
    );
  });

  it("explains when the container is gone", async () => {
    setup({
      [`GET ${base}/${id}/inspect`]: () =>
        json(404, { error: { code: "not_found", message: "no such container" } }),
      [`GET ${base}`]: () => json(200, { namespace: "default", containers: [] }),
    });
    renderApp(`/env/local/containers/${id}`);
    expect(await screen.findByText("Container not found")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Back to containers" })).toHaveAttribute(
      "href",
      "/env/local/containers",
    );
  });
});
