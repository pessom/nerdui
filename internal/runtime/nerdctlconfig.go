// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/containerd/nerdctl/v2/pkg/config"
	ncdefaults "github.com/containerd/nerdctl/v2/pkg/defaults"
	"github.com/pelletier/go-toml/v2"
)

// LoadNerdctlConfig reads nerdctl.toml exactly like the nerdctl CLI does ($NERDCTL_TOML or
// the platform default, e.g. /etc/nerdctl/nerdctl.toml) so nerdui uses the same data root
// and CNI paths as the CLI. A missing file yields nerdctl's defaults.
func LoadNerdctlConfig() (*config.Config, string, error) {
	path := ncdefaults.NerdctlTOML()
	if v, ok := os.LookupEnv("NERDCTL_TOML"); ok {
		path = v
	}
	cfg := config.New()
	f, err := os.Open(path) //nolint:gosec // G304: nerdctl's own config path
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return cfg, path, nil
	case err != nil:
		return cfg, path, err
	}
	defer func() { _ = f.Close() }()
	if err := toml.NewDecoder(f).DisallowUnknownFields().Decode(cfg); err != nil {
		return config.New(), path, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, path, nil
}
