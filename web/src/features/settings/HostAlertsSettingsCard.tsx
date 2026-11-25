// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Button,
  Group,
  NumberInput,
  Paper,
  Stack,
  Switch,
  Table,
  Text,
  Title,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap, type components } from "../../api/client";

type Alerts = components["schemas"]["HostAlerts"];
type Metric = keyof Alerts;

const hostAlertsKey = ["settings", "host-alerts"] as const;

const rows: { metric: Metric; label: string; minutes: boolean }[] = [
  { metric: "cpu", label: "CPU", minutes: true },
  { metric: "memory", label: "Memory", minutes: true },
  { metric: "disk", label: "Each disk", minutes: false },
];

/** Thresholds of host usage that send a host.alert notification (#64). */
export function HostAlertsSettingsCard() {
  const alerts = useQuery({
    queryKey: hostAlertsKey,
    queryFn: () => unwrap(api.GET("/api/v1/settings/host-alerts")),
  });
  return (
    <Paper withBorder p="md" component="section" aria-label="Host alerts">
      <Stack gap="sm">
        <Title order={3}>Host alerts</Title>
        <Text size="sm" c="dimmed">
          Notify when the host&apos;s usage stays at or above a threshold, and again when it falls
          back. Channels receive them with the <b>Host usage above a threshold</b> event.
        </Text>
        {alerts.isError && <Alert color="red">{alerts.error.message}</Alert>}
        {alerts.data && <Form initial={alerts.data} />}
      </Stack>
    </Paper>
  );
}

function Form({ initial }: { initial: Alerts }) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState(initial);
  const save = useMutation({
    mutationFn: () => unwrap(api.PUT("/api/v1/settings/host-alerts", { body: form })),
    onSuccess: (data) => {
      queryClient.setQueryData(hostAlertsKey, data);
      notifications.show({ message: "Host alerts saved" });
    },
    onError: (e) => notifications.show({ color: "red", title: "Cannot save", message: e.message }),
  });
  const set = (m: Metric, patch: Partial<Alerts[Metric]>) =>
    setForm({ ...form, [m]: { ...form[m], ...patch } });
  return (
    <Stack gap="sm">
      <Table withRowBorders={false} verticalSpacing={4}>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Alert</Table.Th>
            <Table.Th>At or above</Table.Th>
            <Table.Th>For</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {rows.map(({ metric, label, minutes }) => (
            <Table.Tr key={metric}>
              <Table.Td>
                <Switch
                  label={label}
                  checked={form[metric].enabled}
                  onChange={(e) => set(metric, { enabled: e.currentTarget.checked })}
                />
              </Table.Td>
              <Table.Td>
                <NumberInput
                  aria-label={`${label} threshold`}
                  w={110}
                  min={1}
                  max={100}
                  suffix=" %"
                  value={form[metric].percent}
                  onChange={(v) => set(metric, { percent: Number(v) || 0 })}
                />
              </Table.Td>
              <Table.Td>
                {minutes ? (
                  <NumberInput
                    aria-label={`${label} minutes`}
                    w={110}
                    min={0}
                    max={1440}
                    suffix=" min"
                    value={form[metric].minutes}
                    onChange={(v) => set(metric, { minutes: Number(v) || 0 })}
                  />
                ) : (
                  <Text size="sm" c="dimmed">
                    at once
                  </Text>
                )}
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
      <Group justify="flex-end">
        <Button loading={save.isPending} onClick={() => save.mutate()}>
          Save
        </Button>
      </Group>
    </Stack>
  );
}
