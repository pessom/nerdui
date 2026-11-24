// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { queryOptions } from "@tanstack/react-query";
import { api, unwrap } from "../../api/client";
import type { components } from "../../api/schema";

export type TemplateSummary = components["schemas"]["TemplateSummary"];
export type Template = components["schemas"]["Template"];
export type TemplateVariable = Template["variables"][number];

/** Stack templates (#33, #34, ADR-0051). */
export const templatesQuery = queryOptions({
  queryKey: ["templates"] as const,
  queryFn: () => unwrap(api.GET("/api/v1/templates")),
});

export const templateQuery = (source: string, id: string) =>
  queryOptions({
    queryKey: ["templates", "detail", source, id] as const,
    queryFn: () =>
      unwrap(api.GET("/api/v1/templates/detail", { params: { query: { source, id } } })),
  });

export const templateRegistriesQuery = queryOptions({
  queryKey: ["template-registries"] as const,
  queryFn: () => unwrap(api.GET("/api/v1/template-registries")),
});

/** Where a template comes from, in a word. */
export function sourceLabel(source: string): string {
  if (source === "builtin") return "Built-in";
  if (source === "custom") return "Mine";
  try {
    return new URL(source).host;
  } catch {
    return source;
  }
}

/** A stack name derived from a template's id or name. */
export function stackName(t: TemplateSummary): string {
  const base = (t.source === "custom" ? t.name : t.id)
    .toLowerCase()
    .replace(/[^a-z0-9_.-]+/g, "-")
    .replace(/^[^a-z0-9]+|-+$/g, "");
  return base || "app";
}

function quote(v: string): string {
  return /^[A-Za-z0-9_./:@,+-]*$/.test(v) ? v : JSON.stringify(v);
}

/**
 * The .env of a stack deployed from a template: the .env.example with the values entered;
 * secrets are left out, they become variables of the stack.
 */
export function renderEnv(
  example: string,
  values: Record<string, string>,
  secret: Record<string, boolean>,
): string {
  const lines = example.split("\n").map((line) => {
    const name = /^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=/.exec(line)?.[1];
    if (!name || !(name in values)) return line;
    return secret[name]
      ? `# ${name} is a secret variable of the stack`
      : `${name}=${quote(values[name] ?? "")}`;
  });
  return lines.join("\n");
}

const secretName = /password|passwd|secret|token|api_?key|private_?key/i;

/** An .env.example from a stack's .env: values of secret-looking names are emptied. */
export function exampleFromEnv(env: string): string {
  return env
    .split("\n")
    .map((line) => {
      const m = /^(\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=)/.exec(line);
      return m?.[1] && m[2] && secretName.test(m[2]) ? m[1] : line;
    })
    .join("\n");
}
