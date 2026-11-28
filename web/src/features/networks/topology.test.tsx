// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Network } from "../../api/queries";
import { renderApp, signedIn } from "../../test/render";
import { fakeApi, json } from "../../test/server";
import { topologyContainers } from "./topology";

const web = "d".repeat(64);
const db = "e".repeat(64);
const old = "c".repeat(64);

const networks = [
  {
    namespace: "default",
    name: "bridge",
    driver: "bridge",
    labels: {},
    ipam: [{ subnet: "10.4.0.0/24", gateway: "10.4.0.1" }],
    internal: false,
    default: true,
    removable: false,
    containers: [{ id: web, name: "web", running: true, ips: ["10.4.0.5"] }],
  },
  {
    namespace: "default",
    name: "backend",
    driver: "bridge",
    labels: {},
    ipam: [{ subnet: "10.20.0.0/24" }],
    internal: true,
    default: false,
    removable: true,
    containers: [
      { id: db, name: "db", running: true, ips: ["10.20.0.3"] },
      { id: web, name: "web", running: true, ips: ["10.20.0.2"] },
      { id: old, name: "old", running: false },
    ],
  },
  {
    namespace: "default",
    name: "empty",
    driver: "macvlan",
    labels: {},
    ipam: [],
    internal: false,
    default: false,
    removable: true,
    containers: [],
  },
] satisfies Network[];

describe("network topology", () => {
  it("groups a container on several networks into one node ordered by first network", () => {
    const nodes = topologyContainers(networks);
    expect(nodes.map((n) => n.name)).toEqual(["web", "db", "old"]);
    expect(nodes[0]?.links.map((l) => [l.network, l.ips])).toEqual([
      ["bridge", ["10.4.0.5"]],
      ["backend", ["10.20.0.2"]],
    ]);
    expect(nodes[2]).toMatchObject({ running: false, links: [{ network: "backend", ips: [] }] });
  });

  it("draws networks, containers with their addresses and links to containers", async () => {
    fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/networks": () => json(200, { namespace: "dev", networks }),
    });
    renderApp("/env/local/networks?ns=dev");

    fireEvent.click(await screen.findByRole("radio", { name: "Topology" }));
    const graph = await screen.findByTestId("network-topology");
    expect(within(graph).queryByRole("table")).not.toBeInTheDocument();

    const nets = within(graph).getByRole("list", { name: "Networks" });
    expect(
      within(nets)
        .getAllByRole("listitem")
        .map((li) => li.getAttribute("aria-label")),
    ).toEqual(["Network bridge", "Network backend", "Network empty"]);
    expect(within(nets).getByText("10.4.0.0/24")).toBeInTheDocument();
    expect(within(nets).getByText(/no containers/)).toBeInTheDocument();

    const ctrs = within(graph).getByRole("list", { name: "Containers" });
    expect(within(ctrs).getAllByRole("listitem")).toHaveLength(3);
    const webNode = within(ctrs).getByRole("listitem", { name: "Container web" });
    expect(within(webNode).getByText("10.4.0.5")).toBeInTheDocument();
    expect(within(webNode).getByText("10.20.0.2")).toBeInTheDocument();
    expect(within(webNode).getByRole("link", { name: "web" })).toHaveAttribute(
      "href",
      `/env/local/containers/${web}?ns=dev`,
    );
    const oldNode = within(ctrs).getByRole("listitem", { name: "Container old" });
    expect(within(oldNode).getByText("stopped")).toBeInTheDocument();

    // One edge per attachment; the multi-network container has two.
    const edges = [...graph.querySelectorAll("path[data-edge]")].map((p) =>
      p.getAttribute("data-edge"),
    );
    expect(edges).toEqual([`${web}/bridge`, `${web}/backend`, `${db}/backend`, `${old}/backend`]);
    expect(graph.querySelector(`path[data-edge="${old}/backend"]`)).toHaveAttribute(
      "stroke-dasharray",
      "4 4",
    );

    fireEvent.click(screen.getByRole("radio", { name: "Table" }));
    expect(await screen.findByRole("table")).toBeInTheDocument();
  });
});
