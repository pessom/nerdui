// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { ActionIcon, Menu, useMantineColorScheme, type MantineColorScheme } from "@mantine/core";
import { IconDeviceDesktop, IconMoon, IconSun } from "@tabler/icons-react";

const options: { value: MantineColorScheme; label: string; icon: typeof IconSun }[] = [
  { value: "light", label: "Light", icon: IconSun },
  { value: "dark", label: "Dark", icon: IconMoon },
  { value: "auto", label: "System", icon: IconDeviceDesktop },
];

/** Color scheme switch; Mantine persists the choice in localStorage (#90). */
export function ThemeToggle() {
  const { colorScheme, setColorScheme } = useMantineColorScheme();
  const Current = options.find((o) => o.value === colorScheme)?.icon ?? IconDeviceDesktop;
  return (
    <Menu position="bottom-end" withArrow>
      <Menu.Target>
        <ActionIcon variant="default" size="lg" aria-label="Color scheme">
          <Current size={18} stroke={1.5} />
        </ActionIcon>
      </Menu.Target>
      <Menu.Dropdown>
        {options.map(({ value, label, icon: Icon }) => (
          <Menu.Item
            key={value}
            leftSection={<Icon size={16} stroke={1.5} />}
            onClick={() => setColorScheme(value)}
            fw={value === colorScheme ? 600 : undefined}
          >
            {label}
          </Menu.Item>
        ))}
      </Menu.Dropdown>
    </Menu>
  );
}
