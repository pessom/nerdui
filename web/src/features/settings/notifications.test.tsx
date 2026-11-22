// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { fakeApi, json } from "../../test/server";

const kinds = [
  "image.update",
  "autoupdate.updated",
  "autoupdate.failed",
  "autoupdate.pending",
  "container.died",
  "container.oom",
  "container.unhealthy",
  "container.autoheal",
  "job.failed",
];

describe("notification channels", () => {
  it("adds, tests, edits and removes a channel", async () => {
    let channels: Record<string, unknown>[] = [];
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/notifications/channels": () => json(200, { channels, kinds }),
      "POST /api/v1/notifications/test": () => json(200, { sent: true }),
      "POST /api/v1/notifications/channels": (c) => {
        const body = c.body as Record<string, unknown>;
        channels = [
          {
            ...body,
            id: 7,
            config: { chatId: "-100" },
            secrets: ["token"],
            lastError: "400 Bad Request: chat not found",
          },
        ];
        return json(201, channels[0]);
      },
      "PUT /api/v1/notifications/channels/7": (c) =>
        json(200, { ...channels[0], ...(c.body as object) }),
      "DELETE /api/v1/notifications/channels/7": () => {
        channels = [];
        return new Response(null, { status: 204 });
      },
    });
    renderApp("/env/local/settings");

    const card = await screen.findByRole("region", { name: "Notifications" });
    expect(await within(card).findByText("No channels yet.")).toBeInTheDocument();
    fireEvent.click(within(card).getByRole("button", { name: "Add channel" }));

    const dialog = await screen.findByRole("dialog", { name: "Add notification channel" });
    fireEvent.change(within(dialog).getByLabelText(/^Name/), { target: { value: "phone" } });
    fireEvent.change(within(dialog).getByLabelText(/^Bot token/), {
      target: { value: "123:abc" },
    });
    fireEvent.change(within(dialog).getByLabelText(/^Chat ID/), { target: { value: "-100" } });
    fireEvent.click(within(dialog).getByRole("checkbox", { name: "A new image for a tag in use" }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Send test" }));
    expect(await screen.findByText("Test message sent")).toBeInTheDocument();
    expect(calls.find((c) => c.path === "/api/v1/notifications/test")?.body).toMatchObject({
      name: "phone",
      type: "telegram",
      config: { token: "123:abc", chatId: "-100" },
    });

    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await within(card).findByText("400 Bad Request: chat not found")).toBeInTheDocument();
    const created = calls.find(
      (c) => c.method === "POST" && c.path === "/api/v1/notifications/channels",
    )?.body as { events: string[] };
    expect(created.events).toContain("image.update");
    expect(created.events).toContain("job.failed");

    // Editing shows the saved token without revealing it.
    fireEvent.click(within(card).getByRole("button", { name: "Edit phone" }));
    const edit = await screen.findByRole("dialog", { name: "Edit notification channel" });
    expect(within(edit).getByPlaceholderText("Saved; leave empty to keep")).toHaveValue("");
    fireEvent.click(within(edit).getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === "PUT")?.body).toMatchObject({
        name: "phone",
        config: { chatId: "-100" },
      }),
    );

    fireEvent.click(await within(card).findByRole("button", { name: "Remove phone" }));
    const confirm = await screen.findByRole("dialog", { name: "Remove channel" });
    fireEvent.click(within(confirm).getByRole("button", { name: "Remove" }));
    expect(await within(card).findByText("No channels yet.")).toBeInTheDocument();
  });
});
