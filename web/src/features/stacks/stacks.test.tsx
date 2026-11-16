// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { fakeApi, json } from "../../test/server";

const base = "/api/v1/environments/local/stacks";

const app = {
  name: "app",
  mode: "managed",
  path: "/var/lib/nerdui/stacks/app",
  composeFile: "compose.yaml",
  status: "partial",
  containers: 3,
  running: 2,
  services: [
    {
      name: "web",
      defined: true,
      running: 1,
      containers: [{ id: "a".repeat(64), name: "app-web-1", state: "running" }],
    },
    { name: "db", defined: true, running: 0, containers: [] },
    {
      name: "old",
      defined: false,
      running: 1,
      containers: [{ id: "b".repeat(64), name: "app-old-1", state: "running" }],
    },
  ],
};

const manual = {
  name: "manual",
  mode: "unmanaged",
  status: "unknown",
  containers: 1,
  running: 1,
  services: [{ name: "web", defined: false, running: 1, containers: [] }],
};

describe("stacks", () => {
  it("lists stacks with their status and opens one", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      [`GET ${base}`]: () => json(200, { namespace: "default", stacks: [app, manual] }),
      [`GET ${base}/app`]: () => json(200, app),
    });
    renderApp("/env/local/stacks");

    const table = await screen.findByRole("table");
    const row = within(table).getByRole("link", { name: "app" }).closest("tr")!;
    expect(within(row).getByText("partial")).toBeInTheDocument();
    expect(within(row).getByText("1 / 2 running")).toBeInTheDocument();
    expect(within(row).getByText("2 / 3 running")).toBeInTheDocument();
    const manualRow = within(table).getByRole("link", { name: "manual" }).closest("tr")!;
    expect(within(manualRow).getByText("unmanaged")).toBeInTheDocument();
    expect(within(manualRow).getByText("unknown")).toBeInTheDocument();
    expect(calls.find((c) => c.path === base)?.query.get("namespace")).toBe("default");

    fireEvent.click(within(table).getByRole("link", { name: "app" }));
    expect(await screen.findByRole("heading", { name: "app" })).toBeInTheDocument();
    const web = await screen.findByRole("region", { name: "web" });
    expect(within(web).getByRole("link", { name: "app-web-1" })).toBeInTheDocument();
    expect(
      within(screen.getByRole("region", { name: "db" })).getByText("not created"),
    ).toBeInTheDocument();
    expect(
      within(screen.getByRole("region", { name: "old" })).getByText("not in compose file"),
    ).toBeInTheDocument();
    const nav = screen.getByRole("navigation", { name: "Main navigation" });
    expect(within(nav).getByRole("link", { name: "Stacks" })).toHaveAttribute(
      "data-active",
      "true",
    );
  });
});
