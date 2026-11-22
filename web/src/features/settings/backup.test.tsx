// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { attachmentName } from "../../lib/download";
import { renderApp, signedIn } from "../../test/render";
import { fakeApi, json } from "../../test/server";

describe("backup", () => {
  it("reads the file name of a download", () => {
    expect(attachmentName('attachment; filename="a.tar.gz.age"', "x")).toBe("a.tar.gz.age");
    expect(attachmentName(null, "x")).toBe("x");
  });

  it("downloads a backup with a confirmed password", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      "POST /api/v1/backup": () =>
        new Response("age-encryption.org/v1", {
          status: 200,
          headers: {
            "Content-Type": "application/octet-stream",
            "Content-Disposition": 'attachment; filename="nerdui-backup-1.tar.gz.age"',
          },
        }),
      "GET /api/v1/settings/security": () => json(200, { requireMfa: false }),
    });
    const names: string[] = [];
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (
      this: HTMLAnchorElement,
    ) {
      names.push(this.download);
    });
    URL.createObjectURL = vi.fn(() => "blob:x");
    URL.revokeObjectURL = vi.fn();
    renderApp("/env/local/settings");
    const card = await screen.findByRole("region", { name: "Backup" });
    const button = within(card).getByRole("button", { name: "Download backup" });
    fireEvent.change(within(card).getByLabelText("Backup password"), {
      target: { value: "short" },
    });
    expect(await within(card).findByText("At least 12 characters")).toBeInTheDocument();
    expect(button).toBeDisabled();
    fireEvent.change(within(card).getByLabelText("Backup password"), {
      target: { value: "correct horse battery" },
    });
    fireEvent.change(within(card).getByLabelText("Repeat password"), {
      target: { value: "correct horse battery" },
    });
    fireEvent.click(button);
    await waitFor(() => expect(names).toEqual(["nerdui-backup-1.tar.gz.age"]));
    expect(calls.find((c) => c.path === "/api/v1/backup")?.body).toEqual({
      password: "correct horse battery",
    });
    click.mockRestore();
  });
});
