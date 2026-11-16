// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";

vi.mock("./StackFileEditor", () => ({
  default: ({
    value,
    onChange,
    label,
    error,
  }: {
    value: string;
    onChange: (v: string) => void;
    label: string;
    error?: { line?: number; message: string };
  }) => (
    <div>
      <textarea aria-label={label} value={value} onChange={(e) => onChange(e.target.value)} />
      {error && <span data-testid="marker">line {error.line}</span>}
    </div>
  ),
}));
vi.mock("./StackFileDiff", () => ({
  default: ({ original, modified }: { original: string; modified: string }) => (
    <pre aria-label="Changes">
      {original} =&gt; {modified}
    </pre>
  ),
}));
vi.mock("../jobs/JobTerminal", () => ({
  default: ({ jobId }: { jobId: string }) => <pre aria-label="Job output">output of {jobId}</pre>,
}));

const base = "/api/v1/environments/local/stacks";
const app = {
  name: "app",
  mode: "managed",
  path: "/var/lib/nerdui/stacks/app",
  composeFile: "compose.yaml",
  status: "stopped",
  containers: 0,
  running: 0,
  services: [{ name: "web", defined: true, running: 0, containers: [] }],
};
const v1 = "services:\n  web:\n    image: nginx\n";

describe("stack file editor", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("checks while editing, shows the diff and saves with the loaded version", async () => {
    let onDisk = {
      kind: "compose",
      name: "compose.yaml",
      content: v1,
      version: "v1",
      exists: true,
    };
    let conflict = false;
    const { calls } = fakeApi({
      ...signedIn,
      [`GET ${base}/app`]: () => json(200, app),
      "GET /api/v1/compose-schema": () => json(200, { properties: {} }),
      [`GET ${base}/app/files/compose`]: () => json(200, onDisk),
      [`POST ${base}/app/validate`]: (c) => {
        const body = c.body as { compose: string };
        return body.compose.includes("imagee")
          ? json(200, {
              valid: false,
              error: "services.web additional properties 'imagee' not allowed",
              services: [],
            })
          : json(200, { valid: true, services: ["web", "db"] });
      },
      [`PUT ${base}/app/files/compose`]: (c) => {
        if (conflict) {
          return json(409, {
            error: { code: "conflict", message: "compose.yaml changed since it was loaded" },
          });
        }
        const body = c.body as { content: string };
        onDisk = { ...onDisk, content: body.content, version: "v2" };
        return json(200, onDisk);
      },
      "GET /api/v1/jobs": () => json(200, { jobs: [] }),
    });
    renderApp("/env/local/stacks/app?tab=compose");

    const editor = await screen.findByLabelText("compose.yaml of app");
    expect(editor).toHaveValue(v1);
    expect(await screen.findByText(/valid · 2 services/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Review and save" })).toBeDisabled();

    // An invalid edit: the error is shown at the line of the quoted key; saving is blocked.
    fireEvent.change(editor, { target: { value: "services:\n  web:\n    imagee: nginx\n" } });
    expect(await screen.findByText("unsaved changes")).toBeInTheDocument();
    expect(await screen.findByText("Line 3", {}, { timeout: 3000 })).toBeInTheDocument();
    expect(screen.getByTestId("marker")).toHaveTextContent("line 3");
    expect(screen.getByRole("button", { name: "Review and save" })).toBeDisabled();

    // A valid edit: review the diff, save with the version that was loaded.
    const v2 = "services:\n  web:\n    image: nginx:1.27\n";
    fireEvent.change(editor, { target: { value: v2 } });
    await waitFor(
      () => expect(screen.getByRole("button", { name: "Review and save" })).toBeEnabled(),
      {
        timeout: 3000,
      },
    );
    fireEvent.click(screen.getByRole("button", { name: "Review and save" }));
    const dialog = await screen.findByRole("dialog", { name: "Save compose.yaml" });
    expect(await within(dialog).findByLabelText("Changes")).toHaveTextContent("image: nginx:1.27");
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === "PUT")?.body).toEqual({ content: v2, version: "v1" }),
    );
    await waitFor(() => expect(screen.queryByText("unsaved changes")).not.toBeInTheDocument());

    // Changed on disk meanwhile: no overwrite; reloading takes the file from disk.
    conflict = true;
    fireEvent.change(editor, { target: { value: v2 + "# mine\n" } });
    await waitFor(
      () => expect(screen.getByRole("button", { name: "Review and save" })).toBeEnabled(),
      {
        timeout: 3000,
      },
    );
    fireEvent.click(screen.getByRole("button", { name: "Review and save" }));
    fireEvent.click(
      within(await screen.findByRole("dialog", { name: "Save compose.yaml" })).getByRole("button", {
        name: "Save",
      }),
    );
    expect(await screen.findByText("The file changed on disk")).toBeInTheDocument();
    onDisk = { ...onDisk, content: "services: {}\n", version: "v3" };
    fireEvent.click(screen.getByRole("button", { name: "Reload from disk" }));
    await waitFor(() =>
      expect(screen.getByLabelText("compose.yaml of app")).toHaveValue("services: {}\n"),
    );
  });

  it("saves and deploys", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      [`GET ${base}/app`]: () => json(200, app),
      "GET /api/v1/compose-schema": () => json(200, { properties: {} }),
      [`GET ${base}/app/files/env`]: () =>
        json(200, { kind: "env", name: ".env", content: "", version: "", exists: false }),
      [`POST ${base}/app/validate`]: () => json(200, { valid: true, services: ["web"] }),
      [`PUT ${base}/app/files/env`]: (c) =>
        json(200, {
          kind: "env",
          name: ".env",
          content: (c.body as { content: string }).content,
          version: "e1",
          exists: true,
        }),
      [`POST ${base}/app/up`]: () =>
        json(202, {
          job: {
            id: "j1",
            kind: "stack.up",
            title: "Deploy stack app",
            environment: "local",
            namespace: "default",
            status: "running",
            startedAt: "2027-03-01T10:00:00Z",
          },
        }),
      "GET /api/v1/jobs": () => json(200, { jobs: [] }),
    });
    renderApp("/env/local/stacks/app?tab=env");

    const editor = await screen.findByLabelText(".env of app");
    expect(screen.getByText("new file")).toBeInTheDocument();
    fireEvent.change(editor, { target: { value: "TAG=1.27\n" } });
    await waitFor(
      () => expect(screen.getByRole("button", { name: "Review and save" })).toBeEnabled(),
      {
        timeout: 3000,
      },
    );
    const validate = calls.filter((c) => c.path.endsWith("/validate")).at(-1);
    expect(validate?.body).toEqual({ env: "TAG=1.27\n" });
    fireEvent.click(screen.getByRole("button", { name: "Review and save" }));
    fireEvent.click(
      within(await screen.findByRole("dialog", { name: "Save .env" })).getByRole("button", {
        name: "Save and deploy",
      }),
    );
    await waitFor(() => expect(calls.some((c) => c.path === `${base}/app/up`)).toBe(true));
    expect(calls.find((c) => c.method === "PUT")?.body).toEqual({
      content: "TAG=1.27\n",
      version: "",
    });
    expect(await screen.findByRole("dialog", { name: "Jobs" })).toBeInTheDocument();
  });

  it("creates a stack and opens its compose file", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      [`GET ${base}`]: () => json(200, { namespace: "default", stacks: [] }),
      [`POST ${base}`]: () => json(201, { ...app, name: "blog" }),
      [`GET ${base}/blog`]: () => json(200, { ...app, name: "blog" }),
      "GET /api/v1/compose-schema": () => json(200, { properties: {} }),
      [`GET ${base}/blog/files/compose`]: () =>
        json(200, {
          kind: "compose",
          name: "compose.yaml",
          content: v1,
          version: "v1",
          exists: true,
        }),
      [`POST ${base}/blog/validate`]: () => json(200, { valid: true, services: ["web"] }),
    });
    renderApp("/env/local/stacks");
    fireEvent.click(await screen.findByRole("button", { name: "New stack" }));
    const dialog = await screen.findByRole("dialog", { name: "New stack" });
    fireEvent.change(within(dialog).getByLabelText(/Name/), { target: { value: "blog" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Create" }));
    expect(await screen.findByLabelText("compose.yaml of blog")).toHaveValue(v1);
    expect(calls.find((c) => c.method === "POST" && c.path === base)?.body).toEqual({
      name: "blog",
    });
  });
});
