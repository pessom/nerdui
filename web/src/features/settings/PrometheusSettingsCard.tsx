// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Button,
  Code,
  Group,
  Loader,
  Paper,
  PasswordInput,
  SegmentedControl,
  Stack,
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import {
  keys,
  prometheusSettingsQuery,
  type PrometheusSettings,
  type PrometheusSettingsInput,
} from "../../api/queries";

type Auth = PrometheusSettingsInput["auth"];

const containerdSnippet = `[metrics]
  address = "127.0.0.1:1338"`;

const scrapeSnippet = `scrape_configs:
  - job_name: containerd
    metrics_path: /v1/metrics
    static_configs:
      - targets: ["127.0.0.1:1338"]`;

/** Prometheus connection of an environment for the metrics history (#176, ADR-0023). */
export function PrometheusSettingsCard({ env }: { env: string }) {
  const settings = useQuery(prometheusSettingsQuery(env));
  return (
    <Paper withBorder p="md" component="section" aria-label="Metrics history">
      <Stack gap="sm">
        <div>
          <Title order={3}>Metrics history (Prometheus)</Title>
          <Text size="sm" c="dimmed">
            With a Prometheus server that scrapes containerd&apos;s built-in metrics, the Stats tab
            of a container shows CPU, memory, block I/O and PIDs for the last hour up to a week.
            nerdui only reads from Prometheus; its address is never exposed to the browser.
          </Text>
        </div>
        {settings.isPending && <Loader aria-label="Loading settings" />}
        {settings.isError && (
          <Alert color="red" title="Cannot read settings">
            {settings.error.message}
          </Alert>
        )}
        {settings.data && <PrometheusForm env={env} current={settings.data} />}
        <details>
          <summary>
            <Text component="span" size="sm">
              How to enable containerd metrics
            </Text>
          </summary>
          <Stack gap="xs" mt="xs">
            <Text size="sm">
              1. In <Code>/etc/containerd/config.toml</Code>, then restart containerd (running
              containers keep running):
            </Text>
            <Code block>{containerdSnippet}</Code>
            <Text size="sm">2. In prometheus.yml, add a scrape job:</Text>
            <Code block>{scrapeSnippet}</Code>
          </Stack>
        </details>
      </Stack>
    </Paper>
  );
}

function PrometheusForm({ env, current }: { env: string; current: PrometheusSettings }) {
  const queryClient = useQueryClient();
  const [url, setUrl] = useState(current.url ?? "");
  const [auth, setAuth] = useState<Auth>(current.auth ?? "none");
  const [username, setUsername] = useState(current.username ?? "");
  const [secret, setSecret] = useState("");
  const [check, setCheck] = useState<{ version?: string; containers: number }>();
  // The stored secret is kept when the field is left empty and the mode is unchanged.
  const keepsSecret = current.hasSecret && current.auth === auth;

  const input = (): PrometheusSettingsInput => ({
    url,
    auth,
    username: auth === "basic" ? username : "",
    secret,
  });
  const path = { params: { path: { env } } };
  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: keys.prometheus(env) });
    void queryClient.invalidateQueries({ queryKey: keys.envStatus(env) });
    void queryClient.invalidateQueries({ queryKey: ["env", env, "containers"] });
  };

  const test = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/environments/{env}/settings/prometheus/test", { ...path, body: input() }),
      ),
    onMutate: () => setCheck(undefined),
    onSuccess: setCheck,
  });
  const save = useMutation({
    mutationFn: () =>
      unwrap(api.PUT("/api/v1/environments/{env}/settings/prometheus", { ...path, body: input() })),
    onSuccess: () => {
      setSecret("");
      notifications.show({ color: "teal", message: "Prometheus settings saved" });
      invalidate();
    },
  });
  const remove = useMutation({
    mutationFn: () =>
      unwrapEmpty(api.DELETE("/api/v1/environments/{env}/settings/prometheus", path)),
    onSuccess: () => {
      setUrl("");
      setAuth("none");
      setUsername("");
      setSecret("");
      setCheck(undefined);
      notifications.show({ message: "Metrics history turned off" });
      invalidate();
    },
  });
  const error = test.error ?? save.error ?? remove.error;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  return (
    <form onSubmit={submit}>
      <Stack gap="sm">
        <TextInput
          label="Prometheus URL"
          description="Base URL of the server, without /api/v1"
          placeholder="http://127.0.0.1:9090"
          required
          value={url}
          onChange={(e) => setUrl(e.currentTarget.value)}
        />
        <div>
          <Text size="sm" fw={500} mb={4}>
            Authentication
          </Text>
          <SegmentedControl
            aria-label="Authentication"
            value={auth}
            onChange={(v) => setAuth(v as Auth)}
            data={[
              { value: "none", label: "None" },
              { value: "basic", label: "Basic" },
              { value: "bearer", label: "Bearer token" },
            ]}
          />
        </div>
        {auth === "basic" && (
          <TextInput
            label="Username"
            required
            value={username}
            onChange={(e) => setUsername(e.currentTarget.value)}
          />
        )}
        {auth !== "none" && (
          <PasswordInput
            label={auth === "basic" ? "Password" : "Token"}
            required={!keepsSecret}
            placeholder={keepsSecret ? "unchanged" : undefined}
            value={secret}
            onChange={(e) => setSecret(e.currentTarget.value)}
          />
        )}
        {error && (
          <Alert color="red" title="Prometheus settings">
            {error.message}
          </Alert>
        )}
        {check &&
          (check.containers > 0 ? (
            <Alert color="teal" title="Connected">
              {check.version ? `Prometheus ${check.version}: ` : ""}containerd metrics of{" "}
              {check.containers} container{check.containers === 1 ? "" : "s"} found.
            </Alert>
          ) : (
            <Alert color="yellow" title="Connected, but no containerd metrics">
              Prometheus answers{check.version ? ` (version ${check.version})` : ""}, but has no
              containerd task metrics yet. Enable containerd&apos;s metrics and add a scrape job
              (see below); the history starts when Prometheus scrapes them.
            </Alert>
          ))}
        <Group justify="space-between">
          <Group gap="xs">
            <Button type="submit" loading={save.isPending} disabled={!url}>
              Save
            </Button>
            <Button
              variant="default"
              loading={test.isPending}
              disabled={!url}
              onClick={() => test.mutate()}
            >
              Test connection
            </Button>
          </Group>
          {current.configured && (
            <Button
              variant="subtle"
              color="red"
              loading={remove.isPending}
              onClick={() => remove.mutate()}
            >
              Turn off
            </Button>
          )}
        </Group>
      </Stack>
    </form>
  );
}
