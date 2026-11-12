// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/api/types"
	"github.com/containerd/nerdctl/v2/pkg/cmd/login"
	"github.com/containerd/nerdctl/v2/pkg/cmd/logout"
	"github.com/containerd/nerdctl/v2/pkg/imgutil/dockerconfigresolver"
)

// Registry is a registry the host is logged in to (#51). Keep in sync with api/openapi.yaml.
type Registry struct {
	// Server is the key in the docker config, e.g. "https://index.docker.io/v1/" or
	// "registry.local:5000".
	Server   string `json:"server"`
	Username string `json:"username,omitempty"`
}

// LoginResult reports a successful login.
type LoginResult struct {
	Registry
	// Warning is set when the password is stored unencrypted in the docker config file.
	Warning string `json:"warning,omitempty"`
}

// Registries manages registry logins of the host (#51, ADR-0019): the docker config of the
// user running nerdui, shared with `nerdctl login` and used by pull, push and compose.
type Registries interface {
	List(ctx context.Context) ([]Registry, error)
	// Login verifies the credentials with the registry and stores them.
	Login(ctx context.Context, server, username, password string) (LoginResult, error)
	// Logout removes the stored credentials of server.
	Logout(ctx context.Context, server string) error
}

// Registries returns the registry logins of the local host.
func (c *Client) Registries() Registries { return clientRegistries{c} }

type clientRegistries struct{ c *Client }

// registryMu serializes nerdui's own changes of the docker config, which nerdctl rewrites as a
// whole. Concurrent `nerdctl login` on the host can still race, as with the docker CLI.
var registryMu sync.Mutex

func (cr clientRegistries) List(context.Context) ([]Registry, error) {
	store, err := dockerconfigresolver.NewCredentialsStore("")
	if err != nil {
		return nil, fmt.Errorf("read docker config: %w", err)
	}
	servers := store.ShellCompletion()
	slices.Sort(servers)
	out := make([]Registry, 0, len(servers))
	for _, server := range servers {
		r := Registry{Server: server}
		if u, err := dockerconfigresolver.Parse(server); err == nil {
			if cred, err := store.Retrieve(u, true); err == nil {
				r.Username = cred.Username
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func (cr clientRegistries) Login(ctx context.Context, server, username, password string) (LoginResult, error) {
	server, username = strings.TrimSpace(server), strings.TrimSpace(username)
	// Without both, nerdctl would prompt on the terminal.
	if server == "" || username == "" || password == "" {
		return LoginResult{}, fmt.Errorf("%w: server, username and password are required", errdefs.ErrInvalidArgument)
	}
	if _, err := dockerconfigresolver.Parse(server); err != nil {
		return LoginResult{}, fmt.Errorf("%w: invalid registry %q: %w", errdefs.ErrInvalidArgument, server, err)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	cfg := cr.c.nerdctlFor(cr.c.namespace)
	var out bytes.Buffer
	err := login.Login(ctx, types.LoginCommandOptions{
		GOptions: types.GlobalCommandOptions(*cfg), ServerAddress: server,
		Username: username, Password: password,
	}, &out)
	if err != nil {
		return LoginResult{}, fmt.Errorf("login to %s: %w", server, classifyLoginErr(err))
	}
	res := LoginResult{Registry: Registry{Server: server, Username: username}}
	if text := out.String(); strings.Contains(text, "WARNING") {
		res.Warning = strings.TrimSpace(strings.Split(text, "\nLogin Succeeded")[0])
	}
	return res, nil
}

func (cr clientRegistries) Logout(ctx context.Context, server string) error {
	if strings.TrimSpace(server) == "" {
		return fmt.Errorf("%w: server is required", errdefs.ErrInvalidArgument)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	failures, err := logout.Logout(ctx, server)
	if err != nil {
		if errors.Is(err, dockerconfigresolver.ErrUnableToErase) {
			return fmt.Errorf("%w: not logged in to %s", errdefs.ErrNotFound, server)
		}
		return fmt.Errorf("logout from %s: %w", server, err)
	}
	for key, e := range failures {
		return fmt.Errorf("logout from %s (%s): %w", server, key, e)
	}
	return nil
}

// classifyLoginErr turns a rejected login into 400 (the registry answered, the credentials
// are wrong) and an unreachable registry into 503.
func classifyLoginErr(err error) error {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "unauthorized"), strings.Contains(msg, "401"),
		strings.Contains(msg, "incorrect username or password"), strings.Contains(msg, "authentication required"):
		return fmt.Errorf("%w: %w", errdefs.ErrInvalidArgument, err)
	case strings.Contains(msg, "connection refused"), strings.Contains(msg, "no such host"),
		strings.Contains(msg, "i/o timeout"), strings.Contains(msg, "deadline exceeded"):
		return fmt.Errorf("%w: %w", errdefs.ErrUnavailable, err)
	}
	return err
}
