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

package version

import (
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGet(t *testing.T) {
	info := Get()

	assert.NotEmpty(t, info.Version, "Version should not be empty")
	assert.NotEmpty(t, info.BuildTime, "BuildTime should not be empty")
	assert.NotEmpty(t, info.GitCommit, "GitCommit should not be empty")
	assert.NotEmpty(t, info.GoVersion, "GoVersion should not be empty")
	assert.NotEmpty(t, info.Platform, "Platform should not be empty")

	// Verify platform format
	assert.Contains(t, info.Platform, "/", "Platform should contain OS/ARCH separator")
}

func TestString(t *testing.T) {
	str := String()

	assert.NotEmpty(t, str, "String should not be empty")
	assert.Contains(t, str, "QuicBaaS", "String should contain QuicBaaS")
	assert.Contains(t, str, Version, "String should contain version")
	assert.Contains(t, str, GitCommit, "String should contain git commit")
	assert.Contains(t, str, BuildTime, "String should contain build time")
	assert.Contains(t, str, GoVersion, "String should contain Go version")
}

func TestShort(t *testing.T) {
	short := Short()

	assert.NotEmpty(t, short, "Short should not be empty")
	assert.Equal(t, Version, short, "Short should return version")
}

func TestFull(t *testing.T) {
	full := Full()

	assert.NotEmpty(t, full, "Full should not be empty")
	assert.Contains(t, full, "QuicBaaS Version Information", "Full should contain header")
	assert.Contains(t, full, Version, "Full should contain version")
	assert.Contains(t, full, GitCommit, "Full should contain git commit")
	assert.Contains(t, full, BuildTime, "Full should contain build time")
	assert.Contains(t, full, GoVersion, "Full should contain Go version")

	// Verify platform is included
	expectedPlatform := runtime.GOOS + "/" + runtime.GOARCH
	assert.Contains(t, full, expectedPlatform, "Full should contain platform")
}

func TestBuildInfo(t *testing.T) {
	info := BuildInfo{
		Version:   "1.0.0",
		BuildTime: "2025-10-05",
		GitCommit: "abc123",
		GoVersion: "go1.21.0",
		Platform:  "linux/amd64",
	}

	assert.Equal(t, "1.0.0", info.Version)
	assert.Equal(t, "2025-10-05", info.BuildTime)
	assert.Equal(t, "abc123", info.GitCommit)
	assert.Equal(t, "go1.21.0", info.GoVersion)
	assert.Equal(t, "linux/amd64", info.Platform)
}

func TestVersionVariables(t *testing.T) {
	// Test that version variables have expected values or defaults
	assert.NotEmpty(t, Version, "Version should not be empty")
	assert.NotEmpty(t, BuildTime, "BuildTime should not be empty")
	assert.NotEmpty(t, GitCommit, "GitCommit should not be empty")
	assert.NotEmpty(t, GoVersion, "GoVersion should not be empty")

	// GoVersion should start with "go"
	assert.True(t, strings.HasPrefix(GoVersion, "go"), "GoVersion should start with 'go'")
}

func TestGetConsistency(t *testing.T) {
	// Multiple calls should return consistent data
	info1 := Get()
	info2 := Get()

	assert.Equal(t, info1.Version, info2.Version)
	assert.Equal(t, info1.BuildTime, info2.BuildTime)
	assert.Equal(t, info1.GitCommit, info2.GitCommit)
	assert.Equal(t, info1.GoVersion, info2.GoVersion)
	assert.Equal(t, info1.Platform, info2.Platform)
}

// TestBuildTimeInjection verifies that build-time variables can be injected.
func TestBuildTimeInjection(t *testing.T) {
	// Save original values
	origVersion := Version
	origBuildTime := BuildTime
	origGitCommit := GitCommit

	// Simulate build-time injection
	Version = "1.2.3"
	BuildTime = "2025-10-05_12:34:56"
	GitCommit = "abc123def456"

	// Verify injection worked
	info := Get()
	assert.Equal(t, "1.2.3", info.Version)
	assert.Equal(t, "2025-10-05_12:34:56", info.BuildTime)
	assert.Equal(t, "abc123def456", info.GitCommit)

	// Restore original values
	Version = origVersion
	BuildTime = origBuildTime
	GitCommit = origGitCommit
}

// BenchmarkGet benchmarks the Get function.
func BenchmarkGet(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Get()
	}
}

// BenchmarkString benchmarks the String function.
func BenchmarkString(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = String()
	}
}

// BenchmarkFull benchmarks the Full function.
func BenchmarkFull(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Full()
	}
}

// ExampleGet demonstrates how to use the Get function.
func ExampleGet() {
	info := Get()
	println(info.Version)
	println(info.Platform)
}

// ExampleString demonstrates how to use the String function.
func ExampleString() {
	version := String()
	println(version)
}

// ExampleShort demonstrates how to use the Short function.
func ExampleShort() {
	version := Short()
	println(version)
}

// ExampleFull demonstrates how to use the Full function.
func ExampleFull() {
	version := Full()
	println(version)
}
