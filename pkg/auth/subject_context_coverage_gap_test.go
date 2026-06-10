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

package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// TestSubjectFromContext_RoundTrip verifies a Subject injected via
// SubjectIntoContext is recoverable, guarding the per-key authz propagation path.
func TestSubjectFromContext_RoundTrip(t *testing.T) {
	want := interfaces.Subject{
		ID:            "user-1",
		Username:      "alice",
		Authenticated: true,
		Roles:         []string{"readwrite"},
	}
	ctx := SubjectIntoContext(context.Background(), want)

	got, ok := SubjectFromContext(ctx)
	assert.True(t, ok, "subject must be present after injection")
	assert.Equal(t, want.ID, got.ID)
	assert.Equal(t, want.Username, got.Username)
	assert.True(t, got.Authenticated)
}

// TestSubjectFromContext_Missing verifies the fail-closed default: with no
// Subject injected the lookup reports absence rather than a phantom principal.
func TestSubjectFromContext_Missing(t *testing.T) {
	got, ok := SubjectFromContext(context.Background())
	assert.False(t, ok, "no subject must report ok==false")
	assert.False(t, got.Authenticated, "zero Subject must be unauthenticated")
}

// TestSystemContext_MarkerSet verifies NewSystemContext marks the context so
// IsSystemContext reports true for intra-process bypass paths.
func TestSystemContext_MarkerSet(t *testing.T) {
	ctx := NewSystemContext(context.Background())
	assert.True(t, IsSystemContext(ctx))
}

// TestSystemContext_NotSetByDefault guards the security invariant that a plain
// (network-derived) context is never treated as a system context.
func TestSystemContext_NotSetByDefault(t *testing.T) {
	assert.False(t, IsSystemContext(context.Background()))
}
