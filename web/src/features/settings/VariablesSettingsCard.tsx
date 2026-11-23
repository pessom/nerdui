// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Code, Paper, Stack, Text, Title } from "@mantine/core";
import { VariablesEditor } from "../variables/VariablesEditor";

/** Global variables of all stacks (#35, ADR-0049). */
export function VariablesSettingsCard() {
  return (
    <Paper withBorder p="md" component="section" aria-label="Global variables">
      <Stack gap="xs">
        <Title order={3}>Global variables</Title>
        <Text size="sm" c="dimmed">
          Passed to every stack&apos;s <Code>nerdctl compose</Code> as environment variables, so
          compose files use them as <Code>{"${NAME}"}</Code>. A stack&apos;s own variable of the
          same name wins; both win over <Code>.env</Code>. Values are encrypted in nerdui&apos;s
          database.
        </Text>
        <VariablesEditor scope={{ kind: "global" }} />
      </Stack>
    </Paper>
  );
}
