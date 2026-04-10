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

// Package main provides tests for formatFetchResult with invalid output
// format to cover the ParseOutputFormat error path.
package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	clisecrets "github.com/jeremyhahn/go-quicbaas/pkg/cli/secrets"
)

func TestFormatFetchResult_InvalidFormat(t *testing.T) {
	result := &clisecrets.FetchResult{
		SecretsCount: 5,
		KeysCount:    10,
	}
	var buf bytes.Buffer
	err := formatFetchResult(result, "invalid_xyz", &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_xyz")
}
