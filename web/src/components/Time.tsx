// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Text, type TextProps } from "@mantine/core";
import { formatTime, usePreferences } from "../features/preferences/context";

/** A timestamp in the user's date format (#90), with the other forms in the title. */
export function Time({ value, ...props }: { value: string } & TextProps) {
  const { prefs } = usePreferences();
  const { text, title } = formatTime(value, prefs.dateFormat);
  return (
    <Text span size="sm" title={title} {...props}>
      {text}
    </Text>
  );
}
