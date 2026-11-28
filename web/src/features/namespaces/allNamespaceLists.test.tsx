// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";

const env = "/api/v1/environments/local";
const cid = "c".repeat(64);

/** Lists of every namespace (#276): a Namespace column, rows acting in their own namespace. */
describe("<All namespaces> on list pages", () => {
  beforeEach(() => {
    vi.stubGlobal("WebSocket", SilentWebSocket);
    localStorage.clear();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("volumes", async () => {
    const volume = (namespace: string) => ({
      name: "data",
      namespace,
      mountpoint: `/var/lib/nerdctl/x/volumes/${namespace}/data/_data`,
      labels: {},
      anonymous: false,
      containers: [{ id: cid, name: `db-${namespace}` }],
    });
    const { calls } = fakeApi({
      ...signedIn,
      [`GET ${env}/volumes`]: () =>
        json(200, { namespace: "*", volumes: [volume("default"), volume("dev")] }),
    });
    renderApp("/env/local/volumes?ns=*");
    const table = await screen.findByRole("table");
    expect(within(table).getByRole("columnheader", { name: "Namespace" })).toBeInTheDocument();
    expect(within(table).getByRole("link", { name: "db-dev" })).toHaveAttribute(
      "href",
      `/env/local/containers/${cid}?ns=dev`,
    );
    expect(screen.getByRole("button", { name: "Create volume" })).toBeDisabled();
    expect(calls.find((c) => c.path === `${env}/volumes`)?.query.get("namespace")).toBe("*");
  });

  it("networks", async () => {
    const network = (namespace: string) => ({
      name: "bridge",
      namespace,
      driver: "bridge",
      labels: {},
      ipam: [],
      internal: false,
      default: true,
      removable: false,
      containers: [{ id: cid, name: `web-${namespace}`, running: true, ips: [] }],
    });
    fakeApi({
      ...signedIn,
      [`GET ${env}/networks`]: () =>
        json(200, { namespace: "*", networks: [network("default"), network("dev")] }),
    });
    renderApp("/env/local/networks?ns=*");
    const table = await screen.findByRole("table");
    expect(within(table).getByRole("columnheader", { name: "Namespace" })).toBeInTheDocument();
    expect(within(table).getAllByRole("row")).toHaveLength(3);
    expect(within(table).getByRole("link", { name: "web-dev" })).toHaveAttribute(
      "href",
      `/env/local/containers/${cid}?ns=dev`,
    );
    expect(screen.getByRole("button", { name: "Create network" })).toBeDisabled();
  });

  it("stacks", async () => {
    const stack = (namespace: string) => ({
      name: "app",
      namespace,
      mode: "unmanaged",
      status: "running",
      services: [],
      containers: 1,
      running: 1,
    });
    fakeApi({
      ...signedIn,
      [`GET ${env}/stacks`]: () =>
        json(200, { namespace: "*", stacks: [stack("default"), stack("dev")] }),
      [`GET ${env}/containers`]: () => json(200, { namespace: "*", containers: [] }),
    });
    renderApp("/env/local/stacks?ns=*");
    const table = await screen.findByRole("table");
    const links = within(table).getAllByRole("link", { name: "app" });
    expect(links.map((l) => l.getAttribute("href"))).toEqual([
      "/env/local/stacks/app?ns=default",
      "/env/local/stacks/app?ns=dev",
    ]);
    expect(screen.getByRole("button", { name: "New stack" })).toBeDisabled();
  });

  it("images", async () => {
    const image = (namespace: string) => ({
      name: "docker.io/library/nginx:1.27",
      namespace,
      repository: "nginx",
      tag: "1.27",
      digest: "sha256:" + "a".repeat(64),
      createdAt: "2027-03-01T10:00:00Z",
      platforms: ["linux/amd64"],
      size: 1,
      contentSize: 1,
      containers: 0,
      labels: {},
    });
    const { calls } = fakeApi({
      ...signedIn,
      [`GET ${env}/images`]: () =>
        json(200, { namespace: "*", images: [image("default"), image("dev")] }),
    });
    renderApp("/env/local/images?ns=*");
    const table = await screen.findByRole("table");
    expect(within(table).getByRole("columnheader", { name: /Namespace/ })).toBeInTheDocument();
    expect(within(table).getAllByRole("checkbox", { name: /^Select nginx/ })).toHaveLength(2);
    expect(screen.getByRole("button", { name: "Pull image" })).toBeDisabled();
    // Image updates are asked per namespace of the listed images.
    await waitFor(() =>
      expect(
        calls
          .filter((c) => c.path === `${env}/image-updates`)
          .map((c) => c.query.get("namespace"))
          .sort(),
      ).toEqual(["default", "dev"]),
    );
  });
});
