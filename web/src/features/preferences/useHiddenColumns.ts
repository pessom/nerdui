// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { useMemo } from "react";
import { usePreferences } from "./context";

/** The user's hidden columns of a table (#90) as TanStack column visibility. */
export function useHiddenColumns(table: string) {
  const { prefs, update } = usePreferences();
  const hidden = prefs.hiddenColumns?.[table];
  const columnVisibility = useMemo(
    () => Object.fromEntries((hidden ?? []).map((id) => [id, false])),
    [hidden],
  );
  const toggle = (id: string, visible: boolean) => {
    const rest = (hidden ?? []).filter((h) => h !== id);
    update({
      hiddenColumns: { ...prefs.hiddenColumns, [table]: visible ? rest : [...rest, id] },
    });
  };
  return { hidden: hidden ?? [], columnVisibility, toggle };
}
