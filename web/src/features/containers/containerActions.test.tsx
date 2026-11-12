// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, envStatus, fakeApi, json, runtime, type Call } from "../../test/server";
import { canApply } from "./actions";
import type { Container } from "./format";

function container(id: string, name: string, state: Container["state"]): Container {
  return {
    id: id.repeat(64),
    name,
    namespace: "default",
    image: "alpine:3",
    command: "sleep 300",
    state,
    createdAt: "2027-03-01T10:00:00Z",
    ports: [],
    labels: {},
  };
}

const web = container("1", "web", "running");
const db = container("2", "db", "exited");
const cache = container("3", "cache", "paused");
const sample = [web, db, cache];
const base = "/api/v1/environments/local/containers";

function setup(extra: Record<string, (call: Call) => Response> = {}) {
  return fakeApi({
    ...signedIn,
    [`GET ${base}`]: () => json(200, { namespace: "default", containers: sample }),
    ...extra,
  });
}

const actionCalls = (calls: Call[]) => calls.filter((c) => c.method !== "GET");

async function openMenu(name: string) {
  fireEvent.click(await screen.findByRole("button", { name: `Actions for ${name}` }));
  // Mantine's open transition never finishes in jsdom, so the dropdown stays display:none.
  return screen.findByRole("menu", { hidden: true });
}

describe("canApply", () => {
  it("matches the server's state checks", () => {
    expect(canApply("start", web)).toBe(false);
    expect(canApply("start", db)).toBe(true);
    expect(canApply("stop", db)).toBe(false);
    expect(canApply("kill", cache)).toBe(true);
    expect(canApply("pause", cache)).toBe(false);
    expect(canApply("unpause", cache)).toBe(true);
    expect(canApply("remove", web)).toBe(true);
    expect(canApply("restart", db)).toBe(true);
  });
});

describe("container actions", () => {
  beforeEach(() => {
    vi.stubGlobal("WebSocket", SilentWebSocket);
    localStorage.clear();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("stops a container from its row menu", async () => {
    const { calls } = setup({
      [`POST ${base}/${web.id}/stop`]: () => new Response(null, { status: 204 }),
    });
    renderApp("/env/local/containers?ns=default");

    const menu = await openMenu("web");
    // Only actions that fit the state are enabled.
    expect(within(menu).getByRole("menuitem", { name: "Start", hidden: true })).toBeDisabled();
    fireEvent.click(within(menu).getByRole("menuitem", { name: "Stop", hidden: true }));

    expect(await screen.findByText("Stopped web")).toBeInTheDocument();
    const [call] = actionCalls(calls);
    expect(call?.path).toBe(`${base}/${web.id}/stop`);
    expect(call?.query.get("namespace")).toBe("default");
    expect(call?.headers.get("X-CSRF-Token")).toBe("csrf-123");
    // The list is refreshed afterwards.
    await waitFor(() => expect(calls.filter((c) => c.path === base).length).toBeGreaterThan(1));
  });

  it("asks before kill and shows server errors", async () => {
    const { calls } = setup({
      [`POST ${base}/${web.id}/kill`]: () =>
        json(409, { error: { code: "conflict", message: "container is not running" } }),
    });
    renderApp("/env/local/containers");

    fireEvent.click(
      within(await openMenu("web")).getByRole("menuitem", { name: "Kill…", hidden: true }),
    );
    const dialog = await screen.findByRole("dialog", { name: "Kill container?" });
    expect(actionCalls(calls)).toHaveLength(0);
    fireEvent.click(within(dialog).getByRole("button", { name: "Kill" }));

    expect(await screen.findByText("Cannot kill web")).toBeInTheDocument();
    expect(screen.getByText("container is not running")).toBeInTheDocument();
  });

  it("removes a running container only with force", async () => {
    const { calls } = setup({
      [`DELETE ${base}/${web.id}`]: () => new Response(null, { status: 204 }),
    });
    renderApp("/env/local/containers");

    fireEvent.click(
      within(await openMenu("web")).getByRole("menuitem", { name: "Remove…", hidden: true }),
    );
    const dialog = await screen.findByRole("dialog", { name: "Remove container?" });
    const remove = within(dialog).getByRole("button", { name: "Remove" });
    expect(remove).toBeDisabled();
    fireEvent.click(within(dialog).getByLabelText(/Force: kill the running container/));
    fireEvent.click(within(dialog).getByLabelText("Also remove anonymous volumes"));
    fireEvent.click(remove);

    expect(await screen.findByText("Removed web")).toBeInTheDocument();
    const [call] = actionCalls(calls);
    expect(call?.method).toBe("DELETE");
    expect(call?.query.get("force")).toBe("true");
    expect(call?.query.get("volumes")).toBe("true");
  });

  it("applies bulk actions to the selected containers they fit", async () => {
    const { calls } = setup({
      [`POST ${base}/actions`]: () =>
        json(200, {
          action: "stop",
          results: [
            { id: web.id, ok: true },
            {
              id: cache.id,
              ok: false,
              error: { code: "internal", message: "timeout waiting for exit" },
            },
          ],
        }),
    });
    renderApp("/env/local/containers");

    fireEvent.click(await screen.findByLabelText("Select all shown containers"));
    const toolbar = screen.getByRole("toolbar", { name: "Selected containers" });
    expect(within(toolbar).getByText("3 selected")).toBeInTheDocument();
    // Stop fits two of three (db has exited).
    fireEvent.click(within(toolbar).getByRole("button", { name: /Stop \(2\)/ }));

    expect(await screen.findByText("Stopped 1 of 2 containers")).toBeInTheDocument();
    expect(screen.getByText(/cache: timeout waiting for exit/)).toBeInTheDocument();
    const [call] = actionCalls(calls);
    expect(call?.body).toEqual({ action: "stop", ids: [web.id, cache.id] });

    fireEvent.click(within(toolbar).getByRole("button", { name: "Clear selection" }));
    expect(screen.queryByRole("toolbar")).not.toBeInTheDocument();
  });

  it("disables actions in read-only mode", async () => {
    setup({
      "GET /api/v1/environments/local": () =>
        json(200, {
          ...envStatus,
          runtime: {
            ...runtime,
            nerdctl: { ...runtime.nerdctl, version: "2.3.0", compatible: false },
          },
        }),
    });
    renderApp("/env/local/containers");
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Actions for web" })).toBeDisabled(),
    );
    fireEvent.click(screen.getByLabelText("Select web"));
    const toolbar = screen.getByRole("toolbar", { name: "Selected containers" });
    for (const b of within(toolbar).getAllByRole("button")) {
      if (b.getAttribute("aria-label") !== "Clear selection") expect(b).toBeDisabled();
    }
  });
});
