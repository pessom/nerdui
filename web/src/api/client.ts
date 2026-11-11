// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import createClient, { type Middleware } from "openapi-fetch";
import type { components, paths } from "./schema";

export type { components, paths } from "./schema";
export type ErrorBody = components["schemas"]["Error"];

/** Error thrown by API helpers; carries the HTTP status and the ADR-0006 error code. */
export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

// The per-session CSRF token (ADR-0005), set after login/setup/me.
let csrfToken = "";

export function setCsrfToken(token: string) {
  csrfToken = token;
}

const UNSAFE = new Set(["POST", "PUT", "PATCH", "DELETE"]);

/** Adds X-CSRF-Token to unsafe requests. */
export const csrfMiddleware: Middleware = {
  onRequest({ request }) {
    if (UNSAFE.has(request.method) && csrfToken) {
      request.headers.set("X-CSRF-Token", csrfToken);
    }
    return request;
  },
};

/**
 * Typed API client generated from api/openapi.yaml.
 * Run `npm run api:gen` (or `make api-gen`) after changing the spec.
 */
export const api = createClient<paths>({
  baseUrl: globalThis.location.origin,
  credentials: "same-origin",
  // Resolve fetch lazily so tests can stub globalThis.fetch.
  fetch: (request) => globalThis.fetch(request),
});
api.use(csrfMiddleware);

/** Returns data or throws ApiError, for use in TanStack Query functions. */
export async function unwrap<T>(
  promise: Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T> {
  const { data, error, response } = await promise;
  if (error !== undefined || data === undefined) {
    const body = error as Partial<ErrorBody> | undefined;
    throw new ApiError(
      response.status,
      body?.error?.code ?? "unknown",
      body?.error?.message ?? `${response.status} ${response.statusText}`,
    );
  }
  return data;
}

/** Like unwrap, for endpoints without a response body (204). */
export async function unwrapEmpty(
  promise: Promise<{ error?: unknown; response: Response }>,
): Promise<void> {
  const { error, response } = await promise;
  if (!response.ok) {
    const body = error as Partial<ErrorBody> | undefined;
    throw new ApiError(
      response.status,
      body?.error?.code ?? "unknown",
      body?.error?.message ?? `${response.status} ${response.statusText}`,
    );
  }
}
