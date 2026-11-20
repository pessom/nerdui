// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  autocompletion,
  completionKeymap,
  type CompletionContext,
  type CompletionResult,
} from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { yaml } from "@codemirror/lang-yaml";
import {
  defaultHighlightStyle,
  foldGutter,
  foldKeymap,
  indentUnit,
  syntaxHighlighting,
} from "@codemirror/language";
import { lintGutter, setDiagnostics, type Diagnostic } from "@codemirror/lint";
import { highlightSelectionMatches, search, searchKeymap } from "@codemirror/search";
import { Compartment, EditorState, type Extension } from "@codemirror/state";
import { oneDark } from "@codemirror/theme-one-dark";
import { EditorView, highlightActiveLine, keymap, lineNumbers } from "@codemirror/view";
import { Paper, useComputedColorScheme } from "@mantine/core";
import { useEffect, useRef } from "react";
import { complete, type Schema } from "./composeSchema";

const baseTheme = EditorView.theme({
  "&": { fontSize: "var(--mantine-font-size-sm)", height: "60vh" },
  ".cm-scroller": { fontFamily: "var(--mantine-font-family-monospace)", overflow: "auto" },
});

const themeFor = (dark: boolean) =>
  dark ? oneDark : syntaxHighlighting(defaultHighlightStyle, { fallback: true });

/** Completion from the Compose schema (composeSchema.ts). */
function schemaCompletion(schema: Schema) {
  return (ctx: CompletionContext): CompletionResult | null => {
    const line = ctx.state.doc.lineAt(ctx.pos);
    const before = line.text.slice(0, ctx.pos - line.from);
    // Only on request or while typing a word, not on every cursor move.
    if (!ctx.explicit && !/[\w.-]$/.test(before) && !/:\s$/.test(before)) return null;
    const lines = ctx.state.doc.toString().split("\n");
    const r = complete(schema, lines, line.number - 1, ctx.pos - line.from);
    if (!r) return null;
    return {
      from: line.from + r.from,
      options: r.options.map((o) => ({
        label: o.label,
        apply: o.apply,
        detail: o.detail,
        info: o.info,
        type: o.type,
      })),
      validFor: /^[\w.-]*$/,
    };
  };
}

export interface EditorError {
  /** 1-based line; the first line when unknown. */
  line?: number;
  message: string;
}

/**
 * Editor of a stack's compose or .env file (#26, CodeMirror 6): YAML highlighting, completion
 * from the Compose schema, the server's validation error at its line, search and folding.
 * Loaded lazily with the file tabs.
 */
export default function StackFileEditor({
  value,
  onChange,
  kind,
  schema,
  error,
  label,
  readOnly = false,
}: {
  value: string;
  onChange: (value: string) => void;
  kind: "compose" | "env";
  schema?: Schema;
  error?: EditorError;
  label: string;
  /** The file cannot be changed here (the compose file of a Git stack). */
  readOnly?: boolean;
}) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView>(null);
  const theme = useRef(new Compartment());
  const completion = useRef(new Compartment());
  const changed = useRef(onChange);
  const dark = useComputedColorScheme("light") === "dark";

  useEffect(() => {
    changed.current = onChange;
  }, [onChange]);

  useEffect(() => {
    if (!host.current) return;
    const lang: Extension[] = kind === "compose" ? [yaml(), foldGutter()] : [];
    const v = new EditorView({
      parent: host.current,
      state: EditorState.create({
        doc: value,
        extensions: [
          lineNumbers(),
          history(),
          highlightActiveLine(),
          highlightSelectionMatches(),
          search({ top: true }),
          indentUnit.of("  "),
          EditorState.tabSize.of(2),
          EditorState.readOnly.of(readOnly),
          ...lang,
          lintGutter(),
          completion.current.of([]),
          keymap.of([
            ...completionKeymap,
            ...defaultKeymap,
            ...historyKeymap,
            ...searchKeymap,
            ...foldKeymap,
            indentWithTab,
          ]),
          baseTheme,
          theme.current.of(themeFor(dark)),
          EditorView.contentAttributes.of({ "aria-label": label }),
          EditorView.updateListener.of((u) => {
            if (u.docChanged) changed.current(u.state.doc.toString());
          }),
        ],
      }),
    });
    view.current = v;
    return () => {
      v.destroy();
      view.current = null;
    };
    // The view is created once per file; value changes go through the effect below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kind, label]);

  // A value from outside (reload, revert) replaces the document.
  useEffect(() => {
    const v = view.current;
    if (v && v.state.doc.toString() !== value) {
      v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: value } });
    }
  }, [value]);

  useEffect(() => {
    view.current?.dispatch({ effects: theme.current.reconfigure(themeFor(dark)) });
  }, [dark]);

  useEffect(() => {
    view.current?.dispatch({
      effects: completion.current.reconfigure(
        schema && kind === "compose"
          ? autocompletion({ override: [schemaCompletion(schema)] })
          : [],
      ),
    });
  }, [schema, kind]);

  useEffect(() => {
    const v = view.current;
    if (!v) return;
    const diagnostics: Diagnostic[] = [];
    if (error) {
      const n = Math.min(Math.max(error.line ?? 1, 1), v.state.doc.lines);
      const line = v.state.doc.line(n);
      diagnostics.push({ from: line.from, to: line.to, severity: "error", message: error.message });
    }
    v.dispatch(setDiagnostics(v.state, diagnostics));
  }, [error, value]);

  return <Paper withBorder ref={host} style={{ overflow: "hidden" }} />;
}
