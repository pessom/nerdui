// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json, session, unauthenticated } from "../../test/server";

const bytes = (s: string) => new TextEncoder().encode(s).buffer;

/** A browser with WebAuthn: navigator.credentials answers with fixed bytes. */
function stubWebAuthn() {
  vi.stubGlobal("PublicKeyCredential", function PublicKeyCredential() {});
  vi.stubGlobal("isSecureContext", true);
  const create = vi.fn(async () => ({
    id: "Y3JlZA",
    rawId: bytes("cred"),
    type: "public-key",
    authenticatorAttachment: "platform",
    getClientExtensionResults: () => ({}),
    response: {
      clientDataJSON: bytes("{}"),
      attestationObject: bytes("att"),
      getTransports: () => ["internal"],
    },
  }));
  const get = vi.fn(async () => ({
    id: "Y3JlZA",
    rawId: bytes("cred"),
    type: "public-key",
    authenticatorAttachment: null,
    getClientExtensionResults: () => ({}),
    response: {
      clientDataJSON: bytes("{}"),
      authenticatorData: bytes("ad"),
      signature: bytes("sig"),
      userHandle: bytes("handle"),
    },
  }));
  Object.defineProperty(navigator, "credentials", { value: { create, get }, configurable: true });
  return { create, get };
}

const options = { publicKey: { challenge: "Y2hhbGxlbmdl", rpId: "localhost" } };

describe("passkeys", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("signs in with a passkey alone", async () => {
    const { get } = stubWebAuthn();
    let loggedIn = false;
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/auth/me": () => (loggedIn ? json(200, session) : unauthenticated()),
      "POST /api/v1/auth/passkey/options": () => json(200, { token: "t1", options }),
      "POST /api/v1/auth/passkey/login": () => {
        loggedIn = true;
        return json(200, session);
      },
    });
    const { router } = renderApp("/login");
    fireEvent.click(await screen.findByRole("button", { name: "Sign in with a passkey" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/env/local"));
    expect(get).toHaveBeenCalledOnce();
    expect(calls.find((c) => c.path === "/api/v1/auth/passkey/login")?.body).toEqual({
      token: "t1",
      credential: {
        id: "Y3JlZA",
        rawId: "Y3JlZA",
        type: "public-key",
        response: {
          clientDataJSON: "e30",
          authenticatorData: "YWQ",
          signature: "c2ln",
          userHandle: "aGFuZGxl",
        },
        clientExtensionResults: {},
      },
    });
  });

  it("answers the second step with a passkey", async () => {
    stubWebAuthn();
    let loggedIn = false;
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/auth/me": () => (loggedIn ? json(200, session) : unauthenticated()),
      "POST /api/v1/auth/login": () =>
        json(200, { mfaRequired: true, mfaToken: "tok", totp: false, passkey: true }),
      "POST /api/v1/auth/login/mfa/passkey": () => json(200, { options }),
      "POST /api/v1/auth/login/mfa": () => {
        loggedIn = true;
        return json(200, session);
      },
    });
    const { router } = renderApp("/login");
    fireEvent.change(await screen.findByLabelText(/Username/), { target: { value: "admin" } });
    fireEvent.change(screen.getByLabelText(/Password/), { target: { value: "pw" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByText("One of your backup codes")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Use a passkey" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/env/local"));
    const body = calls.find((c) => c.path === "/api/v1/auth/login/mfa")?.body as {
      mfaToken: string;
      passkey: { id: string };
    };
    expect(body.mfaToken).toBe("tok");
    expect(body.passkey.id).toBe("Y3JlZA");
  });

  it("adds a passkey and shows the backup codes of the first second factor", async () => {
    const { create } = stubWebAuthn();
    let keys: unknown[] = [];
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/auth/mfa": () =>
        json(200, {
          enabled: keys.length > 0,
          totp: false,
          passkeys: keys.length,
          backupCodesLeft: keys.length ? 10 : 0,
          required: false,
        }),
      "GET /api/v1/auth/passkeys": () => json(200, keys),
      "POST /api/v1/auth/passkeys/options": () =>
        json(200, {
          token: "reg",
          options: {
            publicKey: { ...options.publicKey, user: { id: "aGFuZGxl", name: "admin" } },
          },
        }),
      "POST /api/v1/auth/passkeys": () => {
        const passkey = {
          id: 1,
          name: "Work laptop",
          host: "localhost",
          createdAt: new Date().toISOString(),
        };
        keys = [passkey];
        return json(201, { passkey, backupCodes: ["aaaaa-bbbbb"] });
      },
    });
    renderApp("/security");
    const card = await screen.findByRole("region", { name: "Passkeys" });
    expect(await within(card).findByText("No passkeys yet.")).toBeInTheDocument();
    fireEvent.click(within(card).getByRole("button", { name: "Add passkey" }));

    const dialog = await screen.findByRole("dialog", { name: "Add a passkey" });
    fireEvent.change(within(dialog).getByLabelText(/^Name/), { target: { value: "Work laptop" } });
    fireEvent.change(within(dialog).getByLabelText(/Current password/), {
      target: { value: "pw" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Add passkey" }));
    expect(await within(dialog).findByText("aaaaa-bbbbb")).toBeInTheDocument();
    expect(create).toHaveBeenCalledOnce();
    const add = calls.find((c) => c.method === "POST" && c.path === "/api/v1/auth/passkeys");
    expect(add?.body).toMatchObject({
      token: "reg",
      name: "Work laptop",
      credential: { response: { attestationObject: "YXR0", transports: ["internal"] } },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "I saved them" }));
    expect(await within(card).findByText("Work laptop")).toBeInTheDocument();
  });
});
