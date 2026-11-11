// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Center, Paper, Stack, Title } from "@mantine/core";
import type { ReactNode } from "react";
import { Footer } from "./Footer";

/** Centered card for the login and setup pages. */
export function AuthCard({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Center mih="100vh" p="md">
      <Stack w="100%" maw={420}>
        <Paper withBorder shadow="sm" p="xl">
          <Stack>
            <Title order={2}>{title}</Title>
            {children}
          </Stack>
        </Paper>
        <Footer />
      </Stack>
    </Center>
  );
}
