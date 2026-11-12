// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json, type Call } from "../../test/server";

const base = "/api/v1/environments/local/containers";

function setup() {
  return fakeApi({
    ...signedIn,
    [`GET ${base}`]: (c) => json(200, { namespace: c.query.get("namespace"), containers: [] }),
  });
}

const listedNamespaces = (calls: Call[]) =>
  calls.filter((c) => c.path === base).map((c) => c.query.get("namespace"));

async function chooseNamespace(name: string) {
  fireEvent.click(await screen.findByLabelText("Namespace", { selector: "input" }));
  fireEvent.click(await screen.findByRole("option", { name, hidden: true }));
}

describe("namespace selector", () => {
  beforeEach(() => {
    vi.stubGlobal("WebSocket", SilentWebSocket);
    localStorage.clear();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("stays on the current page and remembers the choice", async () => {
    const { calls } = setup();
    const { router } = renderApp("/env/local/containers?ns=default");
    await waitFor(() => expect(listedNamespaces(calls)).toContain("default"));

    await chooseNamespace("k8s.io");

    await waitFor(() => expect(router.state.location.search).toEqual({ ns: "k8s.io" }));
    expect(router.state.location.pathname).toBe("/env/local/containers");
    await waitFor(() => expect(listedNamespaces(calls)).toContain("k8s.io"));
    expect(localStorage.getItem("nerdui.namespace.local")).toBe("k8s.io");
  });

  it("is not shown on the host-wide overview", async () => {
    setup();
    renderApp("/env/local");
    expect(await screen.findByRole("heading", { name: "Local host" })).toBeInTheDocument();
    expect(screen.getByLabelText("Environment", { selector: "input" })).toBeInTheDocument();
    expect(screen.queryByLabelText("Namespace", { selector: "input" })).not.toBeInTheDocument();
  });

  it("restores the remembered namespace and keeps it in the menu link", async () => {
    localStorage.setItem("nerdui.namespace.local", "k8s.io");
    const { calls } = setup();
    const { router } = renderApp("/env/local");

    await waitFor(() => expect(router.state.location.search).toEqual({ ns: "k8s.io" }));
    fireEvent.click(screen.getByRole("link", { name: "Containers" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/env/local/containers"));
    expect(router.state.location.search).toEqual({ ns: "k8s.io" });
    await waitFor(() => expect(listedNamespaces(calls)).toEqual(["k8s.io"]));
  });

  it("forgets a remembered namespace that no longer exists", async () => {
    localStorage.setItem("nerdui.namespace.local", "gone");
    const { calls } = setup();
    const { router } = renderApp("/env/local/containers");

    await waitFor(() => expect(listedNamespaces(calls)).toContain("default"));
    await waitFor(() => expect(localStorage.getItem("nerdui.namespace.local")).toBeNull());
    expect(router.state.location.search).toEqual({});
  });
});
