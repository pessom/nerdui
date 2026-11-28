// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Button,
  Checkbox,
  FileInput,
  Group,
  Modal,
  Progress,
  SegmentedControl,
  Select,
  Stack,
  Text,
  TextInput,
  Textarea,
} from "@mantine/core";
import { IconFileZip } from "@tabler/icons-react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useRef, useState, type FormEvent } from "react";
import { api, unwrap } from "../../api/client";
import { gitCredentialsQuery } from "../../api/queries";
import { useCan } from "../../lib/permissions";
import { useJobsPanel } from "../jobs/JobsContext";
import { isSSH } from "../stacks/gitUrl";
import { parseArgs, parseTags, uploadBuild, type BuildRequest } from "./build";

type Source = BuildRequest["source"];

/**
 * Builds an image with BuildKit (#45, ADR-0061) from a Dockerfile, a Git repository or an
 * uploaded context archive; the build runs as a job whose log opens in the jobs panel.
 */
export function BuildImageDialog({
  env,
  namespace,
  opened,
  onClose,
}: {
  env: string;
  namespace: string;
  opened: boolean;
  onClose: () => void;
}) {
  const jobs = useJobsPanel();
  const can = useCan();
  const builder = useQuery({
    queryKey: ["images", "builder", env, namespace],
    queryFn: () =>
      unwrap(
        api.GET("/api/v1/environments/{env}/images/builder", {
          params: { path: { env }, query: { namespace } },
        }),
      ),
    enabled: opened,
    staleTime: 30_000,
  });
  const credentials = useQuery({
    ...gitCredentialsQuery,
    enabled: opened && can("settings:read"),
  });
  const [source, setSource] = useState<Source>("dockerfile");
  const [dockerfile, setDockerfile] = useState("");
  const [url, setUrl] = useState("");
  const [ref, setRef] = useState("");
  const [context, setContext] = useState("");
  const [file, setFile] = useState("");
  const [credential, setCredential] = useState<string | null>(null);
  const [archive, setArchive] = useState<File | null>(null);
  const [tags, setTags] = useState("");
  const [argsText, setArgsText] = useState("");
  const [target, setTarget] = useState("");
  const [noCache, setNoCache] = useState(false);
  const [pull, setPull] = useState(false);
  const [push, setPush] = useState(false);
  const [progress, setProgress] = useState(0);
  const abort = useRef<AbortController>(undefined);

  const parsed = parseArgs(argsText);
  const tagList = parseTags(tags);
  const request = (): BuildRequest => ({
    source,
    tags: tagList,
    args: Object.keys(parsed.args).length ? parsed.args : undefined,
    target: target.trim() || undefined,
    noCache,
    pull,
    push,
    ...(source === "dockerfile" ? { dockerfile } : {}),
    ...(source === "git"
      ? {
          url: url.trim(),
          ref: ref.trim() || undefined,
          context: context.trim() || undefined,
          credentialId: credential ? Number(credential) : undefined,
        }
      : {}),
    ...(source !== "dockerfile" && file.trim() ? { file: file.trim() } : {}),
  });

  const build = useMutation({
    mutationFn: async () => {
      if (source === "archive") {
        if (!archive) throw new Error("Choose an archive");
        abort.current = new AbortController();
        return uploadBuild(env, namespace, request(), archive, setProgress, abort.current.signal);
      }
      const { job } = await unwrap(
        api.POST("/api/v1/environments/{env}/images/build", {
          params: { path: { env }, query: { namespace } },
          body: request(),
        }),
      );
      return job;
    },
    onSuccess: (job) => {
      jobs.open(job.id);
      close();
    },
  });

  const close = () => {
    abort.current?.abort();
    setProgress(0);
    build.reset();
    onClose();
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    build.mutate();
  };

  const ssh = isSSH(url.trim());
  const credentialOptions = (credentials.data?.credentials ?? [])
    .filter((c) => (ssh ? c.kind === "ssh" : c.kind === "token"))
    .map((c) => ({ value: String(c.id), label: c.name }));
  const ready =
    tagList.length > 0 &&
    !parsed.bad &&
    (source === "dockerfile"
      ? dockerfile.trim() !== ""
      : source === "git"
        ? url.trim() !== ""
        : archive !== null);

  return (
    <Modal opened={opened} onClose={close} title="Build image" size="lg">
      <form onSubmit={submit}>
        <Stack gap="sm">
          {builder.data && !builder.data.available && (
            <Alert color="yellow" title="BuildKit is not available">
              Building needs buildctl and a running buildkitd on the host (for example{" "}
              <code>systemctl enable --now buildkit</code> with nerdctl-full). {builder.data.error}
            </Alert>
          )}
          <SegmentedControl
            aria-label="Source"
            value={source}
            onChange={(v) => setSource(v as Source)}
            data={[
              { value: "dockerfile", label: "Dockerfile" },
              { value: "git", label: "Git repository" },
              { value: "archive", label: "Archive" },
            ]}
          />
          {source === "dockerfile" && (
            <Textarea
              label="Dockerfile"
              description="Built with an empty context: COPY and ADD can only use URLs"
              placeholder={"FROM alpine:3\nRUN apk add --no-cache curl"}
              required
              autosize
              minRows={8}
              maxRows={20}
              styles={{ input: { fontFamily: "var(--mantine-font-family-monospace)" } }}
              value={dockerfile}
              onChange={(e) => setDockerfile(e.currentTarget.value)}
            />
          )}
          {source === "git" && (
            <>
              <TextInput
                label="Repository URL"
                placeholder="https://github.com/owner/repo.git"
                required
                value={url}
                onChange={(e) => {
                  setUrl(e.currentTarget.value);
                  setCredential(null);
                }}
              />
              <Group grow align="flex-start">
                <TextInput
                  label="Branch or tag"
                  description="Empty: the default branch"
                  value={ref}
                  onChange={(e) => setRef(e.currentTarget.value)}
                />
                <TextInput
                  label="Context"
                  description="Directory in the repository; empty: the root"
                  value={context}
                  onChange={(e) => setContext(e.currentTarget.value)}
                />
              </Group>
              <Select
                label="Credential"
                description="Needed for private repositories (Settings → Git credentials)"
                placeholder={
                  credentialOptions.length ? "None (public repository)" : "No matching credentials"
                }
                data={credentialOptions}
                value={credential}
                onChange={setCredential}
                clearable
              />
            </>
          )}
          {source === "archive" && (
            <FileInput
              label="Context archive"
              description="A tar or tar.gz of the build context"
              placeholder="Choose a .tar or .tar.gz file"
              accept=".tar,.tar.gz,.tgz,application/x-tar,application/gzip"
              leftSection={<IconFileZip size={16} />}
              value={archive}
              onChange={setArchive}
              clearable
              required
            />
          )}
          {source !== "dockerfile" && (
            <TextInput
              label="Dockerfile path"
              description="In the context; empty: Dockerfile"
              value={file}
              onChange={(e) => setFile(e.currentTarget.value)}
            />
          )}
          <TextInput
            label="Tags"
            description="One or more, separated by spaces or commas"
            placeholder="registry.example.com/app:1.0 app:latest"
            required
            value={tags}
            onChange={(e) => setTags(e.currentTarget.value)}
          />
          <Group grow align="flex-start">
            <Textarea
              label="Build arguments"
              description="NAME=value per line; they stay visible in the image history"
              autosize
              minRows={2}
              styles={{ input: { fontFamily: "var(--mantine-font-family-monospace)" } }}
              value={argsText}
              onChange={(e) => setArgsText(e.currentTarget.value)}
              error={parsed.bad ? `Not NAME=value: ${parsed.bad}` : undefined}
            />
            <TextInput
              label="Target"
              description="Stage to build; empty: the last"
              value={target}
              onChange={(e) => setTarget(e.currentTarget.value)}
            />
          </Group>
          <Group>
            <Checkbox
              label="No cache"
              checked={noCache}
              onChange={(e) => setNoCache(e.currentTarget.checked)}
            />
            <Checkbox
              label="Pull base images"
              checked={pull}
              onChange={(e) => setPull(e.currentTarget.checked)}
            />
            <Checkbox
              label="Push after the build"
              checked={push}
              onChange={(e) => setPush(e.currentTarget.checked)}
            />
          </Group>
          {build.isPending && source === "archive" && (
            <Progress value={progress * 100} aria-label="Upload progress" />
          )}
          {build.error && build.error.name !== "AbortError" && (
            <Alert color="red" title="Cannot start the build">
              {build.error.message}
            </Alert>
          )}
          <Text size="xs" c="dimmed">
            The build runs as a job: its log opens in the jobs panel.
          </Text>
          <Group justify="flex-end">
            <Button variant="default" onClick={close}>
              Cancel
            </Button>
            <Button type="submit" loading={build.isPending} disabled={!ready}>
              Build
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}
