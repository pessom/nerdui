// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	composecli "github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/identifiers"
)

// Compose reads compose projects like `nerdctl compose` (ADR-0008): compose-go, at the version
// the linked nerdctl uses, with the project options of nerdctl's composer.New — includes,
// extends, profiles and .env interpolation behave the same.
type Compose interface {
	// Services returns the services of a project that run by default (no profile), loading
	// file from dir under the project name.
	Services(ctx context.Context, namespace, project, dir, file string) ([]string, error)
	// Describe loads a project found on disk under its own name (`name:` or the directory
	// name) for discovery and adoption (#25).
	Describe(ctx context.Context, dir, file string) (ComposeProject, error)
	// Validate loads the project of dir with compose and env as the content of its compose and
	// .env files (nil: the files on disk), without writing them. It returns the services or an
	// invalid-argument error explaining the problem.
	Validate(ctx context.Context, project, dir, composeName string, compose, env []byte) ([]string, error)
	// Run runs a stack operation with the host's `nerdctl compose` in dir (ADR-0026), writing
	// its output to out.
	Run(ctx context.Context, namespace, project, dir, op string, opts ComposeOptions, out io.Writer) error
}

// ComposeProject describes a compose project on disk.
type ComposeProject struct {
	Name     string
	Services []string
	// Files are the compose files loaded (main, override, includes).
	Files []string
	// LocalPaths are paths inside the project directory the project uses besides its compose
	// and .env files: bind mounts, build contexts, env files, config and secret files.
	LocalPaths []string
}

// Compose returns compose operations of the local containerd.
func (c *Client) Compose() Compose { return clientCompose{c} }

type clientCompose struct{ c *Client }

// The package composer is not imported: its test dependencies reference a nerdctl-internal
// module (mod/tigron) that `go mod tidy` cannot resolve outside nerdctl's repository.
func (clientCompose) Services(ctx context.Context, _, project, dir, file string) ([]string, error) {
	if err := identifiers.ValidateDockerCompat(project); err != nil {
		return nil, fmt.Errorf("%w: project name: %w", errdefs.ErrInvalidArgument, err)
	}
	p, err := loadProject(ctx, project, dir, file)
	if err != nil {
		return nil, err
	}
	return p.ServiceNames(), nil
}

func (clientCompose) Describe(ctx context.Context, dir, file string) (ComposeProject, error) {
	p, err := loadProject(ctx, "", dir, file)
	if err != nil {
		return ComposeProject{}, err
	}
	out := ComposeProject{Name: p.Name, Services: p.ServiceNames(), Files: p.ComposeFiles}
	seen := map[string]bool{}
	add := func(path string) {
		if path == "" {
			return
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		path = filepath.Clean(path)
		if rel, err := filepath.Rel(dir, path); err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
			return // outside the project directory
		}
		if slices.Contains(p.ComposeFiles, path) || filepath.Base(path) == ".env" && filepath.Dir(path) == dir {
			return
		}
		if !seen[path] {
			seen[path] = true
			out.LocalPaths = append(out.LocalPaths, path)
		}
	}
	for _, svc := range p.Services {
		for _, v := range svc.Volumes {
			if v.Type == "bind" {
				add(v.Source)
			}
		}
		if svc.Build != nil {
			add(svc.Build.Context)
		}
		for _, e := range svc.EnvFiles {
			add(e.Path)
		}
	}
	for _, c := range p.Configs {
		add(c.File)
	}
	for _, sc := range p.Secrets {
		add(sc.File)
	}
	sort.Strings(out.LocalPaths)
	return out, nil
}

// loadProject loads a compose project like `nerdctl compose` run in dir without -f: the default
// files (compose.yaml and compose.override.yaml, …) are found there; file only has to exist.
// An empty name derives it from the file or the directory.
func loadProject(ctx context.Context, project, dir, file string) (*types.Project, error) {
	if _, err := os.Stat(file); err != nil {
		return nil, fmt.Errorf("%w: compose file: %w", errdefs.ErrInvalidArgument, err)
	}
	// The options of nerdctl's composer.New, in its order.
	opts, err := composecli.NewProjectOptions(nil,
		composecli.WithOsEnv,
		composecli.WithWorkingDirectory(dir),
		composecli.WithConfigFileEnv,
		composecli.WithDefaultConfigPath,
		composecli.WithEnvFiles(),
		composecli.WithDotEnv,
		composecli.WithName(project),
		composecli.WithProfiles(nil),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errdefs.ErrInvalidArgument, err)
	}
	p, err := opts.LoadProject(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errdefs.ErrInvalidArgument, err)
	}
	return p, nil
}

// Stack operations (#28) and their `nerdctl compose` arguments.
const (
	ComposeUp      = "up"
	ComposeDown    = "down"
	ComposeStart   = "start"
	ComposeStop    = "stop"
	ComposeRestart = "restart"
	ComposePull    = "pull"
)

// ComposeOptions modify a stack operation.
type ComposeOptions struct {
	// Volumes removes the project's named volumes on down (`down -v`): their data is lost.
	Volumes bool
	// RemoveOrphans removes containers of services no longer in the compose file (up, down).
	RemoveOrphans bool
	// Pull pulls images before up (`up --pull always`).
	Pull bool
	// Services limits up and pull to these services (#54); empty means all.
	Services []string
}

// serviceName matches compose service names (compose-go: [a-zA-Z0-9._-]).
var serviceName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// ComposeArgs returns the `nerdctl compose` arguments of an operation on project in dir, after
// the global flags. The project is the stack name; the directory gives the default files.
func ComposeArgs(project, dir, op string, opts ComposeOptions) ([]string, error) {
	if err := identifiers.ValidateDockerCompat(project); err != nil {
		return nil, fmt.Errorf("%w: project name: %w", errdefs.ErrInvalidArgument, err)
	}
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("%w: project directory must be absolute", errdefs.ErrInvalidArgument)
	}
	args := []string{"compose", "--project-name=" + project, "--project-directory=" + dir, op}
	switch op {
	case ComposeUp:
		args = append(args, "--detach")
		if opts.Pull {
			args = append(args, "--pull=always")
		}
		if opts.RemoveOrphans {
			args = append(args, "--remove-orphans")
		}
	case ComposeDown:
		if opts.Volumes {
			args = append(args, "--volumes")
		}
		if opts.RemoveOrphans {
			args = append(args, "--remove-orphans")
		}
	case ComposeStart, ComposeStop, ComposeRestart, ComposePull:
	default:
		return nil, fmt.Errorf("%w: unknown stack operation %q", errdefs.ErrInvalidArgument, op)
	}
	if (opts.Volumes && op != ComposeDown) || (opts.Pull && op != ComposeUp) ||
		(opts.RemoveOrphans && op != ComposeUp && op != ComposeDown) ||
		(len(opts.Services) > 0 && op != ComposeUp && op != ComposePull) {
		return nil, fmt.Errorf("%w: option not supported by %s", errdefs.ErrInvalidArgument, op)
	}
	for _, svc := range opts.Services {
		if !serviceName.MatchString(svc) {
			return nil, fmt.Errorf("%w: invalid service name %q", errdefs.ErrInvalidArgument, svc)
		}
	}
	// Validated names cannot start with "-", so they are never read as flags.
	return append(args, opts.Services...), nil
}

func (cc clientCompose) Run(ctx context.Context, namespace, project, dir, op string, opts ComposeOptions, out io.Writer) error {
	args, err := ComposeArgs(project, dir, op, opts)
	if err != nil {
		return err
	}
	cfg := cc.c.nerdctlFor(namespace)
	bin, global := cc.c.nerdctlInvocation(cfg)
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("%w: nerdctl binary %q not found", errdefs.ErrFailedPrecondition, bin)
	}
	global = append(global, "--cni-path="+cfg.CNIPath, "--cni-netconfpath="+cfg.CNINetConfPath)
	argv := slices.Concat(global, args)
	if op != ComposeStart && op != ComposePull {
		defer expect(projectKey(namespace, project))()
	}
	_, _ = fmt.Fprintf(out, "\x1b[1m$ nerdctl %s\x1b[0m\r\n", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, bin, argv...) //nolint:gosec // G204: validated arguments, no shell (ADR-0026)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("nerdctl compose %s failed (exit code %d)", op, exit.ExitCode())
		}
		return fmt.Errorf("nerdctl compose %s: %w", op, err)
	}
	return nil
}

// overrideFileNames are compose-go's override files (cli.DefaultOverrideFileNames).
var overrideFileNames = []string{"compose.override.yml", "compose.override.yaml", "docker-compose.override.yml", "docker-compose.override.yaml"}

func (clientCompose) Validate(ctx context.Context, project, dir, composeName string, compose, env []byte) ([]string, error) {
	if err := identifiers.ValidateDockerCompat(project); err != nil {
		return nil, fmt.Errorf("%w: project name: %w", errdefs.ErrInvalidArgument, err)
	}
	if composeName == "" || filepath.Base(composeName) != composeName {
		return nil, fmt.Errorf("%w: compose file name %q", errdefs.ErrInvalidArgument, composeName)
	}
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return nil, fmt.Errorf("%w: project directory must be a clean absolute path", errdefs.ErrInvalidArgument)
	}
	main := filepath.Join(dir, composeName)
	// Candidates are written next to the real files, so includes, extends and relative paths
	// resolve the same way; they are hidden and removed afterwards.
	var temps []string
	defer func() {
		for _, t := range temps {
			_ = os.Remove(t)
		}
	}()
	writeTemp := func(pattern string, b []byte) (string, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return "", fmt.Errorf("validate: %w", err)
		}
		temps = append(temps, f.Name())
		_, werr := f.Write(b)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		return f.Name(), werr
	}
	if compose != nil {
		t, err := writeTemp(".nerdui-validate-*.yaml", compose)
		if err != nil {
			return nil, err
		}
		main = t
	}
	paths := []string{main}
	for _, n := range overrideFileNames {
		if fi, err := os.Stat(filepath.Join(dir, n)); err == nil && fi.Mode().IsRegular() {
			paths = append(paths, filepath.Join(dir, n))
			break
		}
	}
	envFiles := composecli.WithEnvFiles()
	if env != nil {
		t, err := writeTemp(".nerdui-validate-*.env", env)
		if err != nil {
			return nil, err
		}
		envFiles = composecli.WithEnvFiles(t)
	}
	opts, err := composecli.NewProjectOptions(paths,
		composecli.WithOsEnv,
		composecli.WithWorkingDirectory(dir),
		envFiles,
		composecli.WithDotEnv,
		composecli.WithName(project),
		composecli.WithProfiles(nil),
	)
	if err == nil {
		var p *types.Project
		if p, err = opts.LoadProject(ctx); err == nil {
			return p.ServiceNames(), nil
		}
	}
	// Name the files as the user knows them, not the temporary copies.
	msg := err.Error()
	for _, t := range temps {
		name := composeName
		if strings.HasSuffix(t, ".env") {
			name = ".env"
		}
		msg = strings.ReplaceAll(msg, t, name)
		msg = strings.ReplaceAll(msg, filepath.Base(t), name)
	}
	return nil, fmt.Errorf("%w: %s", errdefs.ErrInvalidArgument, msg)
}
