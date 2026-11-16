// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Modal, SegmentedControl, Select, Stack, Text } from "@mantine/core";
import { useState } from "react";
import { formatTime, usePreferences, type Preferences } from "./context";

const refreshOptions = [
  { value: "0", label: "Off (live events only)" },
  { value: "5", label: "Every 5 seconds" },
  { value: "10", label: "Every 10 seconds" },
  { value: "30", label: "Every 30 seconds" },
  { value: "60", label: "Every minute" },
];

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <Stack gap={4}>
      <Text size="sm" fw={500}>
        {label}
      </Text>
      {children}
    </Stack>
  );
}

/** The signed-in user's interface preferences (#90); changes apply and save at once. */
export function PreferencesModal({ opened, onClose }: { opened: boolean; onClose: () => void }) {
  const { prefs, update } = usePreferences();
  // An example timestamp: an hour before the modal first renders.
  const [sample] = useState(() => new Date(Date.now() - 3_600_000).toISOString());
  return (
    <Modal opened={opened} onClose={onClose} title="Preferences">
      <Stack gap="md">
        <Field label="Theme">
          <SegmentedControl
            aria-label="Theme"
            value={prefs.theme}
            onChange={(v) => update({ theme: v as Preferences["theme"] })}
            data={[
              { value: "auto", label: "System" },
              { value: "light", label: "Light" },
              { value: "dark", label: "Dark" },
            ]}
          />
        </Field>
        <Field label="Dates">
          <SegmentedControl
            aria-label="Date format"
            value={prefs.dateFormat}
            onChange={(v) => update({ dateFormat: v as Preferences["dateFormat"] })}
            data={(["relative", "local", "iso"] as const).map((f) => ({
              value: f,
              label: formatTime(sample, f).text,
            }))}
          />
        </Field>
        <Select
          label="Auto-refresh lists"
          description="Lists follow live events; polling also catches changes made outside nerdui's view."
          data={refreshOptions}
          value={String(prefs.refreshSeconds)}
          onChange={(v) =>
            v && update({ refreshSeconds: Number(v) as Preferences["refreshSeconds"] })
          }
          allowDeselect={false}
        />
        <Text size="xs" c="dimmed">
          Saved to your account and applied in every browser you sign in from.
        </Text>
      </Stack>
    </Modal>
  );
}
