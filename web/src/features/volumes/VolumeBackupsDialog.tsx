// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Anchor,
  Button,
  Checkbox,
  Group,
  Loader,
  Modal,
  Select,
  Stack,
  Table,
  Text,
  TextInput,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap } from "../../api/client";
import { RouterAnchor } from "../../components/RouterAnchor";
import { Time } from "../../components/Time";
import { useJobsPanel } from "../jobs/JobsContext";
import { formatBytes } from "../system/info";
import {
  backupTargetsQuery,
  describeTarget,
  volumeBackupsQuery,
  type VolumeBackup,
} from "./backups";

/** Backs up a volume now, lists its archives in a target and restores one (#59, ADR-0050). */
export function VolumeBackupsDialog({
  env,
  namespace,
  volume,
  readOnly,
  onClose,
}: {
  env: string;
  namespace: string;
  volume?: string;
  readOnly: boolean;
  onClose: () => void;
}) {
  return (
    <Modal
      opened={!!volume}
      onClose={onClose}
      size="xl"
      title={volume ? `Backups of ${volume}` : "Backups"}
    >
      {volume && (
        <Content
          env={env}
          namespace={namespace}
          volume={volume}
          readOnly={readOnly}
          onClose={onClose}
        />
      )}
    </Modal>
  );
}

function Content({
  env,
  namespace,
  volume,
  readOnly,
  onClose,
}: {
  env: string;
  namespace: string;
  volume: string;
  readOnly: boolean;
  onClose: () => void;
}) {
  const jobs = useJobsPanel();
  const targets = useQuery(backupTargetsQuery);
  const [chosen, setChosen] = useState<number>();
  const target = chosen ?? targets.data?.targets[0]?.id;
  const [stop, setStop] = useState(false);
  const [restoring, setRestoring] = useState<VolumeBackup | null>(null);
  const backups = useQuery(volumeBackupsQuery(env, namespace, volume, target));
  const backUp = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/environments/{env}/volumes/{name}/backups", {
          params: { path: { env, name: volume }, query: { namespace } },
          body: { targetId: target ?? 0, stopContainers: stop },
        }),
      ),
    onSuccess: ({ job }) => {
      onClose();
      jobs.open(job.id);
    },
    onError: (e) =>
      notifications.show({ color: "red", title: "Cannot back up", message: e.message }),
  });

  if (targets.isPending) return <Loader aria-label="Loading targets" />;
  if (targets.isError) return <Alert color="red">{targets.error.message}</Alert>;
  if (targets.data.targets.length === 0)
    return (
      <Text size="sm">
        No backup targets yet: add a directory or an S3 bucket under{" "}
        <RouterAnchor to="/env/$env/settings" params={{ env }}>
          Settings
        </RouterAnchor>
        .
      </Text>
    );

  return (
    <Stack>
      <Select
        label="Target"
        data={targets.data.targets.map((t) => ({
          value: String(t.id),
          label: `${t.name} — ${describeTarget(t)}`,
        }))}
        value={target != null ? String(target) : null}
        onChange={(v) => v && setChosen(Number(v))}
        allowDeselect={false}
      />
      <Group justify="space-between" align="flex-end">
        <Checkbox
          label="Stop containers using the volume meanwhile"
          description="Consistent copies of databases; they are started again afterwards"
          checked={stop}
          onChange={(e) => setStop(e.currentTarget.checked)}
        />
        <Button loading={backUp.isPending} disabled={readOnly} onClick={() => backUp.mutate()}>
          Back up now
        </Button>
      </Group>
      <Text size="xs" c="dimmed">
        Back up on a schedule under{" "}
        <Anchor component={RouterAnchor} to="/schedules" size="xs">
          Schedules
        </Anchor>
        , which also removes old archives.
      </Text>
      {backups.isPending && <Loader aria-label="Loading backups" />}
      {backups.isError && (
        <Alert color="red" title="Cannot list backups">
          {backups.error.message}
        </Alert>
      )}
      {backups.data && backups.data.backups.length === 0 && (
        <Text size="sm" c="dimmed">
          No backups of {volume} in this target.
        </Text>
      )}
      {backups.data && backups.data.backups.length > 0 && (
        <Table.ScrollContainer minWidth={520}>
          <Table verticalSpacing={4} fz="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Archive</Table.Th>
                <Table.Th>Size</Table.Th>
                <Table.Th>Time</Table.Th>
                <Table.Th />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {backups.data.backups.map((b) => (
                <Table.Tr key={b.key}>
                  <Table.Td>
                    <Text size="sm" ff="monospace">
                      {b.key.split("/").pop()}
                    </Text>
                  </Table.Td>
                  <Table.Td>{formatBytes(b.size)}</Table.Td>
                  <Table.Td>
                    <Time value={b.time} size="sm" />
                  </Table.Td>
                  <Table.Td>
                    <Group justify="flex-end">
                      <Button
                        size="xs"
                        variant="default"
                        disabled={readOnly}
                        aria-label={`Restore ${b.key.split("/").pop()}`}
                        onClick={() => setRestoring(b)}
                      >
                        Restore
                      </Button>
                    </Group>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      )}
      {restoring && target != null && (
        <RestoreDialog
          env={env}
          namespace={namespace}
          volume={volume}
          target={target}
          archive={restoring}
          onClose={() => setRestoring(null)}
          onStarted={(id) => {
            setRestoring(null);
            onClose();
            jobs.open(id);
          }}
        />
      )}
    </Stack>
  );
}

function RestoreDialog({
  env,
  namespace,
  volume,
  target,
  archive,
  onClose,
  onStarted,
}: {
  env: string;
  namespace: string;
  volume: string;
  target: number;
  archive: VolumeBackup;
  onClose: () => void;
  onStarted: (job: string) => void;
}) {
  const [into, setInto] = useState(volume);
  const [stop, setStop] = useState(false);
  const restore = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/environments/{env}/volumes/{name}/restore", {
          params: { path: { env, name: into.trim() }, query: { namespace } },
          body: { targetId: target, key: archive.key, stopContainers: stop },
        }),
      ),
    onSuccess: ({ job }) => onStarted(job.id),
  });
  const replacing = into.trim() === volume;
  return (
    <Modal opened onClose={onClose} title="Restore volume">
      <Stack>
        {restore.isError && <Alert color="red">{restore.error.message}</Alert>}
        <Text size="sm">
          Restore <b>{archive.key.split("/").pop()}</b>
        </Text>
        <TextInput
          label="Into volume"
          description="Another name restores into a new volume"
          required
          value={into}
          onChange={(e) => setInto(e.currentTarget.value)}
        />
        {replacing && (
          <Alert color="orange">
            The current contents of {volume} are replaced. Containers using it must be stopped.
          </Alert>
        )}
        <Checkbox
          label="Stop running containers using the volume, and start them again afterwards"
          checked={stop}
          onChange={(e) => setStop(e.currentTarget.checked)}
        />
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>
            Cancel
          </Button>
          <Button
            color={replacing ? "red" : undefined}
            loading={restore.isPending}
            disabled={!into.trim()}
            onClick={() => restore.mutate()}
          >
            {replacing ? "Replace contents" : "Restore"}
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}
