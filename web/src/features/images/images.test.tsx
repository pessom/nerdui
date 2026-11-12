// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Image } from "../../api/queries";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";
import { displayName, matchesUsage, shortID } from "./format";

const img = (over: Partial<Image>): Image => ({
  name: "docker.io/library/nginx:1.27",
  repository: "nginx",
  tag: "1.27",
  digest: "sha256:" + "a".repeat(64),
  createdAt: "2027-03-01T10:00:00Z",
  platforms: ["linux/amd64"],
  size: 200 * 1024 ** 2,
  contentSize: 70 * 1024 ** 2,
  containers: 2,
  labels: {},
  ...over,
});

const nginx = img({});
const images: Image[] = [
  nginx,
  img({
    name: "docker.io/library/alpine:3",
    repository: "alpine",
    tag: "3",
    digest: "sha256:" + "b".repeat(64),
    size: 8 * 1024 ** 2,
    containers: 0,
    platforms: ["linux/amd64", "linux/arm64"],
  }),
  img({
    name: "sha256:" + "c".repeat(64),
    repository: "",
    tag: "",
    digest: "sha256:" + "c".repeat(64),
    size: 1024 ** 3,
    containers: 0,
  }),
];

describe("image helpers", () => {
  it("formats names and filters by usage", () => {
    expect(displayName(nginx)).toBe("nginx:1.27");
    expect(displayName(images.at(-1)!)).toBe("<none> cccccccccccc");
    expect(displayName(img({ tag: "" }))).toBe("nginx");
    expect(shortID("sha256:0123456789abcdef")).toBe("0123456789ab");
    expect(images.filter((i) => matchesUsage(i, "unused")).length).toBe(2);
    expect(images.filter((i) => matchesUsage(i, "used")).length).toBe(1);
  });
});

describe("images page", () => {
  beforeEach(() => {
    vi.stubGlobal("WebSocket", SilentWebSocket);
    localStorage.clear();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("lists images with usage, size and filters", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/images": () => json(200, { namespace: "default", images }),
    });
    const { router } = renderApp("/env/local");
    fireEvent.click(await screen.findByRole("link", { name: "Images" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/env/local/images"));

    const table = await screen.findByRole("table");
    const nginx = within(table).getByText("nginx:1.27").closest("tr")!;
    expect(within(nginx).getByText("2 containers")).toBeInTheDocument();
    expect(within(nginx).getByText("200.0 MiB")).toBeInTheDocument();
    expect(within(table).getByText("linux/amd64, linux/arm64")).toBeInTheDocument();
    expect(screen.getByText(/1\.2 GiB unpacked in total/)).toBeInTheDocument();
    expect(calls.find((c) => c.path.endsWith("/images"))?.query.get("namespace")).toBe("default");

    fireEvent.click(screen.getByText("Unused (2)"));
    expect(within(table).queryByText("nginx:1.27")).not.toBeInTheDocument();
    expect(within(table).getByText("alpine:3")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Search images"), { target: { value: "arm64" } });
    expect(within(table).getAllByRole("row")).toHaveLength(2); // header + alpine

    fireEvent.change(screen.getByLabelText("Search images"), { target: { value: "nothing" } });
    expect(within(table).getByText("No images match the filters.")).toBeInTheDocument();
  });

  it("sorts by size", async () => {
    fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/images": () => json(200, { namespace: "default", images }),
    });
    renderApp("/env/local/images");
    const table = await screen.findByRole("table");
    fireEvent.click(within(table).getByRole("button", { name: /Size/ }));
    const names = () =>
      within(table)
        .getAllByRole("row")
        .slice(1)
        .map((r) => r.querySelectorAll("td")[1]?.textContent); // after the selection checkbox
    expect(names()).toEqual(["<none> cccccccccccc", "nginx:1.27", "alpine:3"]);
  });
});

describe("image actions", () => {
  beforeEach(() => {
    vi.stubGlobal("WebSocket", SilentWebSocket);
    localStorage.clear();
  });
  afterEach(() => vi.unstubAllGlobals());

  const listed = () => json(200, { namespace: "default", images });

  async function openMenu(name: string) {
    fireEvent.click(await screen.findByRole("button", { name: `Actions for ${name}` }));
    return screen.findByRole("menu", { hidden: true });
  }

  it("tags an image", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/images": listed,
      "POST /api/v1/environments/local/images/tag": () => new Response(null, { status: 204 }),
    });
    renderApp("/env/local/images");
    const menu = await openMenu("nginx:1.27");
    fireEvent.click(within(menu).getByRole("menuitem", { name: "Tag…", hidden: true }));
    const dialog = await screen.findByRole("dialog", { name: "Tag nginx:1.27" });
    fireEvent.change(within(dialog).getByLabelText(/New reference/), {
      target: { value: "reg.local:5000/nginx:1.27" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Tag" }));
    expect(await screen.findByText("Tagged reg.local:5000/nginx:1.27")).toBeInTheDocument();
    expect(calls.find((c) => c.path.endsWith("/images/tag"))?.body).toEqual({
      source: "docker.io/library/nginx:1.27",
      target: "reg.local:5000/nginx:1.27",
    });
  });

  it("pushes with the image reference prefilled", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/images": listed,
      "POST /api/v1/environments/local/images/push": () =>
        json(202, {
          job: {
            id: "p1",
            kind: "image.push",
            title: "Push docker.io/library/alpine:3",
            environment: "local",
            status: "running",
            startedAt: "2027-03-01T10:00:00Z",
          },
        }),
      "GET /api/v1/jobs": () => json(200, { jobs: [] }),
    });
    renderApp("/env/local/images");
    const menu = await openMenu("alpine:3");
    fireEvent.click(within(menu).getByRole("menuitem", { name: "Push…", hidden: true }));
    const dialog = await screen.findByRole("dialog", { name: "Push image" });
    expect(within(dialog).getByLabelText(/^Image/)).toHaveValue("docker.io/library/alpine:3");
    fireEvent.click(within(dialog).getByRole("button", { name: "Push" }));
    await screen.findByRole("dialog", { name: "Jobs" });
    expect(calls.find((c) => c.path.endsWith("/images/push"))?.body).toEqual({
      reference: "docker.io/library/alpine:3",
    });
  });

  it("removes selected images with force and reports failures", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/images": listed,
      "POST /api/v1/environments/local/images/remove": () =>
        json(200, {
          results: [
            { reference: "docker.io/library/nginx:1.27", ok: true },
            {
              reference: "docker.io/library/alpine:3",
              ok: false,
              error: { code: "conflict", message: "image is being used by running container x" },
            },
          ],
        }),
    });
    renderApp("/env/local/images");
    fireEvent.click(await screen.findByLabelText("Select nginx:1.27"));
    fireEvent.click(screen.getByLabelText("Select alpine:3"));
    expect(screen.getByText("2 selected")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Remove…" }));

    const dialog = await screen.findByRole("dialog", { name: "Remove 2 images?" });
    expect(within(dialog).getByText(/used by 2 containers/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByLabelText(/Force/));
    fireEvent.click(within(dialog).getByRole("button", { name: "Remove" }));

    expect(await screen.findByText("Removed docker.io/library/nginx:1.27")).toBeInTheDocument();
    expect(screen.getByText(/being used by running container/)).toBeInTheDocument();
    expect(calls.find((c) => c.path.endsWith("/images/remove"))?.body).toEqual({
      references: ["docker.io/library/nginx:1.27", "docker.io/library/alpine:3"],
      force: true,
    });
  });

  it("refuses to remove only used images without force", async () => {
    fakeApi({ ...signedIn, "GET /api/v1/environments/local/images": listed });
    renderApp("/env/local/images");
    const menu = await openMenu("nginx:1.27");
    fireEvent.click(within(menu).getByRole("menuitem", { name: "Remove…", hidden: true }));
    const dialog = await screen.findByRole("dialog", { name: "Remove nginx:1.27?" });
    expect(within(dialog).getByRole("button", { name: "Remove" })).toBeDisabled();
    fireEvent.click(within(dialog).getByLabelText(/Force/));
    expect(within(dialog).getByRole("button", { name: "Remove" })).toBeEnabled();
  });
});
