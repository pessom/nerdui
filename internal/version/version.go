// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package version exposes build metadata injected at link time via -ldflags.
package version

import "runtime"

// These variables are overridden at build time, e.g.:
//
//	-X github.com/pessom/nerdui/internal/version.Version=v0.1.0
//
// Source must point to the source code of the running build. AGPL-3.0 section 13
// requires offering the corresponding source to users interacting with nerdui over
// a network, so forks must set it to their own repository.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
	Source  = "https://github.com/pessom/nerdui"
)

// License is the SPDX identifier of the nerdui license.
const License = "AGPL-3.0-only"

// Info describes the running nerdui build.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`
	Source    string `json:"source"`
	License   string `json:"license"`
}

// Get returns build information for the current binary.
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
		Source:    Source,
		License:   License,
	}
}

// String returns a single-line human readable version.
func (i Info) String() string {
	return "nerdui " + i.Version + " (commit " + i.Commit + ", built " + i.Date + ", " + i.GoVersion + " " + i.Platform + ")\n" +
		"License: " + i.License + ", source: " + i.Source
}
