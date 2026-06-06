//go:build iam

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

package iam

import (
	"context"
	"errors"
	"testing"
)

// UpdateProvider rejects renaming a provider onto a name another provider owns,
// and reports not-found for an unknown provider.
func TestFederationManager_UpdateProvider_NameCollision(t *testing.T) {
	manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)
	ctx := context.Background()

	p1 := createTestProvider("tenant1", "alpha")
	p1.ID = "p-alpha"
	p1.ApplyDefaults()
	if err := providerStorage.CreateProvider(ctx, p1); err != nil {
		t.Fatalf("CreateProvider(alpha): %v", err)
	}
	p2 := createTestProvider("tenant1", "beta")
	p2.ID = "p-beta"
	p2.ApplyDefaults()
	if err := providerStorage.CreateProvider(ctx, p2); err != nil {
		t.Fatalf("CreateProvider(beta): %v", err)
	}

	// Rename beta to alpha -> collision.
	p2.Name = "alpha"
	if err := manager.UpdateProvider(ctx, p2); err == nil {
		t.Fatal("UpdateProvider onto an existing name should fail")
	}

	// Update a provider that does not exist -> not found.
	missing := createTestProvider("tenant1", "ghost")
	missing.ID = "p-ghost"
	missing.ApplyDefaults()
	if err := manager.UpdateProvider(ctx, missing); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("UpdateProvider(missing) err = %v, want ErrProviderNotFound", err)
	}
}

// DeleteProvider validates input and reports not-found for an unknown provider.
func TestFederationManager_DeleteProvider_Errors(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)
	ctx := context.Background()

	if err := manager.DeleteProvider(ctx, "", "p1"); err == nil {
		t.Fatal("DeleteProvider(empty tenant) want error")
	}
	if err := manager.DeleteProvider(ctx, "tenant1", ""); err == nil {
		t.Fatal("DeleteProvider(empty provider) want error")
	}
	if err := manager.DeleteProvider(ctx, "tenant1", "no-such-provider"); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("DeleteProvider(missing) err = %v, want ErrProviderNotFound", err)
	}
}
