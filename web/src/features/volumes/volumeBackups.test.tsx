// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { fakeApi, json } from "../../test/server";

const base = "/api/v1/environments/local/volumes";
const job = (id: string, kind: string) => ({
  id,
  kind,
  title: kind,
  environment: "local",
  namespace: "default",
  status: "running",
  startedAt: "2026-11-24T10:00:00Z",
});

describe("volume backups", () => {
  it("adds an S3 target in Settings", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/backup-targets": () => json(200, { targets: [] }),
      "POST /api/v1/backup-targets": (c) =>
        json(201, { id: 1, ...(c.body as object), createdAt: "x", updatedAt: "x" }),
    });
    renderApp("/env/local/settings");
    const card = await screen.findByRole("region", { name: "Volume backup targets" });
    fireEvent.click(await within(card).findByRole("button", { name: "Add target" }));
    const dialog = await screen.findByRole("dialog", { name: "Add backup target" });
    fireEvent.change(within(dialog).getByLabelText(/^Name/), { target: { value: "minio" } });
    fireEvent.click(within(dialog).getByText("S3 bucket"));
    fireEvent.change(within(dialog).getByLabelText(/^Endpoint/), {
      target: { value: "http://minio:9000" },
    });
    fireEvent.change(within(dialog).getByLabelText(/^Bucket/), { target: { value: "backups" } });
    fireEvent.change(within(dialog).getByLabelText(/^Access key/), { target: { value: "ak" } });
    fireEvent.change(within(dialog).getByLabelText(/^Secret key/), { target: { value: "sk" } });
    fireEvent.click(within(dialog).getByLabelText(/^Path-style/));
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === "POST")?.body).toEqual({
        name: "minio",
        kind: "s3",
        s3: {
          endpoint: "http://minio:9000",
          bucket: "backups",
          accessKey: "ak",
          region: "",
          prefix: "",
          pathStyle: true,
        },
        secretKey: "sk",
      }),
    );
  });

  it("backs up a volume now and restores an archive into a new volume", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      [`GET ${base}`]: () =>
        json(200, {
          namespace: "default",
          volumes: [
            {
              name: "pgdata",
              mountpoint: "/x",
              labels: {},
              anonymous: false,
              containers: [],
            },
          ],
        }),
      "GET /api/v1/backup-targets": () =>
        json(200, {
          targets: [
            {
              id: 4,
              name: "local",
              kind: "local",
              directory: "/srv/backups",
              createdAt: "x",
              updatedAt: "x",
            },
          ],
        }),
      [`GET ${base}/pgdata/backups`]: () =>
        json(200, {
          backups: [
            {
              key: "default/pgdata/pgdata-20261124-030000.tar.gz",
              size: 2048,
              time: "2026-11-24T03:00:00Z",
            },
          ],
        }),
      [`POST ${base}/pgdata/backups`]: () => json(202, { job: job("j1", "volume.backup") }),
      [`POST ${base}/pgdata-copy/restore`]: () => json(202, { job: job("j2", "volume.restore") }),
      "GET /api/v1/jobs": () => json(200, { jobs: [] }),
    });
    renderApp("/env/local/volumes");

    fireEvent.click(await screen.findByRole("button", { name: "Backups of pgdata" }));
    let dialog = await screen.findByRole("dialog", { name: "Backups of pgdata" });
    expect(await within(dialog).findByText("pgdata-20261124-030000.tar.gz")).toBeInTheDocument();
    expect(calls.find((c) => c.path === `${base}/pgdata/backups`)?.query.get("target")).toBe("4");

    fireEvent.click(
      within(dialog).getByRole("button", { name: "Restore pgdata-20261124-030000.tar.gz" }),
    );
    const restore = await screen.findByRole("dialog", { name: "Restore volume" });
    expect(within(restore).getByRole("button", { name: "Replace contents" })).toBeInTheDocument();
    fireEvent.change(within(restore).getByLabelText(/^Into volume/), {
      target: { value: "pgdata-copy" },
    });
    fireEvent.click(within(restore).getByRole("button", { name: "Restore" }));
    await waitFor(() =>
      expect(calls.find((c) => c.path.endsWith("/restore"))?.body).toEqual({
        targetId: 4,
        key: "default/pgdata/pgdata-20261124-030000.tar.gz",
        stopContainers: false,
      }),
    );

    fireEvent.click(await screen.findByRole("button", { name: "Backups of pgdata" }));
    dialog = await screen.findByRole("dialog", { name: "Backups of pgdata" });
    fireEvent.click(within(dialog).getByLabelText(/Stop containers/));
    fireEvent.click(within(dialog).getByRole("button", { name: "Back up now" }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === "POST" && c.path.endsWith("/backups"))?.body).toEqual({
        targetId: 4,
        stopContainers: true,
      }),
    );
  });
});
