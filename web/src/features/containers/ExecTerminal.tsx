// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { useEffect, useRef } from "react";
import { streamURL } from "../../lib/ws";

export type ExecState =
  | { status: "connecting" }
  | { status: "open" }
  | { status: "exited"; exitCode: number }
  | { status: "error"; message: string }
  | { status: "closed" };

/**
 * Interactive terminal over WebSocket (#15, ADR-0022): keystrokes go out as binary frames,
 * output comes back as binary frames, size changes as {"type":"resize"}. Unmounting closes the
 * WebSocket, which kills the process in the container. Loaded lazily with the Terminal tab.
 */
export default function ExecTerminal({
  path,
  dark,
  onState,
}: {
  /** API path with query (namespace, cmd, user, workdir); cols and rows are added. */
  path: string;
  dark: boolean;
  onState: (s: ExecState) => void;
}) {
  const host = useRef<HTMLDivElement>(null);
  const onStateRef = useRef(onState);
  useEffect(() => {
    onStateRef.current = onState;
  }, [onState]);

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    const t = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
      theme: dark
        ? { background: "#1a1b1e", foreground: "#c1c2c5" }
        : { background: "#ffffff", foreground: "#212529", cursor: "#212529" },
    });
    const fit = new FitAddon();
    t.loadAddon(fit);
    t.open(el);
    t.textarea?.setAttribute("aria-label", "Terminal input");
    fit.fit();
    const sep = path.includes("?") ? "&" : "?";
    const ws = new WebSocket(streamURL(`${path}${sep}cols=${t.cols}&rows=${t.rows}`));
    ws.binaryType = "arraybuffer";
    const encoder = new TextEncoder();
    let ended = false;
    onStateRef.current({ status: "connecting" });
    ws.onopen = () => {
      onStateRef.current({ status: "open" });
      t.focus();
    };
    ws.onmessage = (ev: MessageEvent<ArrayBuffer | string>) => {
      if (typeof ev.data !== "string") {
        t.write(new Uint8Array(ev.data));
        return;
      }
      const msg = JSON.parse(ev.data) as {
        type: string;
        data?: { exitCode?: number };
        error?: { message: string };
      };
      if (msg.type === "data" && msg.data?.exitCode !== undefined) {
        ended = true;
        t.write(`\r\n\x1b[2m[process exited with code ${msg.data.exitCode}]\x1b[0m\r\n`);
        onStateRef.current({ status: "exited", exitCode: msg.data.exitCode });
      } else if (msg.type === "error") {
        ended = true;
        t.write(`\r\n\x1b[31m${msg.error?.message ?? "error"}\x1b[0m\r\n`);
        onStateRef.current({ status: "error", message: msg.error?.message ?? "error" });
      }
    };
    ws.onclose = () => {
      if (!ended) onStateRef.current({ status: "closed" });
    };
    const input = t.onData((d) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(encoder.encode(d));
    });
    const sendSize = () => {
      if (ws.readyState === WebSocket.OPEN)
        ws.send(JSON.stringify({ type: "resize", cols: t.cols, rows: t.rows }));
    };
    const resized = t.onResize(sendSize);
    const observer = new ResizeObserver(() => fit.fit());
    observer.observe(el);
    return () => {
      observer.disconnect();
      input.dispose();
      resized.dispose();
      ws.onclose = null;
      ws.close();
      t.dispose();
    };
  }, [path, dark]);

  return <div ref={host} style={{ height: "100%", width: "100%" }} />;
}
