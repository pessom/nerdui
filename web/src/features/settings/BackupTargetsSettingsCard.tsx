// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Checkbox,
  Group,
  Loader,
  Modal,
  Paper,
  PasswordInput,
  SegmentedControl,
  SimpleGrid,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
  Tooltip,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { IconPencil, IconPlugConnected, IconTrash } from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import {
  backupTargetsQuery,
  describeTarget,
  type BackupTarget,
  type BackupTargetInput,
} from "../volumes/backups";

/** Targets of volume backups: host directories and S3 buckets (#59, ADR-0050). */
export function BackupTargetsSettingsCard() {
  const queryClient = useQueryClient();
  const targets = useQuery(backupTargetsQuery);
  const [editing, setEditing] = useState<BackupTarget | "new" | null>(null);
  const [removing, setRemoving] = useState<BackupTarget | null>(null);
  const remove = useMutation({
    mutationFn: (id: number) =>
      unwrapEmpty(api.DELETE("/api/v1/backup-targets/{id}", { params: { path: { id } } })),
    onSuccess: () => {
      setRemoving(null);
      void queryClient.invalidateQueries({ queryKey: backupTargetsQuery.queryKey });
    },
    onError: (e) =>
      notifications.show({ color: "red", title: "Cannot delete", message: e.message }),
  });
  const test = useMutation({
    mutationFn: (t: BackupTarget) =>
      unwrap(api.POST("/api/v1/backup-targets/{id}/test", { params: { path: { id: t.id } } })).then(
        (r) => ({ ...r, name: t.name }),
      ),
    onSuccess: (r) =>
      notifications.show(
        r.ok
          ? { color: "teal", message: `${r.name} works` }
          : { color: "red", title: `${r.name} does not work`, message: r.error },
      ),
    onError: (e) => notifications.show({ color: "red", title: "Cannot test", message: e.message }),
  });

  return (
    <Paper withBorder p="md" component="section" aria-label="Volume backup targets">
      <Stack gap="sm">
        <Group justify="space-between" align="flex-start">
          <div>
            <Title order={3}>Volume backup targets</Title>
            <Text size="sm" c="dimmed">
              Where backups of volumes go: a directory on the host or an S3 bucket (AWS, MinIO,
              Backblaze B2…). Back up from the volumes page or on a schedule.
            </Text>
          </div>
          <Button onClick={() => setEditing("new")}>Add target</Button>
        </Group>
        {targets.isPending && <Loader aria-label="Loading targets" />}
        {targets.isError && (
          <Alert color="red" title="Cannot read targets">
            {targets.error.message}
          </Alert>
        )}
        {targets.data && targets.data.targets.length === 0 && (
          <Text size="sm" c="dimmed">
            No targets yet.
          </Text>
        )}
        {targets.data && targets.data.targets.length > 0 && (
          <Table.ScrollContainer minWidth={560}>
            <Table verticalSpacing={4} fz="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Name</Table.Th>
                  <Table.Th>Kind</Table.Th>
                  <Table.Th>Location</Table.Th>
                  <Table.Th />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {targets.data.targets.map((t) => (
                  <Table.Tr key={t.id}>
                    <Table.Td>{t.name}</Table.Td>
                    <Table.Td>
                      <Badge size="sm" variant="light" color={t.kind === "s3" ? "orange" : "gray"}>
                        {t.kind === "s3" ? "S3" : "Directory"}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" ff="monospace" style={{ wordBreak: "break-all" }}>
                        {describeTarget(t)}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Group gap={4} justify="flex-end" wrap="nowrap">
                        <Tooltip label="Test">
                          <ActionIcon
                            variant="subtle"
                            aria-label={`Test ${t.name}`}
                            loading={test.isPending && test.variables?.id === t.id}
                            onClick={() => test.mutate(t)}
                          >
                            <IconPlugConnected size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Edit">
                          <ActionIcon
                            variant="subtle"
                            aria-label={`Edit ${t.name}`}
                            onClick={() => setEditing(t)}
                          >
                            <IconPencil size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Delete">
                          <ActionIcon
                            variant="subtle"
                            color="red"
                            aria-label={`Delete ${t.name}`}
                            onClick={() => setRemoving(t)}
                          >
                            <IconTrash size={16} />
                          </ActionIcon>
                        </Tooltip>
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        )}
      </Stack>
      {editing && (
        <TargetDialog
          target={editing === "new" ? null : editing}
          onClose={() => setEditing(null)}
        />
      )}
      <Modal opened={!!removing} onClose={() => setRemoving(null)} title="Delete backup target">
        {removing && (
          <Stack>
            <Text size="sm">
              Delete <b>{removing.name}</b>? Its archives stay where they are.
            </Text>
            <Group justify="flex-end">
              <Button variant="default" onClick={() => setRemoving(null)}>
                Cancel
              </Button>
              <Button
                color="red"
                loading={remove.isPending}
                onClick={() => remove.mutate(removing.id)}
              >
                Delete
              </Button>
            </Group>
          </Stack>
        )}
      </Modal>
    </Paper>
  );
}

function TargetDialog({ target, onClose }: { target: BackupTarget | null; onClose: () => void }) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState<BackupTargetInput>(() => ({
    name: target?.name ?? "",
    kind: target?.kind ?? "local",
    directory: target?.directory ?? "",
    s3: target?.s3 ?? { endpoint: "", bucket: "", accessKey: "", region: "", prefix: "" },
    secretKey: "",
  }));
  const s3 = form.s3 ?? { endpoint: "", bucket: "", accessKey: "" };
  const setS3 = (patch: Partial<typeof s3>) => setForm((f) => ({ ...f, s3: { ...s3, ...patch } }));
  const keepsSecret = !!target?.hasSecret && target.kind === "s3";
  const save = useMutation({
    mutationFn: () => {
      const body: BackupTargetInput =
        form.kind === "local"
          ? { name: form.name, kind: "local", directory: form.directory }
          : { name: form.name, kind: "s3", s3, secretKey: form.secretKey };
      return target
        ? unwrap(
            api.PUT("/api/v1/backup-targets/{id}", { params: { path: { id: target.id } }, body }),
          )
        : unwrap(api.POST("/api/v1/backup-targets", { body }));
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: backupTargetsQuery.queryKey });
      onClose();
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  return (
    <Modal
      opened
      onClose={onClose}
      size="lg"
      title={target ? `Edit ${target.name}` : "Add backup target"}
    >
      <form onSubmit={submit}>
        <Stack>
          {save.isError && <Alert color="red">{save.error.message}</Alert>}
          <TextInput
            label="Name"
            required
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.currentTarget.value })}
          />
          <SegmentedControl
            aria-label="Kind"
            data={[
              { value: "local", label: "Directory on the host" },
              { value: "s3", label: "S3 bucket" },
            ]}
            value={form.kind}
            onChange={(v) => setForm({ ...form, kind: v as BackupTargetInput["kind"] })}
          />
          {form.kind === "local" ? (
            <TextInput
              label="Directory"
              description="Absolute path on the host; archives go to <namespace>/<volume>/ below it"
              placeholder="/var/backups/volumes"
              required
              value={form.directory ?? ""}
              onChange={(e) => setForm({ ...form, directory: e.currentTarget.value })}
            />
          ) : (
            <>
              <TextInput
                label="Endpoint"
                description="host[:port] for HTTPS, or http://host:port"
                placeholder="s3.eu-central-1.amazonaws.com"
                required
                value={s3.endpoint}
                onChange={(e) => setS3({ endpoint: e.currentTarget.value })}
              />
              <SimpleGrid cols={{ base: 1, sm: 2 }}>
                <TextInput
                  label="Bucket"
                  required
                  value={s3.bucket}
                  onChange={(e) => setS3({ bucket: e.currentTarget.value })}
                />
                <TextInput
                  label="Region"
                  placeholder="us-east-1"
                  value={s3.region ?? ""}
                  onChange={(e) => setS3({ region: e.currentTarget.value })}
                />
                <TextInput
                  label="Prefix"
                  placeholder="nerdui"
                  value={s3.prefix ?? ""}
                  onChange={(e) => setS3({ prefix: e.currentTarget.value })}
                />
                <TextInput
                  label="Access key"
                  required
                  autoComplete="off"
                  value={s3.accessKey}
                  onChange={(e) => setS3({ accessKey: e.currentTarget.value })}
                />
              </SimpleGrid>
              <PasswordInput
                label="Secret key"
                description={
                  keepsSecret ? "Leave empty to keep the stored key" : "Stored encrypted"
                }
                required={!keepsSecret}
                autoComplete="new-password"
                value={form.secretKey ?? ""}
                onChange={(e) => setForm({ ...form, secretKey: e.currentTarget.value })}
              />
              <Checkbox
                label="Path-style addressing (MinIO and most self-hosted servers)"
                checked={!!s3.pathStyle}
                onChange={(e) => setS3({ pathStyle: e.currentTarget.checked })}
              />
            </>
          )}
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
