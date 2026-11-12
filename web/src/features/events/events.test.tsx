// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { HistoryEvent } from "../../api/queries";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";
import { eventColor, eventDetails, eventLabel, eventObject } from "./format";

const id = "a".repeat(64);
const ev = (over: Partial<HistoryEvent>): HistoryEvent => ({
  id: 1,
  ts: "2027-03-01T10:00:00Z",
  namespace: "default",
  topic: "/tasks/exit",
  kind: "task",
  action: "exit",
  objectId: id,
  objectName: "web",
  attributes: { exitStatus: "137", pid: "42" },
  ...over,
});

describe("event formatting", () => {
  it("labels, colors and objects", () => {
    expect(eventLabel(ev({}))).toBe("task exit (137)");
    expect(eventColor(ev({}))).toBe("red");
    expect(eventColor(ev({ attributes: { exitStatus: "0" } }))).toBe("gray");
    expect(eventLabel(ev({ kind: "nerdui", action: "disconnected" }))).toBe(
      "containerd disconnected",
    );
    expect(eventObject(ev({ objectName: "" }))).toBe("aaaaaaaaaaaa");
    expect(eventObject(ev({ kind: "image", objectName: "", objectId: "alpine:3" }))).toBe(
      "alpine:3",
    );
    expect(eventDetails(ev({}))).toBe("pid=42");
  });
});

describe("events page", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("shows events, filters and pages", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/events": (c) =>
        c.query.get("before")
          ? json(200, {
              events: [ev({ id: 1, action: "start", topic: "/tasks/start", attributes: {} })],
              more: false,
              recording: true,
            })
          : json(200, { events: [ev({ id: 2 })], more: true, recording: true }),
    });
    const { router } = renderApp("/env/local");
    fireEvent.click(await screen.findByRole("link", { name: "Events" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/env/local/events"));

    const table = await screen.findByRole("table");
    expect(within(table).getByText("task exit (137)")).toBeInTheDocument();
    expect(within(table).getByRole("link", { name: "web" })).toHaveAttribute(
      "href",
      `/env/local/containers/${id}?ns=default`,
    );

    fireEvent.click(screen.getByRole("button", { name: "Load older events" }));
    expect(await within(table).findByText("task start")).toBeInTheDocument();
    expect(calls.some((c) => c.query.get("before") === "2")).toBe(true);

    fireEvent.click(screen.getByRole("radio", { name: "Containers" }));
    await waitFor(() =>
      expect(
        calls.some((c) => c.path.endsWith("/events") && c.query.get("kind") === "container"),
      ).toBe(true),
    );
    fireEvent.click(screen.getByLabelText("All namespaces"));
    await waitFor(() =>
      expect(
        calls.some((c) => c.path.endsWith("/events") && c.query.get("namespace") === "*"),
      ).toBe(true),
    );
    const search = screen.getByLabelText("Filter by object");
    fireEvent.change(search, { target: { value: "web" } });
    fireEvent.keyDown(search, { key: "Enter" });
    await waitFor(() =>
      expect(calls.some((c) => c.path.endsWith("/events") && c.query.get("object") === "web")).toBe(
        true,
      ),
    );
  });

  it("explains when recording is off", async () => {
    fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/events": () => json(200, { events: [], recording: false }),
    });
    renderApp("/env/local/events");
    expect(await screen.findByText("Event history is disabled")).toBeInTheDocument();
  });
});
