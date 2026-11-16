// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { fakeApi, json } from "../../test/server";

const key = {
  id: 7,
  name: "home-assistant",
  prefix: "nerdui_AbC123",
  readOnly: true,
  createdAt: "2027-03-01T10:00:00Z",
  expiresAt: "2099-01-01T00:00:00Z",
};

describe("API keys", () => {
  it("creates a key, shows it once and revokes keys", async () => {
    let keys = [key];
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/auth/api-keys": () => json(200, { keys }),
      "POST /api/v1/auth/api-keys": (c) => {
        const body = c.body as { name: string; readOnly: boolean };
        const k = {
          ...key,
          id: 8,
          name: body.name,
          readOnly: body.readOnly,
          prefix: "nerdui_ZzZ999",
        };
        keys = [k, ...keys];
        return json(201, { ...k, key: "nerdui_ZzZ999-secret" });
      },
      "DELETE /api/v1/auth/api-keys/7": () => {
        keys = keys.filter((k) => k.id !== 7);
        return new Response(null, { status: 204 });
      },
    });
    renderApp("/api-keys");

    const table = await screen.findByRole("table");
    expect(within(table).getByText("home-assistant")).toBeInTheDocument();
    expect(within(table).getByText("read-only")).toBeInTheDocument();
    expect(within(table).getByText("nerdui_AbC123…")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Create API key" }));
    const dialog = await screen.findByRole("dialog", { name: "Create API key" });
    fireEvent.change(within(dialog).getByLabelText(/Name/), { target: { value: "ci" } });
    fireEvent.click(within(dialog).getByLabelText(/Read-only/));
    expect(within(dialog).getByText(/can do everything you can/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Create" }));
    expect(await within(dialog).findByLabelText("New API key")).toHaveTextContent(
      "nerdui_ZzZ999-secret",
    );
    const body = calls.find((c) => c.method === "POST")?.body as {
      name: string;
      readOnly: boolean;
      expiresAt: string;
    };
    expect(body.name).toBe("ci");
    expect(body.readOnly).toBe(false);
    // 90 days by default.
    expect(Date.parse(body.expiresAt) - Date.now()).toBeGreaterThan(89 * 86_400_000);
    fireEvent.click(within(dialog).getByRole("button", { name: "Done" }));
    await waitFor(() => expect(screen.queryByLabelText("New API key")).not.toBeInTheDocument());
    expect(await within(await screen.findByRole("table")).findByText("ci")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Revoke home-assistant" }));
    const confirm = await screen.findByRole("dialog", { name: "Revoke API key" });
    fireEvent.click(within(confirm).getByRole("button", { name: "Revoke" }));
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE")).toBe(true));
    await waitFor(() => expect(screen.queryByText("home-assistant")).not.toBeInTheDocument());
  });
});
