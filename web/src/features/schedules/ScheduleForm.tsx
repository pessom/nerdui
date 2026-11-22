// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Button,
  Checkbox,
  Group,
  Modal,
  NumberInput,
  PasswordInput,
  Select,
  SimpleGrid,
  Stack,
  Switch,
  Text,
  TextInput,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "../../api/client";
import { environmentsQuery } from "../../api/queries";
import {
  describeCron,
  kindLabels,
  needsEnvironment,
  needsNamespace,
  targetKinds,
  type Schedule,
  type ScheduleInput,
  type ScheduleKind,
  schedulesKey,
} from "./kinds";

const cronExamples = [
  { value: "0 3 * * *", label: "Every day at 03:00" },
  { value: "0 */6 * * *", label: "Every 6 hours" },
  { value: "0 4 * * 0", label: "Sundays at 04:00" },
  { value: "*/15 * * * *", label: "Every 15 minutes" },
];

function initial(s?: Schedule): ScheduleInput {
  return s
    ? {
        name: s.name,
        kind: s.kind,
        environment: s.environment ?? "",
        namespace: s.namespace ?? "",
        target: s.target ?? "",
        options: { ...s.options },
        password: "",
        cron: s.cron,
        enabled: s.enabled,
      }
    : {
        name: "",
        kind: "prune",
        environment: "local",
        namespace: "default",
        target: "",
        options: { images: true },
        password: "",
        cron: "0 3 * * *",
        enabled: true,
      };
}

/** Creates or edits a schedule (#82). */
export function ScheduleForm({
  schedule,
  opened,
  onClose,
}: {
  schedule?: Schedule;
  opened: boolean;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const envs = useQuery(environmentsQuery);
  const [form, setForm] = useState<ScheduleInput>(() => initial(schedule));
  const set = (patch: Partial<ScheduleInput>) => setForm((f) => ({ ...f, ...patch }));
  const setOption = (patch: Partial<NonNullable<ScheduleInput["options"]>>) =>
    setForm((f) => ({ ...f, options: { ...f.options, ...patch } }));
  const save = useMutation({
    mutationFn: () =>
      schedule
        ? unwrap(
            api.PUT("/api/v1/schedules/{id}", {
              params: { path: { id: schedule.id } },
              body: form,
            }),
          )
        : unwrap(api.POST("/api/v1/schedules", { body: form })),
    onSuccess: (s) => {
      void queryClient.invalidateQueries({ queryKey: schedulesKey });
      notifications.show({ color: "teal", message: `Schedule ${s.name} saved` });
      onClose();
    },
  });
  const kind = form.kind;
  const o = form.options ?? {};
  const cronText = describeCron(form.cron);
  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  return (
    <Modal
      opened={opened}
      onClose={onClose}
      size="lg"
      title={schedule ? `Edit ${schedule.name}` : "New schedule"}
    >
      <form onSubmit={onSubmit}>
        <Stack>
          {save.error && <Alert color="red">{save.error.message}</Alert>}
          <TextInput
            label="Name"
            required
            value={form.name}
            onChange={(e) => set({ name: e.currentTarget.value })}
          />
          <Select
            label="Task"
            data={Object.entries(kindLabels).map(([value, label]) => ({ value, label }))}
            value={kind}
            onChange={(v) => v && set({ kind: v as ScheduleKind })}
            allowDeselect={false}
          />
          {needsEnvironment(kind) && (
            <SimpleGrid cols={needsNamespace(kind) ? 2 : 1}>
              <Select
                label="Environment"
                data={(envs.data ?? []).map((e) => ({ value: e.id, label: e.name }))}
                value={form.environment}
                onChange={(v) => v && set({ environment: v })}
                allowDeselect={false}
              />
              {needsNamespace(kind) && (
                <TextInput
                  label="Namespace"
                  required
                  value={form.namespace}
                  onChange={(e) => set({ namespace: e.currentTarget.value })}
                />
              )}
            </SimpleGrid>
          )}
          {targetKinds.includes(kind) && (
            <TextInput
              label={kind.startsWith("stack") ? "Stack" : "Container name"}
              required
              value={form.target}
              onChange={(e) => set({ target: e.currentTarget.value })}
            />
          )}
          {kind === "prune" && (
            <Stack gap={6}>
              <Text size="sm" fw={500}>
                Remove
              </Text>
              <Checkbox
                label="Stopped containers"
                checked={!!o.containers}
                onChange={(e) => setOption({ containers: e.currentTarget.checked })}
              />
              <Checkbox
                label="Dangling images"
                checked={!!o.images}
                onChange={(e) => setOption({ images: e.currentTarget.checked })}
              />
              <Checkbox
                ml="lg"
                label="All images unused by containers"
                disabled={!o.images}
                checked={!!o.allImages}
                onChange={(e) => setOption({ allImages: e.currentTarget.checked })}
              />
              <Checkbox
                label="Unused networks"
                checked={!!o.networks}
                onChange={(e) => setOption({ networks: e.currentTarget.checked })}
              />
              <Checkbox
                label="Build cache"
                checked={!!o.buildCache}
                onChange={(e) => setOption({ buildCache: e.currentTarget.checked })}
              />
            </Stack>
          )}
          {kind === "backup" && (
            <>
              <SimpleGrid cols={2}>
                <TextInput
                  label="Directory on the host"
                  placeholder="<data-dir>/backups"
                  value={o.directory ?? ""}
                  onChange={(e) => setOption({ directory: e.currentTarget.value })}
                />
                <NumberInput
                  label="Keep"
                  description="Newest archives kept"
                  min={1}
                  max={1000}
                  placeholder="7"
                  value={o.keep ?? ""}
                  onChange={(v) => setOption({ keep: v === "" ? undefined : Number(v) })}
                />
              </SimpleGrid>
              <PasswordInput
                label="Backup password"
                description={
                  schedule?.hasPassword
                    ? "Leave empty to keep the stored password"
                    : "At least 12 characters; needed to restore"
                }
                required={!schedule?.hasPassword}
                autoComplete="new-password"
                value={form.password}
                onChange={(e) => set({ password: e.currentTarget.value })}
              />
            </>
          )}
          <Group align="flex-end" grow>
            <TextInput
              label="Cron"
              description={cronText ?? "Minute, hour, day, month, weekday; or @daily, @every 6h"}
              error={form.cron.trim() && !cronText ? "Not a valid cron expression" : undefined}
              ff="monospace"
              required
              value={form.cron}
              onChange={(e) => set({ cron: e.currentTarget.value })}
            />
            <Select
              label="Examples"
              placeholder="Pick one"
              data={cronExamples}
              value={null}
              onChange={(v) => v && set({ cron: v })}
            />
          </Group>
          <Text size="xs" c="dimmed">
            Times are in the server&apos;s time zone. A run missed while nerdui was stopped runs
            once after it starts.
          </Text>
          <Switch
            label="Enabled"
            checked={form.enabled}
            onChange={(e) => set({ enabled: e.currentTarget.checked })}
          />
          <Group justify="flex-end">
            <Button variant="default" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={save.isPending}>
              Save
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}
