// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Button,
  Checkbox,
  Code,
  Group,
  Menu,
  Modal,
  Stack,
  Text,
  TextInput,
  Tooltip,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import {
  IconChevronDown,
  IconDownload,
  IconPlayerPlay,
  IconPlayerStop,
  IconRefresh,
  IconRocket,
  IconTrash,
} from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap } from "../../api/client";
import { keys } from "../../api/queries";
import { useJobsPanel } from "../jobs/JobsContext";

type Op = "up" | "down" | "start" | "stop" | "restart" | "pull";
interface OpOptions {
  pull?: boolean;
  removeOrphans?: boolean;
  volumes?: boolean;
  confirm?: string;
}

/**
 * Stack operations (#28): each runs `nerdctl compose` as a job whose output opens in the Jobs
 * panel (#31). Down asks for confirmation; removing volumes asks to type the stack name.
 */
export function StackActions({
  env,
  namespace,
  name,
  running,
  readOnly,
}: {
  env: string;
  namespace: string;
  name: string;
  /** Some containers of the stack run. */
  running: boolean;
  readOnly: boolean;
}) {
  const queryClient = useQueryClient();
  const jobs = useJobsPanel();
  const [downOpen, setDownOpen] = useState(false);
  const run = useMutation({
    mutationFn: ({ op, body }: { op: Op; body?: OpOptions }) =>
      unwrap(
        api.POST("/api/v1/environments/{env}/stacks/{name}/{op}", {
          params: { path: { env, name, op }, query: { namespace } },
          body: body ?? {},
        }),
      ),
    onSuccess: ({ job }) => {
      void queryClient.invalidateQueries({ queryKey: keys.jobs });
      void queryClient.invalidateQueries({ queryKey: keys.stacks(env, namespace) });
      jobs.open(job.id);
      setDownOpen(false);
    },
    onError: (e) =>
      notifications.show({ color: "red", title: "Cannot run the operation", message: e.message }),
  });
  const pending = (op: Op) => run.isPending && run.variables?.op === op;
  const disabledHint = readOnly ? "Read-only mode: host nerdctl is incompatible" : undefined;

  return (
    <Group gap="xs">
      <Tooltip label={disabledHint} disabled={!readOnly}>
        <Group gap={0} wrap="nowrap">
          <Button
            leftSection={<IconRocket size={16} />}
            disabled={readOnly}
            loading={pending("up")}
            onClick={() => run.mutate({ op: "up" })}
            style={{ borderTopRightRadius: 0, borderBottomRightRadius: 0 }}
          >
            Deploy
          </Button>
          <Menu position="bottom-end">
            <Menu.Target>
              <Button
                aria-label="Deploy options"
                disabled={readOnly}
                px={6}
                style={{ borderTopLeftRadius: 0, borderBottomLeftRadius: 0 }}
              >
                <IconChevronDown size={14} />
              </Button>
            </Menu.Target>
            <Menu.Dropdown>
              <Menu.Item onClick={() => run.mutate({ op: "up", body: { pull: true } })}>
                Pull images and deploy
              </Menu.Item>
              <Menu.Item onClick={() => run.mutate({ op: "up", body: { removeOrphans: true } })}>
                Deploy and remove orphan containers
              </Menu.Item>
            </Menu.Dropdown>
          </Menu>
        </Group>
      </Tooltip>
      {running ? (
        <Button
          variant="default"
          leftSection={<IconPlayerStop size={16} />}
          disabled={readOnly}
          loading={pending("stop")}
          onClick={() => run.mutate({ op: "stop" })}
        >
          Stop
        </Button>
      ) : (
        <Button
          variant="default"
          leftSection={<IconPlayerPlay size={16} />}
          disabled={readOnly}
          loading={pending("start")}
          onClick={() => run.mutate({ op: "start" })}
        >
          Start
        </Button>
      )}
      <Button
        variant="default"
        leftSection={<IconRefresh size={16} />}
        disabled={readOnly}
        loading={pending("restart")}
        onClick={() => run.mutate({ op: "restart" })}
      >
        Restart
      </Button>
      <Button
        variant="default"
        leftSection={<IconDownload size={16} />}
        disabled={readOnly}
        loading={pending("pull")}
        onClick={() => run.mutate({ op: "pull" })}
      >
        Pull
      </Button>
      <Button
        variant="light"
        color="red"
        leftSection={<IconTrash size={16} />}
        disabled={readOnly}
        onClick={() => setDownOpen(true)}
      >
        Down
      </Button>
      <DownDialog
        name={name}
        opened={downOpen}
        loading={pending("down")}
        onClose={() => setDownOpen(false)}
        onConfirm={(body) => run.mutate({ op: "down", body })}
      />
    </Group>
  );
}

function DownDialog({
  name,
  opened,
  loading,
  onClose,
  onConfirm,
}: {
  name: string;
  opened: boolean;
  loading: boolean;
  onClose: () => void;
  onConfirm: (body: OpOptions) => void;
}) {
  const [volumes, setVolumes] = useState(false);
  const [orphans, setOrphans] = useState(false);
  const [typed, setTyped] = useState("");
  const close = () => {
    setVolumes(false);
    setOrphans(false);
    setTyped("");
    onClose();
  };
  const blocked = volumes && typed !== name;
  return (
    <Modal opened={opened} onClose={close} title="Down stack">
      <Stack gap="sm">
        <Text size="sm">
          Stops and removes the containers and networks of <Code>{name}</Code>. The compose files
          stay; Deploy recreates the stack.
        </Text>
        <Checkbox
          label="Also remove containers of services no longer in the compose file"
          checked={orphans}
          onChange={(e) => setOrphans(e.currentTarget.checked)}
        />
        <Checkbox
          color="red"
          label="Remove the stack's volumes"
          checked={volumes}
          onChange={(e) => setVolumes(e.currentTarget.checked)}
        />
        {volumes && (
          <>
            <Alert color="red" title="The data in the stack's volumes will be deleted">
              Named volumes of this compose project are removed. This cannot be undone.
            </Alert>
            <TextInput
              label={`Type ${name} to confirm`}
              value={typed}
              onChange={(e) => setTyped(e.currentTarget.value)}
            />
          </>
        )}
        <Group justify="flex-end">
          <Button variant="default" onClick={close}>
            Cancel
          </Button>
          <Button
            color="red"
            loading={loading}
            disabled={blocked}
            onClick={() =>
              onConfirm({
                removeOrphans: orphans || undefined,
                volumes: volumes || undefined,
                confirm: volumes ? typed : undefined,
              })
            }
          >
            {volumes ? "Down and remove volumes" : "Down"}
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}
