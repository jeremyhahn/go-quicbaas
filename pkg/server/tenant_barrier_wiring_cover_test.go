//go:build !integration

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

// buildTenantBarrierService returns nil in the pre-wire state (no embedded
// qrdb node), so callers skip registering the per-tenant barrier surface.
func TestBuildTenantBarrierService_NilClient(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	if svc := s.buildTenantBarrierService(); svc != nil {
		t.Fatalf("expected nil facade without a qrdb node, got %T", svc)
	}
}

// buildTenantBarrierService sources the facade from the NODE's tenant
// servicer, never from the ShardedClient: a Server carrying only a
// ShardedClient (no node) must return nil. The node's own SDK client does
// not register a TenantServicer on its embedded transport, so a
// ShardedClient-backed facade would fail every call with the qrdb embedded
// transport's NilServiceError — the wave-2 tenant-barrier regression.
func TestBuildTenantBarrierService_ShardedClientAloneInsufficient(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	s := &Server{
		logger:     buildTestLogger(t),
		qrdbClient: newNodeBackedShardedClient(node),
		// qrdbNode intentionally nil: the facade must not fall back to the
		// ShardedClient's tenantless node client.
	}
	if svc := s.buildTenantBarrierService(); svc != nil {
		t.Fatalf("expected nil facade without a qrdb node, got %T", svc)
	}
}

// buildTenantBarrierService builds a non-nil facade over the node's tenant
// servicer on the happy path.
func TestBuildTenantBarrierService_Success(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	s := &Server{
		logger:     buildTestLogger(t),
		qrdbNode:   node,
		qrdbClient: newNodeBackedShardedClient(node),
	}
	svc := s.buildTenantBarrierService()
	if svc == nil {
		t.Fatal("expected a non-nil tenant barrier facade on the happy path")
	}
	if s.tenantSDKClient == nil {
		t.Fatal("expected the tenant SDK client to be cached")
	}
	t.Cleanup(func() { _ = s.tenantSDKClient.Close() })
}
