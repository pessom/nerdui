// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Button,
  Checkbox,
  Code,
  CopyButton,
  Fieldset,
  Group,
  NumberInput,
  PasswordInput,
  Select,
  SimpleGrid,
  Stack,
  Text,
  TextInput,
  Tooltip,
} from "@mantine/core";
import { useDebouncedValue } from "@mantine/hooks";
import { IconCheck, IconCopy, IconPlus, IconTrash } from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent, type ReactNode } from "react";
import { api, unwrap } from "../../api/client";
import { keys } from "../../api/queries";
import { useJobsPanel } from "../jobs/JobsContext";
import {
  emptyForm,
  secretsMasked,
  toRequest,
  type CreateForm,
  type EnvRow,
  type LabelRow,
  type MountRow,
  type PortRow,
} from "./create";

/** Rows editor: one line per item with a remove button and an add button below. */
function Rows<T>({
  label,
  rows,
  onChange,
  empty,
  render,
}: {
  label: string;
  rows: T[];
  onChange: (rows: T[]) => void;
  empty: T;
  render: (row: T, set: (patch: Partial<T>) => void, i: number) => ReactNode;
}) {
  return (
    <Fieldset legend={label} variant="unstyled">
      <Stack gap={6}>
        {rows.map((r, i) => (
          <Group key={i} gap="xs" wrap="nowrap" align="flex-end">
            {render(
              r,
              (patch) => onChange(rows.map((x, j) => (j === i ? { ...x, ...patch } : x))),
              i,
            )}
            <ActionIcon
              variant="subtle"
              color="gray"
              aria-label={`Remove ${label.toLowerCase()} ${i + 1}`}
              onClick={() => onChange(rows.filter((_, j) => j !== i))}
              mb={4}
            >
              <IconTrash size={16} />
            </ActionIcon>
          </Group>
        ))}
        <Button
          size="xs"
          variant="subtle"
          leftSection={<IconPlus size={14} />}
          onClick={() => onChange([...rows, empty])}
          w="fit-content"
        >
          Add {label.toLowerCase().replace(/s$/, "")}
        </Button>
      </Stack>
    </Fieldset>
  );
}

/** Create container form (#17): typed fields, the equivalent nerdctl command, a job. */
export function CreateContainerForm({
  env,
  namespace,
  readOnly,
  onStarted,
}: {
  env: string;
  namespace: string;
  readOnly: boolean;
  onStarted: () => void;
}) {
  const queryClient = useQueryClient();
  const panel = useJobsPanel();
  const [form, setForm] = useState<CreateForm>(emptyForm);
  const set = (patch: Partial<CreateForm>) => setForm((f) => ({ ...f, ...patch }));
  const { request, error: parseError } = toRequest(form);
  const [debounced] = useDebouncedValue(JSON.stringify(request), 400);

  const preview = useQuery({
    queryKey: [...keys.env(env), "create-command", debounced],
    queryFn: () =>
      unwrap(
        api.POST("/api/v1/environments/{env}/containers/command", {
          params: { path: { env } },
          body: JSON.parse(debounced) as typeof request,
        }),
      ),
    enabled: !!request.image,
    retry: false,
  });
  const create = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/environments/{env}/containers", {
          params: { path: { env }, query: { namespace } },
          body: request,
        }),
      ),
    onSuccess: async ({ job }) => {
      await queryClient.invalidateQueries({ queryKey: keys.jobs });
      panel.open(job.id);
      onStarted();
    },
  });
  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (request.image && !parseError) create.mutate();
  };

  return (
    <form onSubmit={onSubmit}>
      <Stack>
        {readOnly && (
          <Alert color="orange">Read-only mode: the host nerdctl is incompatible.</Alert>
        )}
        <SimpleGrid cols={{ base: 1, sm: 2 }}>
          <TextInput
            label="Image"
            description="Pulled first when it is not on the host"
            placeholder="nginx:1.27"
            required
            value={form.image}
            onChange={(e) => set({ image: e.currentTarget.value })}
          />
          <TextInput
            label="Name"
            placeholder="random if empty"
            value={form.name}
            onChange={(e) => set({ name: e.currentTarget.value })}
          />
          <TextInput
            label="Command"
            description="Replaces the image's command; quotes group words"
            placeholder="image default"
            value={form.command}
            error={parseError}
            onChange={(e) => set({ command: e.currentTarget.value })}
          />
          <TextInput
            label="Entrypoint"
            placeholder="image default"
            value={form.entrypoint}
            onChange={(e) => set({ entrypoint: e.currentTarget.value })}
          />
        </SimpleGrid>

        <Rows<PortRow>
          label="Ports"
          rows={form.ports}
          onChange={(ports) => set({ ports })}
          empty={{ hostIp: "", hostPort: "", containerPort: "", protocol: "tcp" }}
          render={(p, upd, i) => (
            <>
              <TextInput
                size="xs"
                label={i === 0 ? "Host IP" : undefined}
                aria-label={`Host IP ${i + 1}`}
                placeholder="all"
                w={130}
                value={p.hostIp}
                onChange={(e) => upd({ hostIp: e.currentTarget.value })}
              />
              <TextInput
                size="xs"
                label={i === 0 ? "Host port" : undefined}
                aria-label={`Host port ${i + 1}`}
                placeholder="random"
                w={100}
                value={p.hostPort}
                onChange={(e) => upd({ hostPort: e.currentTarget.value })}
              />
              <TextInput
                size="xs"
                label={i === 0 ? "Container port" : undefined}
                aria-label={`Container port ${i + 1}`}
                w={110}
                value={p.containerPort}
                onChange={(e) => upd({ containerPort: e.currentTarget.value })}
              />
              <Select
                size="xs"
                label={i === 0 ? "Protocol" : undefined}
                aria-label={`Protocol ${i + 1}`}
                w={80}
                data={["tcp", "udp"]}
                value={p.protocol}
                onChange={(v) => upd({ protocol: (v ?? "tcp") as PortRow["protocol"] })}
                allowDeselect={false}
              />
            </>
          )}
        />

        <Rows<EnvRow>
          label="Environment variables"
          rows={form.env}
          onChange={(env) => set({ env })}
          empty={{ name: "", value: "", secret: false }}
          render={(e, upd, i) => (
            <>
              <TextInput
                size="xs"
                aria-label={`Variable name ${i + 1}`}
                placeholder="NAME"
                w={200}
                value={e.name}
                onChange={(ev) => upd({ name: ev.currentTarget.value })}
              />
              {e.secret ? (
                <PasswordInput
                  size="xs"
                  aria-label={`Variable value ${i + 1}`}
                  placeholder="value"
                  style={{ flex: 1 }}
                  value={e.value}
                  onChange={(ev) => upd({ value: ev.currentTarget.value })}
                />
              ) : (
                <TextInput
                  size="xs"
                  aria-label={`Variable value ${i + 1}`}
                  placeholder="value"
                  style={{ flex: 1 }}
                  value={e.value}
                  onChange={(ev) => upd({ value: ev.currentTarget.value })}
                />
              )}
              <Checkbox
                size="xs"
                label="Secret"
                mb={6}
                checked={e.secret}
                onChange={(ev) => upd({ secret: ev.currentTarget.checked })}
              />
            </>
          )}
        />

        <Rows<MountRow>
          label="Volumes and mounts"
          rows={form.mounts}
          onChange={(mounts) => set({ mounts })}
          empty={{ type: "volume", source: "", target: "", readOnly: false }}
          render={(m, upd, i) => (
            <>
              <Select
                size="xs"
                aria-label={`Mount type ${i + 1}`}
                w={100}
                data={["volume", "bind", "tmpfs"]}
                value={m.type}
                onChange={(v) => upd({ type: (v ?? "volume") as MountRow["type"] })}
                allowDeselect={false}
              />
              <TextInput
                size="xs"
                aria-label={`Mount source ${i + 1}`}
                placeholder={
                  m.type === "bind"
                    ? "/host/path"
                    : m.type === "volume"
                      ? "volume name (anonymous if empty)"
                      : "—"
                }
                disabled={m.type === "tmpfs"}
                style={{ flex: 1 }}
                value={m.source}
                onChange={(e) => upd({ source: e.currentTarget.value })}
              />
              <TextInput
                size="xs"
                aria-label={`Mount target ${i + 1}`}
                placeholder="/path/in/container"
                style={{ flex: 1 }}
                value={m.target}
                onChange={(e) => upd({ target: e.currentTarget.value })}
              />
              <Checkbox
                size="xs"
                label="Read-only"
                mb={6}
                checked={m.readOnly}
                onChange={(e) => upd({ readOnly: e.currentTarget.checked })}
              />
            </>
          )}
        />

        <SimpleGrid cols={{ base: 1, sm: 2, md: 4 }}>
          <TextInput
            label="Network"
            placeholder="bridge"
            value={form.network}
            onChange={(e) => set({ network: e.currentTarget.value })}
          />
          <TextInput
            label="Hostname"
            value={form.hostname}
            onChange={(e) => set({ hostname: e.currentTarget.value })}
          />
          <TextInput
            label="User"
            placeholder="image default"
            value={form.user}
            onChange={(e) => set({ user: e.currentTarget.value })}
          />
          <TextInput
            label="Working directory"
            placeholder="image default"
            value={form.workdir}
            onChange={(e) => set({ workdir: e.currentTarget.value })}
          />
          <NumberInput
            label="CPUs"
            placeholder="no limit"
            min={0}
            step={0.5}
            decimalScale={2}
            value={form.cpus}
            onChange={(v) => set({ cpus: String(v) })}
          />
          <NumberInput
            label="Memory (MiB)"
            placeholder="no limit"
            min={0}
            value={form.memoryMiB}
            onChange={(v) => set({ memoryMiB: String(v) })}
          />
          <Select
            label="Restart policy"
            data={["no", "always", "unless-stopped", "on-failure"]}
            value={form.restart}
            onChange={(v) => set({ restart: (v ?? "no") as CreateForm["restart"] })}
            allowDeselect={false}
          />
          {form.restart === "on-failure" && (
            <NumberInput
              label="Max retries"
              placeholder="unlimited"
              min={0}
              value={form.retries}
              onChange={(v) => set({ retries: String(v) })}
            />
          )}
        </SimpleGrid>

        <Rows<LabelRow>
          label="Labels"
          rows={form.labels}
          onChange={(labels) => set({ labels })}
          empty={{ key: "", value: "" }}
          render={(l, upd, i) => (
            <>
              <TextInput
                size="xs"
                aria-label={`Label key ${i + 1}`}
                placeholder="key"
                w={240}
                value={l.key}
                onChange={(e) => upd({ key: e.currentTarget.value })}
              />
              <TextInput
                size="xs"
                aria-label={`Label value ${i + 1}`}
                placeholder="value"
                style={{ flex: 1 }}
                value={l.value}
                onChange={(e) => upd({ value: e.currentTarget.value })}
              />
            </>
          )}
        />

        <Fieldset legend="Equivalent command" variant="unstyled">
          {!request.image ? (
            <Text size="sm" c="dimmed">
              Enter an image.
            </Text>
          ) : preview.isError ? (
            <Text size="sm" c="red" aria-label="Validation">
              {preview.error.message}
            </Text>
          ) : (
            <Group gap="xs" wrap="nowrap" align="flex-start">
              <Code block style={{ flex: 1, whiteSpace: "pre-wrap", wordBreak: "break-all" }}>
                {secretsMasked(preview.data?.command ?? "…", form.env)}
              </Code>
              {preview.data && (
                <CopyButton value={preview.data.command}>
                  {({ copied, copy }) => (
                    <Tooltip label={copied ? "Copied" : "Copy (with secret values)"}>
                      <ActionIcon
                        variant="subtle"
                        color="gray"
                        onClick={copy}
                        aria-label="Copy command"
                      >
                        {copied ? <IconCheck size={16} /> : <IconCopy size={16} />}
                      </ActionIcon>
                    </Tooltip>
                  )}
                </CopyButton>
              )}
            </Group>
          )}
        </Fieldset>

        {create.error && <Alert color="red">{create.error.message}</Alert>}
        <Group justify="space-between">
          <Checkbox
            label="Start the container after creating it"
            checked={form.start}
            onChange={(e) => set({ start: e.currentTarget.checked })}
          />
          <Button
            type="submit"
            loading={create.isPending}
            disabled={readOnly || !request.image || !!parseError || preview.isError}
          >
            {form.start ? "Create and start" : "Create"}
          </Button>
        </Group>
      </Stack>
    </form>
  );
}
