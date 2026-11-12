// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon, type ISearchOptions } from "@xterm/addon-search";
import { Terminal, type ITheme } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { useEffect, useImperativeHandle, useRef, type Ref } from "react";
import { MAX_LINES } from "./logs";

export interface TerminalLine {
  seq: number;
  text: string;
}

export interface LogTerminalHandle {
  findNext: () => boolean;
  findPrevious: () => boolean;
  scrollToBottom: () => void;
}

export interface LogTerminalProps {
  /** Lines to show, oldest first; only lines with a new seq are appended. */
  lines: readonly TerminalLine[];
  /** Changing it redraws everything (e.g. another filter or timestamps toggled). */
  resetKey: string;
  /** Highlighted matches (search addon); empty = none. */
  search: { term: string; regex: boolean };
  dark: boolean;
  label: string;
  ref?: Ref<LogTerminalHandle>;
}

const light: ITheme = {
  background: "#ffffff",
  foreground: "#212529",
  cursor: "#ffffff",
  selectionBackground: "#a5d8ff",
  black: "#212529",
  red: "#c92a2a",
  green: "#2b8a3e",
  yellow: "#e67700",
  blue: "#1864ab",
  magenta: "#a61e4d",
  cyan: "#0b7285",
  white: "#868e96",
  brightBlack: "#495057",
  brightWhite: "#adb5bd",
};
const dark: ITheme = {
  background: "#1a1b1e",
  foreground: "#c1c2c5",
  cursor: "#1a1b1e",
  selectionBackground: "#364fc7",
};

const decorations: ISearchOptions["decorations"] = {
  matchBackground: "#ffe066",
  matchOverviewRuler: "#fab005",
  activeMatchBackground: "#ff922b",
  activeMatchColorOverviewRuler: "#fd7e14",
};

/**
 * Read-only xterm.js view of log lines (#14, ADR-0014): ANSI colors, fast rendering of
 * thousands of lines, search highlighting. Loaded lazily with the Logs tab.
 */
export default function LogTerminal({
  lines,
  resetKey,
  search,
  dark: isDark,
  label,
  ref,
}: LogTerminalProps) {
  const host = useRef<HTMLDivElement>(null);
  const term = useRef<Terminal>(null);
  const searcher = useRef<SearchAddon>(null);
  const written = useRef({ key: "", seq: 0 });

  useEffect(() => {
    if (!host.current) return;
    const t = new Terminal({
      disableStdin: true,
      convertEol: true,
      cursorStyle: "bar",
      cursorInactiveStyle: "none",
      scrollback: MAX_LINES,
      fontSize: 13,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
      allowProposedApi: true, // search decorations
      theme: isDark ? dark : light,
      screenReaderMode: false,
    });
    const fit = new FitAddon();
    const s = new SearchAddon();
    t.loadAddon(fit);
    t.loadAddon(s);
    t.open(host.current);
    t.textarea?.setAttribute("aria-label", label);
    fit.fit();
    const observer = new ResizeObserver(() => fit.fit());
    observer.observe(host.current);
    term.current = t;
    searcher.current = s;
    written.current = { key: "", seq: 0 };
    return () => {
      observer.disconnect();
      t.dispose();
      term.current = null;
      searcher.current = null;
    };
    // Theme and label changes are applied below without recreating the terminal.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (term.current) term.current.options.theme = isDark ? dark : light;
  }, [isDark]);

  useEffect(() => {
    const t = term.current;
    if (!t) return;
    const w = written.current;
    let todo: readonly TerminalLine[];
    if (w.key !== resetKey) {
      t.reset();
      todo = lines;
    } else {
      todo = lines.filter((l) => l.seq > w.seq);
    }
    if (todo.length > 0) t.write(todo.map((l) => l.text).join("\n") + "\n");
    written.current = { key: resetKey, seq: lines.at(-1)?.seq ?? w.seq };
  }, [lines, resetKey]);

  useEffect(() => {
    const s = searcher.current;
    if (!s) return;
    if (!search.term) {
      s.clearDecorations();
      return;
    }
    // Matches in lines that arrive later are found with findNext/findPrevious.
    s.findNext(search.term, { regex: search.regex, decorations });
  }, [search.term, search.regex]);

  useImperativeHandle(
    ref,
    () => ({
      findNext: () =>
        !!search.term &&
        !!searcher.current?.findNext(search.term, { regex: search.regex, decorations }),
      findPrevious: () =>
        !!search.term &&
        !!searcher.current?.findPrevious(search.term, { regex: search.regex, decorations }),
      scrollToBottom: () => term.current?.scrollToBottom(),
    }),
    [search.term, search.regex],
  );

  return <div ref={host} style={{ height: "100%", width: "100%" }} />;
}
