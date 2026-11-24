// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { queryOptions } from "@tanstack/react-query";
import { api, unwrap } from "../../api/client";
import type { components } from "../../api/schema";

export type BackupTarget = components["schemas"]["BackupTarget"];
export type BackupTargetInput = components["schemas"]["BackupTargetInput"];
export type VolumeBackup = components["schemas"]["VolumeBackup"];

/** Where volume backups go (#59, ADR-0050). */
export const backupTargetsQuery = queryOptions({
  queryKey: ["backup-targets"] as const,
  queryFn: () => unwrap(api.GET("/api/v1/backup-targets")),
});

export const volumeBackupsKey = (env: string, ns: string, volume: string, target?: number) =>
  ["volume-backups", env, ns, volume, target] as const;

export const volumeBackupsQuery = (env: string, ns: string, volume: string, target?: number) =>
  queryOptions({
    queryKey: volumeBackupsKey(env, ns, volume, target),
    queryFn: () =>
      unwrap(
        api.GET("/api/v1/environments/{env}/volumes/{name}/backups", {
          params: { path: { env, name: volume }, query: { namespace: ns, target: target ?? 0 } },
        }),
      ),
    enabled: target != null,
  });

/** Where a target writes, in a few words. */
export function describeTarget(t: BackupTarget): string {
  if (t.kind === "local") return t.directory ?? "";
  const s = t.s3;
  return s ? `s3://${s.bucket}${s.prefix ? `/${s.prefix}` : ""} at ${s.endpoint}` : "";
}
