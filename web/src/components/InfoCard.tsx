// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Card, Table, Title } from "@mantine/core";
import type { ReactNode } from "react";

/** A titled card with label/value rows. */
export function InfoCard({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Card withBorder component="section" aria-label={title}>
      <Title order={4} mb="xs">
        {title}
      </Title>
      <Table>
        <Table.Tbody>{children}</Table.Tbody>
      </Table>
    </Card>
  );
}

export function Row({ label, value }: { label: string; value: ReactNode }) {
  return (
    <Table.Tr>
      <Table.Th w="45%" fw={500} c="dimmed">
        {label}
      </Table.Th>
      <Table.Td>{value}</Table.Td>
    </Table.Tr>
  );
}
