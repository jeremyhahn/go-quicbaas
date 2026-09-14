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

package iam

import (
	"context"
	"testing"
	"time"
)

// =============================================================================
// go-quicbaas #274, second half — the refresh-token value index must resolve
// from an UNTENANTED context.
//
// These run over the real BackendStorage on top of routingTestKV, the double
// that mirrors production's per-tenant KVStore routing. A flat store cannot
// reproduce the defect: it was created entirely by the write and the read
// resolving to different tenant keyspaces, so only a routed store pins it.
// =============================================================================

// storeRefreshToken writes one refresh-token record the way issueRefreshCredential
// does — under the token's own tenant-scoped context.
func storeRefreshToken(t *testing.T, storage *BackendStorage, tenantID, jti, secret string) {
	t.Helper()
	err := storage.StoreToken(withTokenTenant(context.Background(), tenantID), &Token{
		JTI:       jti,
		Subject:   "operator@example.test",
		TenantID:  tenantID,
		Expires:   time.Now().Add(time.Hour),
		Issued:    time.Now(),
		TokenType: TokenTypeRefresh,
		ValueHash: HashTokenValue(secret),
	})
	if err != nil {
		t.Fatalf("StoreToken for tenant %q: %v", tenantID, err)
	}
}

// TestGetTokenByValue_ResolvesWithNoTenantInContext is the regression: the
// public refresh endpoint carries an opaque secret and nothing else, so the
// redemption read has no tenant bound. A credential issued inside a tenant's
// login context must still resolve — for a barrier-isolated tenant, a
// logical-prefix tenant, and the system tenant alike.
func TestGetTokenByValue_ResolvesWithNoTenantInContext(t *testing.T) {
	for _, tc := range []struct {
		name     string
		tenantID string
		barrier  bool
	}{
		{"barrier-isolated tenant", "burnin", true},
		{"logical-prefix tenant", "acme", false},
		{"system tenant", "system", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kv := newRoutingTestKV()
			if tc.barrier {
				kv.barrierTenant(tc.tenantID)
			}
			storage := NewBackendStorage(kv)
			const secret = "opaque-refresh-secret"
			storeRefreshToken(t, storage, tc.tenantID, "jti-1", secret)

			// No tenant in context — exactly what POST /api/v1/iam/refresh has.
			stored, err := storage.GetTokenByValue(context.Background(), secret)
			if err != nil {
				t.Fatalf("untenanted GetTokenByValue: %v (this is the #274 401)", err)
			}
			if stored.JTI != "jti-1" {
				t.Fatalf("resolved JTI = %q, want jti-1", stored.JTI)
			}
			// The RECORD stays tenant-scoped; only the pointer to it is global.
			if stored.TenantID != tc.tenantID {
				t.Fatalf("resolved tenant = %q, want %q", stored.TenantID, tc.tenantID)
			}
		})
	}
}

// TestGetTokenByValue_KeepsRecordsTenantIsolated proves the shared index does
// not merge two tenants' token state: each secret resolves only its own
// tenant's record.
func TestGetTokenByValue_KeepsRecordsTenantIsolated(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("alpha")
	kv.barrierTenant("beta")
	storage := NewBackendStorage(kv)

	storeRefreshToken(t, storage, "alpha", "jti-alpha", "secret-alpha")
	storeRefreshToken(t, storage, "beta", "jti-beta", "secret-beta")

	ctx := context.Background()
	for _, tc := range []struct{ secret, jti, tenant string }{
		{"secret-alpha", "jti-alpha", "alpha"},
		{"secret-beta", "jti-beta", "beta"},
	} {
		stored, err := storage.GetTokenByValue(ctx, tc.secret)
		if err != nil {
			t.Fatalf("GetTokenByValue(%s): %v", tc.secret, err)
		}
		if stored.JTI != tc.jti || stored.TenantID != tc.tenant {
			t.Fatalf("%s resolved %s/%s, want %s/%s", tc.secret, stored.TenantID, stored.JTI, tc.tenant, tc.jti)
		}
	}
}

// TestGetTokenByValue_RejectsAPointerToAnotherRecord proves the index pointer
// is treated as a claim and not a proof. Moving the index to the shared store
// widens who could plant an entry, so a pointer that names a record whose
// ValueHash does not match the presented secret must not redeem that record —
// otherwise anyone able to write the shared index could aim their own secret at
// a victim's refresh JTI.
func TestGetTokenByValue_RejectsAPointerToAnotherRecord(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("victim")
	storage := NewBackendStorage(kv)

	storeRefreshToken(t, storage, "victim", "victim-jti", "victim-secret")

	// Plant an index entry mapping the ATTACKER's secret at the victim's record.
	attackerHash := HashTokenValue("attacker-secret")
	if err := kv.Put(tokenValueIndexContext(context.Background()),
		tokenValueKey(attackerHash), []byte("victim:victim-jti")); err != nil {
		t.Fatalf("plant index entry: %v", err)
	}

	if _, err := storage.GetTokenByValue(context.Background(), "attacker-secret"); err == nil {
		t.Fatal("a pointer to a record the presented secret does not hash to was redeemed")
	}
	// The victim's own credential still works.
	if _, err := storage.GetTokenByValue(context.Background(), "victim-secret"); err != nil {
		t.Fatalf("the legitimate credential stopped resolving: %v", err)
	}
}

// TestGetTokenByValue_ReadsTheLegacyTenantScopedIndex proves the upgrade does
// not strand credentials minted by the previous scheme. Before this change the
// index was written into the ISSUING tenant's keyspace, and the OAuth2 token
// endpoint — which scopes its context from the client id — read it from there
// successfully. Those tokens must keep working after the index moves.
func TestGetTokenByValue_ReadsTheLegacyTenantScopedIndex(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	storage := NewBackendStorage(kv)
	const secret = "legacy-refresh-secret"

	tenantCtx := withTokenTenant(context.Background(), "acme")
	// The record, as the old code wrote it.
	storeRefreshToken(t, storage, "acme", "legacy-jti", secret)
	// Remove the shared entry this version writes, leaving ONLY the
	// tenant-scoped one an older daemon would have left behind.
	if err := kv.Delete(tokenValueIndexContext(context.Background()),
		tokenValueKey(HashTokenValue(secret))); err != nil {
		t.Fatalf("clear shared index: %v", err)
	}

	stored, err := storage.GetTokenByValue(tenantCtx, secret)
	if err != nil {
		t.Fatalf("a pre-upgrade credential became unresolvable: %v", err)
	}
	if stored.JTI != "legacy-jti" {
		t.Fatalf("resolved JTI = %q, want legacy-jti", stored.JTI)
	}

	// The legacy entry is reachable only to a tenant-scoped caller; an
	// untenanted read must not silently succeed off some other store.
	if _, err := storage.GetTokenByValue(context.Background(), secret); err == nil {
		t.Fatal("an untenanted read resolved a tenant-scoped legacy entry")
	}
}

// TestGetTokenByValue_SealedTenantFailsClosed proves the global index does not
// become a way around a sealed barrier: the pointer resolves, then the record
// read routes into the sealed tenant and fails.
func TestGetTokenByValue_SealedTenantFailsClosed(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("burnin")
	storage := NewBackendStorage(kv)
	const secret = "opaque-refresh-secret"
	storeRefreshToken(t, storage, "burnin", "jti-1", secret)

	kv.sealTenant("burnin")

	if _, err := storage.GetTokenByValue(context.Background(), secret); err == nil {
		t.Fatal("a sealed tenant's token record resolved through the shared index; it must fail closed")
	}
}

// TestStoreToken_IndexWriteFailureIsFatal proves an unwritable index fails the
// issuance instead of handing back a credential that is already dead.
func TestStoreToken_IndexWriteFailureIsFatal(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("burnin")
	// The index is written untenanted, so sealing the SYSTEM tenant is what
	// makes that specific write fail while the record's own write succeeds.
	kv.sealTenant("")
	storage := NewBackendStorage(kv)

	err := storage.StoreToken(withTokenTenant(context.Background(), "burnin"), &Token{
		JTI:       "jti-1",
		Subject:   "operator@example.test",
		TenantID:  "burnin",
		Expires:   time.Now().Add(time.Hour),
		Issued:    time.Now(),
		TokenType: TokenTypeRefresh,
		ValueHash: HashTokenValue("opaque-refresh-secret"),
	})
	if err == nil {
		t.Fatal("StoreToken swallowed a failed value-index write; the credential would be unredeemable")
	}
}

// TestDeleteExpiredTokens_RemovesTheValueIndex proves expiry cleanup actually
// matches the index entry. It previously deleted tokenValueKey(JTI) from the
// tenant-routed store — a key that is never written — and so leaked one entry
// per expired token.
func TestDeleteExpiredTokens_RemovesTheValueIndex(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("burnin")
	storage := NewBackendStorage(kv)
	const secret = "opaque-refresh-secret"

	err := storage.StoreToken(withTokenTenant(context.Background(), "burnin"), &Token{
		JTI:       "jti-1",
		Subject:   "operator@example.test",
		TenantID:  "burnin",
		Expires:   time.Now().Add(-time.Hour),
		Issued:    time.Now().Add(-2 * time.Hour),
		TokenType: TokenTypeRefresh,
		ValueHash: HashTokenValue(secret),
	})
	if err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	// Assert on the raw index KEYS, not on GetTokenByValue: a lookup failure
	// would also be satisfied by an index that was never reachable, which is
	// how the old cleanup "passed" while leaking every entry it meant to
	// remove. Presence before and absence after is what proves a deletion —
	// and BOTH copies StoreToken writes must go, the shared one and the
	// tenant-scoped rollback copy.
	indexKey := tokenValueKey(HashTokenValue(secret))
	sharedCtx := tokenValueIndexContext(context.Background())
	ctx := withTokenTenant(context.Background(), "burnin")

	if _, err := kv.Get(sharedCtx, indexKey); err != nil {
		t.Fatalf("the shared index entry is not where cleanup must find it: %v", err)
	}
	if _, err := kv.Get(ctx, indexKey); err != nil {
		t.Fatalf("the rollback copy is not where cleanup must find it: %v", err)
	}

	if err := storage.DeleteExpiredTokens(ctx); err != nil {
		t.Fatalf("DeleteExpiredTokens: %v", err)
	}
	if _, err := kv.Get(sharedCtx, indexKey); err == nil {
		t.Fatal("the shared value index entry survived expiry cleanup")
	}
	if _, err := kv.Get(ctx, indexKey); err == nil {
		t.Fatal("the tenant-scoped rollback copy survived expiry cleanup")
	}
	if _, err := storage.GetTokenByValue(context.Background(), secret); err == nil {
		t.Fatal("the expired credential still resolves")
	}
}
