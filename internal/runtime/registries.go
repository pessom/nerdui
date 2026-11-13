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
	// Aliases are the other keys the same login is stored under: nerdctl saves logins on port
	// 443 with and without the port, and logout removes all of them.
	Aliases []string `json:"aliases,omitempty"`
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
	return groupRegistries(store.ShellCompletion(), func(server string) string {
		if u, err := dockerconfigresolver.Parse(server); err == nil {
			if cred, err := store.Retrieve(u, true); err == nil {
				return cred.Username
			}
		}
		return ""
	}), nil
}

// groupRegistries shows keys that nerdctl treats as one registry (same canonical identifier,
// e.g. "host" and "host:443") as one entry: the shortest key, the others as aliases.
func groupRegistries(keys []string, username func(string) string) []Registry {
	groups := map[string][]string{}
	var order []string
	for _, k := range keys {
		id := k
		if u, err := dockerconfigresolver.Parse(k); err == nil {
			id = u.CanonicalIdentifier()
		}
		if _, ok := groups[id]; !ok {
			order = append(order, id)
		}
		groups[id] = append(groups[id], k)
	}
	out := make([]Registry, 0, len(order))
	for _, id := range order {
		ks := groups[id]
		slices.SortFunc(ks, func(a, b string) int {
			if len(a) != len(b) {
				return len(a) - len(b)
			}
			return strings.Compare(a, b)
		})
		r := Registry{Server: ks[0], Aliases: ks[1:]}
		for _, k := range ks {
			if r.Username = username(k); r.Username != "" {
				break
			}
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Registry) int { return strings.Compare(a.Server, b.Server) })
	return out
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
	// The docker file store erases unknown keys without an error: check first.
	if !loggedIn(server) {
		return fmt.Errorf("%w: not logged in to %s", errdefs.ErrNotFound, server)
	}
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

// loggedIn reports whether credentials for server (or one of its variants) are stored.
func loggedIn(server string) bool {
	u, err := dockerconfigresolver.Parse(server)
	if err != nil {
		return false
	}
	store, err := dockerconfigresolver.NewCredentialsStore("")
	if err != nil {
		return false
	}
	c, err := store.Retrieve(u, true)
	return err == nil && (c.Username != "" || c.Password != "" || c.IdentityToken != "" || c.RegistryToken != "")
}
