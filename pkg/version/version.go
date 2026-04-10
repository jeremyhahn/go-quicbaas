// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// Package version provides version information for QuicBaaS.
// Build-time variables are injected via ldflags during compilation.
package version

import (
	"fmt"
	"runtime"
)

var (
	// Version is the semantic version number (injected at build time).
	Version = "1.0.0"

	// BuildTime is the build timestamp (injected at build time).
	BuildTime = "unknown"

	// GitCommit is the git commit hash (injected at build time).
	GitCommit = "unknown"

	// GoVersion is the Go version used to build the binary.
	GoVersion = runtime.Version()
)

// BuildInfo contains all version and build information.
type BuildInfo struct {
	Version   string
	BuildTime string
	GitCommit string
	GoVersion string
	Platform  string
}

// Get returns the current build information.
func Get() BuildInfo {
	return BuildInfo{
		Version:   Version,
		BuildTime: BuildTime,
		GitCommit: GitCommit,
		GoVersion: GoVersion,
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}

// String returns a formatted version string.
func String() string {
	return fmt.Sprintf("QuicBaaS %s (commit: %s, built: %s, go: %s)",
		Version, GitCommit, BuildTime, GoVersion)
}

// Short returns a short version string.
func Short() string {
	return Version
}

// Full returns a detailed version string with all information.
func Full() string {
	info := Get()
	return fmt.Sprintf(`QuicBaaS Version Information:
  Version:    %s
  Git Commit: %s
  Build Time: %s
  Go Version: %s
  Platform:   %s`,
		info.Version,
		info.GitCommit,
		info.BuildTime,
		info.GoVersion,
		info.Platform,
	)
}
