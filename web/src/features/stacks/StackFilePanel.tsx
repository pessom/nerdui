// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Badge, Button, Group, Loader, Modal, Stack, Text } from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { IconCheck, IconX } from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Suspense, lazy, useEffect, useState } from "react";
import { ApiError, api, unwrap } from "../../api/client";
import { keys } from "../../api/queries";
import { useJobsPanel } from "../jobs/JobsContext";
import { errorLine, type Schema } from "./composeSchema";

const StackFileEditor = lazy(() => import("./StackFileEditor"));
const StackFileDiff = lazy(() => import("./StackFileDiff"));

type Kind = "compose" | "env";

const fileKey = (env: string, name: string, kind: Kind) =>
  ["env", env, "stack-file", name, kind] as const;

/** The Compose schema of the server's build, for completion (#26). */
const schemaQuery = {
  queryKey: ["compose-schema"] as const,
  queryFn: async () => (await unwrap(api.GET("/api/v1/compose-schema"))) as Schema,
  staleTime: Infinity,
};

/**
 * Edits a stack's compose or .env file (#26): checked on the server while typing, saved after
 * reviewing the diff, optionally deployed right away. A file changed on disk meanwhile is not
 * overwritten.
 */
export function StackFilePanel({
  env,
  namespace,
  name,
  kind,
  readOnly,
}: {
  env: string;
  namespace: string;
  name: string;
  kind: Kind;
  /** Deploy needs a compatible nerdctl; saving does not. */
  readOnly: boolean;
}) {
  const queryClient = useQueryClient();
  const jobs = useJobsPanel();
  const file = useQuery({
    queryKey: fileKey(env, name, kind),
    queryFn: () =>
      unwrap(
        api.GET("/api/v1/environments/{env}/stacks/{name}/files/{file}", {
          params: { path: { env, name, file: kind } },
        }),
      ),
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    refetchInterval: false, // never under the user's edits (#90)
  });
  const schema = useQuery({ ...schemaQuery, enabled: kind === "compose" });
  const [draft, setDraft] = useState<string>();
  const [reviewing, setReviewing] = useState(false);
  const [conflict, setConflict] = useState<string>();
  const saved = file.data?.content ?? "";
  const text = draft ?? saved;
  const dirty = draft !== undefined && draft !== saved;

  // Check the edited content a moment after typing stops.
  const validate = useMutation({
    mutationFn: (content: string) =>
      unwrap(
        api.POST("/api/v1/environments/{env}/stacks/{name}/validate", {
          params: { path: { env, name } },
          body: kind === "compose" ? { compose: content } : { env: content },
        }),
      ),
  });
  const { mutate: check } = validate;
  useEffect(() => {
    if (!file.data) return;
    const t = setTimeout(() => check(text), 600);
    return () => clearTimeout(t);
  }, [text, file.data, check]);

  // Leaving the page with unsaved changes asks first.
  useEffect(() => {
    if (!dirty) return;
    const warn = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);

  const save = useMutation({
    mutationFn: async (deploy: boolean) => {
      const f = await unwrap(
        api.PUT("/api/v1/environments/{env}/stacks/{name}/files/{file}", {
          params: { path: { env, name, file: kind } },
          body: { content: text, version: file.data?.version ?? "" },
        }),
      );
      queryClient.setQueryData(fileKey(env, name, kind), f);
      setDraft(undefined);
      if (!deploy) return undefined;
      return unwrap(
        api.POST("/api/v1/environments/{env}/stacks/{name}/{op}", {
          params: { path: { env, name, op: "up" }, query: { namespace } },
          body: {},
        }),
      );
    },
    onSuccess: (deployed) => {
      setReviewing(false);
      notifications.show({ color: "teal", message: `${file.data?.name ?? kind} saved` });
      void queryClient.invalidateQueries({ queryKey: keys.stacks(env, namespace) });
      if (deployed) {
        void queryClient.invalidateQueries({ queryKey: keys.jobs });
        jobs.open(deployed.job.id);
      }
    },
    onError: (e) => {
      setReviewing(false);
      if (e instanceof ApiError && e.status === 409 && /changed since/.test(e.message)) {
        setConflict(e.message);
      } else {
        notifications.show({ color: "red", title: "Cannot save", message: e.message });
      }
    },
  });

  const reload = async () => {
    setConflict(undefined);
    setDraft(undefined);
    await file.refetch();
  };

  if (file.isPending) return <Loader aria-label="Loading file" />;
  if (file.isError)
    return (
      <Alert color="red" title="Cannot read the file">
        {file.error.message}
      </Alert>
    );

  // The result applies only to the text it was computed for.
  const result = validate.variables === text ? validate.data : undefined;
  const invalid = result !== undefined && !result.valid;
  const checkFailed = validate.isError && validate.variables === text;
  const error = invalid
    ? {
        line: kind === "compose" ? errorLine(text, result.error ?? "", result.line) : undefined,
        message: result.error ?? "invalid",
      }
    : undefined;

  return (
    <Stack gap="sm">
      <Group justify="space-between">
        <Group gap="xs">
          <Text size="sm" ff="monospace">
            {file.data.name}
          </Text>
          {!file.data.exists && (
            <Badge size="sm" variant="outline" color="gray">
              new file
            </Badge>
          )}
          {dirty && (
            <Badge size="sm" color="yellow" variant="light">
              unsaved changes
            </Badge>
          )}
          {!result && !checkFailed && <Loader size="xs" aria-label="Checking" />}
          {result?.valid && (
            <Badge size="sm" color="teal" variant="light" leftSection={<IconCheck size={12} />}>
              valid · {result.services.length} service{result.services.length === 1 ? "" : "s"}
            </Badge>
          )}
          {invalid && (
            <Badge size="sm" color="red" variant="light" leftSection={<IconX size={12} />}>
              invalid
            </Badge>
          )}
        </Group>
        <Group gap="xs">
          <Button variant="default" disabled={!dirty} onClick={() => setDraft(undefined)}>
            Revert
          </Button>
          <Button disabled={!dirty || !result?.valid} onClick={() => setReviewing(true)}>
            Review and save
          </Button>
        </Group>
      </Group>
      {conflict && (
        <Alert color="orange" title="The file changed on disk">
          <Stack gap="xs" align="flex-start">
            <Text size="sm">{conflict}. Reloading discards your edits.</Text>
            <Button size="xs" variant="default" onClick={() => void reload()}>
              Reload from disk
            </Button>
          </Stack>
        </Alert>
      )}
      {checkFailed && (
        <Alert color="red" title="Cannot check the file">
          {validate.error.message}
        </Alert>
      )}
      {invalid && (
        <Alert color="red" title={error?.line ? `Line ${error.line}` : "The project does not load"}>
          <Text size="sm" style={{ whiteSpace: "pre-wrap" }}>
            {result.error}
          </Text>
        </Alert>
      )}
      <Suspense fallback={<Loader aria-label="Loading editor" />}>
        <StackFileEditor
          value={text}
          onChange={setDraft}
          kind={kind}
          schema={schema.data}
          error={error}
          label={`${file.data.name} of ${name}`}
        />
      </Suspense>
      {kind === "compose" && (
        <Text size="xs" c="dimmed">
          Ctrl+Space suggests keys from the Compose schema. The project is checked as nerdctl
          compose loads it, with the stack&apos;s .env and override file.
        </Text>
      )}
      <Modal
        opened={reviewing}
        onClose={() => setReviewing(false)}
        title={`Save ${file.data.name}`}
        size="90%"
      >
        <Stack gap="sm">
          <Text size="sm" c="dimmed">
            Saved on the left, edited on the right. The current file is kept as {file.data.name}
            .bak.
          </Text>
          {reviewing && (
            <Suspense fallback={<Loader aria-label="Loading diff" />}>
              <StackFileDiff original={saved} modified={text} yamlSyntax={kind === "compose"} />
            </Suspense>
          )}
          <Group justify="flex-end">
            <Button variant="default" onClick={() => setReviewing(false)}>
              Cancel
            </Button>
            <Button
              variant="default"
              loading={save.isPending && save.variables === false}
              onClick={() => save.mutate(false)}
            >
              Save
            </Button>
            <Button
              disabled={readOnly}
              loading={save.isPending && save.variables === true}
              onClick={() => save.mutate(true)}
            >
              Save and deploy
            </Button>
          </Group>
        </Stack>
      </Modal>
    </Stack>
  );
}
