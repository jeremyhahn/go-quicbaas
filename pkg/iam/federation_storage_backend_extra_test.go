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

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// UpdateProvider reconciles the name index; DeleteProvider clears record + index.
func TestBackendProviderStorage_UpdateDeleteList(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendProviderStorage(kv)
	ctx := context.Background()

	if err := store.CreateProvider(ctx, testProvider("acme", "p1", "Okta")); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	// Rename the provider; the old name index must stop resolving.
	p := testProvider("acme", "p1", "OktaRenamed")
	if err := store.UpdateProvider(ctx, p); err != nil {
		t.Fatalf("UpdateProvider: %v", err)
	}
	if _, err := store.GetProviderByName(ctx, "acme", "Okta"); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("old name index still resolves: %v", err)
	}
	byName, err := store.GetProviderByName(ctx, "acme", "OktaRenamed")
	if err != nil || byName.ID != "p1" {
		t.Fatalf("GetProviderByName(new) = %v, %v", byName, err)
	}

	// Updating a missing provider errors.
	if err := store.UpdateProvider(ctx, testProvider("acme", "missing", "X")); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("UpdateProvider(missing) err = %v, want ErrProviderNotFound", err)
	}

	// Rename collision: create a second provider then rename onto its name.
	if err := store.CreateProvider(ctx, testProvider("acme", "p2", "Auth0")); err != nil {
		t.Fatalf("CreateProvider(p2): %v", err)
	}
	if err := store.UpdateProvider(ctx, testProvider("acme", "p1", "Auth0")); err == nil {
		t.Fatal("UpdateProvider onto existing name want collision error")
	}

	// List with pagination across the two providers.
	all, err := store.ListProviders(ctx, "acme", ListOptions{})
	if err != nil || len(all) != 2 {
		t.Fatalf("ListProviders = %d, %v; want 2, nil", len(all), err)
	}
	page, err := store.ListProviders(ctx, "acme", ListOptions{Limit: 1})
	if err != nil || len(page) != 1 {
		t.Fatalf("ListProviders(limit 1) = %d, %v; want 1, nil", len(page), err)
	}
	none, err := store.ListProviders(ctx, "acme", ListOptions{Offset: 99})
	if err != nil || len(none) != 0 {
		t.Fatalf("ListProviders(offset 99) = %d, %v; want 0, nil", len(none), err)
	}

	// Delete the renamed provider; record + name index gone.
	if err := store.DeleteProvider(ctx, "acme", "p1"); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}
	if _, err := store.GetProvider(ctx, "acme", "p1"); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("GetProvider after delete err = %v, want ErrProviderNotFound", err)
	}

	// Delete of a missing provider errors.
	if err := store.DeleteProvider(ctx, "acme", "p1"); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("DeleteProvider(missing) err = %v, want ErrProviderNotFound", err)
	}
}

// UpdateLink reconciles external + user indexes; DeleteLink clears everything.
func TestBackendFederationStorage_UpdateDeleteLink(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendFederationStorage(kv)
	ctx := tenantctx.WithTenant(context.Background(), "acme")

	link := testLink("acme", 30001, "l1", "p1", "ext-123")
	if err := store.CreateLink(ctx, link); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	// Change external id and owner; both indexes must follow.
	updated := testLink("acme", 30002, link.ID, "p1", "ext-456")
	if err := store.UpdateLink(ctx, updated); err != nil {
		t.Fatalf("UpdateLink: %v", err)
	}
	if _, err := store.GetLinkByExternal(ctx, "p1", "ext-123"); !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("old external index still resolves: %v", err)
	}
	byExt, err := store.GetLinkByExternal(ctx, "p1", "ext-456")
	if err != nil || byExt.UserID != 30002 {
		t.Fatalf("GetLinkByExternal(new) = %v, %v", byExt, err)
	}
	byUser, err := store.GetLinksByUser(ctx, 30002)
	if err != nil || len(byUser) != 1 {
		t.Fatalf("GetLinksByUser(u2) = %d, %v; want 1, nil", len(byUser), err)
	}

	// Updating a missing link errors.
	missing := testLink("acme", 30009, "missing", "p1", "ext-x")
	if err := store.UpdateLink(ctx, missing); !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("UpdateLink(missing) err = %v, want ErrLinkNotFound", err)
	}

	// DeleteLink clears the record plus indexes.
	if err := store.DeleteLink(ctx, link.ID); err != nil {
		t.Fatalf("DeleteLink: %v", err)
	}
	if _, err := store.GetLink(ctx, link.ID); !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("GetLink after delete err = %v, want ErrLinkNotFound", err)
	}
	if _, err := store.GetLinkByExternal(ctx, "p1", "ext-456"); !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("external index survived DeleteLink: %v", err)
	}

	// Delete of a missing link errors.
	if err := store.DeleteLink(ctx, link.ID); !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("DeleteLink(missing) err = %v, want ErrLinkNotFound", err)
	}
}
