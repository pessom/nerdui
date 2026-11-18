-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- Latest registry digest of each image reference checked for updates (#53, ADR-0030). Whether
-- an update is available is decided when reading, against the local digest at that moment.

-- +goose Up

CREATE TABLE image_updates (
    environment   TEXT    NOT NULL,
    ref           TEXT    NOT NULL,
    remote_digest TEXT    NOT NULL DEFAULT '',
    error         TEXT    NOT NULL DEFAULT '',
    checked_at    INTEGER NOT NULL,
    PRIMARY KEY (environment, ref)
) STRICT;
