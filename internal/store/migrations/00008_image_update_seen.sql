-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- When each registry digest was first seen, for the minimum age of automatic updates (#56,
-- ADR-0033). Rows checked before this migration count from their last check.

-- +goose Up

ALTER TABLE image_updates ADD COLUMN seen_at INTEGER NOT NULL DEFAULT 0;
UPDATE image_updates SET seen_at = checked_at;
