// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Anchor,
  Button,
  Checkbox,
  Code,
  Collapse,
  Group,
  Loader,
  Modal,
  PasswordInput,
  ScrollArea,
  SegmentedControl,
  Stack,
  Table,
  Text,
  TextInput,
} from "@mantine/core";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "../../api/client";
import { keys } from "../../api/queries";
import { useCan } from "../../lib/permissions";
import { fromRequest } from "../containers/create";
import { setContainerDraft } from "../containers/draft";
import { useJobsPanel } from "../jobs/JobsContext";
import {
  renderEnv,
  stackName,
  templateQuery,
  type Template,
  type TemplateSummary,
} from "./templates";

/**
 * Deploys a template as a new stack (#33): fills its variables, secrets become variables. A
 * template with one service can instead fill in the new container form (ADR-0059).
 */
export function DeployTemplateDialog({
  env,
  namespace,
  template,
  onClose,
}: {
  env: string;
  namespace: string;
  template: TemplateSummary | null;
  onClose: () => void;
}) {
  return (
    <Modal
      opened={!!template}
      onClose={onClose}
      size="lg"
      title={template ? `Deploy ${template.name}` : "Deploy"}
    >
      {template && <Loaded env={env} namespace={namespace} summary={template} onClose={onClose} />}
    </Modal>
  );
}

function Loaded({
  env,
  namespace,
  summary,
  onClose,
}: {
  env: string;
  namespace: string;
  summary: TemplateSummary;
  onClose: () => void;
}) {
  const detail = useQuery(templateQuery(summary.source, summary.id));
  if (detail.isPending) return <Loader aria-label="Loading the template" />;
  if (detail.isError)
    return (
      <Alert color="red" title="Cannot read the template">
        {detail.error.message}
      </Alert>
    );
  return (
    <Form
      env={env}
      namespace={namespace}
      template={detail.data}
      summary={summary}
      onClose={onClose}
    />
  );
}

function Form({
  env,
  namespace,
  template,
  summary,
  onClose,
}: {
  env: string;
  namespace: string;
  template: Template;
  summary: TemplateSummary;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const jobs = useJobsPanel();
  const [name, setName] = useState(() => stackName(summary));
  const [values, setValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(template.variables.map((v) => [v.name, v.default])),
  );
  const [secret, setSecret] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(template.variables.map((v) => [v.name, v.secret])),
  );
  const [deploy, setDeploy] = useState(true);
  const [showCompose, setShowCompose] = useState(false);
  const can = useCan();
  const canContainer = (template.services ?? []).length === 1 && can("containers:write");
  const canStack = can("stacks:write");
  const [as, setAs] = useState<"stack" | "container">(canStack ? "stack" : "container");
  const container = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/templates/container", {
          body: { compose: template.compose, values, name },
        }),
      ),
    onSuccess: (c) => {
      setContainerDraft({
        form: fromRequest(c.spec),
        origin: `template ${template.name}`,
        unsupported: c.unsupported,
      });
      onClose();
      void navigate({ to: "/env/$env/containers/new", params: { env }, search: { ns: namespace } });
    },
  });
  const create = useMutation({
    mutationFn: async () => {
      const stack = await unwrap(
        api.POST("/api/v1/environments/{env}/stacks", {
          params: { path: { env }, query: { namespace } },
          body: {
            name,
            compose: template.compose,
            env: renderEnv(template.env, values, secret),
            variables: template.variables
              .filter((v) => secret[v.name] && (values[v.name] ?? "") !== "")
              .map((v) => ({ name: v.name, value: values[v.name] ?? "", secret: true })),
          },
        }),
      );
      if (!deploy) return { stack, job: undefined };
      const { job } = await unwrap(
        api.POST("/api/v1/environments/{env}/stacks/{name}/{op}", {
          params: { path: { env, name: stack.name, op: "up" }, query: { namespace } },
          body: {},
        }),
      );
      return { stack, job };
    },
    onSuccess: ({ stack, job }) => {
      void queryClient.invalidateQueries({ queryKey: keys.stacks(env, namespace) });
      onClose();
      if (job) jobs.open(job.id);
      void navigate({
        to: "/env/$env/stacks/$name",
        params: { env, name: stack.name },
        search: { ns: namespace },
      });
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (as === "container") container.mutate();
    else create.mutate();
  };
  const failed = as === "container" ? container.error : create.error;
  if (!canStack && !canContainer)
    return (
      <Alert color="gray" title="Cannot deploy this template">
        Your role may create containers, not stacks, and this template has{" "}
        {(template.services ?? []).length} services: only a template with one service can start a
        container.
      </Alert>
    );
  return (
    <form onSubmit={submit}>
      <Stack>
        {template.description && <Text size="sm">{template.description}</Text>}
        {template.documentation && (
          <Anchor href={template.documentation} target="_blank" rel="noreferrer" size="sm">
            Documentation
          </Anchor>
        )}
        {failed && (
          <Alert color="red" title="Cannot deploy">
            {failed.message}
          </Alert>
        )}
        {canContainer && canStack && (
          <SegmentedControl
            aria-label="Deploy as"
            value={as}
            onChange={(v) => setAs(v as "stack" | "container")}
            data={[
              { value: "stack", label: "Stack" },
              { value: "container", label: "Container" },
            ]}
          />
        )}
        <TextInput
          label={as === "container" ? "Container name" : "Stack name"}
          description={`In namespace ${namespace}`}
          required
          value={name}
          onChange={(e) => setName(e.currentTarget.value)}
        />
        {template.variables.length > 0 && (
          <Table verticalSpacing={6} fz="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Variable</Table.Th>
                <Table.Th>Value</Table.Th>
                {as === "stack" && <Table.Th>Secret</Table.Th>}
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {template.variables.map((v) => {
                const props = {
                  "aria-label": v.name,
                  value: values[v.name] ?? "",
                  onChange: (e: React.ChangeEvent<HTMLInputElement>) => {
                    const value = e.currentTarget.value;
                    setValues((x) => ({ ...x, [v.name]: value }));
                  },
                  size: "xs" as const,
                };
                return (
                  <Table.Tr key={v.name}>
                    <Table.Td>
                      <Code>{v.name}</Code>
                      {v.description && (
                        <Text size="xs" c="dimmed">
                          {v.description}
                        </Text>
                      )}
                    </Table.Td>
                    <Table.Td miw={180}>
                      {secret[v.name] ? (
                        <PasswordInput {...props} autoComplete="new-password" />
                      ) : (
                        <TextInput {...props} />
                      )}
                    </Table.Td>
                    <Table.Td display={as === "stack" ? undefined : "none"}>
                      <Checkbox
                        aria-label={`${v.name} is secret`}
                        checked={!!secret[v.name]}
                        onChange={(e) => {
                          const checked = e.currentTarget.checked;
                          setSecret((x) => ({ ...x, [v.name]: checked }));
                        }}
                      />
                    </Table.Td>
                  </Table.Tr>
                );
              })}
            </Table.Tbody>
          </Table>
        )}
        <Text size="xs" c="dimmed">
          {as === "container"
            ? "The values fill in the new container form, where you review every setting before creating it."
            : "Values go to the stack's .env; secrets are stored encrypted as variables of the stack and never shown again."}
        </Text>
        <Anchor
          component="button"
          type="button"
          size="sm"
          onClick={() => setShowCompose((x) => !x)}
        >
          {showCompose ? "Hide" : "Show"} the compose file
        </Anchor>
        <Collapse expanded={showCompose}>
          <ScrollArea.Autosize mah={300}>
            <Code block>{template.compose}</Code>
          </ScrollArea.Autosize>
        </Collapse>
        {as === "stack" && (
          <Checkbox
            label="Deploy now (nerdctl compose up)"
            checked={deploy}
            onChange={(e) => setDeploy(e.currentTarget.checked)}
          />
        )}
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={create.isPending || container.isPending}>
            {as === "container" ? "Continue" : deploy ? "Create and deploy" : "Create stack"}
          </Button>
        </Group>
      </Stack>
    </form>
  );
}
