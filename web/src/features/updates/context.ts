// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { createContext, useContext } from "react";
import type { ImageUpdateStatus } from "../../api/queries";

/** Update status of the shown namespace's images by reference (#53). */
export const ImageUpdatesContext = createContext<ReadonlyMap<string, ImageUpdateStatus>>(new Map());

export function useImageUpdate(ref: string): ImageUpdateStatus | undefined {
  return useContext(ImageUpdatesContext).get(ref);
}
