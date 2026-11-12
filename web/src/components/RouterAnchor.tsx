// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Anchor, type AnchorProps } from "@mantine/core";
import { createLink, type LinkComponent } from "@tanstack/react-router";
import { forwardRef, type AnchorHTMLAttributes } from "react";

type Props = AnchorProps & AnchorHTMLAttributes<HTMLAnchorElement>;

const MantineAnchor = forwardRef<HTMLAnchorElement, Props>((props, ref) => (
  <Anchor component="a" ref={ref} {...props} />
));
MantineAnchor.displayName = "MantineAnchor";

const CreatedAnchor = createLink(MantineAnchor);

/** Mantine Anchor with type-safe TanStack Router navigation. */
export const RouterAnchor: LinkComponent<typeof MantineAnchor> = (props) => (
  <CreatedAnchor preload="intent" {...props} />
);
