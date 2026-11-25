// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";

vi.mock("./StackFileEditor", () => ({
  default: ({
    value,
    onChange,
    label,
    readOnly,
  }: {
    value: string;
    onChange: (v: string) => void;
    label: string;
    readOnly?: boolean;
  }) => (
    <textarea
      aria-label={label}
      value={value}
      readOnly={readOnly}
      onChange={(e) => onChange(e.target.value)}
    />
  ),
}));

const base = "/api/v1/environments/local/stacks";
const app = {
  name: "app",
  mode: "managed",
  path: "/var/lib/nerdui/stacks/app",
  composeFile: "compose.yaml",
  status: "stopped",
  containers: 0,
  running: 0,
  services: [{ name: "web", defined: true, running: 0, containers: [] }],
};

describe("stack files", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("browses, edits, creates and deletes the files of a stack", async () => {
    let entries = [
      { path: "conf", dir: true, size: 0 },
      { path: "conf/site.conf", dir: false, size: 10 },
      { path: "compose.yaml", dir: false, size: 30, protected: true },
    ];
    const site = {
      path: "conf/site.conf",
      content: "server {}\n",
      version: "s1",
      exists: true,
      size: 10,
    };
    const { calls } = fakeApi({
      ...signedIn,
      [`GET ${base}/app`]: () => json(200, app),
      [`GET ${base}/app/tree`]: () => json(200, { entries, truncated: false, readOnly: false }),
      [`GET ${base}/app/tree/file`]: (c) => {
        const p = c.query.get("path");
        return p === "conf/site.conf"
          ? json(200, site)
          : json(200, { path: p, content: "", version: "e", exists: true, size: 0 });
      },
      [`PUT ${base}/app/tree/file`]: (c) => {
        const p = c.query.get("path") ?? "";
        if (!entries.some((e) => e.path === p))
          entries = [...entries, { path: p, dir: false, size: 0 }];
        return json(200, {
          file: { path: p, content: "", version: "s2", exists: true, size: 0 },
          validation:
            p === "compose.override.yaml"
              ? { valid: false, error: "services.web: bad", services: [] }
              : { valid: true, services: ["web"] },
        });
      },
      [`DELETE ${base}/app/tree/file`]: (c) => {
        const p = c.query.get("path");
        entries = entries.filter((e) => e.path !== p);
        return new Response(null, { status: 204 });
      },
    });
    renderApp("/env/local/stacks/app?tab=files");

    const nav = await screen.findByRole("navigation", { name: "Stack files" });
    expect(within(nav).getByText("conf")).toBeInTheDocument();

    // Edit a config file and save it with the loaded version.
    fireEvent.click(within(nav).getByRole("button", { name: "site.conf" }));
    const editor = await screen.findByLabelText("conf/site.conf of app");
    fireEvent.change(editor, { target: { value: "server { listen 80; }\n" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === "PUT")?.body).toEqual({
        content: "server { listen 80; }\n",
        version: "s1",
      }),
    );

    // The compose file opens its own tab.
    fireEvent.click(within(nav).getByRole("button", { name: "compose.yaml" }));
    expect(await screen.findByText(/has its own tab/)).toBeInTheDocument();

    // A new override file that breaks the project: saved, with a warning.
    fireEvent.click(screen.getByRole("button", { name: "New file" }));
    const dialog = await screen.findByRole("dialog", { name: "New file" });
    fireEvent.change(within(dialog).getByLabelText(/Path in the stack/), {
      target: { value: "compose.override.yaml" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Create" }));
    expect(
      await within(nav).findByRole("button", { name: "compose.override.yaml" }),
    ).toBeInTheDocument();
    const override = await screen.findByLabelText("compose.override.yaml of app");
    fireEvent.change(override, { target: { value: "services:\n  web: {}\n" } });
    const opened = screen.getByRole("region", { name: "File compose.override.yaml" });
    fireEvent.click(within(opened).getByRole("button", { name: "Save" }));
    expect(await screen.findByText("services.web: bad")).toBeInTheDocument();

    // Delete it.
    fireEvent.click(within(opened).getByRole("button", { name: "Delete" }));
    const del = await screen.findByRole("dialog", { name: "Delete compose.override.yaml" });
    fireEvent.click(within(del).getByRole("button", { name: "Delete file" }));
    await waitFor(() =>
      expect(
        within(nav).queryByRole("button", { name: "compose.override.yaml" }),
      ).not.toBeInTheDocument(),
    );
    expect(calls.find((c) => c.method === "DELETE")?.query.get("path")).toBe(
      "compose.override.yaml",
    );
  });

  it("shows the files of a Git stack read-only", async () => {
    fakeApi({
      ...signedIn,
      [`GET ${base}/app`]: () => json(200, { ...app, mode: "git" }),
      [`GET ${base}/app/tree`]: () =>
        json(200, {
          entries: [{ path: "nginx.conf", dir: false, size: 3 }],
          truncated: false,
          readOnly: true,
        }),
      [`GET ${base}/app/tree/file`]: () =>
        json(200, { path: "nginx.conf", content: "x\n", version: "n", exists: true, size: 3 }),
    });
    renderApp("/env/local/stacks/app?tab=files");
    const nav = await screen.findByRole("navigation", { name: "Stack files" });
    expect(screen.queryByRole("button", { name: "New file" })).not.toBeInTheDocument();
    fireEvent.click(within(nav).getByRole("button", { name: "nginx.conf" }));
    expect(await screen.findByLabelText("nginx.conf of app")).toHaveAttribute("readonly");
    const opened = screen.getByRole("region", { name: "File nginx.conf" });
    expect(within(opened).queryByRole("button", { name: "Save" })).not.toBeInTheDocument();
    expect(within(opened).queryByRole("button", { name: "Delete" })).not.toBeInTheDocument();
  });
});
