// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";
import { nameError } from "./namespaces";

const counts = (total: number, running: number) => ({
  total,
  running,
  paused: 0,
  stopped: total - running,
  unhealthy: 0,
});
const ns = (name: string, containers: number, images: number, volumes: number) => ({
  name,
  containers: counts(containers, 0),
  images,
  volumes,
  networks: 1,
});

describe("namespace names", () => {
  it("follows containerd's identifier rule", () => {
    expect(nameError("lab")).toBeUndefined();
    expect(nameError("my.lab_1-x")).toBeUndefined();
    expect(nameError("bad name")).toBeDefined();
    expect(nameError("-x")).toBeDefined();
    expect(nameError("a..b")).toBeDefined();
    expect(nameError("a".repeat(77))).toBeDefined();
  });
});

describe("namespaces page", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("lists namespaces, creates one and removes only empty ones", async () => {
    let list = [ns("default", 2, 3, 1), ns("lab", 0, 0, 0), ns("moby", 0, 0, 0)];
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/summary": () =>
        json(200, {
          containers: counts(2, 0),
          images: 3,
          volumes: 1,
          networks: 3,
          namespaces: list,
          imagesSize: 0,
          filesystems: [],
        }),
      "POST /api/v1/environments/local/namespaces": (c) => {
        const { name } = c.body as { name: string };
        list = [...list, ns(name, 0, 0, 0)];
        return json(201, { name });
      },
      "DELETE /api/v1/environments/local/namespaces/lab": () => {
        list = list.filter((n) => n.name !== "lab");
        return new Response(null, { status: 204 });
      },
      "DELETE /api/v1/environments/local/namespaces/moby": () =>
        new Response(null, { status: 204 }),
    });
    renderApp("/env/local/namespaces");

    const table = await screen.findByRole("table");
    expect(within(table).getByText("lab")).toBeInTheDocument();
    expect(within(table).getByText("default", { selector: "a" })).toBeInTheDocument();

    // Create.
    fireEvent.click(screen.getByRole("button", { name: "Create namespace" }));
    const create = await screen.findByRole("dialog", { name: "Create namespace" });
    const input = within(create).getByLabelText(/Name/);
    fireEvent.change(input, { target: { value: "lab" } });
    expect(within(create).getByText("Already exists")).toBeInTheDocument();
    fireEvent.change(input, { target: { value: "test-1" } });
    fireEvent.click(within(create).getByRole("button", { name: "Create" }));
    expect(await within(table).findByText("test-1")).toBeInTheDocument();
    expect(calls.find((c) => c.method === "POST")?.body).toEqual({ name: "test-1" });

    // A non-empty namespace cannot be removed.
    fireEvent.click(screen.getByRole("button", { name: "Remove default" }));
    let dialog = await screen.findByRole("dialog", { name: "Remove namespace" });
    expect(within(dialog).getByText("2 containers")).toBeInTheDocument();
    expect(within(dialog).getByText("3 images")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Remove" })).toBeDisabled();
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());

    // A well-known namespace needs its name typed.
    fireEvent.click(screen.getByRole("button", { name: "Remove moby" }));
    dialog = await screen.findByRole("dialog", { name: "Remove namespace" });
    const remove = within(dialog).getByRole("button", { name: "Remove" });
    expect(remove).toBeDisabled();
    fireEvent.change(within(dialog).getByLabelText("Type moby to confirm"), {
      target: { value: "moby" },
    });
    expect(remove).toBeEnabled();
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());

    // An empty one is removed.
    fireEvent.click(screen.getByRole("button", { name: "Remove lab" }));
    dialog = await screen.findByRole("dialog", { name: "Remove namespace" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Remove" }));
    await waitFor(() => expect(within(table).queryByText("lab")).not.toBeInTheDocument());
    expect(calls.some((c) => c.method === "DELETE" && c.path.endsWith("/moby"))).toBe(false);
  });
});
