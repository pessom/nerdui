// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/pressly/goose/v3"
)

// ErrNewerSchema is returned when the database was migrated by a newer nerdui.
var ErrNewerSchema = fmt.Errorf("database schema is newer than this nerdui build")

// migrate applies pending migrations from fsys. Before changing an existing database it
// writes a consistent copy next to it with VACUUM INTO (ADR-0004).
func migrate(ctx context.Context, conn *sql.DB, path string, fsys fs.FS, logf func(string, ...any)) error {
	provider, err := goose.NewProvider(goose.DialectSQLite3, conn, fsys,
		goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	current, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	var latest int64
	for _, src := range provider.ListSources() {
		latest = max(latest, src.Version)
	}
	if current > latest {
		return fmt.Errorf("%w: database is at version %d, this build supports up to %d; "+
			"upgrade nerdui or restore a backup", ErrNewerSchema, current, latest)
	}

	pending, err := provider.HasPending(ctx)
	if err != nil {
		return fmt.Errorf("check pending migrations: %w", err)
	}
	if !pending {
		return nil
	}

	if current > 0 {
		backup := fmt.Sprintf("%s.bak-v%d-%d", path, current, time.Now().Unix())
		if _, err := conn.ExecContext(ctx, "VACUUM INTO ?", backup); err != nil {
			return fmt.Errorf("backup before migration: %w", err)
		}
		if err := os.Chmod(backup, FileMode); err != nil {
			return fmt.Errorf("backup permissions: %w", err)
		}
		logf("database backup written to %s", backup)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	for _, r := range results {
		logf("applied migration %d (%s) in %s", r.Source.Version, r.Source.Path, r.Duration)
	}
	return nil
}

func schemaVersion(ctx context.Context, conn *sql.DB) (int64, error) {
	var v sql.NullInt64
	err := conn.QueryRowContext(ctx,
		"SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&v)
	return v.Int64, err
}
