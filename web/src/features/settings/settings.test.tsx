// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { fakeApi, json } from "../../test/server";

const path = "/api/v1/environments/local/settings/prometheus";

describe("environment settings", () => {
  it("tests, saves and turns off the Prometheus connection", async () => {
    let stored: Record<string, unknown> = { configured: false, hasSecret: false };
    const { calls } = fakeApi({
      ...signedIn,
      [`GET ${path}`]: () => json(200, stored),
      [`POST ${path}/test`]: () => json(200, { version: "3.5.0", containers: 2 }),
      [`PUT ${path}`]: (c) => {
        const body = c.body as { url: string; auth: string; username: string };
        stored = {
          configured: true,
          url: body.url,
          auth: body.auth,
          username: body.username,
          hasSecret: true,
        };
        return json(200, stored);
      },
      [`DELETE ${path}`]: () => {
        stored = { configured: false, hasSecret: false };
        return new Response(null, { status: 204 });
      },
    });
    renderApp("/env/local/settings");

    const card = await screen.findByRole("region", { name: "Metrics history" });
    fireEvent.change(await within(card).findByLabelText(/Prometheus URL/), {
      target: { value: "http://127.0.0.1:9090" },
    });
    fireEvent.click(within(card).getByRole("radio", { name: "Basic" }));
    fireEvent.change(within(card).getByLabelText(/Username/), { target: { value: "nerdui" } });
    fireEvent.change(within(card).getByLabelText(/Password/), { target: { value: "s3cret" } });

    fireEvent.click(within(card).getByRole("button", { name: "Test connection" }));
    expect(
      await within(card).findByText(/Prometheus 3.5.0: containerd metrics of 2 containers found/),
    ).toBeInTheDocument();
    expect(calls.find((c) => c.method === "POST")?.body).toEqual({
      url: "http://127.0.0.1:9090",
      auth: "basic",
      username: "nerdui",
      secret: "s3cret",
    });

    fireEvent.click(within(card).getByRole("button", { name: "Save" }));
    // Saved: the password field is cleared and keeps the stored secret when left empty.
    await waitFor(() =>
      expect(within(card).getByLabelText(/Password/)).toHaveAttribute("placeholder", "unchanged"),
    );
    expect(within(card).getByLabelText(/Password/)).not.toBeRequired();
    expect(calls.find((c) => c.method === "PUT")?.body).toMatchObject({ secret: "s3cret" });

    fireEvent.click(await within(card).findByRole("button", { name: "Turn off" }));
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE")).toBe(true));
    await waitFor(() =>
      expect(within(card).queryByRole("button", { name: "Turn off" })).not.toBeInTheDocument(),
    );
    // Settings is in the menu and has no namespace selector (host-wide).
    expect(screen.getByRole("link", { name: "Settings" })).toHaveAttribute("data-active", "true");
    expect(screen.queryByRole("textbox", { name: "Namespace" })).not.toBeInTheDocument();
  });

  it("explains a server without containerd metrics", async () => {
    fakeApi({
      ...signedIn,
      [`GET ${path}`]: () => json(200, { configured: false, hasSecret: false }),
      [`POST ${path}/test`]: () => json(200, { containers: 0 }),
    });
    renderApp("/env/local/settings");
    const card = await screen.findByRole("region", { name: "Metrics history" });
    fireEvent.change(await within(card).findByLabelText(/Prometheus URL/), {
      target: { value: "http://prom:9090" },
    });
    fireEvent.click(within(card).getByRole("button", { name: "Test connection" }));
    expect(
      await within(card).findByText("Connected, but no containerd metrics"),
    ).toBeInTheDocument();
  });
});
