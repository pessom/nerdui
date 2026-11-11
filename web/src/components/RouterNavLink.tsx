// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { NavLink, type NavLinkProps } from "@mantine/core";
import { createLink, type LinkComponent } from "@tanstack/react-router";
import { forwardRef, type AnchorHTMLAttributes } from "react";

type Props = Omit<NavLinkProps, "component"> & AnchorHTMLAttributes<HTMLAnchorElement>;

const MantineNavLink = forwardRef<HTMLAnchorElement, Props>((props, ref) => (
  <NavLink component="a" ref={ref} {...props} />
));
MantineNavLink.displayName = "MantineNavLink";

const CreatedNavLink = createLink(MantineNavLink);

/** Mantine NavLink with type-safe TanStack Router navigation and active state. */
export const RouterNavLink: LinkComponent<typeof MantineNavLink> = (props) => (
  <CreatedNavLink preload="intent" activeProps={{ active: true }} {...props} />
);
