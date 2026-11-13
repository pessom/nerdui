// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Registry } from "../../api/queries";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";

describe("registries page", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("lists, logs in and logs out", async () => {
    let registries: Registry[] = [
      { server: "https://index.docker.io/v1/", username: "alice" },
      { server: "gitlab.example", username: "ci", aliases: ["gitlab.example:443"] },
    ];
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/registries": () => json(200, { registries }),
      "POST /api/v1/environments/local/registries": (c) => {
        const body = c.body as { server: string; username: string };
        if (body.server === "bad.example")
          return json(400, { error: { code: "invalid_argument", message: "401 Unauthorized" } });
        registries = [...registries, { server: body.server, username: body.username }];
        return json(201, {
          server: body.server,
          username: body.username,
          warning: "WARNING! Your password will be stored unencrypted",
        });
      },
      "DELETE /api/v1/environments/local/registries": (c) => {
        registries = registries.filter((r) => r.server !== c.query.get("server"));
        return new Response(null, { status: 204 });
      },
    });
    const { router } = renderApp("/env/local?ns=k8s.io");
    fireEvent.click(await screen.findByRole("link", { name: "Registries" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/env/local/registries"));

    expect(await screen.findByText("Docker Hub")).toBeInTheDocument();
    expect(screen.getByText("alice")).toBeInTheDocument();
    expect(screen.getByText("also stored as gitlab.example:443")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Log out of gitlab.example" }));
    const both = await screen.findByRole("dialog", { name: "Log out of gitlab.example?" });
    expect(
      within(both).getByText(/also stored as gitlab.example:443; all of them are removed/),
    ).toBeInTheDocument();
    fireEvent.click(within(both).getByRole("button", { name: "Cancel" }));
    // Host-wide page: no namespace selector.
    expect(screen.queryByLabelText("Namespace", { selector: "input" })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Log in to registry" }));
    let dialog = await screen.findByRole("dialog", { name: "Log in to registry" });
    fireEvent.change(within(dialog).getByLabelText(/^Registry/), {
      target: { value: "bad.example" },
    });
    fireEvent.change(within(dialog).getByLabelText(/^Username/), { target: { value: "bob" } });
    fireEvent.change(within(dialog).getByLabelText(/^Password/), { target: { value: "pw" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Log in" }));
    expect(await within(dialog).findByText("401 Unauthorized")).toBeInTheDocument();

    fireEvent.change(within(dialog).getByLabelText(/^Registry/), {
      target: { value: "reg.local:5000" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Log in" }));
    expect(await screen.findByText("Logged in to reg.local:5000")).toBeInTheDocument();
    expect(screen.getByText(/stored unencrypted/)).toBeInTheDocument();
    expect(await screen.findByRole("cell", { name: "reg.local:5000" })).toBeInTheDocument();
    expect(calls.filter((c) => c.method === "POST").at(-1)?.body).toEqual({
      server: "reg.local:5000",
      username: "bob",
      password: "pw",
    });

    fireEvent.click(screen.getByRole("button", { name: "Log out of reg.local:5000" }));
    dialog = await screen.findByRole("dialog", { name: "Log out of reg.local:5000?" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Log out" }));
    expect(await screen.findByText("Logged out of reg.local:5000")).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.queryByRole("cell", { name: "reg.local:5000" })).not.toBeInTheDocument(),
    );
    expect(calls.find((c) => c.method === "DELETE")?.query.get("server")).toBe("reg.local:5000");
  });
});
