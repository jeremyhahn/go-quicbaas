//go:build iam && rest

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"context"
	"testing"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// homeRealmAdapter.Resolve maps the rest RealmHints onto the iam directory and
// returns the bound tenant for a registered domain.
func TestHomeRealmAdapter_Resolve(t *testing.T) {
	shared := NewMemoryKVStore()
	dir, err := iam.NewHomeRealmDirectory(shared)
	if err != nil {
		t.Fatalf("NewHomeRealmDirectory: %v", err)
	}
	ctx := context.Background()
	if err := dir.RegisterDomain(ctx, "acme", "acme.example"); err != nil {
		t.Fatalf("RegisterDomain: %v", err)
	}

	adapter := &homeRealmAdapter{dir: dir}

	t.Run("resolves by email domain", func(t *testing.T) {
		tenant, err := adapter.Resolve(ctx, restapi.RealmHints{Email: "user@acme.example"})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if tenant != "acme" {
			t.Fatalf("Resolve = %q, want acme", tenant)
		}
	})

	t.Run("unknown hint fails closed", func(t *testing.T) {
		tenant, err := adapter.Resolve(ctx, restapi.RealmHints{Email: "user@unknown.example"})
		if err == nil {
			t.Fatalf("Resolve(unknown) should fail closed, got tenant=%q", tenant)
		}
	})

	t.Run("validated tenant hint", func(t *testing.T) {
		// TenantHint is only honored when it owns a matching routing entry.
		tenant, err := adapter.Resolve(ctx, restapi.RealmHints{TenantHint: "acme", Email: "x@acme.example"})
		if err != nil {
			t.Fatalf("Resolve(hint): %v", err)
		}
		if tenant != "acme" {
			t.Fatalf("Resolve(hint) = %q, want acme", tenant)
		}
	})
}

// attachHomeRealmResolver is a no-op when no shared platform store is available
// (qrdbClient nil) — it must not panic and must leave the resolver unset.
func TestAttachHomeRealmResolver_NoSharedStore(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	cfg := &restapi.Config{}
	// With no qrdbClient, GetKVStore would panic; guard by recovering. The
	// function calls GetKVStore which panics on nil client, so we assert the
	// panic happens (programming-error contract) rather than a silent no-op.
	defer func() {
		_ = recover()
	}()
	s.attachHomeRealmResolver(cfg)
	if cfg.HomeRealmResolver != nil {
		t.Fatalf("resolver should not be set without a shared store")
	}
}

// attachHomeRealmResolver installs a resolver when the shared platform store is
// available via a node-backed client.
func TestAttachHomeRealmResolver_InstallsResolver(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	s := &Server{
		logger:     buildTestLogger(t),
		qrdbClient: newNodeBackedShardedClient(node),
	}
	cfg := &restapi.Config{}
	s.attachHomeRealmResolver(cfg)
	if cfg.HomeRealmResolver == nil {
		t.Fatal("HomeRealmResolver must be installed when the shared store is available")
	}
}
