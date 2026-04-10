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

//go:build integration
// +build integration

package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootCommandUsage(t *testing.T) {
	// Capture output
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"--help"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	output := buf.String()

	// Verify help text contains expected content
	assert.Contains(t, output, "quicbaas", "Help should contain command name")
	assert.Contains(t, output, "high-performance, distributed key-value database", "Help should contain description")
	assert.Contains(t, output, "--config", "Help should contain config flag")
	assert.Contains(t, output, "--log-level", "Help should contain log-level flag")
	assert.Contains(t, output, "--fips", "Help should contain fips flag")
	assert.Contains(t, output, "--data-dir", "Help should contain data-dir flag")
}
