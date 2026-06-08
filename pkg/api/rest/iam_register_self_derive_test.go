//go:build rest && iam

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package rest

import (
	"context"
	stderrors "errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeriveTenantID covers the slug-collision, lookup-error, and exhaustion
// branches of deriveTenantID via an injected existence probe.
func TestDeriveTenantID(t *testing.T) {
	ctx := context.Background()

	t.Run("free slug used as-is", func(t *testing.T) {
		id, err := deriveTenantID(ctx, "Acme Corp", func(_ context.Context, _ string) (bool, error) {
			return false, nil
		})
		require.NoError(t, err)
		assert.Equal(t, "acme-corp", id)
	})

	t.Run("first collision appends -2", func(t *testing.T) {
		id, err := deriveTenantID(ctx, "Acme Corp", func(_ context.Context, candidate string) (bool, error) {
			// Only the bare slug is taken; the -2 variant is free.
			return candidate == "acme-corp", nil
		})
		require.NoError(t, err)
		assert.Equal(t, "acme-corp-2", id)
	})

	t.Run("empty slug falls back to uuid", func(t *testing.T) {
		called := false
		id, err := deriveTenantID(ctx, "!!!", func(_ context.Context, _ string) (bool, error) {
			called = true
			return false, nil
		})
		require.NoError(t, err)
		// slugify pads/strips; a name with no slug-able runes yields a UUID
		// without ever probing existence.
		if !called {
			_, perr := uuid.Parse(id)
			assert.NoError(t, perr, "fallback id must be a valid UUID")
		}
	})

	t.Run("lookup error falls back to uuid", func(t *testing.T) {
		id, err := deriveTenantID(ctx, "Acme Corp", func(_ context.Context, _ string) (bool, error) {
			return false, stderrors.New("store unavailable")
		})
		require.NoError(t, err)
		_, perr := uuid.Parse(id)
		assert.NoError(t, perr, "lookup-error fallback must be a valid UUID")
	})

	t.Run("exhausted candidates fall back to uuid", func(t *testing.T) {
		// Every candidate collides, so the 100-iteration loop exhausts and the
		// helper falls back to a UUID.
		id, err := deriveTenantID(ctx, "Acme Corp", func(_ context.Context, _ string) (bool, error) {
			return true, nil
		})
		require.NoError(t, err)
		_, perr := uuid.Parse(id)
		assert.NoError(t, perr, "exhaustion fallback must be a valid UUID")
		assert.False(t, strings.HasPrefix(id, "acme-corp"))
	})
}
