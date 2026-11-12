// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { defaultKeymap } from "@codemirror/commands";
import { json } from "@codemirror/lang-json";
import {
  defaultHighlightStyle,
  foldAll,
  foldGutter,
  foldKeymap,
  syntaxHighlighting,
  unfoldAll,
} from "@codemirror/language";
import {
  highlightSelectionMatches,
  openSearchPanel,
  search,
  searchKeymap,
} from "@codemirror/search";
import { Compartment, EditorState } from "@codemirror/state";
import { oneDark } from "@codemirror/theme-one-dark";
import { EditorView, keymap, lineNumbers } from "@codemirror/view";
import {
  ActionIcon,
  CopyButton,
  Group,
  Paper,
  Tooltip,
  useComputedColorScheme,
} from "@mantine/core";
import { IconCheck, IconCopy, IconFold, IconFoldDown, IconSearch } from "@tabler/icons-react";
import { useEffect, useRef } from "react";

const baseTheme = EditorView.theme({
  "&": { fontSize: "var(--mantine-font-size-sm)", maxHeight: "70vh" },
  ".cm-scroller": { fontFamily: "var(--mantine-font-family-monospace)", overflow: "auto" },
});

const themeFor = (dark: boolean) =>
  dark ? oneDark : syntaxHighlighting(defaultHighlightStyle, { fallback: true });

/**
 * Read-only JSON with highlighting, folding and search (CodeMirror 6, #13). Loaded lazily:
 * the editor is only needed on the Inspect tab.
 */
export default function JsonViewer({ value, label }: { value: string; label: string }) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView>(null);
  const theme = useRef(new Compartment());
  const dark = useComputedColorScheme("light") === "dark";

  useEffect(() => {
    if (!host.current) return;
    const v = new EditorView({
      parent: host.current,
      state: EditorState.create({
        doc: value,
        extensions: [
          EditorState.readOnly.of(true),
          EditorView.contentAttributes.of({ "aria-label": label }),
          lineNumbers(),
          foldGutter(),
          json(),
          search({ top: true }),
          highlightSelectionMatches(),
          keymap.of([...searchKeymap, ...foldKeymap, ...defaultKeymap]),
          baseTheme,
          theme.current.of(themeFor(dark)),
        ],
      }),
    });
    view.current = v;
    return () => {
      v.destroy();
      view.current = null;
    };
    // The document is replaced below; recreating the editor would lose folds and search.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [label]);

  useEffect(() => {
    const v = view.current;
    if (!v || v.state.doc.toString() === value) return;
    v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: value } });
  }, [value]);

  useEffect(() => {
    view.current?.dispatch({
      effects: theme.current.reconfigure(themeFor(dark)),
    });
  }, [dark]);

  const run = (cmd: (v: EditorView) => boolean) => {
    const v = view.current;
    if (v) {
      cmd(v);
      v.focus();
    }
  };

  return (
    <Paper withBorder>
      <Group gap={4} p={4} justify="flex-end" role="toolbar" aria-label="JSON tools">
        <Tooltip label="Search (Ctrl+F)">
          <ActionIcon
            variant="subtle"
            color="gray"
            aria-label="Search"
            onClick={() => run(openSearchPanel)}
          >
            <IconSearch size={16} />
          </ActionIcon>
        </Tooltip>
        <Tooltip label="Fold all">
          <ActionIcon
            variant="subtle"
            color="gray"
            aria-label="Fold all"
            onClick={() => run(foldAll)}
          >
            <IconFold size={16} />
          </ActionIcon>
        </Tooltip>
        <Tooltip label="Unfold all">
          <ActionIcon
            variant="subtle"
            color="gray"
            aria-label="Unfold all"
            onClick={() => run(unfoldAll)}
          >
            <IconFoldDown size={16} />
          </ActionIcon>
        </Tooltip>
        <CopyButton value={value}>
          {({ copied, copy }) => (
            <Tooltip label={copied ? "Copied" : "Copy JSON"}>
              <ActionIcon
                variant="subtle"
                color={copied ? "teal" : "gray"}
                aria-label="Copy JSON"
                onClick={copy}
              >
                {copied ? <IconCheck size={16} /> : <IconCopy size={16} />}
              </ActionIcon>
            </Tooltip>
          )}
        </CopyButton>
      </Group>
      <div ref={host} />
    </Paper>
  );
}
