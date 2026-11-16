// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";
import { defaultPreferences, formatTime, type Preferences } from "./context";

const container = {
  id: "a".repeat(64),
  name: "web",
  namespace: "default",
  image: "docker.io/library/nginx:1",
  command: "nginx",
  state: "running",
  createdAt: "2027-03-01T10:00:00Z",
  ports: [],
  labels: {},
};

describe("formatTime", () => {
  const now = Date.parse("2027-03-01T12:00:00Z");
  it("formats relative, local and iso with the other forms in the title", () => {
    const iso = "2027-03-01T10:00:00Z";
    expect(formatTime(iso, "relative", now).text).toMatch(/2 hours ago/);
    expect(formatTime(iso, "iso", now).text).toMatch(/^2027-03-01 \d\d:00:00$/);
    expect(formatTime(iso, "local", now).text).toBe(new Date(iso).toLocaleString());
    expect(formatTime(iso, "iso", now).title).toContain(iso);
    expect(formatTime("garbage", "iso", now).text).toBe("garbage");
  });
});

describe("preferences", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("hides saved columns and saves column and preference changes", async () => {
    vi.stubGlobal("WebSocket", SilentWebSocket);
    let prefs: Preferences = {
      ...defaultPreferences,
      dateFormat: "iso",
      hiddenColumns: { containers: ["image"] },
    };
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/auth/preferences": () => json(200, prefs),
      "PUT /api/v1/auth/preferences": (c) => {
        prefs = c.body as Preferences;
        return json(200, prefs);
      },
      "GET /api/v1/environments/local/containers": () =>
        json(200, { namespace: "default", containers: [container] }),
    });
    renderApp("/env/local/containers");

    const table = await screen.findByRole("table");
    await waitFor(() =>
      expect(within(table).queryByRole("columnheader", { name: /Image/ })).not.toBeInTheDocument(),
    );
    expect(within(table).getByRole("columnheader", { name: /Status/ })).toBeInTheDocument();
    // The iso date format.
    expect(within(table).getByText(/^2027-03-01 \d\d:00:00$/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Columns" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "Image" }));
    expect(await within(table).findByRole("columnheader", { name: /Image/ })).toBeInTheDocument();
    // jsdom has no layout: floating-ui marks the reopened dropdown hidden.
    fireEvent.click(screen.getByRole("checkbox", { name: "Status", hidden: true }));
    await waitFor(() =>
      expect(within(table).queryByRole("columnheader", { name: /Status/ })).not.toBeInTheDocument(),
    );
    await waitFor(() => expect(prefs.hiddenColumns).toEqual({ containers: ["status"] }));

    // The preferences dialog from the user menu.
    fireEvent.click(screen.getByRole("button", { name: "admin" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Preferences" }));
    const dialog = await screen.findByRole("dialog", { name: "Preferences" });
    fireEvent.click(within(dialog).getByText("Dark"));
    await waitFor(() => expect(prefs.theme).toBe("dark"));
    expect(document.documentElement).toHaveAttribute("data-mantine-color-scheme", "dark");
    expect(prefs.hiddenColumns).toEqual({ containers: ["status"] });
    expect(calls.filter((c) => c.method === "PUT")).toHaveLength(3);
  });
});
