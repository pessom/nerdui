// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { ApiError, getCsrfToken, type ErrorBody, type components } from "../../api/client";
import { withBase } from "../../lib/base";

export type BuildRequest = components["schemas"]["BuildRequest"];
type Job = components["schemas"]["Job"];

/** Tags separated by spaces, commas or new lines. */
export function parseTags(text: string): string[] {
  return text
    .split(/[\s,]+/)
    .map((t) => t.trim())
    .filter(Boolean);
}

/**
 * Build arguments, one NAME=value per line; blank lines and # comments are skipped. Returns the
 * arguments, or the first line that is not NAME=value.
 */
export function parseArgs(text: string): { args: Record<string, string>; bad?: string } {
  const args: Record<string, string> = {};
  for (const raw of text.split("\n")) {
    const line = raw.trim();
    if (!line || line.startsWith("#")) continue;
    const eq = line.indexOf("=");
    const name = eq > 0 ? line.slice(0, eq).trim() : "";
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(name)) return { args, bad: line };
    args[name] = line.slice(eq + 1);
  }
  return { args };
}

/**
 * Starts a build from an uploaded context archive (#45): multipart with the request first,
 * then the archive. Reports the upload progress from 0 to 1 and resolves with the job.
 */
export function uploadBuild(
  env: string,
  namespace: string,
  request: BuildRequest,
  file: File,
  onProgress: (fraction: number) => void,
  signal?: AbortSignal,
): Promise<Job> {
  return new Promise((resolve, reject) => {
    const form = new FormData();
    form.append("request", new Blob([JSON.stringify(request)], { type: "application/json" }));
    form.append("archive", file);
    const xhr = new XMLHttpRequest();
    const q = new URLSearchParams({ namespace });
    xhr.open(
      "POST",
      withBase(`/api/v1/environments/${encodeURIComponent(env)}/images/build?${q.toString()}`),
    );
    const token = getCsrfToken();
    if (token) xhr.setRequestHeader("X-CSRF-Token", token);
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress(e.loaded / e.total);
    };
    xhr.onload = () => {
      let body: unknown;
      try {
        body = JSON.parse(xhr.responseText);
      } catch {
        body = undefined;
      }
      if (xhr.status === 202) {
        resolve((body as { job: Job }).job);
        return;
      }
      const err = (body as Partial<ErrorBody> | undefined)?.error;
      reject(
        new ApiError(
          xhr.status,
          err?.code ?? "unknown",
          err?.message ?? `${xhr.status} ${xhr.statusText}`,
        ),
      );
    };
    xhr.onerror = () => reject(new Error("Upload failed: the connection was interrupted"));
    xhr.onabort = () => reject(new DOMException("Upload canceled", "AbortError"));
    signal?.addEventListener("abort", () => xhr.abort());
    xhr.send(form);
  });
}
