// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { UserDetail } from "../../api/queries";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json, roles, viewerSession } from "../../test/server";

const bob: UserDetail = {
  id: 2,
  username: "bob",
  role: "viewer",
  mustChangePassword: false,
  mfaEnabled: false,
  sso: false,
  disabled: false,
  createdAt: "2027-01-03T00:00:00Z",
};

describe("roles", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("creates a custom role and gives it to a user", async () => {
    let list = [...roles];
    let users = [bob];
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/users": () => json(200, { users }),
      "GET /api/v1/roles": () => json(200, list),
      "POST /api/v1/roles": () => {
        const r = {
          name: "deployer",
          description: "CI",
          builtin: false,
          permissions: ["jobs:read", "stacks:read", "stacks:deploy"],
          users: 0,
        };
        list = [...list, r];
        return json(201, r);
      },
      "PATCH /api/v1/users/2": () => {
        users = [{ ...bob, role: "deployer" }];
        return json(200, users[0]);
      },
    });
    renderApp("/users");

    const section = await screen.findByRole("table", { name: "Roles" });
    expect(await within(section).findByText("operator")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Add role" }));
    const dialog = await screen.findByRole("dialog", { name: "Add role" });
    fireEvent.change(within(dialog).getByLabelText(/^Name/), { target: { value: "deployer" } });
    fireEvent.change(within(dialog).getByLabelText(/Description/), { target: { value: "CI" } });
    fireEvent.click(within(dialog).getByLabelText("stacks:read"));
    fireEvent.click(within(dialog).getByLabelText("stacks:deploy"));
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(within(section).getByText("deployer")).toBeInTheDocument());
    expect(calls.find((c) => c.method === "POST" && c.path === "/api/v1/roles")?.body).toEqual({
      name: "deployer",
      description: "CI",
      permissions: ["stacks:read", "stacks:deploy"],
    });

    fireEvent.click(screen.getByRole("button", { name: "Actions for bob" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Change role", hidden: true }));
    const change = await screen.findByRole("dialog", { name: "Role of bob" });
    fireEvent.click(within(change).getByLabelText(/^Role/, { selector: "input" }));
    fireEvent.click(await screen.findByRole("option", { name: "deployer", hidden: true }));
    fireEvent.click(within(change).getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ role: "deployer" }),
    );
    const usersTable = screen.getByRole("table", { name: "Users" });
    expect(await within(usersTable).findByText("deployer")).toBeInTheDocument();
  });

  it("hides administration from a viewer", async () => {
    fakeApi({
      ...signedIn,
      "GET /api/v1/auth/me": () => json(200, viewerSession),
      "GET /api/v1/users": () => json(403, { error: { code: "forbidden", message: "no" } }),
    });
    renderApp("/env/local/containers");
    const nav = await screen.findByRole("navigation", { name: "Main navigation" });
    expect(await within(nav).findByText("Containers")).toBeInTheDocument();
    for (const item of ["Users", "Settings", "Schedules", "Audit log"]) {
      expect(within(nav).queryByText(item)).not.toBeInTheDocument();
    }
  });
});
