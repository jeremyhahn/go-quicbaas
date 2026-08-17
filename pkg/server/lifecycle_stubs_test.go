//go:build !integration && !rest && !quic && !mcp && !vector && !rag

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

// lifecycle_stubs_test.go covers the no-op stub variants of the lifecycle
// subsystems that compile when their build tag is absent. The build
// constraint pins this file to the lean default build so the stub source
// is visible; when the rest/quic/mcp/vector/rag tags are set the real
// implementations live in tag-gated files instead. The stubs exist so
// callers of Server.start*/stop* keep working regardless of build tags;
// these tests assert the contract that "no subsystem compiled" means "no
// error returned".

package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLifecycle_StopRESTServer_NoServerIsNoOp(t *testing.T) {
	srv := withTempServer(t)
	srv.restServer = nil
	assert.NoError(t, srv.stopRESTServer(context.Background()))
}

func TestLifecycle_StopEmbedding_NoOp(t *testing.T) {
	srv := withTempServer(t)
	assert.NoError(t, srv.stopEmbedding())
}

func TestLifecycle_StartRESTServer_NoOp(t *testing.T) {
	srv := withTempServer(t)
	require.NoError(t, srv.startRESTServer())
}
