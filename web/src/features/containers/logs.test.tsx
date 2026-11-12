// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { RecordingWebSocket, fakeApi, json } from "../../test/server";
import type { LogTerminalProps } from "./LogTerminal";
import { compileFilter, formatLine, streamQuery, stripAnsi, tsKey } from "./logs";

// xterm.js needs a real browser (canvas, layout); the fake shows the lines as text.
vi.mock("./LogTerminal", () => ({
  default: ({ lines, label }: LogTerminalProps) => (
    <pre aria-label={label}>{lines.map((l) => l.text).join("\n")}</pre>
  ),
}));

describe("log helpers", () => {
  it("strips ANSI sequences", () => {
    expect(stripAnsi("\x1b[1;31mred\x1b[0m and \x1b]0;title\x07plain")).toBe("red and plain");
  });

  it("compiles filters", () => {
    expect(compileFilter("", false).test("x")).toBe(true);
    expect(compileFilter("WARN", false).test("a warning")).toBe(true);
    expect(compileFilter("^err\\d", true).test("ERR4 x")).toBe(true);
    expect(compileFilter("^err\\d", true).test("an err4")).toBe(false);
    const bad = compileFilter("(", true);
    expect(bad.error).toBeTruthy();
    expect(bad.test("anything")).toBe(true);
  });

  it("formats lines", () => {
    expect(formatLine({ stream: "stdout", line: "hi" }, false)).toBe(" hi\x1b[0m");
    expect(formatLine({ stream: "stderr", line: "no" }, false)).toBe("\x1b[31m▌\x1b[0mno\x1b[0m");
    expect(
      stripAnsi(formatLine({ stream: "stdout", ts: "2027-03-01T10:00:00Z", line: "x" }, true)),
    ).toBe(`${"2027-03-01T10:00:00Z".padEnd(30)}  x`);
  });

  it("orders timestamps with trimmed fractions", () => {
    expect(tsKey("2027-03-01T10:00:01Z") < tsKey("2027-03-01T10:00:01.5Z")).toBe(true);
    expect(tsKey("2027-03-01T10:00:01.000000002Z") > tsKey("2027-03-01T10:00:01.000000001Z")).toBe(
      true,
    );
  });

  it("builds stream queries", () => {
    expect(streamQuery({ namespace: "default", follow: true, tail: "500" })).toBe(
      "namespace=default&tail=500&follow=true",
    );
    expect(
      streamQuery({ namespace: "k8s.io", follow: false, tail: "all" }, "2027-03-01T10:00:00Z"),
    ).toBe("namespace=k8s.io&tail=all&since=2027-03-01T10%3A00%3A00Z");
  });
});

const id = "b".repeat(64);
const base = "/api/v1/environments/local/containers";

function setup() {
  fakeApi({
    ...signedIn,
    [`GET ${base}`]: () =>
      json(200, {
        namespace: "default",
        containers: [
          {
            id,
            name: "api",
            namespace: "default",
            image: "alpine:3",
            command: "sh",
            state: "running",
            createdAt: "2027-03-01T10:00:00Z",
            ports: [],
            labels: {},
          },
        ],
      }),
    [`GET ${base}/${id}/inspect`]: () =>
      json(200, { Id: id, Name: "api", State: { Status: "running" } }),
  });
}

const logStreams = () => RecordingWebSocket.find("/logs/stream");

describe("logs tab", () => {
  beforeEach(() => {
    RecordingWebSocket.instances = [];
    vi.stubGlobal("WebSocket", RecordingWebSocket);
    localStorage.clear();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("streams, filters and resumes after a disconnect", async () => {
    setup();
    renderApp(`/env/local/containers/${id}?tab=logs`);
    await waitFor(() => expect(logStreams()).toHaveLength(1));
    const ws = logStreams()[0]!;
    const url = new URL(ws.url);
    expect(url.pathname).toBe(`${base}/${id}/logs/stream`);
    expect(Object.fromEntries(url.searchParams)).toEqual({
      namespace: "default",
      tail: "500",
      follow: "true",
    });

    act(() => {
      ws.open();
      ws.message({
        type: "data",
        ts: "",
        data: [
          { stream: "stdout", ts: "2027-03-01T10:00:00Z", line: "\x1b[32mstarted\x1b[0m" },
          { stream: "stderr", ts: "2027-03-01T10:00:01Z", line: "warning: disk" },
          { stream: "stdout", ts: "2027-03-01T10:00:01.5Z", line: "request 1" },
        ],
      });
    });
    const view = await screen.findByLabelText("Logs of api");
    await waitFor(() => expect(stripAnsi(view.textContent ?? "")).toContain("request 1"));
    expect(screen.getByText("Live")).toBeInTheDocument();
    expect(screen.getByText("3 lines")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Filter logs"), { target: { value: "WARN" } });
    await waitFor(() => expect(stripAnsi(view.textContent ?? "")).not.toContain("request 1"));
    expect(screen.getByText("1 of 3 lines")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Filter logs"), { target: { value: "" } });

    fireEvent.click(screen.getByRole("radio", { name: "stdout" }));
    await waitFor(() => expect(screen.getByText("2 of 3 lines")).toBeInTheDocument());

    // A dropped connection resumes from the last timestamp and skips lines already shown.
    act(() => ws.drop());
    await waitFor(() => expect(logStreams()).toHaveLength(2), { timeout: 3000 });
    const again = logStreams()[1]!;
    const q = new URL(again.url).searchParams;
    expect(q.get("since")).toBe("2027-03-01T10:00:01.5Z");
    expect(q.get("tail")).toBe("all");
    act(() => {
      again.open();
      again.message({
        type: "data",
        ts: "",
        data: [
          { stream: "stdout", ts: "2027-03-01T10:00:01.5Z", line: "request 1" },
          { stream: "stdout", ts: "2027-03-01T10:00:02Z", line: "request 2" },
        ],
      });
    });
    await waitFor(() => expect(screen.getByText("3 of 4 lines")).toBeInTheDocument());

    expect(screen.getByRole("link", { name: "Download logs" })).toHaveAttribute(
      "href",
      `${base}/${id}/logs?namespace=default&timestamps=false`,
    );
  });

  it("shows stream errors and the end of the stream", async () => {
    setup();
    renderApp(`/env/local/containers/${id}?tab=logs`);
    await waitFor(() => expect(logStreams()).toHaveLength(1));
    const ws = logStreams()[0]!;
    act(() => {
      ws.open();
      ws.message({
        type: "error",
        ts: "",
        error: { code: "conflict", message: "logging is disabled for this container" },
      });
      ws.message({ type: "end", ts: "" });
      ws.close();
    });
    expect(await screen.findByText("logging is disabled for this container")).toBeInTheDocument();
    expect(screen.getByText("Ended")).toBeInTheDocument();

    // Changing the tail starts a new stream.
    fireEvent.click(screen.getByRole("switch", { name: "Follow" }));
    await waitFor(() => expect(logStreams()).toHaveLength(2));
    expect(new URL(logStreams()[1]!.url).searchParams.get("follow")).toBeNull();
  });
});
