// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Badge, Code, Stack, Table, Text, Title } from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { autoUpdatesQuery, type AutoUpdateEntry } from "../../api/queries";
import { formatTime, usePreferences } from "../preferences/context";

const statusColor: Record<AutoUpdateEntry["status"], string> = {
  updated: "teal",
  failed: "red",
  skipped: "gray",
};

/** Automatic updates of a namespace's opted-in containers and stacks (#54, ADR-0032). */
export function AutoUpdateLog({ env, namespace }: { env: string; namespace: string }) {
  const { prefs } = usePreferences();
  const log = useQuery(autoUpdatesQuery(env, namespace));
  return (
    <Stack gap="xs" component="section" aria-label="Automatic updates">
      <Title order={4}>Automatic updates</Title>
      <Text size="sm" c="dimmed">
        Containers and compose services labeled <Code>nerdui.auto-update=true</Code> are pulled and
        recreated after a scheduled check finds a newer image for their tag. Turn it on with
        &quot;Update automatically&quot; when creating or editing a container, or with the label in
        a compose file. Containers with settings recreating would lose are skipped.
      </Text>
      {log.isError && (
        <Alert color="red" title="Cannot read the log">
          {log.error.message}
        </Alert>
      )}
      {log.data && log.data.entries.length === 0 && (
        <Text size="sm" c="dimmed">
          No automatic updates in namespace {namespace} yet.
        </Text>
      )}
      {log.data && log.data.entries.length > 0 && (
        <Table.ScrollContainer minWidth={560}>
          <Table striped verticalSpacing={4} fz="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>When</Table.Th>
                <Table.Th>Container or stack</Table.Th>
                <Table.Th>Images</Table.Th>
                <Table.Th>Result</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {log.data.entries.map((e) => (
                <Table.Tr key={e.id}>
                  <Table.Td style={{ whiteSpace: "nowrap" }}>
                    {formatTime(e.startedAt, prefs.dateFormat).text}
                  </Table.Td>
                  <Table.Td>
                    {e.kind === "stack" ? "Stack " : ""}
                    {e.name}
                  </Table.Td>
                  <Table.Td style={{ wordBreak: "break-all" }}>{e.refs.join(", ")}</Table.Td>
                  <Table.Td>
                    <Badge size="sm" variant="light" color={statusColor[e.status]}>
                      {e.status}
                    </Badge>
                    {e.message && (
                      <Text size="xs" c="dimmed">
                        {e.message}
                      </Text>
                    )}
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      )}
    </Stack>
  );
}
