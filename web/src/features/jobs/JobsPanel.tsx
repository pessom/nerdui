// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Badge,
  Box,
  Button,
  Drawer,
  Group,
  Indicator,
  Loader,
  Modal,
  Paper,
  Stack,
  Text,
  Tooltip,
  UnstyledButton,
  useComputedColorScheme,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import {
  IconChevronDown,
  IconChevronRight,
  IconListCheck,
  IconMaximize,
  IconPlayerStop,
} from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Suspense, lazy, useCallback, useMemo, useState, type ReactNode } from "react";
import { api, unwrap } from "../../api/client";
import { jobsQuery, keys, type Job } from "../../api/queries";
import { timeAgo } from "../../lib/time";
import { JobsContext } from "./JobsContext";

const JobTerminal = lazy(() => import("./JobTerminal"));

const statusColor: Record<Job["status"], string> = {
  running: "blue",
  succeeded: "teal",
  failed: "red",
  canceled: "gray",
};

/**
 * Background jobs (#126): the header indicator with the number of running jobs and a panel
 * with recent jobs, their live output and cancellation. Provides useJobsPanel().
 */
export function JobsProvider({ children }: { children: ReactNode }) {
  const [opened, setOpened] = useState(false);
  const [expanded, setExpanded] = useState<string>();
  // The job whose output is open full screen; Escape then closes only that modal.
  const [fullScreen, setFullScreen] = useState<string>();
  const open = useCallback((jobId?: string) => {
    if (jobId) setExpanded(jobId);
    setOpened(true);
  }, []);
  const value = useMemo(() => ({ open }), [open]);
  return (
    <JobsContext.Provider value={value}>
      {children}
      <Drawer
        opened={opened}
        onClose={() => setOpened(false)}
        title="Jobs"
        position="right"
        size="xl"
        closeOnEscape={!fullScreen}
        // The expanded job takes the free height: the body is a column down to the bottom.
        styles={{
          content: { display: "flex", flexDirection: "column" },
          body: { flex: 1, minHeight: 0, display: "flex", flexDirection: "column" },
        }}
      >
        {opened && (
          <JobList
            expanded={expanded}
            setExpanded={setExpanded}
            fullScreen={fullScreen}
            setFullScreen={setFullScreen}
          />
        )}
      </Drawer>
    </JobsContext.Provider>
  );
}

/** Header button: running job count; opens the panel. */
export function JobsIndicator({ onOpen }: { onOpen: () => void }) {
  const jobs = useQuery(jobsQuery);
  const running = jobs.data?.filter((j) => j.status === "running").length ?? 0;
  return (
    <Tooltip label={running ? `${running} running` : "Jobs"}>
      <Indicator label={running} size={16} disabled={running === 0} offset={4}>
        <ActionIcon variant="default" size="lg" aria-label="Jobs" onClick={onOpen}>
          {running ? <Loader size={16} /> : <IconListCheck size={18} stroke={1.5} />}
        </ActionIcon>
      </Indicator>
    </Tooltip>
  );
}

function JobList({
  expanded,
  setExpanded,
  fullScreen,
  setFullScreen,
}: {
  expanded?: string;
  setExpanded: (id?: string) => void;
  fullScreen?: string;
  setFullScreen: (id?: string) => void;
}) {
  const jobs = useQuery(jobsQuery);
  if (jobs.isPending) return <Loader aria-label="Loading jobs" />;
  if (jobs.isError) return <Text c="red">{jobs.error.message}</Text>;
  if (jobs.data.length === 0)
    return (
      <Text c="dimmed" size="sm">
        No recent jobs. Pulls and other long operations appear here.
      </Text>
    );
  return (
    <Stack gap="xs" style={{ flex: 1, minHeight: 0, overflowY: "auto" }}>
      {jobs.data.map((j) => (
        <JobItem
          key={j.id}
          job={j}
          expanded={expanded === j.id}
          onToggle={() => setExpanded(expanded === j.id ? undefined : j.id)}
          fullScreen={fullScreen === j.id}
          setFullScreen={(on) => setFullScreen(on ? j.id : undefined)}
        />
      ))}
    </Stack>
  );
}

function JobItem({
  job,
  expanded,
  onToggle,
  fullScreen,
  setFullScreen,
}: {
  job: Job;
  expanded: boolean;
  onToggle: () => void;
  fullScreen: boolean;
  setFullScreen: (on: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const dark = useComputedColorScheme("light") === "dark";
  const navigate = useNavigate();
  const cancel = useMutation({
    mutationFn: () =>
      unwrap(api.POST("/api/v1/jobs/{id}/cancel", { params: { path: { id: job.id } } })),
    onError: (e) =>
      notifications.show({ color: "red", title: "Cannot cancel", message: e.message }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.jobs }),
  });
  // The stream reports the final state before the next poll does.
  const onJob = useCallback(
    (j: Job) => {
      if (j.status !== job.status) void queryClient.invalidateQueries({ queryKey: keys.jobs });
    },
    [job.status, queryClient],
  );
  const Chevron = expanded ? IconChevronDown : IconChevronRight;
  return (
    <Paper
      withBorder
      p="xs"
      component="section"
      aria-label={job.title}
      style={
        expanded
          ? { flex: "1 0 auto", minHeight: 320, display: "flex", flexDirection: "column" }
          : { flex: "none" }
      }
    >
      <Group justify="space-between" wrap="nowrap">
        <UnstyledButton
          onClick={onToggle}
          style={{ flex: 1, minWidth: 0 }}
          aria-expanded={expanded}
        >
          <Group gap="xs" wrap="nowrap">
            <Chevron size={16} />
            <Box style={{ minWidth: 0 }}>
              <Text size="sm" fw={500} truncate="end">
                {job.title}
              </Text>
              <Text size="xs" c="dimmed">
                {job.environment}
                {job.namespace && ` / ${job.namespace}`} · {timeAgo(job.startedAt)}
                {job.user && ` · ${job.user}`}
              </Text>
            </Box>
          </Group>
        </UnstyledButton>
        <Group gap="xs" wrap="nowrap">
          <Badge color={statusColor[job.status]} variant="light">
            {job.status}
          </Badge>
          {job.status === "running" && (
            <Button
              size="compact-xs"
              variant="default"
              leftSection={<IconPlayerStop size={12} />}
              loading={cancel.isPending}
              onClick={() => cancel.mutate()}
            >
              Cancel
            </Button>
          )}
          {expanded && (
            <Tooltip label="Full screen">
              <ActionIcon
                variant="subtle"
                color="gray"
                size="sm"
                aria-label="Full screen output"
                onClick={() => setFullScreen(true)}
              >
                <IconMaximize size={14} />
              </ActionIcon>
            </Tooltip>
          )}
          {job.result?.containerId && (
            <Button
              size="compact-xs"
              variant="light"
              onClick={() =>
                void navigate({
                  to: "/env/$env/containers/$id",
                  params: { env: job.environment, id: job.result?.containerId ?? "" },
                  search: { ns: job.namespace },
                })
              }
            >
              Open container
            </Button>
          )}
        </Group>
      </Group>
      {job.error && job.status === "failed" && (
        <Text size="xs" c="red" mt={4}>
          {job.error}
        </Text>
      )}
      {expanded && !fullScreen && (
        <Box mt="xs" style={{ flex: 1, minHeight: 0 }}>
          <Suspense fallback={<Loader aria-label="Loading output" />}>
            <JobTerminal jobId={job.id} dark={dark} onJob={onJob} />
          </Suspense>
        </Box>
      )}
      <Modal
        opened={fullScreen}
        onClose={() => setFullScreen(false)}
        title={job.title}
        fullScreen
        styles={{
          content: { display: "flex", flexDirection: "column" },
          body: { flex: 1, minHeight: 0 },
        }}
      >
        {fullScreen && (
          <Box h="100%">
            <Suspense fallback={<Loader aria-label="Loading output" />}>
              <JobTerminal jobId={job.id} dark={dark} onJob={onJob} />
            </Suspense>
          </Box>
        )}
      </Modal>
    </Paper>
  );
}
