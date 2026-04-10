//go:build rag

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

package query

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultStreamConfig(t *testing.T) {
	cfg := DefaultStreamConfig()

	assert.Equal(t, 10, cfg.BufferSize)
	assert.Equal(t, 1, cfg.FlushInterval)
}

func TestStreamConfig_CustomValues(t *testing.T) {
	cfg := StreamConfig{
		BufferSize:    20,
		FlushInterval: 5,
	}

	assert.Equal(t, 20, cfg.BufferSize)
	assert.Equal(t, 5, cfg.FlushInterval)
}

func TestStreamConfig_ZeroValues(t *testing.T) {
	cfg := StreamConfig{}

	assert.Equal(t, 0, cfg.BufferSize)
	assert.Equal(t, 0, cfg.FlushInterval)
}
