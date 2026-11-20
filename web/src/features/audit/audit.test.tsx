// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { fakeApi, json } from "../../test/server";

const entry = (id: number, extra: Record<string, unknown>) => ({
  id,
  at: "2027-03-01T10:00:00Z",
  action: "containers:write",
  method: "POST",
  path: "/api/v1/environments/{env}/containers/{id}/recreate",
  environment: "local",
  namespace: "default",
  object: "web",
  status: 202,
  result: "ok",
  clientIp: "203.0.113.7",
  user: "admin",
  ...extra,
});

describe("audit log", () => {
  it("lists, filters and pages entries", async () => {
    const page1 = Array.from({ length: 100 }, (_, i) => entry(200 - i, {}));
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/audit": (c) => {
        if (c.query.get("before") === "101") {
          return json(200, {
            entries: [
              entry(100, {
                result: "denied",
                user: "bob",
                message: "not allowed to containers:write",
              }),
            ],
          });
        }
        return json(200, { entries: c.query.get("result") === "failed" ? [] : page1 });
      },
    });
    renderApp("/audit");

    const table = await screen.findByRole("table", { name: "Audit entries" });
    expect(within(table).getAllByText("containers:write")).toHaveLength(100);
    const first = calls.find((c) => c.path === "/api/v1/audit");
    expect(first?.query.get("since")).toBeTruthy();
    expect(first?.query.get("limit")).toBe("100");

    fireEvent.click(screen.getByRole("button", { name: "Show older entries" }));
    expect(await within(table).findByText("not allowed to containers:write")).toBeInTheDocument();

    const exportLink = screen.getByRole("link", { name: "Export CSV" });
    expect(exportLink.getAttribute("href")).toMatch(/^\/api\/v1\/audit\/export\?since=/);

    fireEvent.click(screen.getByLabelText("Result", { selector: "input" }));
    fireEvent.click(await screen.findByRole("option", { name: "failed", hidden: true }));
    expect(await screen.findByText("No entries match.")).toBeInTheDocument();
    await waitFor(() =>
      expect(
        calls.some((c) => c.path === "/api/v1/audit" && c.query.get("result") === "failed"),
      ).toBe(true),
    );
  });
});
