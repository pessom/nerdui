// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { useEffect, useRef } from "react";
import type { Job, JobFrame } from "../../api/queries";
import { openStream } from "../../lib/ws";

/**
 * Live terminal output of a job (ADR-0018), as `nerdctl pull` shows it in a terminal: the
 * stream starts with the stored screen and continues with the raw output, progress redraws
 * included. Loaded lazily with the jobs panel.
 */
export default function JobTerminal({
  jobId,
  dark,
  onJob,
}: {
  jobId: string;
  dark: boolean;
  onJob?: (job: Job) => void;
}) {
  const host = useRef<HTMLDivElement>(null);
  const onJobRef = useRef(onJob);
  useEffect(() => {
    onJobRef.current = onJob;
  }, [onJob]);

  useEffect(() => {
    if (!host.current) return;
    const t = new Terminal({
      disableStdin: true,
      convertEol: true,
      cursorInactiveStyle: "none",
      scrollback: 5000,
      fontSize: 12,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
      theme: dark
        ? { background: "#1a1b1e", foreground: "#c1c2c5" }
        : { background: "#ffffff", foreground: "#212529", cursor: "#ffffff" },
    });
    const fit = new FitAddon();
    t.loadAddon(fit);
    t.open(host.current);
    t.textarea?.setAttribute("aria-label", "Job output");
    fit.fit();
    const observer = new ResizeObserver(() => fit.fit());
    observer.observe(host.current);
    const stream = openStream<JobFrame>(`/api/v1/jobs/${encodeURIComponent(jobId)}/stream`, {
      onData: (f) => {
        if (f.reset) t.reset();
        if (f.output) t.write(f.output);
        if (f.job) onJobRef.current?.(f.job);
      },
      onError: (e) => t.write(`\r\n\x1b[31m${e.message}\x1b[0m\r\n`),
    });
    return () => {
      stream.close();
      observer.disconnect();
      t.dispose();
    };
  }, [jobId, dark]);

  return <div ref={host} style={{ height: "100%", width: "100%" }} />;
}
