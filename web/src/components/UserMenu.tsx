// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Button, Menu } from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { IconLogout, IconUser } from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { api, setCsrfToken, unwrapEmpty } from "../api/client";

export function UserMenu({ username }: { username: string }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const logout = useMutation({
    mutationFn: () => unwrapEmpty(api.POST("/api/v1/auth/logout")),
    onSettled: async (_data, error) => {
      if (error) notifications.show({ color: "red", message: `Sign-out failed: ${error.message}` });
      setCsrfToken("");
      queryClient.clear();
      await navigate({ to: "/login" });
    },
  });
  return (
    <Menu position="bottom-end" withArrow>
      <Menu.Target>
        <Button variant="default" leftSection={<IconUser size={16} stroke={1.5} />}>
          {username}
        </Button>
      </Menu.Target>
      <Menu.Dropdown>
        <Menu.Item
          leftSection={<IconLogout size={16} stroke={1.5} />}
          onClick={() => logout.mutate()}
        >
          Sign out
        </Menu.Item>
      </Menu.Dropdown>
    </Menu>
  );
}
