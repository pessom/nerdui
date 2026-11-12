// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";
import { groupByProject, portLabel, portURL, statusText, type Container } from "./format";

const now = Date.parse("2027-03-01T12:00:00Z");

function container(over: Partial<Container>): Container {
  return {
    id: "a".repeat(64),
    name: "c",
    namespace: "default",
    image: "docker.io/library/alpine:3",
    command: "sleep 300",
    state: "running",
    createdAt: "2027-03-01T10:00:00Z",
    ports: [],
    labels: {},
    ...over,
  };
}

const sample: Container[] = [
  container({
    id: "1".repeat(64),
    name: "media-web-1",
    image: "nginx:1.29",
    startedAt: "2027-03-01T09:00:00Z",
    createdAt: "2027-03-01T08:00:00Z",
    compose: { project: "media", service: "web" },
    ports: [{ hostIp: "0.0.0.0", hostPort: 8080, containerPort: 80, protocol: "tcp" }],
  }),
  container({
    id: "2".repeat(64),
    name: "media-db-1",
    image: "postgres:18",
    state: "exited",
    exitCode: 1,
    exitedAt: "2027-03-01T11:00:00Z",
    createdAt: "2027-03-01T07:00:00Z",
    compose: { project: "media", service: "db" },
  }),
  container({
    id: "3".repeat(64),
    name: "adhoc",
    image: "busybox",
    createdAt: "2027-03-01T11:30:00Z",
  }),
];

describe("format", () => {
  it("formats status like nerdctl ps", () => {
    expect(statusText(container({ startedAt: "2027-03-01T09:00:00Z" }), now)).toBe("Up 3h");
    expect(statusText(container({ state: "running" }), now)).toBe("Up");
    expect(
      statusText(
        container({ state: "exited", exitCode: 137, exitedAt: "2027-03-01T10:00:00Z" }),
        now,
      ),
    ).toBe("Exited (137) 2 hours ago");
    expect(statusText(container({ state: "created" }), now)).toBe("Created");
    expect(statusText(container({ state: "restarting", exitCode: 1 }), now)).toBe("Restarting (1)");
  });

  it("formats and links ports", () => {
    const any = { hostIp: "0.0.0.0", hostPort: 8080, containerPort: 80, protocol: "tcp" } as const;
    expect(portLabel(any)).toBe("8080→80/tcp");
    expect(portLabel({ ...any, hostIp: "127.0.0.1" })).toBe("127.0.0.1:8080→80/tcp");
    expect(portURL(any, "nas.local")).toBe("http://nas.local:8080");
    expect(portURL(any, "fd00::1")).toBe("http://[fd00::1]:8080");
    expect(portURL({ ...any, hostIp: "127.0.0.1" }, "nas.local")).toBeUndefined();
    expect(portURL({ ...any, protocol: "udp" }, "nas.local")).toBeUndefined();
  });

  it("groups by compose project with standalone last", () => {
    const groups = groupByProject([
      container({ name: "x" }),
      container({ name: "b1", compose: { project: "b" } }),
      container({ name: "a1", compose: { project: "a" } }),
    ]);
    expect(groups.map((g) => g.project ?? "-")).toEqual(["a", "b", "-"]);
  });
});

describe("containers page", () => {
  beforeEach(() => {
    vi.stubGlobal("WebSocket", SilentWebSocket);
    localStorage.clear();
  });
  afterEach(() => vi.unstubAllGlobals());

  const rowNames = () =>
    screen
      .getAllByRole("row")
      // Cell 0 is the selection checkbox.
      .map((r) => within(r).queryAllByRole("cell")[1]?.textContent ?? "")
      .filter((t) => t && !t.includes("running"));

  it("lists containers grouped by compose project and filters them", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/containers": () =>
        json(200, { namespace: "default", containers: sample }),
    });
    renderApp("/env/local/containers");

    expect(await screen.findByText("media-web-1")).toBeInTheDocument();
    expect(screen.getByText("media")).toBeInTheDocument(); // group header
    expect(screen.getByText("Standalone")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "8080→80/tcp" })).toHaveAttribute(
      "href",
      expect.stringContaining(":8080"),
    );
    expect(screen.getByText(/Exited \(1\)/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Containers" })).toHaveAttribute("data-active", "true");
    expect(calls.some((c) => c.path === "/api/v1/environments/local/containers")).toBe(true);

    // Search.
    fireEvent.change(screen.getByLabelText("Search containers"), { target: { value: "postgres" } });
    await waitFor(() => expect(screen.queryByText("media-web-1")).not.toBeInTheDocument());
    expect(screen.getByText("media-db-1")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Search containers"), { target: { value: "" } });

    // Running only.
    fireEvent.click(screen.getByText("Running (2)"));
    await waitFor(() => expect(screen.queryByText("media-db-1")).not.toBeInTheDocument());
    expect(screen.getByText("adhoc")).toBeInTheDocument();
  });

  it("sorts by name without grouping", async () => {
    fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/containers": () =>
        json(200, { namespace: "default", containers: sample }),
    });
    renderApp("/env/local/containers");
    await screen.findByText("media-web-1");

    fireEvent.click(screen.getByLabelText("Group by compose project"));
    // Default: newest first.
    expect(rowNames()).toEqual([
      expect.stringContaining("adhoc"),
      expect.stringContaining("media-web-1"),
      expect.stringContaining("media-db-1"),
    ]);
    fireEvent.click(screen.getByRole("button", { name: /Name/ }));
    await waitFor(() => expect(rowNames()[0]).toContain("adhoc"));
    expect(rowNames()[1]).toContain("media-db-1");
    expect(screen.getByRole("columnheader", { name: /Name/ })).toHaveAttribute(
      "aria-sort",
      "ascending",
    );
  });

  it("uses the namespace from the URL and shows empty and error states", async () => {
    let fail = false;
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/containers": () =>
        fail
          ? json(503, { error: { code: "unavailable", message: "containerd is unavailable" } })
          : json(200, { namespace: "k8s.io", containers: [] }),
    });
    renderApp("/env/local/containers?ns=k8s.io");
    expect(await screen.findByText("No containers in this namespace.")).toBeInTheDocument();
    const call = calls.find((c) => c.path === "/api/v1/environments/local/containers");
    expect(call).toBeDefined();

    fail = true;
    renderApp("/env/local/containers");
    expect(await screen.findByText("Cannot list containers")).toBeInTheDocument();
  });
});
