// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { UserDetail } from "../../api/queries";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json, session, unauthenticated } from "../../test/server";

const admin: UserDetail = {
  id: 1,
  username: "admin",
  role: "admin",
  mustChangePassword: false,
  disabled: false,
  createdAt: "2027-01-01T00:00:00Z",
  lastLoginAt: "2027-01-02T00:00:00Z",
};
const bob: UserDetail = {
  id: 2,
  username: "bob",
  role: "admin",
  mustChangePassword: true,
  disabled: false,
  createdAt: "2027-01-03T00:00:00Z",
};

describe("users page", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("lists users and creates one with a generated password", async () => {
    let users = [admin, bob];
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/users": () => json(200, { users }),
      "POST /api/v1/users": () => {
        const carol = { ...bob, id: 3, username: "carol" };
        users = [...users, carol];
        return json(201, { user: carol, generatedPassword: "generated-secret-123" });
      },
    });
    renderApp("/users");

    const row = (await screen.findByText("bob")).closest("tr")!;
    expect(within(row).getByText("temporary password")).toBeInTheDocument();
    expect(within(row).getByText("never")).toBeInTheDocument();
    // Own account: actions are disabled.
    expect(screen.getByRole("button", { name: "Actions for admin" })).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: "Add user" }));
    const dialog = await screen.findByRole("dialog", { name: "Add user" });
    fireEvent.change(within(dialog).getByLabelText(/Username/), { target: { value: "carol" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Create user" }));

    expect(await within(dialog).findByLabelText("Generated password")).toHaveTextContent(
      "generated-secret-123",
    );
    expect(calls.find((c) => c.method === "POST" && c.path === "/api/v1/users")?.body).toEqual({
      username: "carol",
    });
    expect(await within(screen.getByRole("table")).findByText("carol")).toBeInTheDocument();
  });

  it("sends a typed temporary password and validates its length", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/users": () => json(200, { users: [admin, bob] }),
      "POST /api/v1/users/2/password": () => json(200, {}),
    });
    renderApp("/users");

    fireEvent.click(await screen.findByRole("button", { name: "Actions for bob" }));
    fireEvent.click(
      await screen.findByRole("menuitem", { name: "Set temporary password", hidden: true }),
    );
    const dialog = await screen.findByRole("dialog", { name: /Set temporary password for bob/ });
    fireEvent.click(within(dialog).getByText("Enter manually"));
    const input = within(dialog).getByLabelText(/Temporary password/, { selector: "input" });
    fireEvent.change(input, { target: { value: "short" } });
    expect(within(dialog).getByRole("button", { name: "Set password" })).toBeDisabled();
    fireEvent.change(input, { target: { value: "a long temporary one" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Set password" }));

    expect(await within(dialog).findByText("Temporary password set.")).toBeInTheDocument();
    expect(calls.find((c) => c.path === "/api/v1/users/2/password")?.body).toEqual({
      password: "a long temporary one",
    });
  });

  it("disables and deletes after confirmation and reports errors", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/users": () => json(200, { users: [admin, bob] }),
      "PATCH /api/v1/users/2": () => json(200, { ...bob, disabled: true }),
      "DELETE /api/v1/users/2": () =>
        json(409, {
          error: { code: "conflict", message: "at least one active administrator is required" },
        }),
    });
    renderApp("/users");

    fireEvent.click(await screen.findByRole("button", { name: "Actions for bob" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Disable", hidden: true }));
    const confirm = await screen.findByRole("dialog", { name: "Disable bob?" });
    fireEvent.click(within(confirm).getByRole("button", { name: "Disable" }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ disabled: true }),
    );
    expect(await screen.findByText("bob disabled")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Actions for bob" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Delete", hidden: true }));
    const del = await screen.findByRole("dialog", { name: "Delete bob?" });
    fireEvent.click(within(del).getByRole("button", { name: "Delete" }));
    expect(
      await screen.findByText("at least one active administrator is required"),
    ).toBeInTheDocument();
  });
});

describe("temporary password", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("forces a password change after sign-in", async () => {
    let me: typeof session | undefined;
    const temp = {
      ...session,
      user: { ...session.user, username: "bob", mustChangePassword: true },
    };
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/auth/me": () => (me ? json(200, me) : unauthenticated()),
      "POST /api/v1/auth/login": () => {
        me = temp;
        return json(200, temp);
      },
      "POST /api/v1/auth/password": () => {
        me = { ...temp, user: { ...temp.user, mustChangePassword: false } };
        return json(200, me);
      },
    });
    const { router } = renderApp("/env/local/containers");

    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    fireEvent.change(screen.getByLabelText(/Username/), { target: { value: "bob" } });
    fireEvent.change(screen.getByLabelText(/Password/), { target: { value: "temporary-pass-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/change-password"));
    fireEvent.change(await screen.findByLabelText(/Temporary password/), {
      target: { value: "temporary-pass-1" },
    });
    fireEvent.change(screen.getByLabelText(/^New password/), {
      target: { value: "bob's own password" },
    });
    fireEvent.change(screen.getByLabelText(/Repeat new password/), {
      target: { value: "bob's own password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Set password and continue" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/env/local/containers"));
    expect(calls.find((c) => c.path === "/api/v1/auth/password")?.body).toEqual({
      currentPassword: "temporary-pass-1",
      newPassword: "bob's own password",
    });
  });

  it("redirects an existing session with a temporary password", async () => {
    fakeApi({
      ...signedIn,
      "GET /api/v1/auth/me": () =>
        json(200, { ...session, user: { ...session.user, mustChangePassword: true } }),
    });
    const { router } = renderApp("/users");
    await waitFor(() => expect(router.state.location.pathname).toBe("/change-password"));
    expect(screen.getByRole("heading", { name: "Choose a new password" })).toBeInTheDocument();
  });

  it("changes the own password from the user menu", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      "POST /api/v1/auth/password": () => json(200, session),
    });
    renderApp("/env/local");
    fireEvent.click(await screen.findByRole("button", { name: "admin" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Change password", hidden: true }));
    const dialog = await screen.findByRole("dialog", { name: "Change password" });
    fireEvent.change(within(dialog).getByLabelText(/Current password/), {
      target: { value: "old password 123" },
    });
    fireEvent.change(within(dialog).getByLabelText(/^New password/), {
      target: { value: "new password 456" },
    });
    fireEvent.change(within(dialog).getByLabelText(/Repeat new password/), {
      target: { value: "new password 456" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Change password" }));
    expect(await screen.findByText(/other sessions were signed out/)).toBeInTheDocument();
    expect(calls.some((c) => c.path === "/api/v1/auth/password")).toBe(true);
  });
});
