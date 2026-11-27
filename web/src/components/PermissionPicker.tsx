// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Checkbox, Group, Table, Text } from "@mantine/core";
import type { Permission } from "../lib/permissions";

/** Permissions by area, for API keys (#80) and roles (#78). */
const permissionAreas: { area: string; permissions: Permission[] }[] = [
  {
    area: "Containers",
    permissions: ["containers:read", "containers:operate", "containers:write", "containers:exec"],
  },
  { area: "Stacks", permissions: ["stacks:read", "stacks:deploy", "stacks:write"] },
  { area: "Images", permissions: ["images:read", "images:write"] },
  { area: "Volumes", permissions: ["volumes:read", "volumes:write"] },
  { area: "Networks", permissions: ["networks:read", "networks:write"] },
  { area: "Namespaces", permissions: ["namespaces:read", "namespaces:write"] },
  { area: "System", permissions: ["system:read", "system:prune", "jobs:read"] },
  { area: "Registries", permissions: ["registries:read", "registries:write"] },
  { area: "Settings", permissions: ["settings:read", "settings:write", "audit:read"] },
  { area: "Users", permissions: ["users:read", "users:write"] },
];

/** Checkboxes of permissions by area; disabled ones (e.g. beyond the caller's) cannot be ticked. */
export function PermissionPicker({
  value,
  onChange,
  disabled,
  readOnly,
}: {
  value: Permission[];
  onChange?: (v: Permission[]) => void;
  disabled?: (p: Permission) => boolean;
  readOnly?: boolean;
}) {
  return (
    <Checkbox.Group
      aria-label="Permissions"
      value={value}
      onChange={(v) => onChange?.(v as Permission[])}
    >
      <Table withRowBorders={false} verticalSpacing={4}>
        <Table.Tbody>
          {permissionAreas.map(({ area, permissions: list }) => (
            <Table.Tr key={area}>
              <Table.Td w={110}>
                <Text size="sm">{area}</Text>
              </Table.Td>
              <Table.Td>
                <Group gap="md">
                  {list.map((p) => (
                    <Checkbox
                      key={p}
                      value={p}
                      label={p}
                      size="xs"
                      disabled={readOnly || disabled?.(p)}
                    />
                  ))}
                </Group>
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Checkbox.Group>
  );
}
