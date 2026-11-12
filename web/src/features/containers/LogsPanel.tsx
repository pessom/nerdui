// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Badge,
  Box,
  Checkbox,
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
  IconDownload,
  IconEraser,
  IconRegex,
} from "@tabler/icons-react";
import { Suspense, lazy, useDeferredValue, useEffect, useMemo, useRef, useState } from "react";
import type { LogTerminalHandle } from "./LogTerminal";
import {
  compileFilter,
  formatLine,
  lineMatches,
  logsPath,
  useContainerLogs,
  type LogsStatus,
  type StreamFilter,
} from "./logs";

const LogTerminal = lazy(() => import("./LogTerminal"));

const tails = ["100", "500", "1000", "5000", "all"];

const statusBadge: Record<LogsStatus, { label: string; color: string }> = {
  connecting: { label: "Connecting", color: "gray" },
  open: { label: "Live", color: "teal" },
  reconnecting: { label: "Reconnecting", color: "yellow" },
  closed: { label: "Disconnected", color: "gray" },
  ended: { label: "Ended", color: "gray" },
};

/** Logs tab of the container page (#14): live stream, filter and search, download. */
export function LogsPanel({
  env,
  id,
  name,
  namespace,
  running,
}: {
  env: string;
  id: string;
  name: string;
  namespace: string;
  running: boolean;
}) {
  const [tail, setTail] = useState("500");
  const [follow, setFollow] = useState(true);
  const [timestamps, setTimestamps] = useState(false);
  const [stream, setStream] = useState<StreamFilter>("all");
  const [filterText, setFilterText] = useState("");
  const [regex, setRegex] = useState(false);
  const [onlyMatching, setOnlyMatching] = useState(true);
  const [cleared, setCleared] = useState(0);
  const filter = useDeferredValue(filterText);
  const term = useRef<LogTerminalHandle>(null);
  const dark = useComputedColorScheme("light") === "dark";

  const logs = useContainerLogs(env, id, { namespace, follow, tail });
  const { status, restart } = logs;

  // A followed stream ends when the container stops; resume when it runs again.
  const wasRunning = useRef(running);
  useEffect(() => {
    if (running && !wasRunning.current && follow && status === "ended") restart();
    wasRunning.current = running;
  }, [running, follow, status, restart]);

  const compiled = useMemo(() => compileFilter(filter, regex), [filter, regex]);
  const lines = useMemo(() => {
    const test = onlyMatching ? compiled.test : () => true;
    return logs.entries
      .filter((e) => lineMatches(e, { stream }, test))
      .map((e) => ({ seq: e.seq, text: formatLine(e, timestamps) }));
  }, [logs.entries, stream, compiled, onlyMatching, timestamps]);
  const resetKey = JSON.stringify([
    tail,
    follow,
    timestamps,
    stream,
    onlyMatching && filter,
    regex,
    cleared,
  ]);

  const download = `${logsPath(env, id)}?${new URLSearchParams({
    namespace,
    timestamps: String(timestamps),
  }).toString()}`;
  const badge = statusBadge[status];

  return (
    <Stack gap="xs">
      <Group gap="sm" wrap="wrap" role="toolbar" aria-label="Log options">
        <Select
          aria-label="Lines"
          w={110}
          size="xs"
          data={tails.map((t) => ({ value: t, label: t === "all" ? "All lines" : `Last ${t}` }))}
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
          aria-label="Streams"
          value={stream}
          onChange={(v) => setStream(v as StreamFilter)}
          data={[
            { value: "all", label: "All" },
            { value: "stdout", label: "stdout" },
            { value: "stderr", label: "stderr" },
          ]}
        />
        <Group gap={4} ml="auto">
          <Badge
            color={badge.color}
            variant="light"
            leftSection={status === "open" && follow ? <Loader size={8} type="dots" /> : undefined}
          >
            {status === "open" && !follow ? "Loaded" : badge.label}
          </Badge>
          <Tooltip label="Scroll to end">
            <ActionIcon
              variant="subtle"
              color="gray"
              aria-label="Scroll to end"
              onClick={() => term.current?.scrollToBottom()}
            >
              <IconArrowDown size={16} />
            </ActionIcon>
          </Tooltip>
          <Tooltip label="Clear view">
            <ActionIcon
              variant="subtle"
              color="gray"
              aria-label="Clear view"
              onClick={() => {
                logs.clear();
                setCleared((c) => c + 1);
              }}
            >
              <IconEraser size={16} />
            </ActionIcon>
          </Tooltip>
          <Tooltip label="Download all logs">
            <ActionIcon
              component="a"
              href={download}
              download
              variant="subtle"
              color="gray"
              aria-label="Download logs"
            >
              <IconDownload size={16} />
            </ActionIcon>
          </Tooltip>
        </Group>
      </Group>

      <Group gap="xs" wrap="wrap">
        <TextInput
          size="xs"
          flex={1}
          miw={220}
          placeholder={regex ? "Regular expression" : "Filter text"}
          aria-label="Filter logs"
          value={filterText}
          onChange={(e) => setFilterText(e.currentTarget.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              if (e.shiftKey) term.current?.findPrevious();
              else term.current?.findNext();
            }
          }}
          error={compiled.error}
          rightSection={
            <Tooltip label="Regular expression">
              <ActionIcon
                size="sm"
                variant={regex ? "filled" : "subtle"}
                color={regex ? "blue" : "gray"}
                aria-label="Regular expression"
                aria-pressed={regex}
                onClick={() => setRegex((r) => !r)}
              >
                <IconRegex size={14} />
              </ActionIcon>
            </Tooltip>
          }
        />
        <Checkbox
          size="xs"
          label="Only matching lines"
          checked={onlyMatching}
          onChange={(e) => setOnlyMatching(e.currentTarget.checked)}
        />
        <ActionIcon
          variant="subtle"
          color="gray"
          aria-label="Previous match"
          disabled={!filter || !!compiled.error}
          onClick={() => term.current?.findPrevious()}
        >
          <IconChevronUp size={16} />
        </ActionIcon>
        <ActionIcon
          variant="subtle"
          color="gray"
          aria-label="Next match"
          disabled={!filter || !!compiled.error}
          onClick={() => term.current?.findNext()}
        >
          <IconChevronDown size={16} />
        </ActionIcon>
      </Group>

      {logs.error && (
        <Alert color={logs.error.code === "conflict" ? "gray" : "red"} title="Cannot read logs">
          {logs.error.message}
        </Alert>
      )}

      <Paper withBorder p={4} h="60vh" style={{ overflow: "hidden" }}>
        <Suspense fallback={<Loader aria-label="Loading viewer" />}>
          <Box h="100%">
            <LogTerminal
              ref={term}
              lines={lines}
              resetKey={resetKey}
              search={{ term: compiled.error ? "" : filter, regex }}
              dark={dark}
              label={`Logs of ${name}`}
            />
          </Box>
        </Suspense>
      </Paper>
      <Text size="xs" c="dimmed">
        {lines.length === logs.entries.length
          ? `${logs.entries.length} lines`
          : `${lines.length} of ${logs.entries.length} lines`}
        {logs.entries.length >= 10_000 && " (older lines dropped; download has all)"}
      </Text>
    </Stack>
  );
}
