// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { fakeApi, json } from "../../test/server";

describe("incoming webhooks", () => {
  it("creates a webhook for a container, shows its URL once and replaces it", async () => {
    let webhooks: Record<string, unknown>[] = [];
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/webhooks": () => json(200, { webhooks }),
      "GET /api/v1/environments/local/namespaces": () =>
        json(200, { namespaces: ["default", "k8s.io"] }),
      "GET /api/v1/environments/local/stacks": () =>
        json(200, { namespace: "default", stacks: [] }),
      "GET /api/v1/environments/local/containers": () =>
        json(200, {
          namespace: "default",
          containers: [
            { id: "c1", name: "api", namespace: "default", image: "acme/api", state: "running" },
          ],
        }),
      "POST /api/v1/webhooks": (c) => {
        const w = { ...(c.body as object), id: 3, createdAt: "2027-03-01T10:00:00Z" };
        webhooks = [w];
        return json(201, { webhook: w, path: "/api/v1/hooks/tok1" });
      },
      "POST /api/v1/webhooks/3/token": () => json(200, { path: "/api/v1/hooks/tok2" }),
    });
    renderApp("/env/local/settings");

    const card = await screen.findByRole("region", { name: "Incoming webhooks" });
    expect(await within(card).findByText("No webhooks yet.")).toBeInTheDocument();
    fireEvent.click(within(card).getByRole("button", { name: "Add webhook" }));
    const dialog = await screen.findByRole("dialog", { name: "Add webhook" });
    fireEvent.change(within(dialog).getByLabelText(/^Name\W*$/), { target: { value: "ci-api" } });
    fireEvent.click(within(dialog).getByText("Container"));
    fireEvent.click(within(dialog).getByLabelText(/^Container \*/, { selector: "input" }));
    fireEvent.click(await screen.findByRole("option", { name: "api", hidden: true }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));

    const shown = await screen.findByRole("dialog", { name: "Webhook URL" });
    expect(within(shown).getByLabelText("URL")).toHaveTextContent(
      `${window.location.origin}/api/v1/hooks/tok1`,
    );
    expect(calls.find((c) => c.method === "POST" && c.path === "/api/v1/webhooks")?.body).toEqual({
      name: "ci-api",
      environment: "local",
      namespace: "default",
      targetKind: "container",
      target: "api",
      action: "redeploy",
      enabled: true,
    });
    fireEvent.click(within(shown).getByRole("button", { name: "Done" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "Webhook URL" })).not.toBeInTheDocument(),
    );

    expect(await within(card).findByText("ci-api")).toBeInTheDocument();
    expect(within(card).getByText("Pull images and redeploy")).toBeInTheDocument();
    fireEvent.click(within(card).getByRole("button", { name: "New URL for ci-api" }));
    expect(
      within(await screen.findByRole("dialog", { name: "Webhook URL" })).getByLabelText("URL"),
    ).toHaveTextContent("/api/v1/hooks/tok2");
  });
});
