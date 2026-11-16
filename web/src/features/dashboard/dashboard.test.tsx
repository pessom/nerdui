// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { RecordingWebSocket, fakeApi, json } from "../../test/server";

const counts = (total: number, running: number) => ({
  total,
  running,
  paused: 0,
  stopped: total - running,
  unhealthy: 0,
});

const summary = (volumesSize?: number) => ({
  containers: { total: 5, running: 3, paused: 0, stopped: 2, unhealthy: 1 },
  images: 7,
  volumes: 2,
  networks: 3,
  stacks: 2,
  namespaces: [
    { name: "default", containers: counts(4, 3), images: 6, volumes: 2, networks: 2 },
    { name: "k8s.io", containers: counts(1, 0), images: 1, volumes: 0, networks: 1 },
  ],
  imagesSize: 3 * 1024 ** 3,
  volumesSize,
  filesystems: [{ paths: ["/var/lib/containerd"], total: 100 * 1024 ** 3, free: 20 * 1024 ** 3 }],
});

describe("dashboard", () => {
  beforeEach(() => {
    RecordingWebSocket.instances = [];
    vi.stubGlobal("WebSocket", RecordingWebSocket);
  });
  afterEach(() => vi.unstubAllGlobals());

  it("shows counters, disk usage, live top containers and recent events", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/summary": (c) =>
        json(200, summary(c.query.get("volumeSizes") === "true" ? 512 * 1024 ** 2 : undefined)),
      "GET /api/v1/environments/local/events": () =>
        json(200, {
          recording: true,
          more: false,
          events: [
            {
              id: 1,
              ts: new Date().toISOString(),
              namespace: "default",
              topic: "/tasks/exit",
              kind: "task",
              action: "exit",
              objectId: "c1",
              objectName: "web",
              attributes: { exitStatus: "0" },
            },
          ],
        }),
    });
    renderApp("/env/local");

    const containers = await screen.findByRole("region", { name: "Containers" });
    expect(within(containers).getByText("5")).toBeInTheDocument();
    expect(within(containers).getByText("3 running")).toBeInTheDocument();
    expect(within(containers).getByText("1 unhealthy")).toBeInTheDocument();
    expect(
      within(screen.getByRole("region", { name: "Stacks" })).getByText("2"),
    ).toBeInTheDocument();
    expect(containers.closest("a")).toHaveAttribute("href", "/env/local/containers");

    const disk = screen.getByRole("region", { name: "Disk" });
    expect(within(disk).getByText("3.0 GiB")).toBeInTheDocument();
    expect(within(disk).getByText("20.0 GiB free of 100.0 GiB")).toBeInTheDocument();
    expect(within(disk).getByLabelText("Used 80%")).toBeInTheDocument();
    fireEvent.click(within(disk).getByRole("button", { name: "Volume sizes" }));
    expect(await within(disk).findByText("512.0 MiB")).toBeInTheDocument();
    expect(
      calls.some((c) => c.path.endsWith("/summary") && c.query.get("volumeSizes") === "true"),
    ).toBe(true);

    await waitFor(() => expect(RecordingWebSocket.find("/stats/stream")).toHaveLength(1));
    const ws = RecordingWebSocket.find("/stats/stream")[0]!;
    act(() => {
      ws.open();
      ws.message({
        type: "data",
        ts: "",
        data: {
          ts: "2027-03-01T10:00:00Z",
          running: 2,
          cpu: [
            {
              id: "a".repeat(64),
              name: "db",
              namespace: "default",
              cpuPercent: 42.25,
              memoryUsage: 1,
              memoryPercent: 1,
            },
          ],
          memory: [
            {
              id: "b".repeat(64),
              name: "cache",
              namespace: "k8s.io",
              cpuPercent: 1,
              memoryUsage: 256 * 1024 ** 2,
              memoryPercent: 10,
            },
          ],
        },
      });
    });
    const cpu = screen.getByRole("region", { name: "Top CPU" });
    expect(await within(cpu).findByText("42.3 %")).toBeInTheDocument();
    const mem = screen.getByRole("region", { name: "Top memory" });
    expect(within(mem).getByRole("link", { name: "cache" })).toHaveAttribute(
      "href",
      `/env/local/containers/${"b".repeat(64)}?ns=k8s.io`,
    );
    expect(within(mem).getByText("256.0 MiB")).toBeInTheDocument();

    const events = screen.getByRole("region", { name: "Recent events" });
    expect(await within(events).findByText("web")).toBeInTheDocument();
    expect(calls.find((c) => c.path.endsWith("/events"))?.query.get("namespace")).toBe("*");
  });
});
