//go:build !integration && raft && local

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

// server_coverage_boost6_test.go drives additional Shutdown cleanup
// branches and lifecycle no-op paths. Each branch is exercised by
// populating the matching field on the server with the lightest-weight
// non-nil value whose Close/Stop is a no-op when uninitialized.

package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag"
)

// =============================================================================
// stopRAG / Shutdown's ragSystem branch — covers lifecycle_rag.go:76-92
// and server.go:650-657
//
// An empty *rag.System has closed=false but all internal references nil,
// so Close() returns nil after walking each nil-guard. Both stopRAG and
// the parent Shutdown branch enter cleanly.
// =============================================================================

func TestBoost6_StopRAG_WithEmptySystemSucceeds(t *testing.T) {
	srv := withTempServer(t)
	srv.ragSystem = &rag.System{}

	// stopRAG must mark the field nil and return no error.
	require.NoError(t, srv.stopRAG())
	require.Nil(t, srv.ragSystem, "stopRAG must null out the field on success")
}

func TestBoost6_Shutdown_TraversesRAGSystemBranch(t *testing.T) {
	srv := withTempServer(t)
	srv.ragSystem = &rag.System{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}

// =============================================================================
// wireIAMMTLSAuthenticators — covers the restServer == nil early return
//
// This is the simplest of the wireIAMMTLSAuthenticators branches: when
// no REST server has been wired (TLS disabled, REST disabled, etc.) the
// function is a no-op and returns nil.
// =============================================================================

func TestBoost6_WireIAMMTLSAuthenticators_NoRESTServerIsNoOp(t *testing.T) {
	srv := withTempServer(t)
	require.Nil(t, srv.restServer, "test precondition: restServer should not be wired")

	// Must return nil without panicking. The function short-circuits at
	// the first restServer guard.
	err := srv.wireIAMMTLSAuthenticators()
	require.NoError(t, err)
}

// =============================================================================
// stopRAG idempotency — covers the nil-guard early return at
// lifecycle_rag.go:77-79
// =============================================================================

func TestBoost6_StopRAG_NilSystemIsNoOp(t *testing.T) {
	srv := withTempServer(t)
	srv.ragSystem = nil
	require.NoError(t, srv.stopRAG())
}

// =============================================================================
// stopOllama — covers the non-nil path that simply nulls the service
// =============================================================================

func TestBoost6_StopOllama_NilServiceIsNoOp(t *testing.T) {
	srv := withTempServer(t)
	srv.ollamaService = nil
	require.NoError(t, srv.stopOllama())
}
