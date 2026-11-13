// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { RecordingWebSocket, fakeApi, json } from "../../test/server";
import { HISTORY_SECONDS, addSample, clock, rate, timeWindow, type StatsSample } from "./stats";

vi.mock("./StatsChart", () => ({
  default: ({ label, times }: { label: string; times: number[] }) => (
    <div role="img" aria-label={label} data-points={times.length} />
  ),
}));

const sample = (sec: number, over: Partial<StatsSample> = {}): StatsSample => ({
  ts: new Date(Date.UTC(2027, 2, 1, 10, 0, sec)).toISOString(),
  cpuPercent: 25,
  memoryUsage: 64 * 1024 ** 2,
  memoryLimit: 256 * 1024 ** 2,
  memoryPercent: 25,
  networkRx: sec * 1024,
  networkTx: sec * 512,
  blockRead: 0,
  blockWrite: sec * 2048,
  pids: 3,
  ...over,
});

describe("stats helpers", () => {
  it("keeps a fixed five-minute window and a 24-hour clock", () => {
    // Always five minutes wide, ending at the latest sample; now when there is no data yet.
    expect(timeWindow(1000)).toEqual([1000 - HISTORY_SECONDS, 1000]);
    expect(timeWindow(null, 2000)).toEqual([2000 - HISTORY_SECONDS, 2000]);
    const t = new Date(2027, 2, 1, 14, 5, 9).getTime() / 1000;
    expect(clock(t)).toBe("14:05");
    expect(clock(t, true)).toBe("14:05:09");
  });

  it("derives rates and keeps five minutes", () => {
    expect(rate(1000, 3000, 2)).toBe(1000);
    expect(rate(3000, 1000, 2)).toBe(0); // counter reset
    let points = addSample([], undefined, sample(0));
    points = addSample(points, sample(0), sample(2));
    expect(points[1]).toMatchObject({ rxRate: 1024, txRate: 512, writeRate: 2048 });
    expect(points[0]?.rxRate).toBe(0);
    let prev = sample(2);
    for (let s = 4; s <= HISTORY_SECONDS + 20; s += 2) {
      const next = sample(s);
      points = addSample(points, prev, next);
      prev = next;
    }
    const span = (points.at(-1)?.t ?? 0) - (points[0]?.t ?? 0);
    expect(span).toBeLessThanOrEqual(HISTORY_SECONDS);
  });
});

const id = "c".repeat(64);
const base = "/api/v1/environments/local/containers";

function setup(state: string) {
  fakeApi({
    ...signedIn,
    [`GET ${base}`]: () =>
      json(200, {
        namespace: "default",
        containers: [
          {
            id,
            name: "db",
            namespace: "default",
            image: "postgres:16",
            command: "postgres",
            state,
            createdAt: "2027-03-01T10:00:00Z",
            ports: [],
            labels: {},
          },
        ],
      }),
    [`GET ${base}/${id}/inspect`]: () =>
      json(200, { Id: id, Name: "db", State: { Status: state } }),
  });
}

const statsStreams = () => RecordingWebSocket.find("/stats/stream");

describe("stats tab", () => {
  beforeEach(() => {
    RecordingWebSocket.instances = [];
    vi.stubGlobal("WebSocket", RecordingWebSocket);
  });
  afterEach(() => vi.unstubAllGlobals());

  it("shows live numbers and charts and changes the interval", async () => {
    setup("running");
    renderApp(`/env/local/containers/${id}?tab=stats`);
    await waitFor(() => expect(statsStreams()).toHaveLength(1));
    const ws = statsStreams()[0]!;
    expect(new URL(ws.url).searchParams.get("interval")).toBe("2s");

    act(() => {
      ws.open();
      ws.message({ type: "data", ts: "", data: sample(0) });
      ws.message({ type: "data", ts: "", data: sample(2, { cpuPercent: 150.25 }) });
    });
    expect(await screen.findByText("150.3 %")).toBeInTheDocument();
    const memory = screen.getByRole("region", { name: "Memory" });
    expect(within(memory).getByText("64.0 MiB / 256.0 MiB")).toBeInTheDocument();
    const net = screen.getByRole("region", { name: "Network" });
    expect(within(net).getByText(/↓ 1.0 KiB\/s/)).toBeInTheDocument();
    expect(screen.getByText("live")).toBeInTheDocument();
    expect(await screen.findByRole("img", { name: "CPU chart" })).toHaveAttribute(
      "data-points",
      "2",
    );

    fireEvent.click(screen.getByRole("radio", { name: "5 s" }));
    await waitFor(() => expect(statsStreams()).toHaveLength(2));
    expect(ws.closed).toBe(true);
    expect(new URL(statsStreams()[1]!.url).searchParams.get("interval")).toBe("5s");
    // The history survives an interval change.
    expect(screen.getByRole("img", { name: "CPU chart" })).toHaveAttribute("data-points", "2");
  });

  it("does not stream a stopped container", async () => {
    setup("exited");
    renderApp(`/env/local/containers/${id}?tab=stats`);
    expect(await screen.findByText("Container is not running")).toBeInTheDocument();
    expect(statsStreams()).toHaveLength(0);
  });
});
