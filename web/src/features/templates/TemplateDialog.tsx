// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Button, Group, Modal, Stack, Textarea, TextInput } from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "../../api/client";
import { templatesQuery } from "./templates";

export type TemplateDraft = {
  /** The id of a saved template being edited. */
  id?: string;
  name: string;
  description: string;
  compose: string;
  env: string;
};

const mono = { input: { fontFamily: "var(--mantine-font-family-monospace)", fontSize: 13 } };

/** Saves a stack template: a compose file and an .env.example (#34). */
export function TemplateDialog({
  draft,
  onClose,
}: {
  draft: TemplateDraft | null;
  onClose: () => void;
}) {
  return (
    <Modal
      opened={!!draft}
      onClose={onClose}
      size="xl"
      title={draft?.id ? `Edit template ${draft.name}` : "Save as template"}
    >
      {draft && <Form draft={draft} onClose={onClose} />}
    </Modal>
  );
}

function Form({ draft, onClose }: { draft: TemplateDraft; onClose: () => void }) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState(draft);
  const save = useMutation({
    mutationFn: () => {
      const body = {
        name: form.name,
        description: form.description,
        compose: form.compose,
        env: form.env,
      };
      return draft.id
        ? unwrap(api.PUT("/api/v1/templates/{id}", { params: { path: { id: draft.id } }, body }))
        : unwrap(api.POST("/api/v1/templates", { body }));
    },
    onSuccess: (t) => {
      void queryClient.invalidateQueries({ queryKey: templatesQuery.queryKey });
      notifications.show({ color: "teal", message: `Template ${t.name} saved` });
      onClose();
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  return (
    <form onSubmit={submit}>
      <Stack>
        {save.isError && <Alert color="red">{save.error.message}</Alert>}
        <TextInput
          label="Name"
          required
          value={form.name}
          onChange={(e) => setForm({ ...form, name: e.currentTarget.value })}
        />
        <TextInput
          label="Description"
          value={form.description}
          onChange={(e) => setForm({ ...form, description: e.currentTarget.value })}
        />
        <Textarea
          label="Compose file"
          required
          rows={12}
          styles={mono}
          value={form.compose}
          onChange={(e) => setForm({ ...form, compose: e.currentTarget.value })}
        />
        <Textarea
          label="Variables (.env.example)"
          description="KEY=default lines; a comment right above a line describes it. Values of passwords and tokens are left empty."
          rows={6}
          styles={mono}
          value={form.env}
          onChange={(e) => setForm({ ...form, env: e.currentTarget.value })}
        />
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={save.isPending}>
            Save template
          </Button>
        </Group>
      </Stack>
    </form>
  );
}
