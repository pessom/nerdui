// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { fakeApi, json } from "../../test/server";
import { variableNameError } from "./variables";

describe("global variables", () => {
  it("adds a secret, keeps its value on edit and deletes a variable", async () => {
    let variables: Record<string, unknown>[] = [
      { name: "TZ", value: "Europe/Moscow", secret: false, updatedAt: "2026-11-23T10:00:00Z" },
    ];
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/variables": () => json(200, { variables }),
      "PUT /api/v1/variables/DB_PASSWORD": (c) => {
        const v = {
          name: "DB_PASSWORD",
          value: "",
          secret: true,
          updatedAt: "2026-11-23T10:01:00Z",
        };
        variables = [v, ...variables.filter((x) => x.name !== "DB_PASSWORD")];
        return json(200, { ...v, sent: c.body });
      },
      "DELETE /api/v1/variables/TZ": () => {
        variables = variables.filter((x) => x.name !== "TZ");
        return new Response(null, { status: 204 });
      },
    });
    renderApp("/env/local/settings");

    const card = await screen.findByRole("region", { name: "Global variables" });
    expect(await within(card).findByText("Europe/Moscow")).toBeInTheDocument();

    fireEvent.click(within(card).getByRole("button", { name: "Add variable" }));
    let dialog = await screen.findByRole("dialog", { name: "Add variable" });
    fireEvent.change(within(dialog).getByLabelText(/^Name/), { target: { value: "PATH" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await within(dialog).findByText("PATH is reserved")).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText(/^Name/), {
      target: { value: "DB_PASSWORD" },
    });
    fireEvent.change(within(dialog).getByLabelText(/^Value/), { target: { value: "pw" } });
    fireEvent.click(within(dialog).getByLabelText(/^Secret/));
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "Add variable" })).not.toBeInTheDocument(),
    );
    expect(calls.find((c) => c.method === "PUT")?.body).toEqual({ value: "pw", secret: true });
    expect(await within(card).findByText("secret")).toBeInTheDocument();

    // Saving a secret without a new value keeps the stored one.
    fireEvent.click(within(card).getByRole("button", { name: "Edit DB_PASSWORD" }));
    dialog = await screen.findByRole("dialog", { name: "Edit DB_PASSWORD" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(calls.filter((c) => c.method === "PUT").at(-1)?.body).toEqual({ secret: true }),
    );

    fireEvent.click(await within(card).findByRole("button", { name: "Delete TZ" }));
    const confirm = await screen.findByRole("dialog", { name: "Delete variable" });
    fireEvent.click(within(confirm).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(within(card).queryByText("Europe/Moscow")).not.toBeInTheDocument());
  });

  it("edits a stack's own variables on its Variables tab", async () => {
    const base = "/api/v1/environments/local/stacks";
    const { calls } = fakeApi({
      ...signedIn,
      [`GET ${base}/app`]: () =>
        json(200, {
          name: "app",
          mode: "managed",
          path: "/var/lib/nerdui/stacks/app",
          composeFile: "compose.yaml",
          status: "stopped",
          containers: 0,
          running: 0,
          services: [],
        }),
      [`GET ${base}/app/variables`]: () => json(200, { variables: [] }),
      [`PUT ${base}/app/variables/TAG`]: () =>
        json(200, { name: "TAG", value: "1.27", secret: false, updatedAt: "2026-11-23T10:00:00Z" }),
    });
    renderApp("/env/local/stacks/app?tab=variables");

    expect(await screen.findByText("No variables yet.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Add variable" }));
    const dialog = await screen.findByRole("dialog", { name: "Add variable" });
    fireEvent.change(within(dialog).getByLabelText(/^Name/), { target: { value: "TAG" } });
    fireEvent.change(within(dialog).getByLabelText(/^Value/), { target: { value: "1.27" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === "PUT")?.body).toEqual({ value: "1.27", secret: false }),
    );
  });

  it("checks names like the server", () => {
    expect(variableNameError("DB_HOST")).toBeNull();
    expect(variableNameError("_x1")).toBeNull();
    expect(variableNameError("1A")).toMatch(/Letters/);
    expect(variableNameError("A-B")).toMatch(/Letters/);
    expect(variableNameError("ld_preload")).toMatch(/LD_/);
    expect(variableNameError("Home")).toMatch(/reserved/);
  });
});
