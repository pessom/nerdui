// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Button, Center, Stack, Text, Title } from "@mantine/core";
import { Link, Outlet, createRootRouteWithContext } from "@tanstack/react-router";
import type { RouterContext } from "../router";

export const Route = createRootRouteWithContext<RouterContext>()({
  component: Outlet,
  notFoundComponent: NotFound,
});

function NotFound() {
  return (
    <Center h="100vh">
      <Stack align="center" gap="xs">
        <Title order={2}>Page not found</Title>
        <Text c="dimmed">This page does not exist.</Text>
        <Button component={Link} to="/" variant="light">
          Go to overview
        </Button>
      </Stack>
    </Center>
  );
}
