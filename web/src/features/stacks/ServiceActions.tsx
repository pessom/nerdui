// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { ActionIcon, Menu } from "@mantine/core";
import { notifications } from "@mantine/notifications";
import {
  IconDots,
  IconDownload,
  IconPlayerPlay,
  IconPlayerStop,
  IconRefresh,
} from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "../../api/client";
import { keys } from "../../api/queries";
import { useJobsPanel } from "../jobs/JobsContext";

type Op = "start" | "stop" | "restart" | "redeploy";

/**
 * Operations on one service of a stack (#29, ADR-0042): they leave the other services alone,
 * dependencies included.
 */
export function ServiceActions({
  env,
  namespace,
  stack,
  service,
  running,
  containers,
  composeFile,
}: {
  env: string;
  namespace: string;
  stack: string;
  service: string;
  running: boolean;
  /** The service has containers. */
  containers: boolean;
  /** The stack has a compose file: needed to create and redeploy. */
  composeFile: boolean;
}) {
  const queryClient = useQueryClient();
  const jobs = useJobsPanel();
  const run = useMutation({
    mutationFn: (op: Op) =>
      unwrap(
        api.POST("/api/v1/environments/{env}/stacks/{name}/services/{service}/{op}", {
          params: { path: { env, name: stack, service, op }, query: { namespace } },
        }),
      ),
    onSuccess: ({ job }) => {
      void queryClient.invalidateQueries({ queryKey: keys.jobs });
      void queryClient.invalidateQueries({ queryKey: keys.stacks(env, namespace) });
      jobs.open(job.id);
    },
    onError: (e) =>
      notifications.show({ color: "red", title: `Cannot run on ${service}`, message: e.message }),
  });

  return (
    <Menu position="bottom-end" withinPortal>
      <Menu.Target>
        <ActionIcon
          variant="subtle"
          size="sm"
          aria-label={`Actions of service ${service}`}
          loading={run.isPending}
        >
          <IconDots size={16} />
        </ActionIcon>
      </Menu.Target>
      <Menu.Dropdown>
        {running ? (
          <Menu.Item leftSection={<IconPlayerStop size={14} />} onClick={() => run.mutate("stop")}>
            Stop
          </Menu.Item>
        ) : (
          <Menu.Item
            leftSection={<IconPlayerPlay size={14} />}
            disabled={!containers && !composeFile}
            onClick={() => run.mutate("start")}
          >
            {containers ? "Start" : "Create and start"}
          </Menu.Item>
        )}
        <Menu.Item
          leftSection={<IconRefresh size={14} />}
          disabled={!containers}
          onClick={() => run.mutate("restart")}
        >
          Restart
        </Menu.Item>
        <Menu.Item
          leftSection={<IconDownload size={14} />}
          disabled={!composeFile}
          onClick={() => run.mutate("redeploy")}
        >
          Pull image and recreate
        </Menu.Item>
      </Menu.Dropdown>
    </Menu>
  );
}
