// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { AppShell, Burger, Group, ScrollArea, Text } from "@mantine/core";
import { useDisclosure } from "@mantine/hooks";
import {
  IconActivity,
  IconBox,
  IconKey,
  IconLayoutDashboard,
  IconStack3,
  IconUsers,
} from "@tabler/icons-react";
import { Link, Outlet, createFileRoute, redirect, useParams } from "@tanstack/react-router";
import { ApiError } from "../api/client";
import { meQuery, setupQuery } from "../api/queries";
import { Footer } from "../components/Footer";
import { RouterNavLink } from "../components/RouterNavLink";
import { ThemeToggle } from "../components/ThemeToggle";
import { UserMenu } from "../components/UserMenu";
import { useJobsPanel } from "../features/jobs/JobsContext";
import { JobsIndicator, JobsProvider } from "../features/jobs/JobsPanel";

/**
 * Pathless layout for everything behind authentication: resolves the session (ADR-0005)
 * and renders the application shell.
 */
export const Route = createFileRoute("/_app")({
  beforeLoad: async ({ context, location }) => {
    let session;
    try {
      session = await context.queryClient.ensureQueryData(meQuery);
    } catch (err) {
      if (!(err instanceof ApiError) || err.status !== 401) throw err;
      const setup = await context.queryClient.fetchQuery(setupQuery).catch(() => null);
      if (setup?.required) throw redirect({ to: "/setup" });
      throw redirect({ to: "/login", search: { redirect: location.href } });
    }
    // A temporary password must be replaced before anything else (#74).
    if (session.user.mustChangePassword) {
      throw redirect({ to: "/change-password", search: { redirect: location.href } });
    }
    return { session };
  },
  component: AppLayout,
});

function AppLayout() {
  const [opened, { toggle, close }] = useDisclosure();
  const { session } = Route.useRouteContext();
  const params = useParams({ strict: false }) as { env?: string };
  const env = params.env ?? "local";

  return (
    <JobsProvider>
      <AppShell
        header={{ height: 56 }}
        navbar={{ width: 220, breakpoint: "sm", collapsed: { mobile: !opened } }}
        footer={{ height: 32 }}
        padding="md"
      >
        <AppShell.Header>
          <Group h="100%" px="md" justify="space-between" wrap="nowrap">
            <Group gap="sm" wrap="nowrap">
              <Burger
                opened={opened}
                onClick={toggle}
                hiddenFrom="sm"
                size="sm"
                aria-label="Toggle navigation"
              />
              <Text
                fw={700}
                size="lg"
                component={Link}
                to="/"
                style={{ textDecoration: "none" }}
                c="inherit"
              >
                nerdui
              </Text>
            </Group>
            <Group gap="xs" wrap="nowrap">
              <HeaderJobs />
              <ThemeToggle />
              <UserMenu username={session.user.username} />
            </Group>
          </Group>
        </AppShell.Header>

        <AppShell.Navbar p="xs" component="nav" aria-label="Main navigation">
          <ScrollArea>
            {/* Only implemented sections are listed; new features add their items here. */}
            <RouterNavLink
              to="/env/$env"
              params={{ env }}
              activeOptions={{ exact: true }}
              label="Overview"
              leftSection={<IconLayoutDashboard size={18} stroke={1.5} />}
              onClick={close}
            />
            <RouterNavLink
              to="/env/$env/containers"
              params={{ env }}
              search={(prev: { ns?: string }) => ({ ns: prev.ns })}
              label="Containers"
              leftSection={<IconBox size={18} stroke={1.5} />}
              onClick={close}
            />
            <RouterNavLink
              to="/env/$env/images"
              params={{ env }}
              search={(prev: { ns?: string }) => ({ ns: prev.ns })}
              label="Images"
              leftSection={<IconStack3 size={18} stroke={1.5} />}
              onClick={close}
            />
            <RouterNavLink
              to="/env/$env/registries"
              params={{ env }}
              search={(prev: { ns?: string }) => ({ ns: prev.ns })}
              label="Registries"
              leftSection={<IconKey size={18} stroke={1.5} />}
              onClick={close}
            />
            <RouterNavLink
              to="/env/$env/events"
              params={{ env }}
              search={(prev: { ns?: string }) => ({ ns: prev.ns })}
              label="Events"
              leftSection={<IconActivity size={18} stroke={1.5} />}
              onClick={close}
            />
            <RouterNavLink
              to="/users"
              label="Users"
              leftSection={<IconUsers size={18} stroke={1.5} />}
              onClick={close}
            />
          </ScrollArea>
        </AppShell.Navbar>

        <AppShell.Main>
          <Outlet />
        </AppShell.Main>

        <AppShell.Footer>
          <Group h="100%" justify="center">
            <Footer />
          </Group>
        </AppShell.Footer>
      </AppShell>
    </JobsProvider>
  );
}

function HeaderJobs() {
  const panel = useJobsPanel();
  return <JobsIndicator onOpen={() => panel.open()} />;
}
