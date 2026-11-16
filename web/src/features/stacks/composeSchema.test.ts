// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from "vitest";
import { ITEM, complete, errorLine, pathAt, schemaAt, type Schema } from "./composeSchema";

// A reduced copy of the Compose schema's shape: $defs, allOf, patternProperties, oneOf.
const schema: Schema = {
  type: "object",
  properties: {
    name: { type: "string", description: "Project name" },
    services: {
      type: "object",
      patternProperties: { "^[a-zA-Z0-9._-]+$": { $ref: "#/$defs/service" } },
    },
    volumes: { type: "object" },
    version: { type: "string", deprecated: true },
  },
  $defs: {
    service: {
      allOf: [{ $ref: "#/$defs/container_spec" }],
      properties: { restart: { type: "string", description: "Restart policy" } },
    },
    container_spec: {
      properties: {
        image: { type: "string", description: "Image to start the container from" },
        pull_policy: { type: "string", enum: ["always", "never", "missing"] },
        read_only: { type: ["boolean", "string"] },
        environment: { $ref: "#/$defs/list_or_dict" },
        deploy: { type: "object", properties: { replicas: { type: "integer" } } },
        ports: {
          type: "array",
          items: {
            oneOf: [
              { type: "number" },
              {
                type: "object",
                properties: { target: { type: "integer" }, published: { type: "string" } },
              },
            ],
          },
        },
      },
    },
    list_or_dict: { oneOf: [{ type: "object" }, { type: "array" }] },
  },
};

const at = (text: string) => {
  const cursor = text.indexOf("|");
  const doc = text.slice(0, cursor) + text.slice(cursor + 1);
  const lines = doc.split("\n");
  const before = doc.slice(0, cursor).split("\n");
  return { lines, lineNo: before.length - 1, col: before.at(-1)!.length };
};

const labels = (text: string) => {
  const { lines, lineNo, col } = at(text);
  return complete(schema, lines, lineNo, col)?.options.map((o) => o.label);
};

describe("compose completion", () => {
  it("finds the path of a position", () => {
    const lines = ["services:", "  web:", "    ports:", "      - target: 80", "        "];
    expect(pathAt(lines, 4, 8)).toEqual(["services", "web", "ports", ITEM]);
    expect(pathAt(lines, 2, 4)).toEqual(["services", "web"]);
    expect(schemaAt(schema, ["services", "web", "ports", ITEM])).toBeDefined();
  });

  it("suggests top-level keys, without deprecated and present ones", () => {
    expect(labels("services:\n  web: {}\nna|")).toEqual(["name", "volumes"]);
  });

  it("suggests service keys merged over allOf", () => {
    expect(labels("services:\n  web:\n    image: nginx\n    |")).toEqual([
      "deploy",
      "environment",
      "ports",
      "pull_policy",
      "read_only",
      "restart",
    ]);
    const { lines, lineNo, col } = at("services:\n  web:\n    de|");
    const r = complete(schema, lines, lineNo, col)!;
    expect(r.from).toBe(4);
    const deploy = r.options.find((o) => o.label === "deploy")!;
    expect(deploy.apply).toBe("deploy:\n      ");
    expect(r.options.find((o) => o.label === "image")).toMatchObject({
      apply: "image: ",
      detail: "string",
      info: "Image to start the container from",
    });
  });

  it("suggests keys of list elements and nested objects", () => {
    expect(labels("services:\n  web:\n    ports:\n      - |")).toEqual(["published", "target"]);
    expect(labels("services:\n  web:\n    ports:\n      - target: 80\n        |")).toEqual([
      "published",
    ]);
    expect(labels("services:\n  web:\n    deploy:\n      |")).toEqual(["replicas"]);
  });

  it("suggests enum and boolean values", () => {
    expect(labels("services:\n  web:\n    pull_policy: |")).toEqual(["always", "never", "missing"]);
    expect(labels("services:\n  web:\n    read_only: t|")).toEqual(["true", "false"]);
    expect(labels("services:\n  web:\n    image: |")).toBeUndefined();
  });

  it("offers no keys for free-form names", () => {
    expect(labels("services:\n  |")).toBeUndefined();
  });

  it("locates errors", () => {
    const text = "services:\n  web:\n    imagee: nginx\n";
    expect(errorLine(text, "services.web additional properties 'imagee' not allowed")).toBe(3);
    expect(errorLine(text, "yaml: line 2: bad", 2)).toBe(2);
    expect(errorLine(text, "something else")).toBeUndefined();
  });
});

// The real schema of a compose-go version, when COMPOSE_SCHEMA points to it (run locally with
// $(go env GOMODCACHE)/github.com/compose-spec/compose-go/v2@<version>/schema/compose-spec.json).
declare const process: { env: Record<string, string | undefined> } | undefined;
const realSchemaPath = typeof process === "undefined" ? undefined : process.env.COMPOSE_SCHEMA;

describe("compose completion with the real schema", () => {
  it.skipIf(!realSchemaPath)("resolves services and ports", async () => {
    const fsModule = "node:fs";
    const fs = (await import(/* @vite-ignore */ fsModule)) as {
      readFileSync: (path: string, encoding: string) => string;
    };
    const real = JSON.parse(fs.readFileSync(realSchemaPath!, "utf8")) as Schema;
    const { lines, lineNo, col } = at("services:\n  web:\n    |");
    const keys = complete(real, lines, lineNo, col)!.options.map((o) => o.label);
    expect(keys).toEqual(
      expect.arrayContaining(["image", "ports", "restart", "volumes", "deploy"]),
    );
    const ports = at("services:\n  web:\n    ports:\n      - |");
    const p = complete(real, ports.lines, ports.lineNo, ports.col)!.options.map((o) => o.label);
    expect(p).toEqual(expect.arrayContaining(["target", "published", "protocol"]));
  });
});
