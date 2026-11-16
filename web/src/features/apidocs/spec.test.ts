// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from "vitest";
import { curlExample, operations, typeText, type OpenAPIDoc } from "./spec";

const doc: OpenAPIDoc = {
  openapi: "3.1.0",
  info: { title: "nerdui", version: "1" },
  paths: {
    "/api/v1/version": {
      get: { summary: "Version", security: [], responses: { "200": { description: "ok" } } },
    },
    "/api/v1/environments/{env}/volumes": {
      parameters: [{ $ref: "#/components/parameters/Env" } as never],
      get: {
        tags: ["volumes"],
        summary: "List",
        description: "Requires `volumes:read` (#57).",
        parameters: [{ name: "size", in: "query", schema: { type: "boolean" } }],
        responses: {
          "200": {
            description: "Volumes",
            content: {
              "application/json": {
                schema: {
                  type: "object",
                  properties: {
                    volumes: { type: "array", items: { $ref: "#/components/schemas/Volume" } },
                  },
                  required: ["volumes"],
                },
              },
            },
          },
          "404": { $ref: "#/components/responses/Error" },
        },
      },
      post: {
        tags: ["volumes"],
        summary: "Create",
        description: "Requires `volumes:write`.",
        requestBody: {
          required: true,
          content: {
            "application/json": {
              schema: {
                type: "object",
                required: ["name"],
                properties: {
                  name: { type: "string" },
                  labels: { type: "object", additionalProperties: { type: "string" } },
                },
              },
            },
          },
        },
        responses: { "201": { description: "Created" } },
      },
    },
    "/api/v1/environments/{env}/stats/stream": {
      get: {
        summary: "Top",
        description: "Requires `containers:read`.",
        responses: { "101": { description: "ws" } },
      },
    },
  },
  components: {
    parameters: { Env: { name: "env", in: "path", required: true, schema: { type: "string" } } },
    responses: { Error: { description: "Error envelope" } },
  },
};

describe("API docs", () => {
  it("formats types", () => {
    expect(typeText({ $ref: "#/components/schemas/Volume" })).toBe("Volume");
    expect(typeText({ type: "array", items: { type: "string" } })).toBe("string[]");
    expect(typeText({ type: ["number", "null"] })).toBe("number | null");
    expect(typeText({ enum: ["up", "down"] })).toBe('"up" | "down"');
    expect(typeText({ type: "integer", format: "int64" })).toBe("integer (int64)");
    expect(
      typeText({
        type: "object",
        required: ["a"],
        properties: { a: { type: "string" }, b: { type: "boolean" } },
      }),
    ).toBe("{ a: string; b?: boolean }");
    expect(typeText({ type: "object", additionalProperties: { type: "string" } })).toBe(
      "{ [key]: string }",
    );
  });

  it("reads operations with their permission, parameters and responses", () => {
    const ops = operations(doc);
    expect(ops.map((o) => `${o.method} ${o.path}`)).toEqual([
      "GET /api/v1/version",
      "GET /api/v1/environments/{env}/volumes",
      "POST /api/v1/environments/{env}/volumes",
      "GET /api/v1/environments/{env}/stats/stream",
    ]);
    const [version, list, create, ws] = ops;
    expect(version!.action).toBe("public");
    expect(list!.action).toBe("volumes:read");
    expect(list!.params).toEqual([
      { name: "env", in: "path", required: true, type: "string", description: undefined },
      { name: "size", in: "query", required: false, type: "boolean", description: undefined },
    ]);
    expect(list!.responses).toEqual([
      { status: "200", description: "Volumes", type: "{ volumes: Volume[] }" },
      { status: "404", description: "Error envelope", type: undefined },
    ]);
    expect(create!.body).toMatchObject({
      required: true,
      type: "{ name: string; labels?: { [key]: string } }",
    });
    expect(ws!.websocket).toBe(true);
  });

  it("sketches curl examples with an API key", () => {
    const [, , create, ws] = operations(doc);
    const curl = curlExample(create!, "https://host:9443");
    expect(curl).toContain("-X POST");
    expect(curl).toContain("Authorization: Bearer $NERDUI_KEY");
    expect(curl).toContain(`-d '{"name":"…"}'`);
    expect(curl).toContain("'https://host:9443/api/v1/environments/local/volumes'");
    expect(curlExample(ws!, "https://host:9443")).toContain(
      "wss://host:9443/api/v1/environments/local/stats/stream",
    );
  });
});
