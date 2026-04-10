//go:build !iam && !timeseries

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

// Package main provides tests for stub commands when features are not enabled.
package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// Stub command execution coverage
// =============================================================================
// These tests exercise the RunE function of stub commands that are compiled
// when their respective build tags (iam, timeseries) are not set.

func TestIAMStubCommand_Execute(t *testing.T) {
	cmd := newIAMCommand()
	assert.NotNil(t, cmd)

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	err := cmd.RunE(cmd, nil)
	assert.NoError(t, err)
	output := stdout.String()
	assert.Contains(t, output, "IAM feature is not enabled")
	assert.Contains(t, output, "go build -tags=iam")
}

func TestTimeSeriesStubCommand_Execute(t *testing.T) {
	cmd := newTimeSeriesCommands()
	assert.NotNil(t, cmd)

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	err := cmd.RunE(cmd, nil)
	assert.NoError(t, err)
	output := stdout.String()
	assert.Contains(t, output, "Time series feature is not enabled")
	assert.Contains(t, output, "go build -tags=timeseries")
}
