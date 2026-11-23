// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { queryOptions } from "@tanstack/react-query";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import type { components } from "../../api/schema";

export type Variable = components["schemas"]["Variable"];

/** Where variables live: shared by all stacks, or one stack's own (#35, ADR-0049). */
export type VariableScope = { kind: "global" } | { kind: "stack"; env: string; name: string };

export const variablesKey = (scope: VariableScope) =>
  scope.kind === "global"
    ? (["variables"] as const)
    : (["variables", "stack", scope.env, scope.name] as const);

export const variablesQuery = (scope: VariableScope) =>
  queryOptions({
    queryKey: variablesKey(scope),
    queryFn: () =>
      scope.kind === "global"
        ? unwrap(api.GET("/api/v1/variables"))
        : unwrap(
            api.GET("/api/v1/environments/{env}/stacks/{name}/variables", {
              params: { path: { env: scope.env, name: scope.name } },
            }),
          ),
  });

export function putVariable(
  scope: VariableScope,
  name: string,
  body: { value?: string; secret: boolean },
) {
  return scope.kind === "global"
    ? unwrap(api.PUT("/api/v1/variables/{var}", { params: { path: { var: name } }, body }))
    : unwrap(
        api.PUT("/api/v1/environments/{env}/stacks/{name}/variables/{var}", {
          params: { path: { env: scope.env, name: scope.name, var: name } },
          body,
        }),
      );
}

export function deleteVariable(scope: VariableScope, name: string) {
  return scope.kind === "global"
    ? unwrapEmpty(api.DELETE("/api/v1/variables/{var}", { params: { path: { var: name } } }))
    : unwrapEmpty(
        api.DELETE("/api/v1/environments/{env}/stacks/{name}/variables/{var}", {
          params: { path: { env: scope.env, name: scope.name, var: name } },
        }),
      );
}

const reserved = ["PATH", "HOME", "USER", "LOGNAME", "SHELL", "PWD", "TMPDIR", "IFS"];
const reservedPrefixes = [
  "LD_",
  "XDG_",
  "CONTAINERD_",
  "NERDCTL_",
  "DOCKER_",
  "COMPOSE_",
  "BUILDKIT_",
  "CNI_",
  "NERDUI_",
  "ROOTLESSKIT_",
];

/** The server's checks of a variable name, for the form; null when valid. */
export function variableNameError(name: string): string | null {
  if (!name) return "Enter a name";
  if (name.length > 128 || !/^[A-Za-z_][A-Za-z0-9_]*$/.test(name))
    return "Letters, digits and _, not starting with a digit";
  const up = name.toUpperCase();
  if (reserved.includes(up)) return `${name} is reserved`;
  const p = reservedPrefixes.find((p) => up.startsWith(p));
  return p ? `Names starting with ${p} are reserved` : null;
}
