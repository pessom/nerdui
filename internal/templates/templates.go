// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package templates provides stack templates (#33, #34, ADR-0051): a built-in catalog,
// catalogs from registry.json URLs, and templates users saved. A template is a compose file,
// an .env.example whose comments describe its variables, and a description.
package templates

import (
	"bufio"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/containerd/errdefs"
	"go.yaml.in/yaml/v3"

	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

// Sources of templates besides registry URLs.
const (
	SourceBuiltin = "builtin"
	SourceCustom  = "custom"
)

// MaxFile is the largest compose file or .env.example of a template.
const MaxFile = 256 << 10

//go:embed all:builtin
var builtinFS embed.FS

// Summary describes a template in a catalog. Keep in sync with api/openapi.yaml.
type Summary struct {
	// Source is builtin, custom or the URL of a registry.
	Source        string     `json:"source"`
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Description   string     `json:"description,omitempty"`
	Categories    []string   `json:"categories,omitempty"`
	Logo          string     `json:"logo,omitempty"`
	Documentation string     `json:"documentation,omitempty"`
	UpdatedAt     *time.Time `json:"updatedAt,omitempty"`
}

// Template is a template with its files and variables. Keep in sync with api/openapi.yaml.
type Template struct {
	Summary
	Compose   string     `json:"compose"`
	Env       string     `json:"env"`
	Variables []Variable `json:"variables"`
	// Services of the compose file; with one, the template can also start a container
	// (ADR-0059).
	Services []string `json:"services"`
}

// Variable is a variable of .env.example. Keep in sync with api/openapi.yaml.
type Variable struct {
	Name        string `json:"name"`
	Default     string `json:"default"`
	Description string `json:"description,omitempty"`
	// Secret is guessed from the name (password, secret, token, key).
	Secret bool `json:"secret"`
}

var (
	varLine  = regexp.MustCompile(`^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$`)
	secretRe = regexp.MustCompile(`(?i)(password|passwd|secret|token|api_?key|private_?key)`)
)

// ParseEnv reads the variables of an .env.example: KEY=default lines, each described by the
// comment lines right above it.
func ParseEnv(env string) []Variable {
	var out []Variable
	var comment []string
	seen := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(env))
	sc.Buffer(make([]byte, 64<<10), MaxFile)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
			comment = nil
		case strings.HasPrefix(line, "#"):
			comment = append(comment, strings.TrimSpace(strings.TrimPrefix(line, "#")))
		default:
			m := varLine.FindStringSubmatch(line)
			if m != nil && !seen[m[1]] {
				seen[m[1]] = true
				out = append(out, Variable{
					Name: m[1], Default: unquote(m[2]), Description: strings.Join(comment, " "),
					Secret: secretRe.MatchString(m[1]),
				})
			}
			comment = nil
		}
	}
	return out
}

func unquote(v string) string {
	v = strings.TrimSpace(v)
	if v != "" && (v[0] == '"' || v[0] == '\'') {
		// A quoted value ends at its closing quote; anything after it is a comment.
		for i := 1; i < len(v); i++ {
			if v[0] == '"' && v[i] == '\\' {
				i++
				continue
			}
			if v[i] == v[0] {
				if v[0] == '"' {
					if s, err := strconv.Unquote(v[:i+1]); err == nil {
						return s
					}
				}
				return v[1:i]
			}
		}
		return v
	}
	if i := strings.Index(v, " #"); i >= 0 { // inline comment
		v = strings.TrimSpace(v[:i])
	}
	return v
}

type meta struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Categories    []string `json:"categories"`
	Logo          string   `json:"logo"`
	Documentation string   `json:"documentation"`
}

// Builtin returns the built-in templates sorted by name.
func Builtin() []Template {
	entries, _ := fs.ReadDir(builtinFS, "builtin")
	var out []Template
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t, err := builtin(e.Name())
		if err == nil {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func builtin(id string) (Template, error) {
	dir := "builtin/" + id
	b, err := builtinFS.ReadFile(dir + "/template.json")
	if err != nil {
		return Template{}, fmt.Errorf("%w: template %q not found", errdefs.ErrNotFound, id)
	}
	var m meta
	if err := json.Unmarshal(b, &m); err != nil {
		return Template{}, err
	}
	compose, err := builtinFS.ReadFile(dir + "/compose.yaml")
	if err != nil {
		return Template{}, err
	}
	env, _ := builtinFS.ReadFile(dir + "/.env.example")
	return Template{
		Summary: Summary{
			Source: SourceBuiltin, ID: id, Name: m.Name, Description: m.Description, Categories: m.Categories,
			Logo: m.Logo, Documentation: m.Documentation,
		},
		Compose: string(compose), Env: string(env), Variables: ParseEnv(string(env)),
	}, nil
}

// Store is the database; *db.Queries implements it.
type Store interface {
	ListStackTemplates(ctx context.Context) ([]db.StackTemplate, error)
	GetStackTemplate(ctx context.Context, id int64) (db.StackTemplate, error)
	CreateStackTemplate(ctx context.Context, arg db.CreateStackTemplateParams) (db.StackTemplate, error)
	UpdateStackTemplate(ctx context.Context, arg db.UpdateStackTemplateParams) (db.StackTemplate, error)
	DeleteStackTemplate(ctx context.Context, id int64) (int64, error)
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, arg db.SetSettingParams) error
}

// Service lists and fetches templates.
type Service struct {
	Store    Store
	Registry *Registries
	Now      func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Input creates or changes a custom template. Keep in sync with api/openapi.yaml.
type Input struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Compose     string `json:"compose"`
	Env         string `json:"env"`
}

func bad(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errdefs.ErrInvalidArgument, fmt.Sprintf(format, a...))
}

func (in *Input) validate() error {
	in.Name, in.Description = strings.TrimSpace(in.Name), strings.TrimSpace(in.Description)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 100 {
		return bad("name must be 1 to 100 characters")
	}
	if utf8.RuneCountInString(in.Description) > 1000 {
		return bad("the description is longer than 1000 characters")
	}
	if strings.TrimSpace(in.Compose) == "" {
		return bad("the compose file is empty")
	}
	if len(in.Compose) > MaxFile || len(in.Env) > MaxFile {
		return bad("files must be up to %d KiB", MaxFile>>10)
	}
	if !utf8.ValidString(in.Compose) || !utf8.ValidString(in.Env) {
		return bad("files must be UTF-8 text")
	}
	// The full check needs the values of the variables: it runs on deploy. Syntax errors are
	// caught here, so a saved template does not fail only when it is used.
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(in.Compose), &doc); err != nil {
		return bad("the compose file is not valid YAML: %s", strings.TrimPrefix(err.Error(), "yaml: "))
	}
	if doc == nil {
		return bad("the compose file is empty")
	}
	return nil
}

func custom(r db.StackTemplate) Template {
	t := time.UnixMilli(r.UpdatedAt).UTC()
	return Template{
		Summary: Summary{
			Source: SourceCustom, ID: strconv.FormatInt(r.ID, 10), Name: r.Name, Description: r.Description,
			UpdatedAt: &t,
		},
		Compose: r.Compose, Env: r.Env, Variables: ParseEnv(r.Env),
	}
}

func conflict(err error, name string) error {
	if store.IsUniqueViolation(err) {
		return fmt.Errorf("%w: a template named %q exists", errdefs.ErrAlreadyExists, name)
	}
	return err
}

func customID(id string) (int64, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: template %q not found", errdefs.ErrNotFound, id)
	}
	return n, nil
}

// Create saves a custom template.
func (s *Service) Create(ctx context.Context, in Input, user string) (Template, error) {
	if err := in.validate(); err != nil {
		return Template{}, err
	}
	r, err := s.Store.CreateStackTemplate(ctx, db.CreateStackTemplateParams{
		Name: in.Name, Description: in.Description, Compose: in.Compose, Env: in.Env, CreatedBy: user, Now: s.now().UnixMilli(),
	})
	if err != nil {
		return Template{}, conflict(err, in.Name)
	}
	return custom(r), nil
}

// Update changes a custom template.
func (s *Service) Update(ctx context.Context, id string, in Input) (Template, error) {
	n, err := customID(id)
	if err != nil {
		return Template{}, err
	}
	if err := in.validate(); err != nil {
		return Template{}, err
	}
	r, err := s.Store.UpdateStackTemplate(ctx, db.UpdateStackTemplateParams{
		ID: n, Name: in.Name, Description: in.Description, Compose: in.Compose, Env: in.Env, Now: s.now().UnixMilli(),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Template{}, fmt.Errorf("%w: template %s not found", errdefs.ErrNotFound, id)
	}
	if err != nil {
		return Template{}, conflict(err, in.Name)
	}
	return custom(r), nil
}

// Delete removes a custom template.
func (s *Service) Delete(ctx context.Context, id string) error {
	n, err := customID(id)
	if err != nil {
		return err
	}
	rows, err := s.Store.DeleteStackTemplate(ctx, n)
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("%w: template %s not found", errdefs.ErrNotFound, id)
	}
	return nil
}

// SourceError is a registry that could not be read.
type SourceError struct {
	Source string `json:"source"`
	Error  string `json:"error"`
}

// List returns every template: built-in, custom and from the registries. A registry that
// cannot be read is reported and skipped.
func (s *Service) List(ctx context.Context) ([]Summary, []SourceError, error) {
	var out []Summary
	for _, t := range Builtin() {
		out = append(out, t.Summary)
	}
	rows, err := s.Store.ListStackTemplates(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, r := range rows {
		out = append(out, custom(r).Summary)
	}
	urls, err := s.RegistryURLs(ctx)
	if err != nil {
		return nil, nil, err
	}
	var errs []SourceError
	for _, u := range urls {
		list, err := s.Registry.List(ctx, u)
		if err != nil {
			errs = append(errs, SourceError{Source: u, Error: err.Error()})
			continue
		}
		out = append(out, list...)
	}
	return out, errs, nil
}

// Get returns a template with its files.
func (s *Service) Get(ctx context.Context, source, id string) (Template, error) {
	t, err := s.get(ctx, source, id)
	if err == nil {
		t.Services = Services(t.Compose)
		if t.Services == nil {
			t.Services = []string{}
		}
	}
	return t, err
}

func (s *Service) get(ctx context.Context, source, id string) (Template, error) {
	switch source {
	case SourceBuiltin:
		if strings.ContainsAny(id, "/\\") || id == "" || id == "." || id == ".." {
			return Template{}, fmt.Errorf("%w: template %q not found", errdefs.ErrNotFound, id)
		}
		return builtin(id)
	case SourceCustom:
		n, err := customID(id)
		if err != nil {
			return Template{}, err
		}
		r, err := s.Store.GetStackTemplate(ctx, n)
		if errors.Is(err, sql.ErrNoRows) {
			return Template{}, fmt.Errorf("%w: template %s not found", errdefs.ErrNotFound, id)
		}
		if err != nil {
			return Template{}, err
		}
		return custom(r), nil
	}
	urls, err := s.RegistryURLs(ctx)
	if err != nil {
		return Template{}, err
	}
	for _, u := range urls {
		if u == source {
			return s.Registry.Get(ctx, u, id)
		}
	}
	return Template{}, fmt.Errorf("%w: unknown template source %q", errdefs.ErrNotFound, source)
}

const registriesKey = "templates.registries"

// RegistryURLs returns the configured registry URLs.
func (s *Service) RegistryURLs(ctx context.Context) ([]string, error) {
	v, err := s.Store.GetSetting(ctx, registriesKey)
	if errors.Is(err, sql.ErrNoRows) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var urls []string
	if err := json.Unmarshal([]byte(v), &urls); err != nil {
		return []string{}, nil //nolint:nilerr // a broken setting means none
	}
	return urls, nil
}

// SetRegistryURLs replaces the registry URLs.
func (s *Service) SetRegistryURLs(ctx context.Context, urls []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" || seen[u] {
			continue
		}
		if _, err := checkURL(u); err != nil {
			return nil, err
		}
		seen[u] = true
		out = append(out, u)
	}
	if len(out) > 20 {
		return nil, bad("at most 20 registries")
	}
	b, _ := json.Marshal(out)
	if err := s.Store.SetSetting(ctx, db.SetSettingParams{Key: registriesKey, Value: string(b), Now: s.now().UnixMilli()}); err != nil {
		return nil, err
	}
	s.Registry.Forget()
	return out, nil
}
