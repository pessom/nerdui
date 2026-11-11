// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package store persists nerdui's own data in SQLite (ADR-0004).
//
// Runtime objects (containers, images, ...) are never stored here: containerd and nerdctl
// stay the source of truth. Queries are written in SQL (queries/*.sql) and compiled into
// typed Go code by sqlc (package db); handlers never write SQL directly.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/pessom/nerdui/internal/store/db"
)

// FileName is the database file inside the data directory.
const FileName = "nerdui.db"

// FileMode is the permission of the database file; SQLite creates -wal/-shm with the same mode.
const FileMode fs.FileMode = 0o600

//go:embed migrations/*.sql
var embedded embed.FS

// Store holds a single-connection writer pool and a read-only reader pool.
type Store struct {
	path   string
	writer *sql.DB
	reader *sql.DB
	write  *db.Queries
	read   *db.Queries
	logf   func(format string, args ...any)
}

// Option configures Open.
type Option func(*Store)

// WithLogf sets a logger for migration progress (default: discard).
func WithLogf(logf func(format string, args ...any)) Option {
	return func(s *Store) { s.logf = logf }
}

// Open opens (creating if needed) <dataDir>/nerdui.db and applies pending migrations.
func Open(ctx context.Context, dataDir string, opts ...Option) (*Store, error) {
	migrations, err := fs.Sub(embedded, "migrations")
	if err != nil {
		return nil, err
	}
	return open(ctx, dataDir, migrations, opts...)
}

func open(ctx context.Context, dataDir string, migrations fs.FS, opts ...Option) (*Store, error) {
	s := &Store{path: filepath.Join(dataDir, FileName), logf: func(string, ...any) {}}
	for _, o := range opts {
		o(s)
	}

	if err := createFile(s.path); err != nil {
		return nil, err
	}

	var err error
	// One writer connection serializes writes; _txlock=immediate avoids upgrade deadlocks.
	if s.writer, err = sql.Open("sqlite", dsn(s.path, "immediate", false)); err != nil {
		return nil, err
	}
	s.writer.SetMaxOpenConns(1)

	if err := migrate(ctx, s.writer, s.path, migrations, s.logf); err != nil {
		_ = s.writer.Close()
		return nil, err
	}

	if s.reader, err = sql.Open("sqlite", dsn(s.path, "deferred", true)); err != nil {
		_ = s.writer.Close()
		return nil, err
	}
	s.reader.SetMaxOpenConns(max(4, runtime.NumCPU()))

	s.write = db.New(s.writer)
	s.read = db.New(s.reader)
	return s, nil
}

// createFile creates the database file with FileMode so SQLite never creates it with the
// process umask.
func createFile(path string) error {
	// The path is <data-dir>/nerdui.db from the operator's configuration, not user input.
	f, err := os.OpenFile(filepath.Clean(path), os.O_RDONLY|os.O_CREATE, FileMode) //nolint:gosec // G304: trusted config path
	if err != nil {
		return fmt.Errorf("create database file: %w", err)
	}
	return f.Close()
}

func dsn(path, txlock string, readOnly bool) string {
	q := url.Values{}
	q.Set("_txlock", txlock)
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	if readOnly {
		q.Add("_pragma", "query_only(1)")
	}
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + q.Encode()
}

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

// Read returns queries bound to the read-only pool.
func (s *Store) Read() *db.Queries { return s.read }

// Write returns queries bound to the single writer connection.
func (s *Store) Write() *db.Queries { return s.write }

// Tx runs fn in a write transaction, committing if fn returns nil.
func (s *Store) Tx(ctx context.Context, fn func(q *db.Queries) error) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(s.write.WithTx(tx)); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

// SchemaVersion returns the applied migration version.
func (s *Store) SchemaVersion(ctx context.Context) (int64, error) {
	return schemaVersion(ctx, s.reader)
}

// Close closes both pools.
func (s *Store) Close() error {
	return errors.Join(s.reader.Close(), s.writer.Close())
}

// IsNotFound reports whether err means "no rows".
func IsNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }

// IsUniqueViolation reports whether err is a UNIQUE or PRIMARY KEY constraint violation.
func IsUniqueViolation(err error) bool {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	code := se.Code()
	return code == sqlite3.SQLITE_CONSTRAINT_UNIQUE || code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
}
