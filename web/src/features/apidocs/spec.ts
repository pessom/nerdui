// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * A small reader of nerdui's OpenAPI document for the API documentation page (#87): operations
 * grouped by tag, the action they require, parameters, and schemas shown as compact type text.
 */

export interface SchemaObject {
  $ref?: string;
  type?: string | string[];
  format?: string;
  description?: string;
  enum?: unknown[];
  items?: SchemaObject;
  properties?: Record<string, SchemaObject>;
  required?: string[];
  additionalProperties?: boolean | SchemaObject;
  allOf?: SchemaObject[];
  oneOf?: SchemaObject[];
  anyOf?: SchemaObject[];
  examples?: unknown[];
}

interface ParameterObject {
  $ref?: string;
  name: string;
  in: string;
  required?: boolean;
  description?: string;
  schema?: SchemaObject;
}

interface MediaType {
  schema?: SchemaObject;
}

interface OperationObject {
  tags?: string[];
  operationId?: string;
  summary?: string;
  description?: string;
  parameters?: ParameterObject[];
  requestBody?: { required?: boolean; content?: Record<string, MediaType> };
  responses?: Record<
    string,
    { $ref?: string; description?: string; content?: Record<string, MediaType> }
  >;
  security?: unknown[];
}

export interface OpenAPIDoc {
  openapi: string;
  info: { title: string; version: string; description?: string };
  tags?: { name: string; description?: string }[];
  paths: Record<string, Record<string, OperationObject | ParameterObject[]>>;
  components?: {
    schemas?: Record<string, SchemaObject>;
    parameters?: Record<string, ParameterObject>;
    responses?: Record<string, { description?: string }>;
  };
}

export interface Param {
  name: string;
  in: string;
  required: boolean;
  type: string;
  description?: string;
}

export interface Operation {
  id: string;
  method: string;
  path: string;
  tag: string;
  summary: string;
  description: string;
  /** The authorization action from "Requires `…`", "public", or undefined. */
  action?: string;
  websocket: boolean;
  params: Param[];
  body?: { required: boolean; type: string; schema?: SchemaObject };
  responses: { status: string; description: string; type?: string }[];
}

const METHODS = ["get", "post", "put", "patch", "delete"];

function refName(ref: string): string {
  return ref.slice(ref.lastIndexOf("/") + 1);
}

function resolveParam(doc: OpenAPIDoc, p: ParameterObject): ParameterObject {
  return p.$ref ? (doc.components?.parameters?.[refName(p.$ref)] ?? p) : p;
}

/** A compact type: a schema name, a scalar, an array, an object or a union. */
export function typeText(s: SchemaObject | undefined, depth = 0): string {
  if (!s) return "any";
  if (s.$ref) return refName(s.$ref);
  if (s.enum) return s.enum.map((v) => JSON.stringify(v)).join(" | ");
  const union = s.oneOf ?? s.anyOf;
  if (union) return union.map((u) => typeText(u, depth)).join(" | ");
  if (s.allOf) return s.allOf.map((u) => typeText(u, depth)).join(" & ");
  const types = [s.type].flat().filter((t): t is string => !!t);
  if (types.includes("array")) return `${typeText(s.items, depth)}[]`;
  if (types.includes("object") || s.properties) {
    if (!s.properties) {
      return typeof s.additionalProperties === "object"
        ? `{ [key]: ${typeText(s.additionalProperties, depth)} }`
        : "object";
    }
    if (depth >= 2) return "object";
    const fields = Object.entries(s.properties).map(
      ([k, v]) => `${k}${s.required?.includes(k) ? "" : "?"}: ${typeText(v, depth + 1)}`,
    );
    return `{ ${fields.join("; ")} }`;
  }
  const scalar = types.join(" | ") || "any";
  return s.format ? `${scalar} (${s.format})` : scalar;
}

/** The authorization a description states: "Requires `containers:read`", or public. */
export function requiredAction(op: OperationObject): string | undefined {
  if (Array.isArray(op.security) && op.security.length === 0) return "public";
  const m = /Requires (?:a )?`([^`]+)`/.exec(op.description ?? "");
  if (m) return m[1];
  if (/Requires a password session/.test(op.description ?? "")) return "password session";
  return undefined;
}

/** Every operation of the document, in path order. */
export function operations(doc: OpenAPIDoc): Operation[] {
  const out: Operation[] = [];
  for (const [path, item] of Object.entries(doc.paths)) {
    const shared = (Array.isArray(item.parameters) ? item.parameters : []) as ParameterObject[];
    for (const method of METHODS) {
      const op = item[method] as OperationObject | undefined;
      if (!op || Array.isArray(op)) continue;
      const params = [...shared, ...(op.parameters ?? [])].map((p) => {
        const r = resolveParam(doc, p);
        return {
          name: r.name,
          in: r.in,
          required: !!r.required,
          type: typeText(r.schema),
          description: r.description,
        };
      });
      const bodySchema = op.requestBody?.content
        ? Object.values(op.requestBody.content)[0]?.schema
        : undefined;
      const responses = Object.entries(op.responses ?? {}).map(([status, r]) => {
        const shared = r.$ref ? doc.components?.responses?.[refName(r.$ref)] : undefined;
        const schema = r.content ? Object.values(r.content)[0]?.schema : undefined;
        return {
          status,
          description: r.description ?? shared?.description ?? "",
          type: schema ? typeText(schema) : undefined,
        };
      });
      out.push({
        id: op.operationId ?? `${method}-${path}`,
        method: method.toUpperCase(),
        path,
        tag: op.tags?.[0] ?? "other",
        summary: op.summary ?? "",
        description: op.description ?? "",
        action: requiredAction(op),
        websocket: responses.some((r) => r.status === "101"),
        params,
        body: op.requestBody
          ? { required: !!op.requestBody.required, type: typeText(bodySchema), schema: bodySchema }
          : undefined,
        responses,
      });
    }
  }
  return out;
}

/** A curl example with an API key; a JSON body is sketched from the schema. */
export function curlExample(op: Operation, origin: string): string {
  const path = op.path
    .replace(/\{env\}/, "local")
    .replace(/\{(\w+)\}/g, (_, n: string) => `<${n}>`);
  if (op.websocket) {
    return `websocat -H 'Authorization: Bearer $NERDUI_KEY' '${origin.replace(/^http/, "ws")}${path}'`;
  }
  const parts = [`curl -sS`];
  if (op.method !== "GET") parts.push(`-X ${op.method}`);
  parts.push(`-H 'Authorization: Bearer $NERDUI_KEY'`);
  if (op.body) {
    parts.push(`-H 'Content-Type: application/json'`);
    parts.push(`-d '${JSON.stringify(sketch(op.body.schema))}'`);
  }
  parts.push(`'${origin}${path}'`);
  return parts.join(" \\\n  ");
}

function sketch(s: SchemaObject | undefined): unknown {
  if (!s || s.$ref) return {};
  if (s.properties) {
    const out: Record<string, unknown> = {};
    for (const k of s.required ?? Object.keys(s.properties).slice(0, 3)) {
      const p = s.properties[k];
      const t = [p?.type].flat()[0];
      out[k] =
        p?.enum?.[0] ??
        (t === "boolean"
          ? false
          : t === "integer" || t === "number"
            ? 0
            : t === "array"
              ? []
              : t === "object"
                ? {}
                : "…");
    }
    return out;
  }
  return {};
}
