// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package volbackup backs up volumes as tar.gz archives to a host directory or an S3 bucket,
// keeps the newest ones and restores them (#59, ADR-0050).
package volbackup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/secretbox"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

// Kinds of targets.
const (
	KindLocal = "local"
	KindS3    = "s3"
)

const (
	suffix = ".tar.gz"
	// DefaultKeep is how many archives of a volume are kept by default.
	DefaultKeep = 7
	// MaxKeep bounds the retention.
	MaxKeep = 1000
	aad     = "backup-target"
)

// S3Options locate a bucket. Keep in sync with api/openapi.yaml.
type S3Options struct {
	// Endpoint is host[:port] (HTTPS) or an http(s):// URL, e.g. s3.eu-central-1.amazonaws.com.
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region,omitempty"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix,omitempty"`
	AccessKey string `json:"accessKey"`
	// PathStyle addresses the bucket as endpoint/bucket (MinIO and other self-hosted servers).
	PathStyle bool `json:"pathStyle,omitempty"`
}

type options struct {
	Directory string     `json:"directory,omitempty"`
	S3        *S3Options `json:"s3,omitempty"`
}

// Target is where archives go. Keep in sync with api/openapi.yaml.
type Target struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Kind      string     `json:"kind"`
	Directory string     `json:"directory,omitempty"`
	S3        *S3Options `json:"s3,omitempty"`
	// HasSecret is set for S3 targets with a stored secret key.
	HasSecret bool      `json:"hasSecret,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TargetInput creates or changes a target; an empty SecretKey keeps the stored one.
type TargetInput struct {
	Name      string     `json:"name"`
	Kind      string     `json:"kind"`
	Directory string     `json:"directory"`
	S3        *S3Options `json:"s3"`
	SecretKey string     `json:"secretKey"`
}

// Store is the database; *db.Queries implements it.
type Store interface {
	ListBackupTargets(ctx context.Context) ([]db.BackupTarget, error)
	GetBackupTarget(ctx context.Context, id int64) (db.BackupTarget, error)
	CreateBackupTarget(ctx context.Context, arg db.CreateBackupTargetParams) (db.BackupTarget, error)
	UpdateBackupTarget(ctx context.Context, arg db.UpdateBackupTargetParams) (db.BackupTarget, error)
	DeleteBackupTarget(ctx context.Context, id int64) (int64, error)
}

// Service manages targets, backups and restores.
type Service struct {
	Store Store
	Box   *secretbox.Box
	// InUse names the schedules that write to a target, which cannot be deleted then.
	InUse func(ctx context.Context, target int64) ([]string, error)
	Now   func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func bad(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errdefs.ErrInvalidArgument, fmt.Sprintf(format, a...))
}

func notFound(id int64) error {
	return fmt.Errorf("%w: backup target %d not found", errdefs.ErrNotFound, id)
}

func view(r db.BackupTarget) Target {
	var o options
	_ = json.Unmarshal([]byte(r.Options), &o)
	return Target{
		ID: r.ID, Name: r.Name, Kind: r.Kind, Directory: o.Directory, S3: o.S3, HasSecret: r.Secret != "",
		CreatedAt: time.UnixMilli(r.CreatedAt).UTC(), UpdatedAt: time.UnixMilli(r.UpdatedAt).UTC(),
	}
}

// Targets lists the targets.
func (s *Service) Targets(ctx context.Context) ([]Target, error) {
	rows, err := s.Store.ListBackupTargets(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Target, 0, len(rows))
	for _, r := range rows {
		out = append(out, view(r))
	}
	return out, nil
}

// Target returns a target.
func (s *Service) Target(ctx context.Context, id int64) (Target, error) {
	r, err := s.Store.GetBackupTarget(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Target{}, notFound(id)
	}
	if err != nil {
		return Target{}, err
	}
	return view(r), nil
}

var bucketRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

// validate checks an input; stored is the sealed secret kept when the input has none.
func (s *Service) validate(in TargetInput, stored string) (options, string, error) {
	var o options
	if n := strings.TrimSpace(in.Name); n == "" || utf8.RuneCountInString(n) > 100 {
		return o, "", bad("name must be 1 to 100 characters")
	}
	switch in.Kind {
	case KindLocal:
		dir := filepath.Clean(strings.TrimSpace(in.Directory))
		if !filepath.IsAbs(dir) || dir == "/" {
			return o, "", bad("the directory must be an absolute path other than /")
		}
		o.Directory = dir
		return o, "", nil
	case KindS3:
		if in.S3 == nil {
			return o, "", bad("S3 settings are missing")
		}
		c := *in.S3
		c.Endpoint, c.Bucket, c.AccessKey = strings.TrimSpace(c.Endpoint), strings.TrimSpace(c.Bucket), strings.TrimSpace(c.AccessKey)
		c.Region, c.Prefix = strings.TrimSpace(c.Region), strings.Trim(strings.TrimSpace(c.Prefix), "/")
		if _, _, err := parseEndpoint(c.Endpoint); err != nil {
			return o, "", err
		}
		if !bucketRe.MatchString(c.Bucket) {
			return o, "", bad("bucket %q is not a valid bucket name", c.Bucket)
		}
		if c.AccessKey == "" || len(c.AccessKey) > 256 || len(c.Prefix) > 512 || strings.Contains(c.Prefix, "..") {
			return o, "", bad("check the access key and prefix")
		}
		o.S3 = &c
		secret := stored
		if in.SecretKey != "" {
			if len(in.SecretKey) > 1024 {
				return o, "", bad("the secret key is too long")
			}
			var err error
			if secret, err = s.Box.Seal(in.SecretKey, aad); err != nil {
				return o, "", err
			}
		}
		if secret == "" {
			return o, "", bad("enter the secret key")
		}
		return o, secret, nil
	}
	return o, "", bad("kind must be local or s3")
}

func conflict(err error, name string) error {
	if store.IsUniqueViolation(err) {
		return fmt.Errorf("%w: a backup target named %q exists", errdefs.ErrAlreadyExists, name)
	}
	return err
}

// Create adds a target.
func (s *Service) Create(ctx context.Context, in TargetInput) (Target, error) {
	o, secret, err := s.validate(in, "")
	if err != nil {
		return Target{}, err
	}
	b, _ := json.Marshal(o)
	r, err := s.Store.CreateBackupTarget(ctx, db.CreateBackupTargetParams{
		Name: strings.TrimSpace(in.Name), Kind: in.Kind, Options: string(b), Secret: secret, Now: s.now().UnixMilli(),
	})
	if err != nil {
		return Target{}, conflict(err, in.Name)
	}
	return view(r), nil
}

// Update changes a target.
func (s *Service) Update(ctx context.Context, id int64, in TargetInput) (Target, error) {
	cur, err := s.Store.GetBackupTarget(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Target{}, notFound(id)
	}
	if err != nil {
		return Target{}, err
	}
	stored := ""
	if cur.Kind == in.Kind {
		stored = cur.Secret
	}
	o, secret, err := s.validate(in, stored)
	if err != nil {
		return Target{}, err
	}
	b, _ := json.Marshal(o)
	r, err := s.Store.UpdateBackupTarget(ctx, db.UpdateBackupTargetParams{
		ID: id, Name: strings.TrimSpace(in.Name), Kind: in.Kind, Options: string(b), Secret: secret, Now: s.now().UnixMilli(),
	})
	if err != nil {
		return Target{}, conflict(err, in.Name)
	}
	return view(r), nil
}

// Delete removes a target that no schedule uses; its archives stay.
func (s *Service) Delete(ctx context.Context, id int64) error {
	if s.InUse != nil {
		names, err := s.InUse(ctx, id)
		if err != nil {
			return err
		}
		if len(names) > 0 {
			return fmt.Errorf("%w: the backup target is used by the schedules %s", errdefs.ErrFailedPrecondition, strings.Join(names, ", "))
		}
	}
	n, err := s.Store.DeleteBackupTarget(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return notFound(id)
	}
	return nil
}

func (s *Service) storage(ctx context.Context, id int64) (storage, error) {
	r, err := s.Store.GetBackupTarget(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound(id)
	}
	if err != nil {
		return nil, err
	}
	t := view(r)
	switch t.Kind {
	case KindLocal:
		return localStorage{dir: t.Directory}, nil
	case KindS3:
		if t.S3 == nil {
			return nil, fmt.Errorf("backup target %s has no S3 settings", t.Name)
		}
		secret, err := s.Box.Open(r.Secret, aad)
		if err != nil {
			return nil, err
		}
		return newS3(*t.S3, secret)
	}
	return nil, fmt.Errorf("backup target %s: unknown kind %q", t.Name, t.Kind)
}

// Test writes, lists and removes a small object in a target.
func (s *Service) Test(ctx context.Context, id int64) error {
	st, err := s.storage(ctx, id)
	if err != nil {
		return err
	}
	key := ".nerdui-test/" + fmt.Sprint(s.now().UnixNano()) + suffix
	if err := st.put(ctx, key, strings.NewReader("nerdui")); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if _, err := st.list(ctx, ".nerdui-test/"); err != nil {
		return fmt.Errorf("list: %w", err)
	}
	if err := st.remove(ctx, key); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	return nil
}

// dir is where the archives of a volume are kept in a target.
func dir(namespace, volume string) string { return namespace + "/" + volume + "/" }

var nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// validNames checks a namespace and a volume name, which become directories of archives.
func validNames(namespace, volume string) error {
	if len(namespace) > 255 || len(volume) > 255 || !nameRe.MatchString(namespace) || !nameRe.MatchString(volume) {
		return bad("invalid namespace or volume name")
	}
	return nil
}

// List returns the archives of a volume in a target, newest first.
func (s *Service) List(ctx context.Context, target int64, namespace, volume string) ([]Archive, error) {
	if err := validNames(namespace, volume); err != nil {
		return nil, err
	}
	st, err := s.storage(ctx, target)
	if err != nil {
		return nil, err
	}
	list, err := st.list(ctx, dir(namespace, volume))
	if err != nil {
		return nil, err
	}
	sortOldestFirst(list)
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list, nil
}

// Options of a backup or restore.
type Options struct {
	// Keep is how many archives of the volume are kept after a backup (0: DefaultKeep).
	Keep int
	// StopContainers stops the running containers that use the volume while it is read or
	// written, and starts them again afterwards.
	StopContainers bool
}

// Backup writes an archive of a volume to a target and removes the oldest beyond Keep. It
// returns the archive's key.
func (s *Service) Backup(ctx context.Context, rt runtime.Runtime, target int64, namespace, volume string, o Options, out io.Writer) (string, error) {
	if err := validNames(namespace, volume); err != nil {
		return "", err
	}
	st, err := s.storage(ctx, target)
	if err != nil {
		return "", err
	}
	keep := o.Keep
	if keep <= 0 {
		keep = DefaultKeep
	}
	running, err := s.users(ctx, rt, namespace, volume)
	if err != nil {
		return "", err
	}
	key := dir(namespace, volume) + volume + "-" + s.now().UTC().Format("20060102-150405") + suffix
	err = s.stopped(ctx, rt, namespace, running, o.StopContainers, out, func() error {
		dl, err := rt.Volumes().Download(namespace, volume, "/")
		if err != nil {
			return err
		}
		defer dl.Close() //nolint:errcheck // read-only
		_, _ = fmt.Fprintf(out, "Writing %s\r\n", key)
		pr, pw := io.Pipe()
		go func() { pw.CloseWithError(dl.Write(pw)) }()
		err = st.put(ctx, key, pr)
		_ = pr.CloseWithError(err)
		return err
	})
	if err != nil {
		return "", err
	}
	list, err := st.list(ctx, dir(namespace, volume))
	if err != nil {
		return key, fmt.Errorf("list old backups: %w", err)
	}
	sortOldestFirst(list)
	for len(list) > keep {
		if err := st.remove(ctx, list[0].Key); err != nil {
			return key, fmt.Errorf("remove an old backup: %w", err)
		}
		_, _ = fmt.Fprintf(out, "Removed the old backup %s\r\n", list[0].Key)
		list = list[1:]
	}
	return key, nil
}

// ValidKey checks the key of an archive given by a client.
func ValidKey(key string) error {
	if key == "" || path.Clean(key) != key || strings.HasPrefix(key, "/") || strings.HasPrefix(key, "../") ||
		strings.Contains(key, "/../") || !strings.HasSuffix(key, suffix) || strings.Count(key, "/") != 2 {
		return bad("invalid backup %q", key)
	}
	return nil
}

// Restore replaces the contents of a volume with an archive of a target; the volume is
// created when it does not exist. Running containers using it are refused unless
// StopContainers is set.
func (s *Service) Restore(ctx context.Context, rt runtime.Runtime, target int64, key, namespace, volume string, o Options, out io.Writer) error {
	if err := ValidKey(key); err != nil {
		return err
	}
	if err := validNames(namespace, volume); err != nil {
		return err
	}
	st, err := s.storage(ctx, target)
	if err != nil {
		return err
	}
	rc, err := st.get(ctx, key)
	if err != nil {
		return err
	}
	defer rc.Close() //nolint:errcheck // read-only
	running, err := s.users(ctx, rt, namespace, volume)
	if errdefs.IsNotFound(err) {
		if _, err := rt.Volumes().Create(ctx, namespace, volume, nil); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "Created the volume %s\r\n", volume)
	} else if err != nil {
		return err
	}
	if len(running) > 0 && !o.StopContainers {
		return fmt.Errorf("%w: the volume is used by the running containers %s; stop them first", errdefs.ErrFailedPrecondition, names(running))
	}
	return s.stopped(ctx, rt, namespace, running, true, out, func() error {
		_, _ = fmt.Fprintf(out, "Restoring %s into %s\r\n", key, volume)
		return rt.Volumes().Restore(namespace, volume, rc)
	})
}

// users returns the running containers that mount a volume; a missing volume is not found.
func (s *Service) users(ctx context.Context, rt runtime.Runtime, namespace, volume string) ([]runtime.Container, error) {
	vols, err := rt.Volumes().List(ctx, namespace, false)
	if err != nil {
		return nil, err
	}
	var mounters map[string]bool
	for _, v := range vols {
		if v.Name == volume {
			mounters = map[string]bool{}
			for _, m := range v.Containers {
				mounters[m.ID] = true
			}
		}
	}
	if mounters == nil {
		return nil, fmt.Errorf("%w: volume %s not found", errdefs.ErrNotFound, volume)
	}
	if len(mounters) == 0 {
		return nil, nil
	}
	cs, err := rt.Containers().List(ctx, namespace)
	if err != nil {
		return nil, err
	}
	var running []runtime.Container
	for _, c := range cs {
		if mounters[c.ID] && (c.State == runtime.StateRunning || c.State == "paused" || c.State == "restarting") {
			running = append(running, c)
		}
	}
	return running, nil
}

func names(cs []runtime.Container) string {
	n := make([]string, 0, len(cs))
	for _, c := range cs {
		n = append(n, c.Name)
	}
	return strings.Join(n, ", ")
}

// stopped runs fn, with the running containers stopped when stop is set; they are started
// again even when fn fails.
func (s *Service) stopped(ctx context.Context, rt runtime.Runtime, namespace string, running []runtime.Container, stop bool, out io.Writer, fn func() error) error {
	if !stop || len(running) == 0 {
		return fn()
	}
	var stoppedOnes []runtime.Container
	defer func() {
		for _, c := range stoppedOnes {
			_, _ = fmt.Fprintf(out, "Starting %s\r\n", c.Name)
			if err := rt.Containers().Do(context.WithoutCancel(ctx), namespace, c.ID, runtime.ActionStart, runtime.ActionOptions{}); err != nil {
				_, _ = fmt.Fprintf(out, "Cannot start %s: %v\r\n", c.Name, err)
			}
		}
	}()
	for _, c := range running {
		_, _ = fmt.Fprintf(out, "Stopping %s\r\n", c.Name)
		if err := rt.Containers().Do(ctx, namespace, c.ID, runtime.ActionStop, runtime.ActionOptions{}); err != nil {
			return fmt.Errorf("stop %s: %w", c.Name, err)
		}
		stoppedOnes = append(stoppedOnes, c)
	}
	return fn()
}
