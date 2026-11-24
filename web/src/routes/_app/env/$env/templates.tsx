// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Card,
  Chip,
  Group,
  Image,
  Loader,
  Modal,
  SimpleGrid,
  Stack,
  Text,
  Textarea,
  TextInput,
  Title,
  Tooltip,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { IconPencil, IconPlus, IconSearch, IconTrash, IconWorld } from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, getRouteApi } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { api, unwrap, unwrapEmpty } from "../../../../api/client";
import { envStatusQuery } from "../../../../api/queries";
import { DeployTemplateDialog } from "../../../../features/templates/DeployTemplateDialog";
import { TemplateDialog, type TemplateDraft } from "../../../../features/templates/TemplateDialog";
import {
  sourceLabel,
  templateQuery,
  templateRegistriesQuery,
  templatesQuery,
  type TemplateSummary,
} from "../../../../features/templates/templates";

const envRoute = getRouteApi("/_app/env/$env");

/** The template catalog: built-in, saved and registry templates (#33, #34, ADR-0051). */
export const Route = createFileRoute("/_app/env/$env/templates")({
  component: TemplatesPage,
});

function TemplatesPage() {
  const { env } = Route.useParams();
  const { ns } = envRoute.useSearch();
  const status = useQuery(envStatusQuery(env));
  const namespace = ns ?? status.data?.defaultNamespace ?? "default";
  const queryClient = useQueryClient();
  const list = useQuery(templatesQuery);
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState<string>("all");
  const [deploying, setDeploying] = useState<TemplateSummary | null>(null);
  const [editing, setEditing] = useState<TemplateDraft | null>(null);
  const [removing, setRemoving] = useState<TemplateSummary | null>(null);
  const [registries, setRegistries] = useState(false);
  const remove = useMutation({
    mutationFn: (id: string) =>
      unwrapEmpty(api.DELETE("/api/v1/templates/{id}", { params: { path: { id } } })),
    onSuccess: () => {
      setRemoving(null);
      void queryClient.invalidateQueries({ queryKey: templatesQuery.queryKey });
    },
    onError: (e) =>
      notifications.show({ color: "red", title: "Cannot delete", message: e.message }),
  });
  const edit = useMutation({
    mutationFn: (t: TemplateSummary) => queryClient.fetchQuery(templateQuery(t.source, t.id)),
    onSuccess: (t) =>
      setEditing({
        id: t.id,
        name: t.name,
        description: t.description ?? "",
        compose: t.compose,
        env: t.env,
      }),
    onError: (e) => notifications.show({ color: "red", title: "Cannot open", message: e.message }),
  });

  const templates = useMemo(() => list.data?.templates ?? [], [list.data]);
  const categories = useMemo(() => {
    const all = new Set<string>();
    for (const t of templates) for (const c of t.categories ?? []) all.add(c);
    return [...all].sort();
  }, [templates]);
  const shown = templates.filter((t) => {
    const q = query.trim().toLowerCase();
    const matches =
      !q ||
      t.name.toLowerCase().includes(q) ||
      (t.description ?? "").toLowerCase().includes(q) ||
      (t.categories ?? []).some((c) => c.includes(q));
    const inCategory =
      category === "all" ||
      (category === "mine" ? t.source === "custom" : (t.categories ?? []).includes(category));
    return matches && inCategory;
  });

  return (
    <Stack>
      <Group justify="space-between">
        <Title order={2}>Templates</Title>
        <Group gap="xs">
          <Button
            variant="default"
            leftSection={<IconWorld size={16} />}
            onClick={() => setRegistries(true)}
          >
            Registries
          </Button>
          <Button
            leftSection={<IconPlus size={16} />}
            onClick={() => setEditing({ name: "", description: "", compose: "", env: "" })}
          >
            New template
          </Button>
        </Group>
      </Group>
      <Text size="sm" c="dimmed">
        Deploy an application as a new stack in {namespace}. Save your own stacks as templates from
        their page.
      </Text>
      <TextInput
        placeholder="Search templates"
        aria-label="Search templates"
        leftSection={<IconSearch size={16} />}
        value={query}
        onChange={(e) => setQuery(e.currentTarget.value)}
      />
      <Chip.Group multiple={false} value={category} onChange={(v) => setCategory(v)}>
        <Group gap={6}>
          <Chip value="all" size="xs">
            All
          </Chip>
          <Chip value="mine" size="xs">
            Mine
          </Chip>
          {categories.map((c) => (
            <Chip key={c} value={c} size="xs">
              {c}
            </Chip>
          ))}
        </Group>
      </Chip.Group>
      {list.isPending && <Loader aria-label="Loading templates" />}
      {list.isError && (
        <Alert color="red" title="Cannot read templates">
          {list.error.message}
        </Alert>
      )}
      {list.data?.errors.map((e) => (
        <Alert key={e.source} color="orange" title={`Registry ${e.source} cannot be read`}>
          {e.error}
        </Alert>
      ))}
      {list.data && shown.length === 0 && (
        <Text size="sm" c="dimmed">
          No templates match.
        </Text>
      )}
      <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
        {shown.map((t) => (
          <Card
            key={`${t.source} ${t.id}`}
            withBorder
            padding="md"
            component="article"
            aria-label={t.name}
          >
            <Stack gap="xs" h="100%">
              <Group justify="space-between" wrap="nowrap" align="flex-start">
                <Group gap="xs" wrap="nowrap">
                  {t.logo && <Image src={t.logo} alt="" w={24} h={24} fit="contain" />}
                  <Text fw={600}>{t.name}</Text>
                </Group>
                <Badge size="xs" variant="light" color={t.source === "custom" ? "grape" : "gray"}>
                  {sourceLabel(t.source)}
                </Badge>
              </Group>
              {t.description && (
                <Text size="sm" c="dimmed" lineClamp={3}>
                  {t.description}
                </Text>
              )}
              <Group gap={4}>
                {(t.categories ?? []).map((c) => (
                  <Badge key={c} size="xs" variant="outline">
                    {c}
                  </Badge>
                ))}
              </Group>
              <Group justify="space-between" mt="auto">
                <Button size="xs" onClick={() => setDeploying(t)} aria-label={`Deploy ${t.name}`}>
                  Deploy
                </Button>
                {t.source === "custom" && (
                  <Group gap={4}>
                    <Tooltip label="Edit">
                      <ActionIcon
                        variant="subtle"
                        aria-label={`Edit ${t.name}`}
                        loading={edit.isPending && edit.variables?.id === t.id}
                        onClick={() => edit.mutate(t)}
                      >
                        <IconPencil size={16} />
                      </ActionIcon>
                    </Tooltip>
                    <Tooltip label="Delete">
                      <ActionIcon
                        variant="subtle"
                        color="red"
                        aria-label={`Delete ${t.name}`}
                        onClick={() => setRemoving(t)}
                      >
                        <IconTrash size={16} />
                      </ActionIcon>
                    </Tooltip>
                  </Group>
                )}
              </Group>
            </Stack>
          </Card>
        ))}
      </SimpleGrid>
      <DeployTemplateDialog
        env={env}
        namespace={namespace}
        template={deploying}
        onClose={() => setDeploying(null)}
      />
      <TemplateDialog draft={editing} onClose={() => setEditing(null)} />
      <Modal opened={!!removing} onClose={() => setRemoving(null)} title="Delete template">
        {removing && (
          <Stack>
            <Text size="sm">
              Delete the template <b>{removing.name}</b>? Stacks deployed from it stay.
            </Text>
            <Group justify="flex-end">
              <Button variant="default" onClick={() => setRemoving(null)}>
                Cancel
              </Button>
              <Button
                color="red"
                loading={remove.isPending}
                onClick={() => remove.mutate(removing.id)}
              >
                Delete
              </Button>
            </Group>
          </Stack>
        )}
      </Modal>
      <RegistriesDialog opened={registries} onClose={() => setRegistries(false)} />
    </Stack>
  );
}

function RegistriesDialog({ opened, onClose }: { opened: boolean; onClose: () => void }) {
  return (
    <Modal opened={opened} onClose={onClose} size="lg" title="Template registries">
      {opened && <RegistriesForm onClose={onClose} />}
    </Modal>
  );
}

function RegistriesForm({ onClose }: { onClose: () => void }) {
  const queryClient = useQueryClient();
  const current = useQuery(templateRegistriesQuery);
  const [text, setText] = useState<string>();
  const value = text ?? (current.data?.urls ?? []).join("\n");
  const save = useMutation({
    mutationFn: () =>
      unwrap(
        api.PUT("/api/v1/template-registries", {
          body: {
            urls: value
              .split("\n")
              .map((u) => u.trim())
              .filter(Boolean),
          },
        }),
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: templateRegistriesQuery.queryKey });
      void queryClient.invalidateQueries({ queryKey: templatesQuery.queryKey });
      onClose();
    },
  });
  if (current.isPending) return <Loader aria-label="Loading registries" />;
  return (
    <Stack>
      {save.isError && <Alert color="red">{save.error.message}</Alert>}
      <Textarea
        label="Registry URLs"
        description="One registry.json URL per line, in Arcane's format (compose_url, env_url) or with files inline"
        rows={5}
        value={value}
        onChange={(e) => setText(e.currentTarget.value)}
        styles={{ input: { fontFamily: "var(--mantine-font-family-monospace)" } }}
      />
      <Group justify="flex-end">
        <Button variant="default" onClick={onClose}>
          Cancel
        </Button>
        <Button loading={save.isPending} onClick={() => save.mutate()}>
          Save
        </Button>
      </Group>
    </Stack>
  );
}
