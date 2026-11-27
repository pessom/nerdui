// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";
import { exampleFromEnv, renderEnv, stackName } from "./templates";

vi.mock("../jobs/JobTerminal", () => ({
  default: ({ jobId }: { jobId: string }) => <pre aria-label="Job output">output of {jobId}</pre>,
}));

const postgres = {
  source: "builtin",
  id: "postgres",
  name: "PostgreSQL",
  description: "Relational database server.",
  categories: ["database"],
};
const mine = { source: "custom", id: "3", name: "Shop", categories: [] };
const registry = {
  source: "https://example.com/registry.json",
  id: "whoami",
  name: "whoami",
  categories: ["tools"],
};
const envExample = "# Superuser password\nPOSTGRES_PASSWORD=\n# Port on the host\nPORT=5432\n";

describe("templates helpers", () => {
  it("renders .env without secrets and makes examples", () => {
    expect(
      renderEnv(
        envExample,
        { POSTGRES_PASSWORD: "pw", PORT: "15432 x" },
        { POSTGRES_PASSWORD: true },
      ),
    ).toBe(
      '# Superuser password\n# POSTGRES_PASSWORD is a secret variable of the stack\n# Port on the host\nPORT="15432 x"\n',
    );
    expect(exampleFromEnv("DB_PASSWORD=x\nTAG=1\nAPI_KEY=k")).toBe("DB_PASSWORD=\nTAG=1\nAPI_KEY=");
    expect(stackName(postgres)).toBe("postgres");
    expect(stackName({ ...mine, name: "My Shop!" })).toBe("my-shop");
  });
});

describe("template catalog", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("explains an empty Mine", async () => {
    fakeApi({
      ...signedIn,
      "GET /api/v1/templates": () => json(200, { templates: [postgres], errors: [] }),
    });
    renderApp("/env/local/templates");
    expect(await screen.findByRole("article", { name: "PostgreSQL" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("radio", { name: "Mine" }));
    expect(screen.getByText(/no templates of your own yet/)).toBeInTheDocument();
  });

  it("filters, deploys a template with a secret and opens the stack", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/templates": () =>
        json(200, {
          templates: [postgres, mine, registry],
          errors: [{ source: "https://broken.example/registry.json", error: "404 Not Found" }],
        }),
      "GET /api/v1/templates/detail": () =>
        json(200, {
          ...postgres,
          compose: "services:\n  postgres:\n    image: postgres:17\n",
          env: envExample,
          variables: [
            {
              name: "POSTGRES_PASSWORD",
              default: "",
              description: "Superuser password",
              secret: true,
            },
            { name: "PORT", default: "5432", description: "Port on the host", secret: false },
          ],
        }),
      "POST /api/v1/environments/local/stacks": (c) =>
        json(201, {
          name: (c.body as { name: string }).name,
          mode: "managed",
          status: "stopped",
          services: [],
          containers: 0,
          running: 0,
        }),
      "POST /api/v1/environments/local/stacks/pg/up": () =>
        json(202, {
          job: {
            id: "j1",
            kind: "stack.up",
            title: "Deploy stack pg",
            environment: "local",
            namespace: "default",
            status: "running",
            startedAt: "2026-11-24T10:00:00Z",
          },
        }),
      "GET /api/v1/environments/local/stacks/pg": () =>
        json(200, {
          name: "pg",
          mode: "managed",
          status: "stopped",
          services: [],
          containers: 0,
          running: 0,
        }),
      "GET /api/v1/jobs": () => json(200, { jobs: [] }),
    });
    renderApp("/env/local/templates");

    expect(await screen.findByRole("article", { name: "PostgreSQL" })).toBeInTheDocument();
    expect(screen.getByText(/broken\.example/)).toBeInTheDocument();
    expect(screen.getByText("example.com")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("radio", { name: "Mine" }));
    expect(screen.queryByRole("article", { name: "PostgreSQL" })).not.toBeInTheDocument();
    expect(screen.getByRole("article", { name: "Shop" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("radio", { name: "All" }));
    fireEvent.change(screen.getByLabelText("Search templates"), {
      target: { value: "relational" },
    });
    expect(screen.queryByRole("article", { name: "Shop" })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Deploy PostgreSQL" }));
    const dialog = await screen.findByRole("dialog", { name: "Deploy PostgreSQL" });
    const name = await within(dialog).findByLabelText(/^Stack name/);
    expect(name).toHaveValue("postgres");
    fireEvent.change(name, { target: { value: "pg" } });
    fireEvent.change(within(dialog).getByLabelText("POSTGRES_PASSWORD"), {
      target: { value: "s3cret" },
    });
    fireEvent.change(within(dialog).getByLabelText("PORT"), { target: { value: "15432" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Create and deploy" }));

    await waitFor(() => expect(calls.some((c) => c.path.endsWith("/pg/up"))).toBe(true));
    expect(
      calls.find((c) => c.method === "POST" && c.path === "/api/v1/environments/local/stacks")
        ?.body,
    ).toEqual({
      name: "pg",
      compose: "services:\n  postgres:\n    image: postgres:17\n",
      env: "# Superuser password\n# POSTGRES_PASSWORD is a secret variable of the stack\n# Port on the host\nPORT=15432\n",
      variables: [{ name: "POSTGRES_PASSWORD", value: "s3cret", secret: true }],
    });
    expect(await screen.findByRole("heading", { name: "pg" })).toBeInTheDocument();
  });

  it("edits the registries", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/templates": () => json(200, { templates: [], errors: [] }),
      "GET /api/v1/template-registries": () => json(200, { urls: ["https://a.example/r.json"] }),
      "PUT /api/v1/template-registries": (c) => json(200, c.body as object),
    });
    renderApp("/env/local/templates");
    fireEvent.click(await screen.findByRole("button", { name: "Registries" }));
    const dialog = await screen.findByRole("dialog", { name: "Template registries" });
    const urls = await within(dialog).findByLabelText(/Registry URLs/);
    expect(urls).toHaveValue("https://a.example/r.json");
    fireEvent.change(urls, {
      target: { value: "https://a.example/r.json\n\n https://b.example/r.json " },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === "PUT")?.body).toEqual({
        urls: ["https://a.example/r.json", "https://b.example/r.json"],
      }),
    );
  });

  it("saves a stack as a template without secret values", async () => {
    const base = "/api/v1/environments/local/stacks/shop";
    const { calls } = fakeApi({
      ...signedIn,
      [`GET ${base}`]: () =>
        json(200, {
          name: "shop",
          mode: "managed",
          path: "/var/lib/nerdui/stacks/shop",
          composeFile: "compose.yaml",
          status: "stopped",
          services: [],
          containers: 0,
          running: 0,
        }),
      [`GET ${base}/files/compose`]: () =>
        json(200, {
          kind: "compose",
          name: "compose.yaml",
          content: "services: {}\n",
          version: "v",
          exists: true,
        }),
      [`GET ${base}/files/env`]: () =>
        json(200, {
          kind: "env",
          name: ".env",
          content: "TAG=1\nDB_PASSWORD=hunter2\n",
          version: "e",
          exists: true,
        }),
      "POST /api/v1/templates": (c) =>
        json(201, { ...(c.body as object), source: "custom", id: "9" }),
    });
    renderApp("/env/local/stacks/shop");
    fireEvent.click(await screen.findByRole("button", { name: "Save as template" }));
    const dialog = await screen.findByRole("dialog", { name: "Save as template" });
    expect(within(dialog).getByLabelText(/Variables/)).toHaveValue("TAG=1\nDB_PASSWORD=\n");
    fireEvent.click(within(dialog).getByRole("button", { name: "Save template" }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === "POST")?.body).toEqual({
        name: "shop",
        description: "",
        compose: "services: {}\n",
        env: "TAG=1\nDB_PASSWORD=\n",
      }),
    );
  });
});
