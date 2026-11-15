// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { fakeApi, json } from "../../test/server";
import { parseLabels } from "../../lib/labels";

const base = "/api/v1/environments/local/volumes";

const volumes = [
  {
    name: "old",
    mountpoint: "/var/lib/nerdctl/x/volumes/default/old/_data",
    labels: { app: "web" },
    anonymous: false,
    createdAt: "2027-03-01T10:00:00Z",
    containers: [],
  },
  {
    name: "pgdata",
    mountpoint: "/var/lib/nerdctl/x/volumes/default/pgdata/_data",
    labels: {},
    anonymous: false,
    containers: [{ id: "c".repeat(64), name: "db" }],
  },
];

describe("volumes", () => {
  it("parses labels", () => {
    expect(parseLabels("a=1\n\n b = x=y \nflag")).toEqual({
      labels: { a: "1", b: " x=y", flag: "" },
    });
    expect(parseLabels("=v").error).toMatch(/without a key/);
  });

  it("lists volumes, calculates sizes, creates and removes with the name typed", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      [`GET ${base}`]: (c) =>
        json(200, {
          namespace: "default",
          volumes:
            c.query.get("size") === "true"
              ? volumes.map((v) => ({ ...v, size: v.name === "pgdata" ? 3 * 1024 * 1024 : 0 }))
              : volumes,
        }),
      [`POST ${base}`]: (c) => json(201, { ...volumes[0], ...(c.body as object) }),
      [`DELETE ${base}/old`]: () => new Response(null, { status: 204 }),
    });
    renderApp("/env/local/volumes");

    const row = (await screen.findByText("pgdata")).closest("tr")!;
    expect(within(row).getByRole("link", { name: "db" })).toBeInTheDocument();
    // A used volume cannot be removed.
    expect(within(row).getByRole("button", { name: "Remove pgdata" })).toBeDisabled();
    expect(screen.getByText("app=web")).toBeInTheDocument();
    // Sizes are calculated only on request.
    expect(calls.filter((c) => c.path === base).every((c) => c.query.get("size") === "false")).toBe(
      true,
    );
    fireEvent.click(screen.getByRole("button", { name: "Calculate sizes" }));
    expect(await within(row).findByText("3.0 MiB")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Create volume" }));
    const create = await screen.findByRole("dialog", { name: "Create volume" });
    fireEvent.change(within(create).getByLabelText(/Name/), { target: { value: "cache" } });
    fireEvent.change(within(create).getByLabelText(/Labels/), { target: { value: "app=web" } });
    fireEvent.click(within(create).getByRole("button", { name: "Create" }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === "POST")?.body).toEqual({
        name: "cache",
        labels: { app: "web" },
      }),
    );
    expect(calls.find((c) => c.method === "POST")?.query.get("namespace")).toBe("default");

    const oldRow = screen.getByText("old").closest("tr")!;
    fireEvent.click(within(oldRow).getByRole("button", { name: "Remove old" }));
    const remove = await screen.findByRole("dialog", { name: "Remove volume" });
    const confirm = within(remove).getByRole("button", { name: "Remove volume" });
    expect(confirm).toBeDisabled();
    fireEvent.change(within(remove).getByLabelText(/Type old to confirm/), {
      target: { value: "ol" },
    });
    expect(confirm).toBeDisabled();
    fireEvent.change(within(remove).getByLabelText(/Type old to confirm/), {
      target: { value: "old" },
    });
    fireEvent.click(confirm);
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE")).toBe(true));
    expect(screen.getByRole("link", { name: "Volumes" })).toHaveAttribute("data-active", "true");
  });
});
