// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Badge,
  Button,
  Code,
  Group,
  Loader,
  Modal,
  Paper,
  ScrollArea,
  Stack,
  Text,
  TextInput,
  UnstyledButton,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { IconFile, IconFilePlus, IconFolder, IconLock, IconTrash } from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Suspense, lazy, useEffect, useState } from "react";
import { ApiError, api, unwrap, unwrapEmpty } from "../../api/client";
import type { components } from "../../api/schema";
import { useCan } from "../../lib/permissions";

const StackFileEditor = lazy(() => import("./StackFileEditor"));

type Entry = components["schemas"]["StackTree"]["entries"][number];
type PathFile = components["schemas"]["StackPathFile"];

const treeKey = (env: string, name: string) => ["env", env, "stack-tree", name] as const;
const pathKey = (env: string, name: string, path: string) =>
  ["env", env, "stack-path", name, path] as const;

const editorKind = (path: string) => (/\.ya?ml$/i.test(path) ? "yaml" : "text");

const formatSize = (n: number) =>
  n < 1024
    ? `${n} B`
    : n < 1 << 20
      ? `${(n / 1024).toFixed(1)} KiB`
      : `${(n / (1 << 20)).toFixed(1)} MiB`;

/**
 * The files of a stack's directory (#27, ADR-0055): a tree, an editor for the selected file,
 * and creating and deleting files. The compose file and .env open in their own tabs, which
 * check them while typing; other files are saved, then the project is checked with them.
 */
export function StackFilesPanel({
  env,
  name,
  composeFile,
  onOpenTab,
}: {
  env: string;
  name: string;
  composeFile?: string;
  onOpenTab: (tab: "compose" | "env") => void;
}) {
  const queryClient = useQueryClient();
  const write = useCan()("stacks:write");
  const tree = useQuery({
    queryKey: treeKey(env, name),
    queryFn: () =>
      unwrap(
        api.GET("/api/v1/environments/{env}/stacks/{name}/tree", {
          params: { path: { env, name } },
        }),
      ),
    refetchOnWindowFocus: false,
  });
  const [selected, setSelected] = useState<string>();
  const [creating, setCreating] = useState(false);
  const refreshTree = () => void queryClient.invalidateQueries({ queryKey: treeKey(env, name) });

  if (tree.isPending) return <Loader aria-label="Loading files" />;
  if (tree.isError)
    return (
      <Alert color="red" title="Cannot list the files">
        {tree.error.message}
      </Alert>
    );
  const fromGit = tree.data.readOnly;
  const readOnly = fromGit || !write;
  const exists = selected && tree.data.entries.some((e) => e.path === selected && !e.dir);

  return (
    <Group align="flex-start" gap="md" wrap="nowrap">
      <Paper withBorder p="xs" w={280} style={{ flexShrink: 0 }}>
        <Stack gap="xs">
          <Group justify="space-between">
            <Text size="sm" fw={500}>
              Files
            </Text>
            {!readOnly && (
              <Button
                size="compact-xs"
                variant="default"
                leftSection={<IconFilePlus size={14} />}
                onClick={() => setCreating(true)}
              >
                New file
              </Button>
            )}
          </Group>
          <ScrollArea.Autosize mah="60vh">
            <nav aria-label="Stack files">
              {tree.data.entries.map((e) => (
                <TreeRow
                  key={e.path}
                  entry={e}
                  active={e.path === selected}
                  onSelect={() => setSelected(e.path)}
                />
              ))}
            </nav>
          </ScrollArea.Autosize>
          {tree.data.truncated && (
            <Text size="xs" c="dimmed">
              Only the first {tree.data.entries.length} entries are shown.
            </Text>
          )}
          {fromGit && (
            <Text size="xs" c="dimmed">
              The files come from the Git repository: change them there and pull.
            </Text>
          )}
        </Stack>
      </Paper>
      <div style={{ flex: 1, minWidth: 0 }}>
        {!selected || !exists ? (
          <Text size="sm" c="dimmed">
            Select a file to view or edit it.
          </Text>
        ) : selected === composeFile || selected === ".env" ? (
          <Alert color="blue" title={selected}>
            <Stack gap="xs" align="flex-start">
              <Text size="sm">This file has its own tab, which checks it while you type.</Text>
              <Button
                size="xs"
                variant="default"
                onClick={() => onOpenTab(selected === ".env" ? "env" : "compose")}
              >
                Open the {selected} tab
              </Button>
            </Stack>
          </Alert>
        ) : (
          <PathEditor
            key={selected}
            env={env}
            name={name}
            path={selected}
            readOnly={readOnly}
            onChanged={refreshTree}
            onDeleted={() => {
              setSelected(undefined);
              refreshTree();
            }}
          />
        )}
      </div>
      <NewFileDialog
        env={env}
        name={name}
        opened={creating}
        onClose={() => setCreating(false)}
        onCreated={(p) => {
          setCreating(false);
          refreshTree();
          setSelected(p);
        }}
      />
    </Group>
  );
}

function TreeRow({
  entry,
  active,
  onSelect,
}: {
  entry: Entry;
  active: boolean;
  onSelect: () => void;
}) {
  const parts = entry.path.split("/");
  const label = parts[parts.length - 1];
  const indent = (parts.length - 1) * 14;
  if (entry.dir)
    return (
      <Group gap={6} wrap="nowrap" pl={indent} py={2}>
        <IconFolder size={14} />
        <Text size="sm" c="dimmed" truncate>
          {label}
        </Text>
      </Group>
    );
  return (
    <UnstyledButton
      onClick={onSelect}
      aria-current={active ? "true" : undefined}
      w="100%"
      pl={indent}
      py={2}
      style={{
        borderRadius: "var(--mantine-radius-sm)",
        background: active ? "var(--mantine-color-default-hover)" : undefined,
      }}
    >
      <Group gap={6} wrap="nowrap">
        {entry.protected ? <IconLock size={14} /> : <IconFile size={14} />}
        <Text size="sm" ff="monospace" truncate>
          {label}
        </Text>
      </Group>
    </UnstyledButton>
  );
}

function PathEditor({
  env,
  name,
  path,
  readOnly,
  onChanged,
  onDeleted,
}: {
  env: string;
  name: string;
  path: string;
  readOnly: boolean;
  onChanged: () => void;
  onDeleted: () => void;
}) {
  const queryClient = useQueryClient();
  const file = useQuery({
    queryKey: pathKey(env, name, path),
    queryFn: () =>
      unwrap(
        api.GET("/api/v1/environments/{env}/stacks/{name}/tree/file", {
          params: { path: { env, name }, query: { path } },
        }),
      ),
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });
  const [draft, setDraft] = useState<string>();
  const [deleting, setDeleting] = useState(false);
  const [problem, setProblem] = useState<string>();
  const saved = file.data?.content ?? "";
  const text = draft ?? saved;
  const dirty = draft !== undefined && draft !== saved;

  useEffect(() => {
    if (!dirty) return;
    const warn = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);

  const save = useMutation({
    mutationFn: () =>
      unwrap(
        api.PUT("/api/v1/environments/{env}/stacks/{name}/tree/file", {
          params: { path: { env, name }, query: { path } },
          body: { content: text, version: file.data?.version ?? "" },
        }),
      ),
    onSuccess: (r) => {
      queryClient.setQueryData<PathFile>(pathKey(env, name, path), r.file);
      setDraft(undefined);
      setProblem(r.validation.valid ? undefined : (r.validation.error ?? "invalid"));
      notifications.show({ color: "teal", message: `${path} saved` });
      onChanged();
    },
    onError: (e) =>
      notifications.show({
        color: "red",
        title: "Cannot save",
        message:
          e instanceof ApiError && e.status === 409 && /changed since/.test(e.message)
            ? `${e.message}. Reload to see it; your edits are lost.`
            : e.message,
      }),
  });
  const remove = useMutation({
    mutationFn: () =>
      unwrapEmpty(
        api.DELETE("/api/v1/environments/{env}/stacks/{name}/tree/file", {
          params: { path: { env, name }, query: { path, version: file.data?.version ?? "" } },
        }),
      ),
    onSuccess: () => {
      notifications.show({ message: `${path} deleted` });
      queryClient.removeQueries({ queryKey: pathKey(env, name, path) });
      onDeleted();
    },
  });

  if (file.isPending) return <Loader aria-label="Loading file" />;
  if (file.isError)
    return (
      <Alert color="red" title="Cannot read the file">
        {file.error.message}
      </Alert>
    );
  const f = file.data;
  const editable = !readOnly && !f.binary && !f.tooBig;

  return (
    <Stack gap="sm" component="section" aria-label={`File ${path}`}>
      <Group justify="space-between">
        <Group gap="xs">
          <Text size="sm" ff="monospace">
            {path}
          </Text>
          <Text size="xs" c="dimmed">
            {formatSize(f.size)}
          </Text>
          {dirty && (
            <Badge size="sm" color="yellow" variant="light">
              unsaved changes
            </Badge>
          )}
        </Group>
        {!readOnly && (
          <Group gap="xs">
            <Button
              variant="light"
              color="red"
              leftSection={<IconTrash size={16} />}
              onClick={() => setDeleting(true)}
            >
              Delete
            </Button>
            {editable && (
              <>
                <Button variant="default" disabled={!dirty} onClick={() => setDraft(undefined)}>
                  Revert
                </Button>
                <Button disabled={!dirty} loading={save.isPending} onClick={() => save.mutate()}>
                  Save
                </Button>
              </>
            )}
          </Group>
        )}
      </Group>
      {problem && (
        <Alert color="orange" title="Saved, but the project does not load with it">
          <Text size="sm" style={{ whiteSpace: "pre-wrap" }}>
            {problem}
          </Text>
        </Alert>
      )}
      {f.binary || f.tooBig ? (
        <Alert color="gray" title={f.binary ? "Binary file" : "Large file"}>
          {f.binary ? "It is not shown here." : "Files over 1 MiB are not shown here."}
        </Alert>
      ) : (
        <Suspense fallback={<Loader aria-label="Loading editor" />}>
          <StackFileEditor
            value={text}
            onChange={setDraft}
            kind={editorKind(path)}
            label={`${path} of ${name}`}
            readOnly={!editable}
          />
        </Suspense>
      )}
      <Modal opened={deleting} onClose={() => setDeleting(false)} title={`Delete ${path}`}>
        <Stack gap="sm">
          <Text size="sm">
            <Code>{path}</Code> is deleted from the stack&apos;s directory. There is no backup.
          </Text>
          {remove.isError && (
            <Alert color="red" title="Cannot delete the file">
              {remove.error.message}
            </Alert>
          )}
          <Group justify="flex-end">
            <Button variant="default" onClick={() => setDeleting(false)}>
              Cancel
            </Button>
            <Button color="red" loading={remove.isPending} onClick={() => remove.mutate()}>
              Delete file
            </Button>
          </Group>
        </Stack>
      </Modal>
    </Stack>
  );
}

function NewFileDialog({
  env,
  name,
  opened,
  onClose,
  onCreated,
}: {
  env: string;
  name: string;
  opened: boolean;
  onClose: () => void;
  onCreated: (path: string) => void;
}) {
  const [path, setPath] = useState("");
  const create = useMutation({
    mutationFn: (p: string) =>
      unwrap(
        api.PUT("/api/v1/environments/{env}/stacks/{name}/tree/file", {
          params: { path: { env, name }, query: { path: p } },
          body: { content: "", version: "" },
        }),
      ),
    onSuccess: (r) => {
      setPath("");
      create.reset();
      onCreated(r.file.path);
    },
  });
  const close = () => {
    setPath("");
    create.reset();
    onClose();
  };
  const p = path.trim();
  return (
    <Modal opened={opened} onClose={close} title="New file">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate(p);
        }}
      >
        <Stack gap="sm">
          <TextInput
            label="Path in the stack's directory"
            description="Folders are created as needed, e.g. config/nginx.conf or compose.override.yaml"
            placeholder="config/app.conf"
            data-autofocus
            value={path}
            onChange={(e) => setPath(e.currentTarget.value)}
          />
          {create.isError && (
            <Alert color="red" title="Cannot create the file">
              {create.error.message}
            </Alert>
          )}
          <Group justify="flex-end">
            <Button variant="default" onClick={close}>
              Cancel
            </Button>
            <Button type="submit" loading={create.isPending} disabled={!p}>
              Create
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}
