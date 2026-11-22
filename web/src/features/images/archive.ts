// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { ApiError, getCsrfToken, type ErrorBody } from "../../api/client";

/** URL of a tar archive of images (#46), downloaded by the browser. */
export function saveUrl(env: string, namespace: string, names: string[]): string {
  const q = new URLSearchParams({ namespace });
  for (const n of names) q.append("ref", n);
  return `/api/v1/environments/${encodeURIComponent(env)}/images/save?${q.toString()}`;
}

/** Starts the download of images as a tar archive. */
export function downloadImages(env: string, namespace: string, names: string[]) {
  const a = document.createElement("a");
  a.href = saveUrl(env, namespace, names);
  a.download = "";
  document.body.append(a);
  a.click();
  a.remove();
}

/** Uploads a tar archive of images (#46), reporting progress from 0 to 1. */
export function uploadImages(
  env: string,
  namespace: string,
  file: File,
  onProgress: (fraction: number) => void,
  signal?: AbortSignal,
): Promise<string[]> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    const q = new URLSearchParams({ namespace });
    xhr.open("POST", `/api/v1/environments/${encodeURIComponent(env)}/images/load?${q.toString()}`);
    const gzip = /\.(tgz|gz)$/i.test(file.name);
    xhr.setRequestHeader("Content-Type", gzip ? "application/gzip" : "application/x-tar");
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
      if (xhr.status === 200) {
        resolve((body as { images?: string[] } | undefined)?.images ?? []);
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
    xhr.send(file);
  });
}
