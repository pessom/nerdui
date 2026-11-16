// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { stripAnsi } from "../containers/logs";
import type { LogTerminalProps } from "../containers/LogTerminal";
import { renderApp, signedIn } from "../../test/render";
import { RecordingWebSocket, fakeApi, json } from "../../test/server";
import { formatStackLine, serviceColor } from "./stackLogs";

vi.mock("../containers/LogTerminal", () => ({
  default: ({ lines, label }: LogTerminalProps) => (
    <pre aria-label={label}>{lines.map((l) => stripAnsi(l.text)).join("\n")}</pre>
  ),
}));

const base = "/api/v1/environments/local/stacks";
const stack = {
  name: "app",
  mode: "unmanaged",
  status: "unknown",
  containers: 2,
  running: 2,
  services: [
    {
      name: "web",
      defined: false,
      running: 1,
      containers: [{ id: "w1", name: "app-web-1", state: "running" }],
    },
    {
      name: "db",
      defined: false,
      running: 1,
      containers: [{ id: "d1", name: "app-db-1", state: "running" }],
    },
  ],
};

const line = (service: string, id: string, ts: string, text: string) => ({
  service,
  container: `app-${service}-1`,
  containerId: id,
  stream: "stdout" as const,
  ts,
  line: text,
});

describe("stack log helpers", () => {
  it("labels lines with a stable service color", () => {
    expect(serviceColor(["web", "db"], "db")).not.toBe(serviceColor(["web", "db"], "web"));
    const text = formatStackLine(
      line("db", "d1", "2027-03-01T10:00:01Z", "ready"),
      false,
      ["web", "db"],
      4,
    );
    expect(text).toContain(`\x1b[${serviceColor(["web", "db"], "db")}m`);
    expect(stripAnsi(text)).toBe("db   │ ready");
  });
});

describe("stack logs", () => {
  beforeEach(() => {
    RecordingWebSocket.instances = [];
    vi.stubGlobal("WebSocket", RecordingWebSocket);
  });
  afterEach(() => vi.unstubAllGlobals());

  it("streams the merged logs with service labels, filters services and resumes per container", async () => {
    fakeApi({ ...signedIn, [`GET ${base}/app`]: () => json(200, stack) });
    renderApp("/env/local/stacks/app?tab=logs");

    await waitFor(() => expect(RecordingWebSocket.find("/stacks/app/logs/stream")).toHaveLength(1));
    const ws = RecordingWebSocket.find("/stacks/app/logs/stream")[0]!;
    const q = new URL(ws.url).searchParams;
    expect([q.get("namespace"), q.get("tail"), q.get("follow"), q.get("since")]).toEqual([
      "default",
      "200",
      "true",
      null,
    ]);
    act(() => {
      ws.open();
      ws.message({
        type: "data",
        ts: "",
        data: [
          line("web", "w1", "2027-03-01T10:00:01Z", "GET /"),
          line("db", "d1", "2027-03-01T10:00:02Z", "checkpoint"),
          line("web", "w1", "2027-03-01T10:00:03Z", "GET /health"),
        ],
      });
    });
    const term = await screen.findByLabelText("Logs of stack app");
    await waitFor(() =>
      expect(term).toHaveTextContent("web │ GET / db │ checkpoint web │ GET /health"),
    );

    // Hide a service.
    fireEvent.click(within(screen.getByRole("group", { name: "Services" })).getByText("db"));
    await waitFor(() => expect(term).not.toHaveTextContent("checkpoint"));

    // A reconnect resumes from the oldest per-container timestamp; repeated lines are dropped.
    act(() => ws.drop());
    await waitFor(
      () => expect(RecordingWebSocket.find("/stacks/app/logs/stream")).toHaveLength(2),
      {
        timeout: 5000,
      },
    );
    const ws2 = RecordingWebSocket.find("/stacks/app/logs/stream")[1]!;
    expect(new URL(ws2.url).searchParams.get("since")).toBe("2027-03-01T10:00:02Z");
    act(() => {
      ws2.open();
      ws2.message({
        type: "data",
        ts: "",
        data: [
          line("db", "d1", "2027-03-01T10:00:02Z", "checkpoint"),
          line("web", "w1", "2027-03-01T10:00:03Z", "GET /health"),
          line("web", "w1", "2027-03-01T10:00:04Z", "GET /new"),
        ],
      });
    });
    await waitFor(() => expect(term).toHaveTextContent("GET /new"));
    expect(term.textContent!.match(/GET \/health/g)).toHaveLength(1);
  });
});
