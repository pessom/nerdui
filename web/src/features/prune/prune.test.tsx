// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";

vi.mock("../jobs/JobTerminal", () => ({ default: () => <pre aria-label="Job output" /> }));

describe("prune", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("previews and starts a cleanup job", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/system/prune": (c) =>
        json(200, {
          containers: [{ id: "c1", name: "old-job" }],
          images:
            c.query.get("allImages") === "true"
              ? [{ id: "sha256:a", name: "docker.io/library/nginx:1.27", size: 200 * 1024 ** 2 }]
              : [],
          imagesSize: c.query.get("allImages") === "true" ? 200 * 1024 ** 2 : 0,
          buildKit: false,
        }),
      "POST /api/v1/environments/local/system/prune": () =>
        json(202, {
          job: {
            id: "pr1",
            kind: "system.prune",
            title: "Prune default",
            environment: "local",
            namespace: "default",
            status: "running",
            startedAt: "2027-03-01T10:00:00Z",
          },
        }),
      "GET /api/v1/jobs": () => json(200, { jobs: [] }),
    });
    renderApp("/env/local");
    fireEvent.click(await screen.findByRole("button", { name: "Clean up…" }));
    const dialog = await screen.findByRole("dialog", { name: "Clean up unused resources" });

    expect(await within(dialog).findByText("container old-job")).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Stopped containers (1)")).toBeChecked();
    expect(within(dialog).getByLabelText(/Dangling images \(0/)).toBeChecked();
    expect(within(dialog).getByLabelText(/no BuildKit daemon/)).toBeInTheDocument();

    fireEvent.click(within(dialog).getByLabelText(/All images not used/));
    expect(
      await within(dialog).findByText(/image docker.io\/library\/nginx:1.27/),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByLabelText(/Unused images \(1, up to 200.0 MiB\)/),
    ).toBeInTheDocument();

    fireEvent.click(within(dialog).getByLabelText(/Unused networks/));
    fireEvent.click(within(dialog).getByRole("button", { name: "Clean up" }));
    await screen.findByRole("dialog", { name: "Jobs" });
    const post = calls.find((c) => c.method === "POST" && c.path.endsWith("/system/prune"));
    expect(post?.body).toEqual({
      containers: true,
      images: true,
      allImages: true,
      networks: false,
      buildCache: true,
    });
    expect(post?.query.get("namespace")).toBe("default");
  });

  it("needs at least one category", async () => {
    fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/system/prune": () =>
        json(200, { containers: [], images: [], imagesSize: 0, buildKit: true }),
    });
    renderApp("/env/local");
    fireEvent.click(await screen.findByRole("button", { name: "Clean up…" }));
    const dialog = await screen.findByRole("dialog", { name: "Clean up unused resources" });
    for (const label of [/Stopped containers/, /images/, /Unused networks/, /^Build cache/]) {
      fireEvent.click(within(dialog).getAllByLabelText(label)[0]!);
    }
    await waitFor(() =>
      expect(within(dialog).getByRole("button", { name: "Clean up" })).toBeDisabled(),
    );
  });
});
