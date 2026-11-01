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

package rag

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemMultipleConcurrentCloseAttempts(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	// Attempt concurrent closes
	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			done <- system.Close()
		}()
	}

	// Collect results - all should be nil (idempotent close)
	for i := 0; i < 10; i++ {
		err := <-done
		assert.NoError(t, err)
	}
}
