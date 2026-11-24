// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package volbackup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Archive is a stored backup of a volume. Keep in sync with api/openapi.yaml.
type Archive struct {
	// Key is the archive's path in its target: <namespace>/<volume>/<volume>-<time>.tar.gz.
	Key  string    `json:"key"`
	Size int64     `json:"size"`
	Time time.Time `json:"time"`
}

// storage keeps archives under keys with slashes.
type storage interface {
	put(ctx context.Context, key string, r io.Reader) error
	// list returns the archives directly under dir (a key prefix ending in /).
	list(ctx context.Context, dir string) ([]Archive, error)
	get(ctx context.Context, key string) (io.ReadCloser, error)
	remove(ctx context.Context, key string) error
}

// localStorage keeps archives in a host directory; keys cannot leave it (os.Root).
type localStorage struct{ dir string }

func (l localStorage) root() (*os.Root, error) {
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return nil, err
	}
	return os.OpenRoot(l.dir)
}

func (l localStorage) put(_ context.Context, key string, r io.Reader) error {
	root, err := l.root()
	if err != nil {
		return err
	}
	defer root.Close() //nolint:errcheck // nothing to flush
	if err := root.MkdirAll(path.Dir(key), 0o700); err != nil {
		return err
	}
	tmp := path.Join(path.Dir(key), fmt.Sprintf(".part-%d", time.Now().UnixNano()))
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer root.Remove(tmp) //nolint:errcheck // renamed on success
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return root.Rename(tmp, key)
}

func (l localStorage) list(_ context.Context, dir string) ([]Archive, error) {
	root, err := l.root()
	if err != nil {
		return nil, err
	}
	defer root.Close() //nolint:errcheck // read-only
	entries, err := fs.ReadDir(root.FS(), path.Clean(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Archive
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), suffix) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Archive{Key: dir + e.Name(), Size: fi.Size(), Time: fi.ModTime().UTC()})
	}
	return out, nil
}

func (l localStorage) get(_ context.Context, key string) (io.ReadCloser, error) {
	root, err := l.root()
	if err != nil {
		return nil, err
	}
	defer root.Close() //nolint:errcheck // the file stays open
	f, err := root.Open(key)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: backup %s not found", errdefs.ErrNotFound, key)
	}
	return f, err
}

func (l localStorage) remove(_ context.Context, key string) error {
	root, err := l.root()
	if err != nil {
		return err
	}
	defer root.Close() //nolint:errcheck // nothing to flush
	return root.Remove(key)
}

// s3Storage keeps archives in an S3 bucket under a prefix.
type s3Storage struct {
	client *minio.Client
	bucket string
	prefix string // "" or ending in /
}

func newS3(o S3Options, secret string) (*s3Storage, error) {
	host, secure, err := parseEndpoint(o.Endpoint)
	if err != nil {
		return nil, err
	}
	lookup := minio.BucketLookupAuto
	if o.PathStyle {
		lookup = minio.BucketLookupPath
	}
	c, err := minio.New(host, &minio.Options{
		Creds:        credentials.NewStaticV4(o.AccessKey, secret, ""),
		Secure:       secure,
		Region:       o.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: S3: %w", errdefs.ErrInvalidArgument, err)
	}
	prefix := strings.Trim(o.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	return &s3Storage{client: c, bucket: o.Bucket, prefix: prefix}, nil
}

// parseEndpoint accepts host[:port] (HTTPS) or an http:// or https:// URL.
func parseEndpoint(e string) (host string, secure bool, err error) {
	e = strings.TrimSpace(e)
	if !strings.Contains(e, "://") {
		e = "https://" + e
	}
	u, err := url.Parse(e)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil {
		return "", false, fmt.Errorf("%w: S3 endpoint %q: use host[:port] or http(s)://host[:port]", errdefs.ErrInvalidArgument, e)
	}
	return u.Host, u.Scheme == "https", nil
}

func (s *s3Storage) put(ctx context.Context, key string, r io.Reader) error {
	_, err := s.client.PutObject(ctx, s.bucket, s.prefix+key, r, -1, minio.PutObjectOptions{ContentType: "application/gzip"})
	return s3Err(err)
}

func (s *s3Storage) list(ctx context.Context, dir string) ([]Archive, error) {
	var out []Archive
	for o := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: s.prefix + dir}) {
		if o.Err != nil {
			return nil, s3Err(o.Err)
		}
		key := strings.TrimPrefix(o.Key, s.prefix)
		if strings.HasSuffix(key, "/") || !strings.HasSuffix(key, suffix) || path.Dir(key)+"/" != dir {
			continue
		}
		out = append(out, Archive{Key: key, Size: o.Size, Time: o.LastModified.UTC()})
	}
	return out, nil
}

func (s *s3Storage) get(ctx context.Context, key string) (io.ReadCloser, error) {
	if _, err := s.client.StatObject(ctx, s.bucket, s.prefix+key, minio.StatObjectOptions{}); err != nil {
		if minio.ToErrorResponse(err).Code == minio.NoSuchKey {
			return nil, fmt.Errorf("%w: backup %s not found", errdefs.ErrNotFound, key)
		}
		return nil, s3Err(err)
	}
	o, err := s.client.GetObject(ctx, s.bucket, s.prefix+key, minio.GetObjectOptions{})
	return o, s3Err(err)
}

func (s *s3Storage) remove(ctx context.Context, key string) error {
	return s3Err(s.client.RemoveObject(ctx, s.bucket, s.prefix+key, minio.RemoveObjectOptions{}))
}

func s3Err(err error) error {
	if err == nil {
		return nil
	}
	if r := minio.ToErrorResponse(err); r.Code != "" {
		return fmt.Errorf("S3: %s: %s", r.Code, r.Message)
	}
	return fmt.Errorf("S3: %w", err)
}

// sortOldestFirst sorts archives by key: names end in their time.
func sortOldestFirst(a []Archive) {
	sort.Slice(a, func(i, j int) bool { return a[i].Key < a[j].Key })
}
