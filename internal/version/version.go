// Package version exposes build metadata injected at link time via -ldflags.
package version

import "runtime"

// These variables are overridden at build time, e.g.:
//
//	-X github.com/pessom/nerdui/internal/version.Version=v0.1.0
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// Info describes the running nerdui build.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`
}

// Get returns build information for the current binary.
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
}

// String returns a single-line human readable version.
func (i Info) String() string {
	return "nerdui " + i.Version + " (commit " + i.Commit + ", built " + i.Date + ", " + i.GoVersion + " " + i.Platform + ")"
}
