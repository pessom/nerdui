// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Anchor,
  Badge,
  Code,
  Group,
  Loader,
  Paper,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import { IconSearch } from "@tabler/icons-react";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useDeferredValue, useMemo, useState } from "react";
import { RouterAnchor } from "../../components/RouterAnchor";
import {
  curlExample,
  operations,
  type OpenAPIDoc,
  type Operation,
} from "../../features/apidocs/spec";

/** Documentation of the REST API from the server's own specification (#87). */
export const Route = createFileRoute("/_app/api-docs")({
  component: APIDocsPage,
});

const methodColor: Record<string, string> = {
  GET: "blue",
  POST: "teal",
  PUT: "orange",
  PATCH: "yellow",
  DELETE: "red",
};

/** Descriptions with `code` spans; other Markdown is shown as text. */
function Prose({ text }: { text: string }) {
  return (
    <Text size="sm" style={{ whiteSpace: "pre-line" }}>
      {text
        .split(/(`[^`]+`)/)
        .map((part, i) =>
          part.startsWith("`") && part.endsWith("`") ? (
            <Code key={i}>{part.slice(1, -1)}</Code>
          ) : (
            part
          ),
        )}
    </Text>
  );
}

function OperationCard({ op }: { op: Operation }) {
  return (
    <Paper withBorder p="sm" component="section" aria-label={`${op.method} ${op.path}`} id={op.id}>
      <Stack gap="xs">
        <Group gap="xs" wrap="nowrap">
          <Badge color={op.websocket ? "grape" : methodColor[op.method]} variant="light" w={70}>
            {op.websocket ? "WS" : op.method}
          </Badge>
          <Code fz="sm" style={{ wordBreak: "break-all" }}>
            {op.path}
          </Code>
          {op.action && (
            <Badge size="sm" variant="outline" color={op.action === "public" ? "gray" : "violet"}>
              {op.action}
            </Badge>
          )}
        </Group>
        <Text fw={500} size="sm">
          {op.summary}
        </Text>
        {op.description && <Prose text={op.description} />}
        {op.params.length > 0 && (
          <Table verticalSpacing={2} fz="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Parameter</Table.Th>
                <Table.Th>In</Table.Th>
                <Table.Th>Type</Table.Th>
                <Table.Th>Description</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {op.params.map((p) => (
                <Table.Tr key={`${p.in}-${p.name}`}>
                  <Table.Td>
                    <Code>{p.name}</Code>
                    {p.required && (
                      <Text span c="red" size="xs">
                        {" "}
                        *
                      </Text>
                    )}
                  </Table.Td>
                  <Table.Td>{p.in}</Table.Td>
                  <Table.Td>
                    <Code>{p.type}</Code>
                  </Table.Td>
                  <Table.Td>{p.description}</Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        )}
        {op.body && (
          <Text size="sm">
            Body{op.body.required ? "" : " (optional)"}: <Code>{op.body.type}</Code>
          </Text>
        )}
        <Group gap={6}>
          {op.responses.map((r) => (
            <Badge
              key={r.status}
              size="sm"
              variant="light"
              color={r.status.startsWith("2") || r.status === "101" ? "teal" : "gray"}
              title={r.description}
            >
              {r.status}
              {r.type ? ` ${r.type}` : ""}
            </Badge>
          ))}
        </Group>
        <Code block fz="xs">
          {curlExample(op, window.location.origin)}
        </Code>
      </Stack>
    </Paper>
  );
}

function APIDocsPage() {
  const spec = useQuery({
    queryKey: ["openapi"],
    queryFn: async () => {
      const r = await fetch(new URL("/api/v1/openapi.json", window.location.origin));
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      return (await r.json()) as OpenAPIDoc;
    },
    staleTime: Infinity,
  });
  const [filterText, setFilterText] = useState("");
  const filter = useDeferredValue(filterText.toLowerCase());
  const ops = useMemo(() => (spec.data ? operations(spec.data) : []), [spec.data]);
  const shown = ops.filter(
    (o) =>
      !filter ||
      `${o.method} ${o.path} ${o.summary} ${o.action ?? ""} ${o.tag}`
        .toLowerCase()
        .includes(filter),
  );
  const tags = spec.data?.tags ?? [];
  const byTag = new Map<string, Operation[]>();
  for (const o of shown) byTag.set(o.tag, [...(byTag.get(o.tag) ?? []), o]);

  return (
    <Stack>
      <Group justify="space-between">
        <Title order={2}>API documentation</Title>
        <Group gap="sm">
          <Anchor href="/api/v1/openapi.yaml" download size="sm">
            openapi.yaml
          </Anchor>
          <Anchor href="/api/v1/openapi.json" download size="sm">
            openapi.json
          </Anchor>
        </Group>
      </Group>
      <Text size="sm">
        Every action of the UI is available over the REST API. Authenticate with an API key:{" "}
        <Code>Authorization: Bearer &lt;key&gt;</Code> (create one under{" "}
        <RouterAnchor to="/api-keys" size="sm">
          API keys
        </RouterAnchor>
        ). Errors are <Code>{'{"error": {"code", "message"}}'}</Code>; long operations return a job
        whose output streams over WebSocket. The specification can be imported into Postman,
        Insomnia or a client generator.
      </Text>
      {spec.isPending && <Loader aria-label="Loading specification" />}
      {spec.isError && (
        <Alert color="red" title="Cannot load the specification">
          {spec.error.message}
        </Alert>
      )}
      {spec.data && (
        <>
          <TextInput
            aria-label="Search endpoints"
            placeholder="Search by path, summary or permission"
            leftSection={<IconSearch size={16} />}
            value={filterText}
            onChange={(e) => setFilterText(e.currentTarget.value)}
          />
          <Text size="xs" c="dimmed">
            {spec.data.info.title} {spec.data.info.version} · {shown.length} of {ops.length}{" "}
            endpoints
          </Text>
          {[
            ...tags.map((t) => t.name),
            ...[...byTag.keys()].filter((t) => !tags.some((x) => x.name === t)),
          ]
            .filter((t) => byTag.has(t))
            .map((t) => (
              <Stack key={t} gap="xs" component="section" aria-label={`Tag ${t}`}>
                <Title order={3} tt="capitalize">
                  {t}
                </Title>
                {tags.find((x) => x.name === t)?.description && (
                  <Prose text={tags.find((x) => x.name === t)!.description!} />
                )}
                {byTag.get(t)!.map((o) => (
                  <OperationCard key={`${o.method} ${o.path}`} op={o} />
                ))}
              </Stack>
            ))}
        </>
      )}
    </Stack>
  );
}
