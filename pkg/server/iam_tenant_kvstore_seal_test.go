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

// iam_tenant_kvstore_seal_test.go drives the seal-enabled (StoreResolver
// present) branches of iamTenantKVStore and iamTenantScopeStore that the
// no-resolver tests cannot reach. A seal-passphrase standalone node exposes a
// per-tenant StoreResolver, so both seams take their resolver-backed path.

package server

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// newSealedStandaloneServer builds a standalone server with a seal-passphrase
// barrier so the embedded qrdb node exposes a per-tenant StoreResolver. It is
// closed on test cleanup.
func newSealedStandaloneServer(t *testing.T) *Server {
	t.Helper()

	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Storage.DataDir = t.TempDir()
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "passphrase"

	srv, err := NewServer(cfg, WithSealPassphrase([]byte("super-secret-passphrase")))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.initStandaloneQRDBNode(); err != nil {
		t.Fatalf("initStandaloneQRDBNode: %v", err)
	}
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	return srv
}

// With a seal-enabled node exposing a StoreResolver, iamTenantKVStore returns the
// per-tenant routing store (the resolver branch) rather than the shared platform
// shard.
func TestIamTenantKVStore_ResolverBranch(t *testing.T) {
	srv := newSealedStandaloneServer(t)

	svcs := srv.qrdbNode.Services()
	if svcs == nil || svcs.StoreResolver == nil {
		t.Fatal("seal-enabled node must expose a per-tenant StoreResolver")
	}

	kv := srv.iamTenantKVStore()
	if kv == nil {
		t.Fatal("iamTenantKVStore must return a routing store with a resolver")
	}
	if _, ok := kv.(*tenantRoutingKVStore); !ok {
		t.Fatalf("expected *tenantRoutingKVStore on the resolver path, got %T", kv)
	}
}

// With a seal-enabled node, iamTenantScopeStore builds the per-tenant scope
// resolver over the StoreResolver (its success branch) instead of returning nil.
func TestIamTenantScopeStore_ResolverBranch(t *testing.T) {
	srv := newSealedStandaloneServer(t)

	svcs := srv.qrdbNode.Services()
	if svcs == nil || svcs.StoreResolver == nil {
		t.Fatal("seal-enabled node must expose a per-tenant StoreResolver")
	}

	if srv.iamTenantScopeStore() == nil {
		t.Fatal("iamTenantScopeStore must build a scope store with a resolver")
	}
}
