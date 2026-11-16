// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useState } from "react";
import type { components } from "../../api/client";
import { openStream } from "../../lib/ws";

export type TopStats = components["schemas"]["TopStats"];

/** Running containers using most CPU and memory, refreshed every 5 s while shown (#63). */
export function useTopStats(env: string): TopStats | undefined {
  const [top, setTop] = useState<{ env: string; data: TopStats }>();
  useEffect(() => {
    const stream = openStream<TopStats>(
      `/api/v1/environments/${encodeURIComponent(env)}/stats/stream`,
      { onData: (data) => setTop({ env, data }) },
    );
    return () => stream.close();
  }, [env]);
  return top?.env === env ? top.data : undefined;
}
