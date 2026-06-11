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
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// A serverPermissionResolver over a server with no embedded qrdb node returns a
// nil RBAC client (the token service then falls back to the OIDC scope) rather
// than panicking. This is the fail-open-to-OIDC branch the RBAC seam documents.
func TestServerPermissionResolver_NoNodeReturnsNil(t *testing.T) {
	r := &serverPermissionResolver{server: &Server{}}
	if rbac := r.RBACClient(); rbac != nil {
		t.Fatalf("expected nil RBAC client without a node, got %T", rbac)
	}
}

// With an embedded qrdb node the resolver returns the node's live RBAC client,
// proving the per-token RBAC decision is routed to the single qrdb PDP.
func TestServerPermissionResolver_NodeReturnsRBACClient(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	r := &serverPermissionResolver{server: &Server{qrdbNode: node}}
	if rbac := r.RBACClient(); rbac == nil {
		t.Fatal("expected a non-nil RBAC client from the embedded node")
	}
}

// A role-client resolver built with a nil ShardedClient fails closed with a
// typed QRDBClientNotInitializedError instead of returning a nil RoleClient.
func TestPlatformRoleClientResolver_NoShardsFailsClosed(t *testing.T) {
	r := newPlatformRoleClientResolver(nil)
	rc, err := r.RoleClient(context.Background())
	if err == nil {
		t.Fatal("expected a typed error when no ShardedClient is present")
	}
	if rc != nil {
		t.Fatalf("expected nil RoleClient, got %T", rc)
	}
	var notInit *QRDBClientNotInitializedError
	if !errors.As(err, &notInit) {
		t.Fatalf("expected QRDBClientNotInitializedError, got %T: %v", err, err)
	}
}

// A nil resolver also fails closed rather than panicking.
func TestPlatformRoleClientResolver_NilFailsClosed(t *testing.T) {
	var r *platformRoleClientResolver
	if _, err := r.RoleClient(context.Background()); err == nil {
		t.Fatal("expected a typed error from a nil resolver")
	}
}

// When ForShard fails (the ShardedClient has been closed), the resolver wraps
// the failure in a typed ShardClientError carrying the platform shard ID rather
// than returning a nil RoleClient.
func TestPlatformRoleClientResolver_ForShardErrorWrapped(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	shards := newNodeBackedShardedClient(node)
	if err := shards.Close(); err != nil {
		t.Fatalf("closing sharded client: %v", err)
	}

	r := newPlatformRoleClientResolver(shards)
	rc, err := r.RoleClient(context.Background())
	if err == nil {
		t.Fatal("expected an error after the ShardedClient was closed")
	}
	if rc != nil {
		t.Fatalf("expected nil RoleClient, got %T", rc)
	}
	var shardErr *ShardClientError
	if !errors.As(err, &shardErr) {
		t.Fatalf("expected ShardClientError, got %T: %v", err, err)
	}
}

// On the happy path the resolver returns the platform shard's qrdb RoleClient,
// proving role mutations are routed to the same canonical role store the PDP
// reads.
func TestPlatformRoleClientResolver_Success(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	shards := newNodeBackedShardedClient(node)
	r := newPlatformRoleClientResolver(shards)

	rc, err := r.RoleClient(context.Background())
	if err != nil {
		t.Fatalf("RoleClient: %v", err)
	}
	if rc == nil {
		t.Fatal("expected a non-nil RoleClient on the happy path")
	}
}

// buildPlatformValidator constructs a usable validator from a live OIDC provider:
// it sources the issuer/audience, the live JWKS key source, the liveness backend,
// and the per-tenant mode resolver. This drives the full happy path (issuer +
// deployment-default + cache-TTL + NewValidator) that the per-transport authz
// gates share.
func TestBuildPlatformValidator_Success(t *testing.T) {
	provider := newTestOIDCProvider(t)
	s := &Server{
		logger:          buildTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: &grpcOIDCGetterService{provider: provider},
	}

	v, err := s.buildPlatformValidator(context.Background())
	if err != nil {
		t.Fatalf("buildPlatformValidator: %v", err)
	}
	if v == nil {
		t.Fatal("expected a non-nil validator on the happy path")
	}
}
