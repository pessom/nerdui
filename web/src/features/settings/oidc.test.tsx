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

  describe("provider presets", () => {
    const stored = {
      enabled: false,
      issuer: "",
      clientId: "nerdui",
      hasClientSecret: true,
      scopes: ["profile"],
      buttonLabel: "",
      callbackUrl: "https://nerdui.example.com/api/v1/auth/oidc/callback",
    };

    async function openCard() {
      const { calls } = fakeApi({
        ...signedIn,
        "GET /api/v1/settings/oidc": () => json(200, stored),
        "PUT /api/v1/settings/oidc": (c) => json(200, { ...stored, ...(c.body as object) }),
      });
      renderApp("/env/local/settings");
      const card = await screen.findByRole("region", { name: "Single sign-on" });
      await within(card).findByText("https://nerdui.example.com/api/v1/auth/oidc/callback");
      return { card, calls };
    }

    async function choose(card: HTMLElement, provider: string) {
      fireEvent.click(within(card).getByLabelText("Provider", { selector: "input" }));
      fireEvent.click(await screen.findByRole("option", { name: provider, hidden: true }));
    }

    it("fills the empty fields, keeps the typed ones and the secret", async () => {
      const { card, calls } = await openCard();
      await choose(card, "Microsoft Entra ID");

      const issuer = within(card).getByRole("textbox", { name: /Issuer URL/ });
      expect(issuer).toHaveValue("https://login.microsoftonline.com/<tenant-id>/v2.0");
      expect(within(card).getByText("Replace <tenant-id> with your value")).toBeInTheDocument();
      expect(within(card).getByRole("textbox", { name: /Button label/ })).toHaveValue(
        "Sign in with Microsoft",
      );
      // Typed values are kept until the administrator replaces them.
      expect(within(card).getByRole("textbox", { name: /Scopes/ })).toHaveValue("profile");
      expect(within(card).getByRole("textbox", { name: /Client ID/ })).toHaveValue("nerdui");
      expect(within(card).getByText(/Kept your values of Scopes/)).toBeInTheDocument();
      expect(
        within(card).getByRole("link", { name: "Microsoft Entra ID documentation" }),
      ).toHaveAttribute(
        "href",
        "https://learn.microsoft.com/en-us/entra/identity-platform/v2-protocols-oidc",
      );

      fireEvent.click(
        within(card).getByRole("button", { name: "Use the Microsoft Entra ID values" }),
      );
      expect(within(card).getByRole("textbox", { name: /Scopes/ })).toHaveValue("profile email");
      expect(within(card).queryByText(/Kept your values/)).not.toBeInTheDocument();

      fireEvent.change(issuer, {
        target: { value: "https://login.microsoftonline.com/0000-1111/v2.0" },
      });
      expect(within(card).queryByText(/with your value/)).not.toBeInTheDocument();
      fireEvent.click(within(card).getByRole("button", { name: "Save" }));
      await waitFor(() => expect(calls.find((c) => c.method === "PUT")).toBeDefined());
      const body = calls.find((c) => c.method === "PUT")?.body as Record<string, unknown>;
      expect(body).toMatchObject({
        issuer: "https://login.microsoftonline.com/0000-1111/v2.0",
        clientId: "nerdui",
        scopes: ["profile", "email"],
        buttonLabel: "Sign in with Microsoft",
      });
      expect(body).not.toHaveProperty("clientSecret");
    });

    it("sets the username claim of a provider without usernames", async () => {
      const { card } = await openCard();
      await choose(card, "Google");
      expect(within(card).getByRole("textbox", { name: /Issuer URL/ })).toHaveValue(
        "https://accounts.google.com",
      );
      expect(within(card).getByRole("textbox", { name: /Username claim/ })).toHaveValue("email");
    });

    it("leaves the fields alone for a custom provider", async () => {
      const { card } = await openCard();
      await choose(card, "Keycloak");
      await choose(card, "Custom");
      // Choosing Custom does not undo or change anything.
      expect(within(card).getByRole("textbox", { name: /Issuer URL/ })).toHaveValue(
        "https://<keycloak-host>/realms/<realm>",
      );
      expect(within(card).queryByRole("link", { name: /documentation/ })).not.toBeInTheDocument();
    });

    it("leaves an untouched form alone with Custom", async () => {
      const { card } = await openCard();
      await choose(card, "Custom");
      expect(within(card).getByRole("textbox", { name: /Issuer URL/ })).toHaveValue("");
      expect(within(card).getByRole("textbox", { name: /Scopes/ })).toHaveValue("profile");
      expect(within(card).getByRole("textbox", { name: /Button label/ })).toHaveValue("");
    });
  });
});
