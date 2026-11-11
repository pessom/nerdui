import createClient from "openapi-fetch";
import type { paths } from "./schema";

/**
 * Typed API client generated from api/openapi.yaml.
 * Run `npm run api:gen` (or `make api-gen`) after changing the spec.
 */
export const api = createClient<paths>({
  baseUrl: globalThis.location.origin,
  // Resolve fetch lazily so tests can stub globalThis.fetch.
  fetch: (request) => globalThis.fetch(request),
});

export type { components, paths } from "./schema";
