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
	"time"
)

func testProvider(tenantID, id, name string) *ExternalProvider {
	return &ExternalProvider{
		ID:       id,
		TenantID: tenantID,
		Name:     name,
		Type:     ProviderTypeOAuth2,
	}
}

func testLink(tenantID, userID, id, providerID, externalID string) *FederatedIdentity {
	return &FederatedIdentity{
		ID:         id,
		UserID:     userID,
		TenantID:   tenantID,
		ProviderID: providerID,
		ExternalID: externalID,
		LastLogin:  time.Now(),
	}
}

func testState(tenantID, nonce, providerID string) *LoginState {
	return &LoginState{
		Nonce:       nonce,
		ProviderID:  providerID,
		TenantID:    tenantID,
		RedirectURI: "https://example.com/cb",
		ExpiresAt:   time.Now().Add(10 * time.Minute),
	}
}

// (a) Provider lands in the barrier tenant's store, not the shared store.
func TestBackendProviderStorage_BarrierIsolation(t *testing.T) {
	kv := newRoutingTestKV()
	tenantStore := kv.barrierTenant("acme")
	store := NewBackendProviderStorage(kv)
	ctx := context.Background()

	if err := store.CreateProvider(ctx, testProvider("acme", "p1", "Okta")); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	if kv.shared.keyCount() != 0 {
		t.Fatal("provider leaked into shared store")
	}
	if tenantStore.keyCount() == 0 {
		t.Fatal("provider not in barrier store")
	}

	got, err := store.GetProvider(ctx, "acme", "p1")
	if err != nil || got.Name != "Okta" {
		t.Fatalf("GetProvider = %v, %v", got, err)
	}
	byName, err := store.GetProviderByName(ctx, "acme", "Okta")
	if err != nil || byName.ID != "p1" {
		t.Fatalf("GetProviderByName = %v, %v", byName, err)
	}
}

// (c) Provider sealed fail-closed.
func TestBackendProviderStorage_SealedFailsClosed(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	kv.sealTenant("acme")
	store := NewBackendProviderStorage(kv)
	ctx := context.Background()

	if err := store.CreateProvider(ctx, testProvider("acme", "p1", "Okta")); !errors.Is(err, errTenantSealedTest) {
		t.Fatalf("CreateProvider(sealed) err = %v, want sealed", err)
	}
	if kv.shared.keyCount() != 0 {
		t.Fatalf("sealed op wrote to shared store: %d", kv.shared.keyCount())
	}
}

// (b) Link by-ID lookup parses tenant from encoded id; external/user indexes
// route by context tenant.
func TestBackendFederationStorage_LinkRouting(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendFederationStorage(kv)
	ctx := ContextWithTenant(context.Background(), "acme")

	link := testLink("acme", "u1", "l1", "p1", "ext-123")
	if err := store.CreateLink(ctx, link); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if kv.shared.keyCount() != 0 {
		t.Fatal("link leaked into shared store")
	}
	if tenant, _ := decodeTenantID(link.ID); tenant != "acme" {
		t.Fatalf("link id not tenant-encoded: %q", link.ID)
	}

	// GetLink with empty context resolves via the encoded id.
	got, err := store.GetLink(context.Background(), link.ID)
	if err != nil || got.UserID != "u1" {
		t.Fatalf("GetLink = %v, %v", got, err)
	}

	byExt, err := store.GetLinkByExternal(ctx, "p1", "ext-123")
	if err != nil || byExt.ID != link.ID {
		t.Fatalf("GetLinkByExternal = %v, %v", byExt, err)
	}

	byUser, err := store.GetLinksByUser(ctx, "u1")
	if err != nil || len(byUser) != 1 {
		t.Fatalf("GetLinksByUser = %d, %v; want 1, nil", len(byUser), err)
	}

	if err := store.DeleteUserLinks(ctx, "u1"); err != nil {
		t.Fatalf("DeleteUserLinks: %v", err)
	}
	if _, err := store.GetLink(context.Background(), link.ID); !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("after delete GetLink err = %v, want ErrLinkNotFound", err)
	}
}

// State token by-token lookup parses tenant from encoded token.
func TestBackendStateStorage_TokenRouting(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendStateStorage(kv, nil)
	ctx := context.Background()

	state := testState("acme", "nonce-1", "p1")
	if err := store.SaveState(ctx, state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	if kv.shared.keyCount() != 0 {
		t.Fatal("state leaked into shared store")
	}
	if tenant, _ := decodeTenantID(state.Nonce); tenant != "acme" {
		t.Fatalf("state nonce not tenant-encoded: %q", state.Nonce)
	}

	// GetState with empty context resolves via the encoded token.
	got, err := store.GetState(context.Background(), state.Nonce)
	if err != nil || got.ProviderID != "p1" {
		t.Fatalf("GetState = %v, %v", got, err)
	}

	if err := store.DeleteState(context.Background(), state.Nonce); err != nil {
		t.Fatalf("DeleteState: %v", err)
	}
	if _, err := store.GetState(context.Background(), state.Nonce); !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("after delete GetState err = %v, want ErrStateNotFound", err)
	}
}

// State sealed fail-closed.
func TestBackendStateStorage_SealedFailsClosed(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	kv.sealTenant("acme")
	store := NewBackendStateStorage(kv, nil)
	ctx := context.Background()

	if err := store.SaveState(ctx, testState("acme", "nonce-1", "p1")); !errors.Is(err, errTenantSealedTest) {
		t.Fatalf("SaveState(sealed) err = %v, want sealed", err)
	}
	if kv.shared.keyCount() != 0 {
		t.Fatalf("sealed op wrote to shared store: %d", kv.shared.keyCount())
	}
}

// CleanupExpired sweeps states per tenant via the lister.
func TestBackendStateStorage_CleanupExpiredPerTenant(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendStateStorage(kv, staticTenantLister{ids: []string{"acme"}})
	ctx := context.Background()

	expired := testState("acme", "old", "p1")
	expired.ExpiresAt = time.Now().Add(-time.Hour)
	if err := store.SaveState(ctx, expired); err != nil {
		t.Fatalf("SaveState(expired): %v", err)
	}
	if err := store.SaveState(ctx, testState("acme", "new", "p1")); err != nil {
		t.Fatalf("SaveState(fresh): %v", err)
	}

	n, err := store.CleanupExpired(ctx)
	if err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}
	if n != 1 {
		t.Fatalf("CleanupExpired removed %d, want 1", n)
	}
}
