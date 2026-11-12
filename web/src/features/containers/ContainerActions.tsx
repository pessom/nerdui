// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Button,
  Checkbox,
  Group,
  List,
  Loader,
  Menu,
  Modal,
  Paper,
  ScrollArea,
  Stack,
  Text,
  Tooltip,
} from "@mantine/core";
import {
  IconDots,
  IconPlayerPause,
  IconPlayerPlay,
  IconPlayerStop,
  IconRefresh,
  IconSkull,
  IconTrash,
  IconX,
} from "@tabler/icons-react";
import { useState, type ReactNode } from "react";
import {
  actionInfo,
  actionOrder,
  canApply,
  type ActionRequest,
  type ContainerAction,
} from "./actions";
import { displayName, type Container } from "./format";

const icons: Record<ContainerAction, ReactNode> = {
  start: <IconPlayerPlay size={16} />,
  stop: <IconPlayerStop size={16} />,
  restart: <IconRefresh size={16} />,
  pause: <IconPlayerPause size={16} />,
  unpause: <IconPlayerPlay size={16} />,
  kill: <IconSkull size={16} />,
  remove: <IconTrash size={16} />,
};

/** Actions available to the table; `disabled` explains why changes are not possible. */
export interface TableActions {
  run: (req: ActionRequest) => void;
  busy: ReadonlySet<string>;
  disabled?: string;
}

export function RowActions({
  container,
  actions,
  request,
}: {
  container: Container;
  actions: TableActions;
  request: (action: ContainerAction, containers: Container[]) => void;
}) {
  const name = displayName(container);
  if (actions.busy.has(container.id)) return <Loader size="xs" aria-label={`Working on ${name}`} />;
  return (
    <Menu position="bottom-end" withinPortal>
      <Menu.Target>
        <Tooltip label={actions.disabled} disabled={!actions.disabled}>
          <ActionIcon
            variant="subtle"
            color="gray"
            aria-label={`Actions for ${name}`}
            disabled={!!actions.disabled}
          >
            <IconDots size={16} />
          </ActionIcon>
        </Tooltip>
      </Menu.Target>
      <Menu.Dropdown>
        {actionOrder.map((a) => (
          <Menu.Item
            key={a}
            leftSection={icons[a]}
            color={actionInfo[a].color}
            disabled={!canApply(a, container)}
            onClick={() => request(a, [container])}
          >
            {actionInfo[a].label}
            {actionInfo[a].confirm ? "…" : ""}
          </Menu.Item>
        ))}
      </Menu.Dropdown>
    </Menu>
  );
}

/** Toolbar for the selected containers; buttons apply to the containers they fit. */
export function BulkActions({
  selected,
  actions,
  request,
  onClear,
}: {
  selected: Container[];
  actions: TableActions;
  request: (action: ContainerAction, containers: Container[]) => void;
  onClear: () => void;
}) {
  if (selected.length === 0) return null;
  return (
    <Paper withBorder p="xs" mb="sm" role="toolbar" aria-label="Selected containers">
      <Group gap="xs" wrap="wrap">
        <Text size="sm" fw={500} mr="xs">
          {selected.length} selected
        </Text>
        {actionOrder.map((a) => {
          const n = selected.filter((c) => canApply(a, c)).length;
          return (
            <Button
              key={a}
              size="xs"
              variant="light"
              color={actionInfo[a].color}
              leftSection={icons[a]}
              disabled={!!actions.disabled || n === 0}
              title={n < selected.length ? `Applies to ${n} of ${selected.length}` : undefined}
              onClick={() => request(a, selected)}
            >
              {actionInfo[a].label}
              {n > 0 && n < selected.length ? ` (${n})` : ""}
            </Button>
          );
        })}
        <ActionIcon variant="subtle" color="gray" aria-label="Clear selection" onClick={onClear}>
          <IconX size={16} />
        </ActionIcon>
      </Group>
    </Paper>
  );
}

/** Confirmation for destructive actions; remove offers force and anonymous volumes. */
export function ConfirmDialog({
  pending,
  onCancel,
  onConfirm,
}: {
  pending?: { action: ContainerAction; containers: Container[] };
  onCancel: () => void;
  onConfirm: (opts: { force?: boolean; volumes?: boolean }) => void;
}) {
  const [force, setForce] = useState(false);
  const [volumes, setVolumes] = useState(false);
  const action = pending?.action;
  const containers = pending?.containers ?? [];
  const live = containers.filter((c) => c.state === "running" || c.state === "paused");
  const remove = action === "remove";
  const blocked = remove && live.length > 0 && !force;
  const info = action ? actionInfo[action] : undefined;
  const close = () => {
    setForce(false);
    setVolumes(false);
  };

  return (
    <Modal
      opened={!!pending}
      onClose={() => {
        close();
        onCancel();
      }}
      title={
        info &&
        `${info.label} ${containers.length === 1 ? "container" : `${containers.length} containers`}?`
      }
    >
      <Stack gap="sm">
        <ScrollArea.Autosize mah={200}>
          <List size="sm">
            {containers.map((c) => (
              <List.Item key={c.id}>
                <Text span ff="monospace" size="sm">
                  {displayName(c)}
                </Text>{" "}
                <Text span c="dimmed" size="xs">
                  {c.state}
                </Text>
              </List.Item>
            ))}
          </List>
        </ScrollArea.Autosize>
        {action === "kill" && (
          <Text size="sm">The processes are stopped immediately with SIGKILL.</Text>
        )}
        {remove && (
          <>
            {live.length > 0 && (
              <Checkbox
                label={`Force: kill ${live.length === 1 ? "the running container" : `${live.length} running containers`} first`}
                checked={force}
                onChange={(e) => setForce(e.currentTarget.checked)}
              />
            )}
            <Checkbox
              label="Also remove anonymous volumes"
              checked={volumes}
              onChange={(e) => setVolumes(e.currentTarget.checked)}
            />
          </>
        )}
        <Group justify="flex-end">
          <Button
            variant="default"
            onClick={() => {
              close();
              onCancel();
            }}
          >
            Cancel
          </Button>
          <Button
            color="red"
            disabled={blocked}
            onClick={() => {
              close();
              onConfirm(remove ? { force: live.length > 0 && force, volumes } : {});
            }}
          >
            {info?.label}
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}
