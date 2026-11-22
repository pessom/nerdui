// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Anchor, Badge, Loader, Modal, ScrollArea, Table, Text } from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { api, unwrap } from "../../api/client";
import { Time } from "../../components/Time";
import { useJobsPanel } from "../jobs/JobsContext";
import { statusColor, type Schedule } from "./kinds";

/** The last runs of a schedule (#82). */
export function ScheduleRuns({ schedule, onClose }: { schedule?: Schedule; onClose: () => void }) {
  const jobs = useJobsPanel();
  const runs = useQuery({
    queryKey: ["schedules", schedule?.id, "runs"],
    queryFn: () =>
      unwrap(
        api.GET("/api/v1/schedules/{id}/runs", { params: { path: { id: schedule?.id ?? 0 } } }),
      ),
    enabled: !!schedule,
    refetchInterval: 5000,
  });
  return (
    <Modal
      opened={!!schedule}
      onClose={onClose}
      size="xl"
      title={schedule && `Runs of ${schedule.name}`}
    >
      {runs.isPending && <Loader size="sm" aria-label="Loading" />}
      {runs.isError && <Alert color="red">{runs.error.message}</Alert>}
      {runs.data && runs.data.runs.length === 0 && (
        <Text size="sm" c="dimmed">
          No runs yet.
        </Text>
      )}
      {runs.data && runs.data.runs.length > 0 && (
        <ScrollArea.Autosize mah="60vh">
          <Table striped verticalSpacing={4} aria-label="Schedule runs">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Started</Table.Th>
                <Table.Th>Trigger</Table.Th>
                <Table.Th>Status</Table.Th>
                <Table.Th>Message</Table.Th>
                <Table.Th>Job</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {runs.data.runs.map((r) => (
                <Table.Tr key={r.id}>
                  <Table.Td>
                    <Time value={r.startedAt} />
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{r.trigger}</Text>
                  </Table.Td>
                  <Table.Td>
                    <Badge variant="light" color={statusColor[r.status] ?? "gray"}>
                      {r.status}
                    </Badge>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm" style={{ wordBreak: "break-word" }}>
                      {r.message}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    {r.jobId && (
                      <Anchor component="button" size="sm" onClick={() => jobs.open(r.jobId)}>
                        Output
                      </Anchor>
                    )}
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </ScrollArea.Autosize>
      )}
    </Modal>
  );
}
