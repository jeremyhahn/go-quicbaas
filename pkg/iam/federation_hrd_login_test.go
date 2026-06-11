// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package iam

import (
	"context"
	"errors"
	"testing"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// newHRDFederationManager wires a federation manager with a provider resolver
// backed by a real HomeRealmDirectory and one registered tenant domain.
func newHRDFederationManager(t *testing.T) (*DefaultFederationManager, *MemoryProviderStorage, *HomeRealmDirectory) {
	t.Helper()
	providerStorage := NewMemoryProviderStorage()
	dir, err := NewHomeRealmDirectory(newMockKVStore())
	if err != nil {
		t.Fatalf("NewHomeRealmDirectory: %v", err)
	}
	ctx := context.Background()
	if err := dir.RegisterDomain(ctx, "tenant-acme", "acme.example"); err != nil {
		t.Fatalf("RegisterDomain: %v", err)
	}

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  newMockUserStorage(),
		GroupStorage: newMockGroupStorage(),
		RoleStorage:  newMockRoleStorage(),
	})

	manager, err := NewFederationManager(
		WithProviderStorage(providerStorage),
		WithLinkStorage(NewMemoryFederationStorage()),
		WithStateStorage(NewMemoryStateStorage()),
		WithFederationIdentityStore(identityStore),
		WithProviderResolver(dir),
		WithClientCacheTTL(1*time.Hour),
		WithStateExpiry(5*time.Minute),
	)
	if err != nil {
		t.Fatalf("NewFederationManager: %v", err)
	}
	return manager, providerStorage, dir
}

// BeginLogin with an empty provider name uses HRD selection within the tenant:
// a single configured provider resolves unambiguously and produces an auth URL.
func TestBeginLogin_EmptyProviderUsesHRDSingleProvider(t *testing.T) {
	manager, providerStorage, _ := newHRDFederationManager(t)
	ctx := context.Background()

	p := createTestProvider("tenant-acme", "okta")
	if err := manager.RegisterProvider(ctx, p); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}
	_ = providerStorage

	redirect, err := manager.BeginLogin(ctx, "tenant-acme", "", "https://app/cb")
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	if redirect.AuthURL == "" {
		t.Fatal("expected an auth URL for the HRD-selected provider")
	}
	if redirect.ProviderID != p.ID {
		t.Fatalf("ProviderID = %q, want %q", redirect.ProviderID, p.ID)
	}
}

// BeginLogin still rejects a missing tenant.
func TestBeginLogin_MissingTenant(t *testing.T) {
	manager, _, _ := newHRDFederationManager(t)
	_, err := manager.BeginLogin(context.Background(), "", "", "https://app/cb")
	if !errors.As(err, new(*dberrors.QBError)) {
		t.Fatalf("expected QBError for missing tenant, got %v", err)
	}
}

// The explicit-provider-name path is unaffected by the HRD wiring.
func TestBeginLogin_ExplicitNameStillWorks(t *testing.T) {
	manager, _, _ := newHRDFederationManager(t)
	ctx := context.Background()
	p := createTestProvider("tenant-acme", "okta")
	if err := manager.RegisterProvider(ctx, p); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}

	redirect, err := manager.BeginLogin(ctx, "tenant-acme", "okta", "https://app/cb")
	if err != nil {
		t.Fatalf("BeginLogin(explicit): %v", err)
	}
	if redirect.AuthURL == "" {
		t.Fatal("expected an auth URL")
	}
}

// BeginLoginHRD routes a local-realm tenant to the local-factor path: the
// redirect carries the local sentinel and no auth URL.
func TestBeginLoginHRD_LocalRealm(t *testing.T) {
	manager, _, dir := newHRDFederationManager(t)
	ctx := context.Background()
	if err := dir.SetDefaultProvider(ctx, "tenant-acme", LocalRealmProviderID); err != nil {
		t.Fatalf("SetDefaultProvider(local): %v", err)
	}

	redirect, err := manager.BeginLoginHRD(ctx, RealmHints{Email: "u@acme.example"}, "https://app/cb")
	if err != nil {
		t.Fatalf("BeginLoginHRD: %v", err)
	}
	if redirect.AuthURL != "" {
		t.Fatalf("local realm must not produce an auth URL, got %q", redirect.AuthURL)
	}
	if redirect.ProviderID != LocalRealmProviderID {
		t.Fatalf("ProviderID = %q, want local sentinel", redirect.ProviderID)
	}
}

// BeginLoginHRD routes an email domain to the resolved external IdP.
func TestBeginLoginHRD_ExternalProvider(t *testing.T) {
	manager, _, _ := newHRDFederationManager(t)
	ctx := context.Background()
	p := createTestProvider("tenant-acme", "okta")
	if err := manager.RegisterProvider(ctx, p); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}

	redirect, err := manager.BeginLoginHRD(ctx, RealmHints{Email: "u@acme.example"}, "https://app/cb")
	if err != nil {
		t.Fatalf("BeginLoginHRD: %v", err)
	}
	if redirect.AuthURL == "" || redirect.ProviderID != p.ID {
		t.Fatalf("redirect = %+v, want external provider auth URL", redirect)
	}
}

// BeginLoginHRD fails closed when no resolver is configured.
func TestBeginLoginHRD_NoResolver(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)
	_, err := manager.BeginLoginHRD(context.Background(), RealmHints{Email: "u@acme.example"}, "cb")
	if !errors.As(err, new(*dberrors.QBError)) {
		t.Fatalf("expected QBError when resolver unconfigured, got %v", err)
	}
}

// BeginLoginHRD surfaces ambiguous resolution as a not-found-class error.
func TestBeginLoginHRD_AmbiguousFailClosed(t *testing.T) {
	manager, _, _ := newHRDFederationManager(t)
	ctx := context.Background()
	if err := manager.RegisterProvider(ctx, createTestProvider("tenant-acme", "okta")); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}
	if err := manager.RegisterProvider(ctx, createTestProvider("tenant-acme", "azure")); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}

	_, err := manager.BeginLoginHRD(ctx, RealmHints{Email: "u@acme.example"}, "cb")
	if err == nil {
		t.Fatal("expected fail-closed error for ambiguous provider selection")
	}
}

// BeginLogin with an empty provider name and no resolver fails closed: a
// provider name is required when home-realm discovery is not configured.
func TestBeginLogin_EmptyProviderNoResolver(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)
	_, err := manager.BeginLogin(context.Background(), "tenant1", "", "cb")
	if !errors.As(err, new(*dberrors.QBError)) {
		t.Fatalf("expected QBError, got %v", err)
	}
}

// BeginLogin via HRD routing to the local realm returns the local sentinel with
// no auth URL (the empty-provider-name path).
func TestBeginLogin_EmptyProviderLocalRealm(t *testing.T) {
	manager, _, dir := newHRDFederationManager(t)
	ctx := context.Background()
	if err := dir.SetDefaultProvider(ctx, "tenant-acme", LocalRealmProviderID); err != nil {
		t.Fatalf("SetDefaultProvider(local): %v", err)
	}
	redirect, err := manager.BeginLogin(ctx, "tenant-acme", "", "cb")
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	if redirect.AuthURL != "" || redirect.ProviderID != LocalRealmProviderID {
		t.Fatalf("redirect = %+v, want local realm", redirect)
	}
}

// A tenant default pointing at a non-existent provider ID fails closed: the
// resolved provider cannot be loaded, so no login is begun.
func TestBeginLogin_DanglingDefaultProviderFailsClosed(t *testing.T) {
	manager, _, dir := newHRDFederationManager(t)
	ctx := context.Background()
	if err := dir.SetDefaultProvider(ctx, "tenant-acme", "ghost-provider"); err != nil {
		t.Fatalf("SetDefaultProvider: %v", err)
	}
	_, err := manager.BeginLogin(ctx, "tenant-acme", "", "cb")
	if err == nil {
		t.Fatal("expected fail-closed error for dangling default provider")
	}
}

// BeginLogin with an empty provider name and an ambiguous tenant (multiple
// enabled providers, no default) fails closed via the resolver.
func TestBeginLogin_EmptyProviderAmbiguousFailsClosed(t *testing.T) {
	manager, _, _ := newHRDFederationManager(t)
	ctx := context.Background()
	if err := manager.RegisterProvider(ctx, createTestProvider("tenant-acme", "okta")); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}
	if err := manager.RegisterProvider(ctx, createTestProvider("tenant-acme", "azure")); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}
	_, err := manager.BeginLogin(ctx, "tenant-acme", "", "cb")
	if !errors.As(err, new(*dberrors.QBError)) {
		t.Fatalf("expected QBError (fail-closed), got %v", err)
	}
}

// beginLoginFromRealm surfaces a non-not-found provider-store error as an
// invalid-provider error (distinct from the not-found fail-closed path).
func TestBeginLoginHRD_ProviderStoreError(t *testing.T) {
	dir, err := NewHomeRealmDirectory(newMockKVStore())
	if err != nil {
		t.Fatalf("NewHomeRealmDirectory: %v", err)
	}
	ctx := context.Background()
	if err := dir.RegisterDomain(ctx, "tenant-e", "err.example"); err != nil {
		t.Fatalf("RegisterDomain: %v", err)
	}
	if err := dir.SetDefaultProvider(ctx, "tenant-e", "some-idp"); err != nil {
		t.Fatalf("SetDefaultProvider: %v", err)
	}

	storage := &errorProviderStorage{getErr: errors.New("store unavailable")}
	manager, err := NewFederationManager(
		WithProviderStorage(storage),
		WithLinkStorage(NewMemoryFederationStorage()),
		WithStateStorage(NewMemoryStateStorage()),
		WithFederationIdentityStore(NewIdentityStore(IdentityStoreOptions{
			UserStorage:  newMockUserStorage(),
			GroupStorage: newMockGroupStorage(),
			RoleStorage:  newMockRoleStorage(),
		})),
		WithProviderResolver(dir),
	)
	if err != nil {
		t.Fatalf("NewFederationManager: %v", err)
	}

	_, err = manager.BeginLoginHRD(ctx, RealmHints{Email: "u@err.example"}, "cb")
	if !errors.As(err, new(*dberrors.QBError)) {
		t.Fatalf("expected QBError for provider-store failure, got %v", err)
	}
}

// BeginLogin (explicit name) surfaces a non-not-found GetProviderByName error.
func TestBeginLogin_ExplicitNameStoreError(t *testing.T) {
	storage := &errorProviderStorage{getByNameErr: errors.New("store unavailable")}
	manager, err := NewFederationManager(
		WithProviderStorage(storage),
		WithLinkStorage(NewMemoryFederationStorage()),
		WithStateStorage(NewMemoryStateStorage()),
		WithFederationIdentityStore(NewIdentityStore(IdentityStoreOptions{
			UserStorage:  newMockUserStorage(),
			GroupStorage: newMockGroupStorage(),
			RoleStorage:  newMockRoleStorage(),
		})),
	)
	if err != nil {
		t.Fatalf("NewFederationManager: %v", err)
	}
	_, err = manager.BeginLogin(context.Background(), "tenant-e", "okta", "cb")
	if !errors.As(err, new(*dberrors.QBError)) {
		t.Fatalf("expected QBError, got %v", err)
	}
}
