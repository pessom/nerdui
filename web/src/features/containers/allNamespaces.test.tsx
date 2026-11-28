// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";
import { groupByProject, type Container } from "./format";

const base = "/api/v1/environments/local/containers";

function container(over: Partial<Container>): Container {
  return {
    id: "a".repeat(64),
    name: "c",
    namespace: "default",
    image: "busybox",
    command: "sleep 300",
    state: "running",
    createdAt: "2027-03-01T10:00:00Z",
    ports: [],
    labels: {},
    ...over,
  };
}

const web = container({ id: "1".repeat(64), name: "web", namespace: "default" });
const kube = container({ id: "2".repeat(64), name: "kube-proxy", namespace: "k8s.io" });

describe("containers of all namespaces (#276)", () => {
  beforeEach(() => {
    vi.stubGlobal("WebSocket", SilentWebSocket);
    localStorage.clear();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("lists every namespace with a Namespace column and acts in each container's namespace", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      [`GET ${base}`]: (c) =>
        c.query.get("namespace") === "*"
          ? json(200, { namespace: "*", containers: [web, kube] })
          : json(200, { namespace: c.query.get("namespace"), containers: [web] }),
      [`POST ${base}/actions`]: (c) =>
        json(200, {
          results: (c.body as { ids: string[] }).ids.map((id) => ({ id, ok: true })),
        }),
    });
    const { router } = renderApp("/env/local/containers?ns=default");
    expect(await screen.findByText("web")).toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: /Namespace/ })).not.toBeInTheDocument();

    fireEvent.click(screen.getByLabelText("Namespace", { selector: "input" }));
    fireEvent.click(await screen.findByRole("option", { name: "<All namespaces>", hidden: true }));
    await waitFor(() => expect(router.state.location.search).toEqual({ ns: "*" }));
    expect(await screen.findByText("kube-proxy")).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: /Namespace/ })).toBeInTheDocument();
    // "All" is not remembered: other pages need one namespace.
    expect(localStorage.getItem("nerdui.namespace.local")).toBeNull();

    // Each container opens in its own namespace.
    const kubeRow = screen.getByText("kube-proxy").closest("tr")!;
    expect(within(kubeRow).getByText("k8s.io")).toBeInTheDocument();
    expect(within(kubeRow).getByRole("link", { name: "kube-proxy" })).toHaveAttribute(
      "href",
      expect.stringContaining("ns=k8s.io"),
    );

    // A bulk action is sent once per namespace.
    fireEvent.click(screen.getByLabelText("Select all shown containers"));
    const toolbar = await screen.findByRole("toolbar", { name: "Selected containers" });
    fireEvent.click(within(toolbar).getByRole("button", { name: /Restart/ }));
    await waitFor(() => expect(calls.filter((c) => c.path === `${base}/actions`)).toHaveLength(2));
    const sent = calls
      .filter((c) => c.path === `${base}/actions`)
      .map((c) => [c.query.get("namespace"), (c.body as { ids: string[] }).ids])
      .sort();
    expect(sent).toEqual([
      ["default", [web.id]],
      ["k8s.io", [kube.id]],
    ]);
  });

  it("goes back to one namespace on other pages", async () => {
    localStorage.setItem("nerdui.namespace.local", "k8s.io");
    fakeApi({
      ...signedIn,
      "GET /api/v1/templates": () => json(200, { templates: [] }),
    });
    const { router } = renderApp("/env/local/templates?ns=*");
    await waitFor(() => expect(router.state.location.search).toEqual({ ns: "k8s.io" }));
  });
});

describe("groupByProject", () => {
  it("keeps projects of the same name in different namespaces apart", () => {
    const a = container({ id: "a", namespace: "default", compose: { project: "app" } });
    const b = container({ id: "b", namespace: "k8s.io", compose: { project: "app" } });
    expect(groupByProject([a, b])).toEqual([{ project: "app", containers: [a, b] }]);
    expect(groupByProject([a, b], true)).toEqual([
      { project: "app", namespace: "default", containers: [a] },
      { project: "app", namespace: "k8s.io", containers: [b] },
    ]);
  });
});
