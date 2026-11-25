// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { fakeApi, json, unauthenticated } from "../../test/server";

describe("single sign-on", () => {
  it("shows the provider's button and the reason of a failed sign-in", async () => {
    fakeApi({
      "GET /api/v1/auth/me": () => unauthenticated(),
      "GET /api/v1/setup": () => json(200, { required: false }),
      "GET /api/v1/version": () => json(200, { version: "test" }),
      "GET /api/v1/auth/oidc": () =>
        json(200, { enabled: true, buttonLabel: "Sign in with Keycloak" }),
    });
    renderApp("/login?redirect=%2Fstacks&sso_error=your%20account%20is%20not%20allowed");
    const button = await screen.findByRole("link", { name: "Sign in with Keycloak" });
    expect(button).toHaveAttribute("href", "/api/v1/auth/oidc/login?redirect=%2Fstacks");
    expect(screen.getByText("your account is not allowed")).toBeInTheDocument();
    expect(screen.getByLabelText(/Username/)).toBeInTheDocument();
  });

  it("hides the button while single sign-on is off", async () => {
    fakeApi({
      "GET /api/v1/auth/me": () => unauthenticated(),
      "GET /api/v1/setup": () => json(200, { required: false }),
      "GET /api/v1/version": () => json(200, { version: "test" }),
      "GET /api/v1/auth/oidc": () => json(200, { enabled: false, buttonLabel: "Sign in with SSO" }),
    });
    renderApp("/login");
    expect(await screen.findByLabelText(/Username/)).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Sign in with SSO" })).not.toBeInTheDocument();
  });

  it("saves the provider settings; the client secret is write-only", async () => {
    let stored: Record<string, unknown> = {
      enabled: false,
      issuer: "",
      clientId: "",
      hasClientSecret: true,
      scopes: null,
      callbackUrl: "https://nerdui.example.com/api/v1/auth/oidc/callback",
    };
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/settings/oidc": () => json(200, stored),
      "PUT /api/v1/settings/oidc": (c) => {
        stored = { ...stored, ...(c.body as object), clientSecret: undefined };
        return json(200, stored);
      },
    });
    renderApp("/env/local/settings");
    const card = await screen.findByRole("region", { name: "Single sign-on" });
    expect(
      await within(card).findByText("https://nerdui.example.com/api/v1/auth/oidc/callback"),
    ).toBeInTheDocument();
    fireEvent.click(within(card).getByRole("switch", { name: /OpenID Connect provider/ }));
    fireEvent.change(within(card).getByRole("textbox", { name: /Issuer URL/ }), {
      target: { value: "https://id.example.com/realms/main" },
    });
    fireEvent.change(within(card).getByRole("textbox", { name: /Client ID/ }), {
      target: { value: "nerdui" },
    });
    fireEvent.change(within(card).getByRole("textbox", { name: /^Group\s*\*?$/ }), {
      target: { value: "admins" },
    });
    fireEvent.click(within(card).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(calls.find((c) => c.method === "PUT")).toBeDefined());
    const body = calls.find((c) => c.method === "PUT")?.body as Record<string, unknown>;
    expect(body).toMatchObject({
      enabled: true,
      issuer: "https://id.example.com/realms/main",
      clientId: "nerdui",
      group: "admins",
      scopes: [],
    });
    // The stored secret is kept: it is not sent.
    expect(body).not.toHaveProperty("clientSecret");
  });
});
