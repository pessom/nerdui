// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/containerd/errdefs"
)

const (
	maxRegistry = 2 << 20
	cacheFor    = 10 * time.Minute
)

// registryFile is a registry.json: Arcane's format (compose_url, env_url, icon_url,
// documentation_url, tags) or files inline (compose, env). Relative URLs are resolved against
// the registry's URL.
type registryFile struct {
	Name      string `json:"name"`
	Templates []struct {
		ID               string   `json:"id"`
		Name             string   `json:"name"`
		Description      string   `json:"description"`
		Categories       []string `json:"categories"`
		Tags             []string `json:"tags"`
		Logo             string   `json:"logo"`
		IconURL          string   `json:"icon_url"`
		Documentation    string   `json:"documentation"`
		DocumentationURL string   `json:"documentation_url"`
		Compose          string   `json:"compose"`
		ComposeURL       string   `json:"compose_url"`
		Env              string   `json:"env"`
		EnvURL           string   `json:"env_url"`
	} `json:"templates"`
}

type entry struct {
	summary            Summary
	compose, env       string
	composeURL, envURL string
}

type cached struct {
	at      time.Time
	entries []entry
}

// Registries reads registry.json files over HTTP and caches them.
type Registries struct {
	Client *http.Client
	Now    func() time.Time

	mu    sync.Mutex
	cache map[string]cached
}

func (r *Registries) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Registries) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Forget empties the cache.
func (r *Registries) Forget() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = nil
}

func checkURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return nil, bad("registry URL %q: use http(s)://host/path/registry.json", raw)
	}
	return u, nil
}

func (r *Registries) fetch(ctx context.Context, raw string, limit int64) ([]byte, error) {
	u, err := checkURL(raw)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	res, err := r.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errdefs.ErrUnavailable, err)
	}
	defer res.Body.Close() //nolint:errcheck // read-only
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s: %s", errdefs.ErrUnavailable, raw, res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errdefs.ErrUnavailable, err)
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s is larger than %d KiB", raw, limit>>10)
	}
	return b, nil
}

func resolve(base *url.URL, ref string) string {
	if ref == "" {
		return ""
	}
	u, err := base.Parse(ref)
	if err != nil {
		return ""
	}
	return u.String()
}

func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (r *Registries) entries(ctx context.Context, source string) ([]entry, error) {
	r.mu.Lock()
	c, ok := r.cache[source]
	r.mu.Unlock()
	if ok && r.now().Sub(c.at) < cacheFor {
		return c.entries, nil
	}
	b, err := r.fetch(ctx, source, maxRegistry)
	if err != nil {
		return nil, err
	}
	var f registryFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("not a registry.json: %w", err)
	}
	base, _ := url.Parse(source)
	seen := map[string]bool{}
	var out []entry
	for _, t := range f.Templates {
		if t.ID == "" || seen[t.ID] || (t.Compose == "" && t.ComposeURL == "") {
			continue
		}
		seen[t.ID] = true
		out = append(out, entry{
			summary: Summary{
				Source: source, ID: t.ID, Name: first(t.Name, t.ID), Description: t.Description,
				Categories: append(t.Categories, t.Tags...), Logo: resolve(base, first(t.Logo, t.IconURL)),
				Documentation: resolve(base, first(t.Documentation, t.DocumentationURL)),
			},
			compose: t.Compose, env: t.Env,
			composeURL: resolve(base, t.ComposeURL), envURL: resolve(base, t.EnvURL),
		})
	}
	r.mu.Lock()
	if r.cache == nil {
		r.cache = map[string]cached{}
	}
	r.cache[source] = cached{at: r.now(), entries: out}
	r.mu.Unlock()
	return out, nil
}

// List returns the templates of a registry.
func (r *Registries) List(ctx context.Context, source string) ([]Summary, error) {
	es, err := r.entries(ctx, source)
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(es))
	for _, e := range es {
		out = append(out, e.summary)
	}
	return out, nil
}

// Get returns a template of a registry with its files, fetching them when they are linked.
func (r *Registries) Get(ctx context.Context, source, id string) (Template, error) {
	es, err := r.entries(ctx, source)
	if err != nil {
		return Template{}, err
	}
	for _, e := range es {
		if e.summary.ID != id {
			continue
		}
		compose, env := e.compose, e.env
		if compose == "" {
			b, err := r.fetch(ctx, e.composeURL, MaxFile)
			if err != nil {
				return Template{}, fmt.Errorf("compose file: %w", err)
			}
			compose = string(b)
		}
		if env == "" && e.envURL != "" {
			b, err := r.fetch(ctx, e.envURL, MaxFile)
			if err != nil {
				return Template{}, fmt.Errorf(".env.example: %w", err)
			}
			env = string(b)
		}
		if strings.TrimSpace(compose) == "" {
			return Template{}, fmt.Errorf("template %s has an empty compose file", id)
		}
		return Template{Summary: e.summary, Compose: compose, Env: env, Variables: ParseEnv(env)}, nil
	}
	return Template{}, fmt.Errorf("%w: template %q not found in %s", errdefs.ErrNotFound, id, source)
}
