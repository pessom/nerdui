// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Badge,
  Box,
  Checkbox,
  Chip,
  Group,
  Loader,
  Paper,
  SegmentedControl,
  Select,
  Stack,
  Switch,
  Text,
  TextInput,
  Tooltip,
  useComputedColorScheme,
} from "@mantine/core";
import {
  IconArrowDown,
  IconChevronDown,
  IconChevronUp,
  IconEraser,
  IconRefresh,
  IconRegex,
} from "@tabler/icons-react";
import { Suspense, lazy, useDeferredValue, useEffect, useMemo, useRef, useState } from "react";
import type { LogTerminalHandle } from "../containers/LogTerminal";
import { compileFilter, lineMatches, type LogsStatus, type StreamFilter } from "../containers/logs";
import { STACK_TAILS, formatStackLine, useStackLogs } from "./stackLogs";

const LogTerminal = lazy(() => import("../containers/LogTerminal"));

const statusBadge: Record<LogsStatus, { label: string; color: string }> = {
  connecting: { label: "Connecting", color: "gray" },
  open: { label: "Live", color: "teal" },
  reconnecting: { label: "Reconnecting", color: "yellow" },
  closed: { label: "Disconnected", color: "gray" },
  ended: { label: "Ended", color: "gray" },
};

/**
 * Logs tab of a stack (#30): the merged logs of its containers with a colored service label,
 * a service filter, text filter and search.
 */
export function StackLogsPanel({
  env,
  name,
  namespace,
  services,
  containerIds,
}: {
  env: string;
  name: string;
  namespace: string;
  /** Services of the stack, in a stable order (colors). */
  services: string[];
  /** The stack's containers: a change (deploy, down) reconnects to include new ones. */
  containerIds: string;
}) {
  const [tail, setTail] = useState("200");
  const [follow, setFollow] = useState(true);
  const [timestamps, setTimestamps] = useState(false);
  const [hidden, setHidden] = useState<string[]>([]);
  const [stream, setStream] = useState<StreamFilter>("all");
  const [filterText, setFilterText] = useState("");
  const [regex, setRegex] = useState(false);
  const [onlyMatching, setOnlyMatching] = useState(true);
  const [cleared, setCleared] = useState(0);
  const filter = useDeferredValue(filterText);
  const term = useRef<LogTerminalHandle>(null);
  const dark = useComputedColorScheme("light") === "dark";

  const logs = useStackLogs(env, name, { namespace, follow, tail });
  const { restart } = logs;

  // New containers (after a deploy) are only included by reconnecting.
  const known = useRef(containerIds);
  useEffect(() => {
    if (known.current !== containerIds) {
      known.current = containerIds;
      restart();
    }
  }, [containerIds, restart]);

  const width = Math.max(...services.map((s) => s.length), 4);
  const compiled = useMemo(() => compileFilter(filter, regex), [filter, regex]);
  const lines = useMemo(() => {
    const test = onlyMatching ? compiled.test : () => true;
    return logs.entries
      .filter((e) => !hidden.includes(e.service) && lineMatches(e, { stream }, test))
      .map((e) => ({ seq: e.seq, text: formatStackLine(e, timestamps, services, width) }));
  }, [logs.entries, hidden, stream, compiled, onlyMatching, timestamps, services, width]);
  const resetKey = JSON.stringify([
    tail,
    follow,
    timestamps,
    hidden,
    stream,
    onlyMatching && filter,
    regex,
    cleared,
  ]);
  const badge = statusBadge[logs.status];

  return (
    <Stack gap="xs">
      <Group gap="sm" wrap="wrap" role="toolbar" aria-label="Log options">
        <Select
          aria-label="Lines"
          w={150}
          size="xs"
          data={STACK_TAILS.map((t) => ({ value: t, label: `Last ${t} per container` }))}
          value={tail}
          onChange={(v) => v && setTail(v)}
          allowDeselect={false}
        />
        <Switch
          size="xs"
          label="Follow"
          checked={follow}
          onChange={(e) => setFollow(e.currentTarget.checked)}
        />
        <Switch
          size="xs"
          label="Timestamps"
          checked={timestamps}
          onChange={(e) => setTimestamps(e.currentTarget.checked)}
        />
        <SegmentedControl
          size="xs"
          aria-label="Stream"
          value={stream}
          onChange={(v) => setStream(v as StreamFilter)}
          data={[
            { value: "all", label: "All" },
            { value: "stdout", label: "stdout" },
            { value: "stderr", label: "stderr" },
          ]}
        />
        <Badge
          variant="light"
          color={badge.color}
          leftSection={logs.status === "connecting" ? <Loader size={8} /> : undefined}
        >
          {badge.label}
        </Badge>
        <Tooltip label="Reconnect (include containers created since)">
          <ActionIcon variant="subtle" color="gray" aria-label="Reconnect" onClick={restart}>
            <IconRefresh size={16} />
          </ActionIcon>
        </Tooltip>
      </Group>
      {services.length > 1 && (
        <Group gap={6} role="group" aria-label="Services">
          {services.map((s) => (
            <Chip
              key={s}
              size="xs"
              checked={!hidden.includes(s)}
              onChange={(on) => setHidden((h) => (on ? h.filter((x) => x !== s) : [...h, s]))}
            >
              {s}
            </Chip>
          ))}
        </Group>
      )}
      <Group gap="xs" wrap="nowrap">
        <TextInput
          size="xs"
          aria-label="Filter"
          placeholder={regex ? "Regular expression" : "Filter or search"}
          style={{ flex: 1 }}
          value={filterText}
          onChange={(e) => setFilterText(e.currentTarget.value)}
          onKeyDown={(e) => {
            if (e.key !== "Enter") return;
            if (e.shiftKey) term.current?.findPrevious();
            else term.current?.findNext();
          }}
          error={compiled.error}
          rightSection={
            <Tooltip label="Regular expression">
              <ActionIcon
                size="sm"
                variant={regex ? "filled" : "subtle"}
                aria-label="Regular expression"
                onClick={() => setRegex((r) => !r)}
              >
                <IconRegex size={14} />
              </ActionIcon>
            </Tooltip>
          }
        />
        <Checkbox
          size="xs"
          label="Only matching"
          checked={onlyMatching}
          onChange={(e) => setOnlyMatching(e.currentTarget.checked)}
        />
        <ActionIcon
          variant="default"
          aria-label="Previous match"
          disabled={!filter || !!compiled.error}
          onClick={() => term.current?.findPrevious()}
        >
          <IconChevronUp size={14} />
        </ActionIcon>
        <ActionIcon
          variant="default"
          aria-label="Next match"
          disabled={!filter || !!compiled.error}
          onClick={() => term.current?.findNext()}
        >
          <IconChevronDown size={14} />
        </ActionIcon>
        <ActionIcon
          variant="default"
          aria-label="Scroll to bottom"
          onClick={() => term.current?.scrollToBottom()}
        >
          <IconArrowDown size={14} />
        </ActionIcon>
        <ActionIcon
          variant="default"
          aria-label="Clear"
          onClick={() => {
            logs.clear();
            setCleared((c) => c + 1);
          }}
        >
          <IconEraser size={14} />
        </ActionIcon>
      </Group>
      {logs.error && (
        <Alert color="red" title="Cannot read logs">
          {logs.error.message}
        </Alert>
      )}
      {containerIds === "" ? (
        <Text size="sm" c="dimmed">
          The stack has no containers in this namespace. Deploy it to see its logs.
        </Text>
      ) : (
        <Paper withBorder>
          <Box h="60vh">
            <Suspense fallback={<Loader aria-label="Loading logs" />}>
              <LogTerminal
                ref={term}
                lines={lines}
                resetKey={resetKey}
                search={{ term: compiled.error ? "" : filter, regex }}
                dark={dark}
                label={`Logs of stack ${name}`}
              />
            </Suspense>
          </Box>
        </Paper>
      )}
    </Stack>
  );
}
