// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { createContext, useContext } from "react";
import type { components } from "../../api/client";
import { timeAgo } from "../../lib/time";

export type Preferences = components["schemas"]["Preferences"];

export const defaultPreferences: Preferences = {
  theme: "auto",
  dateFormat: "relative",
  refreshSeconds: 0,
  hiddenColumns: {},
};

export interface PreferencesValue {
  prefs: Preferences;
  /** Saves a change for the user (optimistic). */
  update: (change: Partial<Preferences>) => void;
}

export const PreferencesContext = createContext<PreferencesValue>({
  prefs: defaultPreferences,
  update: () => {},
});

/** The signed-in user's interface preferences (#90). */
export function usePreferences(): PreferencesValue {
  return useContext(PreferencesContext);
}

const pad = (n: number) => String(n).padStart(2, "0");

/**
 * A timestamp in a date format: relative ("5 minutes ago"), local (the browser's locale) or
 * iso (YYYY-MM-DD HH:MM:SS, local time). The title shows the other forms.
 */
export function formatTime(
  iso: string,
  format: Preferences["dateFormat"],
  now: number = Date.now(),
): { text: string; title: string } {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return { text: iso, title: iso };
  const local = d.toLocaleString();
  const isoLocal = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
  const rel = timeAgo(iso, now);
  switch (format) {
    case "local":
      return { text: local, title: `${rel} · ${iso}` };
    case "iso":
      return { text: isoLocal, title: `${rel} · ${iso}` };
    default:
      return { text: rel, title: `${local} · ${iso}` };
  }
}
