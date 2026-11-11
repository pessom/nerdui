// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Fails when the JavaScript loaded on first page view exceeds the budget (ADR-0010,
// NFR-002): every script and modulepreload referenced by dist/index.html, gzip-compressed.
// Lazily loaded route chunks are not counted.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { gzipSync } from "node:zlib";

const BUDGET_KB = 500;
const dist = new URL("../dist/", import.meta.url).pathname;
const html = readFileSync(join(dist, "index.html"), "utf8");

const refs = new Set();
for (const m of html.matchAll(/<script[^>]+src="([^"]+\.js)"/g)) refs.add(m[1]);
for (const m of html.matchAll(/<link[^>]+rel="modulepreload"[^>]+href="([^"]+\.js)"/g))
  refs.add(m[1]);
if (refs.size === 0) {
  console.error("check-bundle: no scripts found in dist/index.html — run the build first");
  process.exit(1);
}

let total = 0;
for (const ref of refs) {
  const size = gzipSync(readFileSync(join(dist, ref.replace(/^\//, "")))).length;
  total += size;
  console.log(`${(size / 1024).toFixed(1).padStart(8)} KB  ${ref}`);
}
const totalKB = total / 1024;
console.log(`${totalKB.toFixed(1).padStart(8)} KB  initial JS (gzip), budget ${BUDGET_KB} KB`);
if (totalKB > BUDGET_KB) {
  console.error(
    `check-bundle: initial JS ${totalKB.toFixed(1)} KB exceeds the ${BUDGET_KB} KB budget`,
  );
  process.exit(1);
}
