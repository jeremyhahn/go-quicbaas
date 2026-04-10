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

//go:build !otel

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInitTracingNoOtel tests tracing initialization when OTEL is disabled.
func TestInitTracingNoOtel(t *testing.T) {
	t.Run("ReturnsNoOpFunction", func(t *testing.T) {
		shutdownFunc, err := initTracing()
		require.NoError(t, err)
		require.NotNil(t, shutdownFunc)

		// The shutdown function should be a no-op and return nil
		ctx := context.Background()
		err = shutdownFunc(ctx)
		assert.NoError(t, err)
	})

	t.Run("MultipleCallsSucceed", func(t *testing.T) {
		// Should be safe to call multiple times
		shutdownFunc1, err := initTracing()
		require.NoError(t, err)
		require.NotNil(t, shutdownFunc1)

		shutdownFunc2, err := initTracing()
		require.NoError(t, err)
		require.NotNil(t, shutdownFunc2)

		// Both shutdown functions should work
		ctx := context.Background()
		assert.NoError(t, shutdownFunc1(ctx))
		assert.NoError(t, shutdownFunc2(ctx))
	})
}
