// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { useMantineColorScheme } from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, type ReactNode } from "react";
import { api, unwrap } from "../../api/client";
import { PreferencesContext, defaultPreferences, type Preferences } from "./context";

const prefsKey = ["preferences"] as const;

/**
 * Loads the user's preferences (#90) and applies them: the color scheme, and polling of the
 * environment's queries at the chosen interval (in addition to live events).
 */
export function PreferencesProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const { setColorScheme } = useMantineColorScheme();
  const query = useQuery({
    queryKey: prefsKey,
    queryFn: () => unwrap(api.GET("/api/v1/auth/preferences")),
    staleTime: Infinity,
  });
  const prefs = query.data ?? defaultPreferences;

  const save = useMutation({
    mutationFn: (p: Preferences) => unwrap(api.PUT("/api/v1/auth/preferences", { body: p })),
    onMutate: (p) => {
      const before = queryClient.getQueryData<Preferences>(prefsKey);
      queryClient.setQueryData(prefsKey, p);
      return { before };
    },
    onError: (e, _p, ctx) => {
      queryClient.setQueryData(prefsKey, ctx?.before);
      notifications.show({ color: "red", title: "Cannot save preferences", message: e.message });
    },
  });
  const { mutate } = save;
  const update = useCallback(
    (change: Partial<Preferences>) => {
      const current = queryClient.getQueryData<Preferences>(prefsKey) ?? defaultPreferences;
      mutate({ ...current, ...change });
    },
    [queryClient, mutate],
  );

  // The saved theme wins over the browser's last choice once loaded.
  const theme = query.data?.theme;
  useEffect(() => {
    if (theme) setColorScheme(theme);
  }, [theme, setColorScheme]);

  const refresh = prefs.refreshSeconds;
  useEffect(() => {
    queryClient.setQueryDefaults(["env"], { refetchInterval: refresh ? refresh * 1000 : false });
  }, [queryClient, refresh]);

  const value = useMemo(() => ({ prefs, update }), [prefs, update]);
  return <PreferencesContext.Provider value={value}>{children}</PreferencesContext.Provider>;
}
