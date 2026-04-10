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

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/version"
)

func TestVersionCommand(t *testing.T) {
	// Capture output
	var buf bytes.Buffer
	versionCmd.SetOut(&buf)

	// Execute command
	versionCmd.Run(versionCmd, []string{})

	output := buf.String()

	// Verify output contains expected content
	assert.Contains(t, output, "QuicBaaS Version Information", "Output should contain header")
	assert.Contains(t, output, "Version:", "Output should contain version label")
	assert.Contains(t, output, "Git Commit:", "Output should contain git commit label")
	assert.Contains(t, output, "Build Time:", "Output should contain build time label")
	assert.Contains(t, output, "Go Version:", "Output should contain Go version label")
	assert.Contains(t, output, "Platform:", "Output should contain platform label")
}

func TestVersionCommandShort(t *testing.T) {
	// Set short flag
	versionShort = true
	defer func() { versionShort = false }()

	// Capture output
	var buf bytes.Buffer
	versionCmd.SetOut(&buf)

	// Execute command
	versionCmd.Run(versionCmd, []string{})

	output := strings.TrimSpace(buf.String())

	// Verify output is just the version number
	assert.Equal(t, version.Short(), output, "Short output should match version.Short()")
	assert.NotContains(t, output, "Version:", "Short output should not contain labels")
	assert.NotContains(t, output, "Git Commit:", "Short output should not contain labels")
}

func TestVersionCommandWithFlags(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		shortFlag  bool
		expectFull bool
	}{
		{
			name:       "no flags - full version",
			args:       []string{},
			shortFlag:  false,
			expectFull: true,
		},
		{
			name:       "short flag",
			args:       []string{"--short"},
			shortFlag:  true,
			expectFull: false,
		},
		{
			name:       "short flag abbreviated",
			args:       []string{"-s"},
			shortFlag:  true,
			expectFull: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset flag
			versionShort = false

			// Capture output
			var buf bytes.Buffer
			versionCmd.SetOut(&buf)
			versionCmd.SetArgs(tt.args)

			// Parse flags
			err := versionCmd.ParseFlags(tt.args)
			require.NoError(t, err)

			// Execute command
			versionCmd.Run(versionCmd, []string{})

			output := buf.String()

			if tt.expectFull {
				assert.Contains(t, output, "QuicBaaS Version Information")
				assert.Contains(t, output, "Version:")
			} else {
				assert.NotContains(t, output, "QuicBaaS Version Information")
				assert.NotContains(t, output, "Version:")
			}
		})
	}
}

func TestVersionCommandHelp(t *testing.T) {
	// Create a fresh command to avoid state pollution
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Display version information",
		Long: `Display detailed version information including:
  - Version number
  - Git commit hash
  - Build timestamp
  - Go version
  - Platform information`,
		Run: runVersion,
	}
	var shortFlag bool
	cmd.Flags().BoolVarP(&shortFlag, "short", "s", false, "print only the version number")

	// Capture output
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	// Execute command
	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()

	// Verify help text
	assert.Contains(t, output, "Display detailed version information", "Help should contain description")
	assert.Contains(t, output, "--short", "Help should contain short flag")
	assert.Contains(t, output, "-s,", "Help should contain short flag abbreviation")
}

func TestRunVersion(t *testing.T) {
	tests := []struct {
		name       string
		shortFlag  bool
		expectFull bool
	}{
		{
			name:       "full version",
			shortFlag:  false,
			expectFull: true,
		},
		{
			name:       "short version",
			shortFlag:  true,
			expectFull: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set flag
			versionShort = tt.shortFlag
			defer func() { versionShort = false }()

			// Capture output
			var buf bytes.Buffer
			versionCmd.SetOut(&buf)

			// Run command
			runVersion(versionCmd, []string{})

			output := buf.String()

			// Verify output
			if tt.expectFull {
				// Full version should contain all information
				assert.Contains(t, output, "QuicBaaS Version Information")
				assert.Contains(t, output, version.Get().Version)
				assert.Contains(t, output, version.Get().GitCommit)
			} else {
				// Short version should only contain version number
				output = strings.TrimSpace(output)
				assert.Equal(t, version.Short(), output)
			}
		})
	}
}

func TestVersionCommandIntegration(t *testing.T) {
	// Test full command execution using version cmd directly
	var buf bytes.Buffer
	versionCmd.SetOut(&buf)

	runVersion(versionCmd, []string{})

	output := buf.String()
	assert.Contains(t, output, "QuicBaaS Version Information")
}

func TestVersionCommandIntegrationShort(t *testing.T) {
	// Set short flag
	versionShort = true
	defer func() { versionShort = false }()

	// Test short version using version cmd directly
	var buf bytes.Buffer
	versionCmd.SetOut(&buf)

	runVersion(versionCmd, []string{})

	output := strings.TrimSpace(buf.String())
	assert.Equal(t, version.Short(), output)
}

// BenchmarkVersionCommand benchmarks the version command.
func BenchmarkVersionCommand(b *testing.B) {
	var buf bytes.Buffer
	versionCmd.SetOut(&buf)

	for i := 0; i < b.N; i++ {
		buf.Reset()
		versionCmd.Run(versionCmd, []string{})
	}
}

// BenchmarkVersionCommandShort benchmarks the short version command.
func BenchmarkVersionCommandShort(b *testing.B) {
	versionShort = true
	defer func() { versionShort = false }()

	var buf bytes.Buffer
	versionCmd.SetOut(&buf)

	for i := 0; i < b.N; i++ {
		buf.Reset()
		versionCmd.Run(versionCmd, []string{})
	}
}
