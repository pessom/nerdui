// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { yaml } from "@codemirror/lang-yaml";
import { defaultHighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { MergeView } from "@codemirror/merge";
import { EditorState } from "@codemirror/state";
import { oneDark } from "@codemirror/theme-one-dark";
import { EditorView } from "@codemirror/view";
import { Paper, useComputedColorScheme } from "@mantine/core";
import { useEffect, useRef } from "react";

const theme = EditorView.theme({
  "&": { fontSize: "var(--mantine-font-size-xs)" },
  ".cm-scroller": { fontFamily: "var(--mantine-font-family-monospace)" },
  ".cm-mergeView": { maxHeight: "60vh", overflow: "auto" },
});

/** Side-by-side diff of the saved file and the edited one (#26), read-only. */
export default function StackFileDiff({
  original,
  modified,
  yamlSyntax,
}: {
  original: string;
  modified: string;
  yamlSyntax: boolean;
}) {
  const host = useRef<HTMLDivElement>(null);
  const dark = useComputedColorScheme("light") === "dark";
  useEffect(() => {
    if (!host.current) return;
    const ext = [
      EditorState.readOnly.of(true),
      EditorView.editable.of(false),
      theme,
      dark ? oneDark : syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
      ...(yamlSyntax ? [yaml()] : []),
    ];
    const m = new MergeView({
      a: { doc: original, extensions: ext },
      b: { doc: modified, extensions: ext },
      parent: host.current,
      collapseUnchanged: { margin: 3, minSize: 6 },
    });
    return () => m.destroy();
  }, [original, modified, yamlSyntax, dark]);
  return <Paper withBorder ref={host} aria-label="Changes" />;
}
