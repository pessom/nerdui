// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { fakeApi, json } from "../../test/server";

const spec = {
  openapi: "3.1.0",
  info: { title: "nerdui API", version: "0.1.0" },
  tags: [{ name: "volumes", description: "Named volumes (#57)" }, { name: "system" }],
  paths: {
    "/api/v1/version": {
      get: {
        tags: ["system"],
        summary: "Version",
        security: [],
        responses: { "200": { description: "ok" } },
      },
    },
    "/api/v1/environments/{env}/volumes/{name}": {
      delete: {
        tags: ["volumes"],
        summary: "Remove a volume and its data",
        description: "Requires `volumes:write`. Refused (409) while used.",
        parameters: [{ name: "name", in: "path", required: true, schema: { type: "string" } }],
        responses: { "204": { description: "Removed" }, "409": { description: "In use" } },
      },
    },
  },
};

describe("API documentation", () => {
  it("lists endpoints by tag with permissions and examples, and filters them", async () => {
    fakeApi({ ...signedIn, "GET /api/v1/openapi.json": () => json(200, spec) });
    renderApp("/api-docs");

    const del = await screen.findByRole("region", {
      name: "DELETE /api/v1/environments/{env}/volumes/{name}",
    });
    expect(within(del).getAllByText("volumes:write")).toHaveLength(2); // badge and description
    expect(within(del).getByText("Remove a volume and its data")).toBeInTheDocument();
    expect(within(del).getByText(/curl -sS/)).toHaveTextContent("-X DELETE");
    expect(
      within(screen.getByRole("region", { name: "GET /api/v1/version" })).getByText("public"),
    ).toBeInTheDocument();
    expect(screen.getByText("Named volumes (#57)")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "openapi.yaml" })).toHaveAttribute(
      "href",
      "/api/v1/openapi.yaml",
    );

    fireEvent.change(screen.getByLabelText("Search endpoints"), {
      target: { value: "volumes:write" },
    });
    expect(await screen.findByText("1 of 2 endpoints", { exact: false })).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "GET /api/v1/version" })).not.toBeInTheDocument();
  });
});
