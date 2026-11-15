// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { fakeApi, json } from "../../test/server";

const base = "/api/v1/environments/local/networks";

const networks = [
  {
    name: "app",
    id: "abc",
    driver: "bridge",
    labels: {},
    ipam: [{ subnet: "10.20.0.0/24", gateway: "10.20.0.1" }],
    internal: true,
    default: false,
    removable: true,
    containers: [{ id: "c".repeat(64), name: "old", running: false }],
  },
  {
    name: "bridge",
    driver: "bridge",
    labels: {},
    ipam: [{ subnet: "10.4.0.0/24", gateway: "10.4.0.1" }],
    internal: false,
    default: true,
    removable: false,
    containers: [{ id: "d".repeat(64), name: "web", running: true, ips: ["10.4.0.5"] }],
  },
];

describe("networks", () => {
  it("lists networks with containers, creates macvlan and removes with a warning", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      [`GET ${base}`]: () => json(200, { namespace: "default", networks }),
      [`POST ${base}`]: (c) => json(201, { ...networks[0], ...(c.body as object) }),
      [`DELETE ${base}/app`]: () => new Response(null, { status: 204 }),
    });
    renderApp("/env/local/networks");

    // "default" is also the namespace: look inside the table.
    const table = await screen.findByRole("table");
    const bridge = within(table).getByText("default").closest("tr")!;
    expect(within(bridge).getByRole("link", { name: "web" })).toBeInTheDocument();
    expect(within(bridge).getByText("10.4.0.5")).toBeInTheDocument();
    expect(within(bridge).getByRole("button", { name: "Remove bridge" })).toBeDisabled();
    const app = within(table).getByText("internal").closest("tr")!;
    expect(within(app).getByText("10.20.0.0/24")).toBeInTheDocument();
    expect(within(app).getByText("stopped")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Create network" }));
    const create = await screen.findByRole("dialog", { name: "Create network" });
    fireEvent.change(within(create).getByLabelText(/Name/), { target: { value: "lan" } });
    fireEvent.click(within(create).getByRole("radio", { name: "macvlan" }));
    expect(within(create).queryByLabelText(/Internal/)).not.toBeInTheDocument();
    fireEvent.change(within(create).getByLabelText(/Parent interface/), {
      target: { value: "eth0" },
    });
    fireEvent.change(within(create).getByLabelText(/Subnet/), {
      target: { value: "192.168.1.0/24" },
    });
    fireEvent.click(within(create).getByRole("button", { name: "Create" }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === "POST")?.body).toEqual({
        name: "lan",
        driver: "macvlan",
        subnet: "192.168.1.0/24",
        parent: "eth0",
        labels: {},
      }),
    );

    fireEvent.click(within(app).getByRole("button", { name: "Remove app" }));
    const remove = await screen.findByRole("dialog", { name: "Remove network" });
    expect(within(remove).getByText("Stopped containers use this network")).toBeInTheDocument();
    expect(within(remove).getByText("old")).toBeInTheDocument();
    fireEvent.click(within(remove).getByRole("button", { name: "Remove network" }));
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE")).toBe(true));
    expect(screen.getByRole("link", { name: "Networks" })).toHaveAttribute("data-active", "true");
  });
});
