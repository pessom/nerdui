// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Image, ImageUpdateOverview } from "../../api/queries";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";

const image = (name: string, digest: string): Image => ({
  name,
  repository: name.replace("docker.io/library/", "").split(":")[0] ?? "",
  tag: name.split(":")[1] ?? "",
  digest,
  createdAt: "2027-03-01T10:00:00Z",
  platforms: ["linux/amd64"],
  size: 1024,
  contentSize: 512,
  containers: 1,
  labels: {},
});

const overview = (checked: boolean): ImageUpdateOverview => ({
  running: false,
  lastCheck: checked ? "2027-03-01T11:00:00Z" : undefined,
  images: checked
    ? [
        {
          ref: "docker.io/library/nginx:1",
          localDigest: "sha256:old",
          remoteDigest: "sha256:new",
          checkedAt: "2027-03-01T11:00:00Z",
          updateAvailable: true,
          containers: 1,
        },
        {
          ref: "docker.io/library/redis:7",
          localDigest: "sha256:same",
          remoteDigest: "sha256:same",
          checkedAt: "2027-03-01T11:00:00Z",
          updateAvailable: false,
          containers: 1,
        },
      ]
    : [],
});

describe("image updates", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("marks images with updates and checks on request", async () => {
    let checked = false;
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/images": () =>
        json(200, {
          namespace: "default",
          images: [
            image("docker.io/library/nginx:1", "sha256:old"),
            image("docker.io/library/redis:7", "sha256:same"),
          ],
        }),
      "GET /api/v1/environments/local/image-updates": () => json(200, overview(checked)),
      "POST /api/v1/environments/local/image-updates/check": () => {
        checked = true;
        return json(200, { checked: 2, updates: 1, failed: 0 });
      },
    });
    renderApp("/env/local/images");

    const table = await screen.findByRole("table");
    expect(within(table).getByText("nginx:1")).toBeInTheDocument();
    expect(within(table).queryByText("update")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Check for updates" }));
    expect(await screen.findByText(/2 images checked, 1 with updates/)).toBeInTheDocument();
    expect(
      await within(table).findByLabelText("Update available for docker.io/library/nginx:1"),
    ).toBeInTheDocument();
    expect(within(table).getAllByText("update")).toHaveLength(1);
    expect(calls.find((c) => c.method === "POST")?.body).toEqual({});
  });

  it("shows checks and automatic updates in Settings", async () => {
    let approved = false;
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/settings/prometheus": () => json(200, { configured: false }),
      "GET /api/v1/environments/local/image-updates": () => json(200, overview(true)),
      "GET /api/v1/environments/local/settings/auto-updates": () =>
        json(200, { delaySeconds: 0, maxBatch: 5 }),
      "PUT /api/v1/environments/local/settings/auto-updates": (c) => json(200, c.body),
      "POST /api/v1/environments/local/auto-updates/approve": () => {
        approved = true;
        return json(202, { updates: 1 });
      },
      "GET /api/v1/environments/local/auto-updates": () =>
        json(200, {
          pending: approved
            ? undefined
            : {
                since: "2027-03-01T10:00:00Z",
                targets: [
                  {
                    kind: "container",
                    namespace: "default",
                    name: "db",
                    refs: ["docker.io/library/postgres:17"],
                  },
                ],
              },
          entries: [
            {
              id: 2,
              namespace: "default",
              kind: "container",
              name: "web",
              refs: ["docker.io/library/nginx:1"],
              status: "skipped",
              message: "recreating would lose settings: tmpfs",
              startedAt: "2027-03-01T10:00:00Z",
              finishedAt: "2027-03-01T10:00:00Z",
            },
            {
              id: 1,
              namespace: "default",
              kind: "stack",
              name: "app",
              refs: ["docker.io/library/redis:7"],
              status: "updated",
              jobId: "j1",
              startedAt: "2027-02-28T10:00:00Z",
              finishedAt: "2027-02-28T10:01:00Z",
            },
          ],
        }),
    });
    renderApp("/env/local/settings");

    const card = await screen.findByRole("region", { name: "Image updates" });
    expect(await within(card).findByText(/Last check/)).toBeInTheDocument();
    expect(within(card).getByRole("link", { name: "Schedules" })).toHaveAttribute(
      "href",
      "/schedules",
    );

    const log = within(card).getByRole("region", { name: "Automatic updates" });
    expect(await within(log).findByText("Stack app")).toBeInTheDocument();
    expect(within(log).getByText("recreating would lose settings: tmpfs")).toBeInTheDocument();
    expect(within(log).getByText("updated")).toBeInTheDocument();

    expect(within(log).getByText("1 updates wait for approval")).toBeInTheDocument();
    expect(within(log).getByText(/db \(default\)/)).toBeInTheDocument();
    fireEvent.click(within(log).getByRole("button", { name: "Approve and update" }));
    await waitFor(() =>
      expect(within(log).queryByText("1 updates wait for approval")).not.toBeInTheDocument(),
    );

    const delay = within(log).getByLabelText("Wait before applying a new image", {
      selector: "input",
    });
    fireEvent.click(delay);
    fireEvent.click(await screen.findByRole("option", { name: "3 days", hidden: true }));
    await waitFor(() =>
      expect(
        calls.find((c) => c.method === "PUT" && c.path.endsWith("/settings/auto-updates"))?.body,
      ).toEqual({ delaySeconds: 259200, maxBatch: 5 }),
    );
  });
});
