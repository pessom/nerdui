// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json, session, unauthenticated } from "../../test/server";

describe("two-factor authentication", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("signs in with a second step", async () => {
    let loggedIn = false;
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/auth/me": () => (loggedIn ? json(200, session) : unauthenticated()),
      "POST /api/v1/auth/login": () => json(200, { mfaRequired: true, mfaToken: "tok" }),
      "POST /api/v1/auth/login/mfa": (c) => {
        if ((c.body as { code: string }).code !== "123456") {
          return json(401, {
            error: { code: "unauthenticated", message: "invalid or already used code" },
          });
        }
        loggedIn = true;
        return json(200, session);
      },
    });
    const { router } = renderApp("/login");
    fireEvent.change(await screen.findByLabelText(/Username/), { target: { value: "admin" } });
    fireEvent.change(screen.getByLabelText(/Password/), {
      target: { value: "correct horse battery" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

    expect(
      await screen.findByRole("heading", { name: "Two-factor authentication" }),
    ).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText(/^Code/), { target: { value: "000000" } });
    fireEvent.click(screen.getByRole("button", { name: "Verify" }));
    expect(await screen.findByText("Invalid or already used code.")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText(/^Code/), { target: { value: " 123456 " } });
    fireEvent.click(screen.getByRole("button", { name: "Verify" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/env/local"));
    expect(calls.filter((c) => c.path === "/api/v1/auth/login/mfa").at(-1)?.body).toEqual({
      mfaToken: "tok",
      code: "123456",
    });
  });

  it("sets up two-factor authentication and shows backup codes", async () => {
    let enabled = false;
    const backupCodes = Array.from({ length: 10 }, (_, i) => `code${i}-abcde`);
    fakeApi({
      ...signedIn,
      "GET /api/v1/auth/mfa": () =>
        json(200, { enabled, backupCodesLeft: enabled ? 10 : 0, required: false }),
      "POST /api/v1/auth/mfa/setup": (c) =>
        (c.body as { password: string }).password === "pw"
          ? json(200, {
              secret: "JBSWY3DPEHPK3PXP",
              uri: "otpauth://totp/nerdui:admin?secret=JBSWY3DPEHPK3PXP",
            })
          : json(400, {
              error: { code: "invalid_argument", message: "current password is incorrect" },
            }),
      "POST /api/v1/auth/mfa/enable": () => {
        enabled = true;
        return json(200, { backupCodes });
      },
    });
    renderApp("/security");
    const card = await screen.findByRole("region", { name: "Two-factor authentication" });
    expect(await within(card).findByText("Off")).toBeInTheDocument();
    fireEvent.click(within(card).getByRole("button", { name: "Set up" }));

    const dialog = await screen.findByRole("dialog", { name: "Set up two-factor authentication" });
    fireEvent.change(within(dialog).getByLabelText(/Current password/), {
      target: { value: "pw" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Continue" }));
    expect(await within(dialog).findByText("JBSW Y3DP EHPK 3PXP")).toBeInTheDocument();

    const pins = within(dialog).getAllByRole("textbox");
    "123456".split("").forEach((d, i) => fireEvent.change(pins[i]!, { target: { value: d } }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Turn on" }));
    const list = await within(dialog).findByLabelText("Backup codes");
    expect(within(list).getAllByText(/^code\d-abcde$/)).toHaveLength(10);
    fireEvent.click(within(dialog).getByRole("button", { name: "I saved them" }));
    expect(await within(card).findByText("On")).toBeInTheDocument();
  });

  it("requires setup before anything else when the administrator demands it", async () => {
    fakeApi({
      ...signedIn,
      "GET /api/v1/auth/me": () => json(200, { ...session, mfaSetupRequired: true }),
    });
    const { router } = renderApp("/env/local");
    expect(
      await screen.findByRole("heading", { name: "Set up two-factor authentication" }),
    ).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/setup-mfa");
  });
});
