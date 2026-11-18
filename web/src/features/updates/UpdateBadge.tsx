// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Badge, Tooltip } from "@mantine/core";
import { IconArrowUp } from "@tabler/icons-react";
import type { ImageUpdateStatus } from "../../api/queries";
import { formatTime, usePreferences } from "../preferences/context";
import { useImageUpdate } from "./context";

const short = (d?: string) => (d ? d.replace(/^sha256:/, "").slice(0, 12) : "");

/** Marks an image whose tag points to a newer digest in the registry (#53). */
export function UpdateBadge({ status }: { status?: ImageUpdateStatus }) {
  const { prefs } = usePreferences();
  if (!status?.updateAvailable) return null;
  const checked = formatTime(status.checkedAt, prefs.dateFormat).text;
  return (
    <Tooltip
      multiline
      w={280}
      label={`The registry has a newer image for this tag (${short(status.remoteDigest)}, local ${short(status.localDigest)}), checked ${checked}. Pull the image and recreate its containers to update.`}
    >
      <Badge
        size="xs"
        color="teal"
        variant="light"
        leftSection={<IconArrowUp size={10} />}
        aria-label={`Update available for ${status.ref}`}
      >
        update
      </Badge>
    </Tooltip>
  );
}

/** The badge of an image reference, from the surrounding ImageUpdatesContext. */
export function UpdateBadgeFor({ imageRef }: { imageRef: string }) {
  return <UpdateBadge status={useImageUpdate(imageRef)} />;
}
