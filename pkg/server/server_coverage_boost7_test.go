//go:build !integration && raft && local && iam && vector

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

// server_coverage_boost7_test.go drives the embedding manager cleanup
// branch in Shutdown and stopEmbedding. The embedding manager is only
// available under the vector build tag (see lifecycle_embedding.go vs
// lifecycle_embedding_noop.go).

package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
)

// =============================================================================
// stopEmbedding / Shutdown's embeddingManager branch — covers
// lifecycle_embedding.go:71-83 and server.go:629-636
//
// An empty *embedding.Manager has inner=nil; Close returns nil
// immediately. The Server cleanup path enters the branch, calls
// stopEmbedding, and observes the no-error result.
// =============================================================================

func TestBoost7_StopEmbedding_WithEmptyManagerSucceeds(t *testing.T) {
	srv := withTempServer(t)
	srv.embeddingManager = &embedding.Manager{}

	require.NoError(t, srv.stopEmbedding())
	require.Nil(t, srv.embeddingManager, "stopEmbedding must null out the field on success")
}

func TestBoost7_Shutdown_TraversesEmbeddingManagerBranch(t *testing.T) {
	srv := withTempServer(t)
	srv.embeddingManager = &embedding.Manager{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}
