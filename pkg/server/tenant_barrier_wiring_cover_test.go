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

package server

import (
	"testing"
)

// buildTenantBarrierService returns nil when the qrdb client is not yet wired,
// so callers skip registering the per-tenant barrier surface in the pre-wire
// state.
func TestBuildTenantBarrierService_NilClient(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	if svc := s.buildTenantBarrierService(); svc != nil {
		t.Fatalf("expected nil facade without a qrdb client, got %T", svc)
	}
}

// buildTenantBarrierService returns nil when ForShard fails (the ShardedClient
// has been closed), rather than constructing a facade over a nil transport.
func TestBuildTenantBarrierService_ForShardError(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	shards := newNodeBackedShardedClient(node)
	if err := shards.Close(); err != nil {
		t.Fatalf("closing sharded client: %v", err)
	}

	s := &Server{logger: buildTestLogger(t), qrdbClient: shards}
	if svc := s.buildTenantBarrierService(); svc != nil {
		t.Fatalf("expected nil facade after ForShard error, got %T", svc)
	}
}

// buildTenantBarrierService builds a non-nil facade over the platform shard's
// qrdb TenantClient on the happy path.
func TestBuildTenantBarrierService_Success(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	s := &Server{
		logger:     buildTestLogger(t),
		qrdbClient: newNodeBackedShardedClient(node),
	}
	if svc := s.buildTenantBarrierService(); svc == nil {
		t.Fatal("expected a non-nil tenant barrier facade on the happy path")
	}
}
