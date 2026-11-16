// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Lightweight completion for compose files from the Compose JSON schema (#26): finds where the
 * cursor is in the YAML by indentation and parent keys, resolves that place in the schema
 * ($ref, allOf/oneOf/anyOf, patternProperties, array items) and offers the keys of that level
 * or the allowed values of a key. Errors come from the server's validation.
 */

export interface SchemaNode {
  $ref?: string;
  type?: string | string[];
  description?: string;
  properties?: Record<string, SchemaNode>;
  patternProperties?: Record<string, SchemaNode>;
  additionalProperties?: boolean | SchemaNode;
  items?: SchemaNode;
  allOf?: SchemaNode[];
  oneOf?: SchemaNode[];
  anyOf?: SchemaNode[];
  enum?: unknown[];
  deprecated?: boolean;
}

export interface Schema extends SchemaNode {
  $defs?: Record<string, SchemaNode>;
  definitions?: Record<string, SchemaNode>;
}

/** Marks an array element in a path. */
export const ITEM = "[]";

function deref(root: Schema, node: SchemaNode | undefined, depth = 0): SchemaNode | undefined {
  if (!node || depth > 20) return node;
  if (node.$ref) {
    const m = /^#\/(\$defs|definitions)\/(.+)$/.exec(node.$ref);
    const target = m ? (root[m[1] as "$defs" | "definitions"] ?? {})[m[2]!] : undefined;
    return deref(root, target, depth + 1);
  }
  return node;
}

/** The node and every object-like alternative it is composed of. */
function variants(root: Schema, node: SchemaNode | undefined, depth = 0): SchemaNode[] {
  const n = deref(root, node);
  if (!n || depth > 10) return [];
  const out = [n];
  for (const sub of [...(n.allOf ?? []), ...(n.oneOf ?? []), ...(n.anyOf ?? [])]) {
    out.push(...variants(root, sub, depth + 1));
  }
  return out;
}

/** Properties of a node, merged over its composition. */
export function propertiesOf(
  root: Schema,
  node: SchemaNode | undefined,
): Record<string, SchemaNode> {
  const out: Record<string, SchemaNode> = {};
  for (const v of variants(root, node)) Object.assign(out, v.properties ?? {});
  return out;
}

function childOf(root: Schema, node: SchemaNode | undefined, key: string): SchemaNode | undefined {
  const vs = variants(root, node);
  for (const v of vs) if (v.properties?.[key]) return deref(root, v.properties[key]);
  for (const v of vs) {
    for (const [pattern, sub] of Object.entries(v.patternProperties ?? {})) {
      try {
        if (new RegExp(pattern).test(key)) return deref(root, sub);
      } catch {
        // an unsupported pattern: try the next one
      }
    }
  }
  for (const v of vs) {
    if (typeof v.additionalProperties === "object") return deref(root, v.additionalProperties);
  }
  return undefined;
}

function itemsOf(root: Schema, node: SchemaNode | undefined): SchemaNode | undefined {
  for (const v of variants(root, node)) if (v.items) return deref(root, v.items);
  return undefined;
}

/** The schema node at a path of keys (ITEM for array elements). */
export function schemaAt(root: Schema, path: string[]): SchemaNode | undefined {
  let node: SchemaNode | undefined = root;
  for (const seg of path) {
    node = seg === ITEM ? itemsOf(root, node) : childOf(root, node, seg);
    if (!node) return undefined;
  }
  return node;
}

interface Line {
  indent: number;
  dash: boolean;
  key?: string;
  /** Indentation of the key: after "- " for list items. */
  keyIndent: number;
}

const lineRe = /^(\s*)(-\s+)?(?:([^\s#:'"][^:#]*?|"[^"]*"|'[^']*')\s*:(?:\s|$))?/;

function parseLine(text: string): Line | undefined {
  if (!text.trim() || text.trimStart().startsWith("#")) return undefined;
  const m = lineRe.exec(text)!;
  const indent = m[1]!.length;
  const dash = !!m[2];
  const key = m[3]?.replace(/^["']|["']$/g, "");
  return { indent, dash, key, keyIndent: indent + (m[2]?.length ?? 0) };
}

/**
 * The path of keys enclosing a position at indentation indent on line index lineNo (0-based),
 * from the lines above it.
 */
export function pathAt(lines: string[], lineNo: number, indent: number): string[] {
  const path: string[] = [];
  let target = indent;
  for (let i = lineNo - 1; i >= 0 && target > 0; i--) {
    const l = parseLine(lines[i]!);
    if (!l) continue;
    if (l.dash && l.indent < target) {
      // Inside this list element; a key on the dash line is a sibling unless it opens a block.
      if (l.key && l.keyIndent < target) path.unshift(l.key);
      path.unshift(ITEM);
      target = l.indent;
    } else if (!l.dash && l.key && l.indent < target) {
      path.unshift(l.key);
      target = l.indent;
    }
  }
  return path;
}

/** Keys present at the same level as line lineNo, to leave them out of suggestions. */
function siblingKeys(lines: string[], lineNo: number, keyIndent: number): Set<string> {
  const keys = new Set<string>();
  for (const dir of [-1, 1]) {
    for (let i = lineNo + dir; i >= 0 && i < lines.length; i += dir) {
      const l = parseLine(lines[i]!);
      if (!l) continue;
      if (l.keyIndent < keyIndent) break;
      if (l.dash && l.indent < keyIndent) {
        // The dash line starts this list element (above) or the next one (below).
        if (dir < 0 && l.keyIndent === keyIndent && l.key) keys.add(l.key);
        break;
      }
      if (l.keyIndent === keyIndent && l.key) keys.add(l.key);
    }
  }
  return keys;
}

export interface Suggestion {
  label: string;
  apply: string;
  detail?: string;
  info?: string;
  type: "property" | "enum";
}

export interface CompletionResult {
  /** Offset in the line where the replaced word starts. */
  from: number;
  options: Suggestion[];
}

function typeLabel(root: Schema, node: SchemaNode | undefined): string | undefined {
  const types = new Set<string>();
  for (const v of variants(root, node)) {
    for (const t of [v.type].flat()) if (t) types.add(t);
  }
  return types.size ? [...types].join(" | ") : undefined;
}

function isContainer(root: Schema, node: SchemaNode | undefined): boolean {
  return variants(root, node).some((v) => {
    const t = [v.type].flat();
    return (
      (t.includes("object") || t.includes("array")) &&
      !t.includes("string") &&
      !t.includes("number")
    );
  });
}

/**
 * Suggestions at column col of line lineNo: keys at a key position, enum values (and
 * booleans) after "key: ".
 */
export function complete(
  root: Schema,
  lines: string[],
  lineNo: number,
  col: number,
): CompletionResult | undefined {
  const before = (lines[lineNo] ?? "").slice(0, col);
  const keyPos = /^(\s*)(-\s+)?([\w.-]*)$/.exec(before);
  if (keyPos) {
    const indent = keyPos[1]!.length;
    const keyIndent = indent + (keyPos[2]?.length ?? 0);
    let path = pathAt(lines, lineNo, indent);
    if (keyPos[2]) path = [...path, ITEM];
    const node = schemaAt(root, path);
    const props = propertiesOf(root, node);
    const used = siblingKeys(lines, lineNo, keyIndent);
    const options = Object.entries(props)
      .filter(([k, v]) => !used.has(k) && !deref(root, v)?.deprecated)
      .map(([k, v]): Suggestion => {
        const sub = deref(root, v);
        return {
          label: k,
          apply: isContainer(root, sub) ? `${k}:\n${" ".repeat(keyIndent + 2)}` : `${k}: `,
          detail: typeLabel(root, sub),
          info: sub?.description,
          type: "property",
        };
      })
      .sort((a, b) => a.label.localeCompare(b.label));
    return options.length ? { from: col - keyPos[3]!.length, options } : undefined;
  }
  const valuePos = /^(\s*)(-\s+)?([\w.-]+):\s+([\w.-]*)$/.exec(before);
  if (valuePos) {
    const indent = valuePos[1]!.length;
    let path = pathAt(lines, lineNo, indent);
    if (valuePos[2]) path = [...path, ITEM];
    const node = schemaAt(root, [...path, valuePos[3]!]);
    const values = new Set<string>();
    for (const v of variants(root, node)) {
      for (const e of v.enum ?? []) values.add(String(e));
      if ([v.type].flat().includes("boolean")) ["true", "false"].forEach((b) => values.add(b));
    }
    if (!values.size) return undefined;
    return {
      from: col - valuePos[4]!.length,
      options: [...values].map((v) => ({ label: v, apply: v, type: "enum" })),
    };
  }
  return undefined;
}

/**
 * The 1-based line of a validation error: the line the loader reports, else the line of a key
 * quoted in the message (e.g. "additional properties 'imagee' not allowed").
 */
export function errorLine(text: string, error: string, line?: number): number | undefined {
  if (line) return line;
  const quoted = /'([\w.-]+)'/.exec(error)?.[1];
  if (!quoted) return undefined;
  const lines = text.split("\n");
  const i = lines.findIndex((l) => parseLine(l)?.key === quoted);
  return i >= 0 ? i + 1 : undefined;
}
