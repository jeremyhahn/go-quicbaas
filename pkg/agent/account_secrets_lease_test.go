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

package agent

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// WHAT A DELIVERY GRANT CONTROLS, AND WHAT IT DOES NOT.
//
// Owner decision, 2026-09-14. A grant governs ONE delivery. While a released
// copy is fresh it is served from memory and no release is attempted, so
// revoking the grant cannot reach it. Once the window closes the entry is
// EVICTED — grant and all — and the next release asks with no grant, which the
// control plane answers by deciding afresh.
//
// The alternative, which this replaced, was for a stale entry to keep carrying
// its dead grant so the refresh could be recognised and refused. It made
// revocation of a single delivery permanent for the host, and because the
// ordinary end of a session revokes that session's grant, one teardown stopped
// the daemon until its process restarted. Measured on the burn-in node: every
// spawn failed for hours with "lease ... was revoked at ...".
//
// Cutting a host off is therefore done by disabling or fencing the ACCOUNT,
// which GrantDelivery re-checks on every first release — and which these tests
// pin, because it is the enforcement that has to survive the change.
// =============================================================================

// TestMaterializingSecrets_AnAgedOutCopyAsksWithNoGrant proves the entry is
// evicted rather than left holding a grant it can no longer use.
func TestMaterializingSecrets_AnAgedOutCopyAsksWithNoGrant(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	secrets, cache := newTestMaterializingSecrets(t, central)
	ctx := context.Background()
	path := accountPath(t, "tenant-a", "acct-1")

	if _, _, err := secrets.Get(ctx, path); err != nil {
		t.Fatalf("first Get() error = %v", err)
	}

	// Age the cached copy out WITHOUT sleeping: the freshness test reads
	// freshUntil, so a copy whose window has already closed is the same fact a
	// wall-clock wait would produce.
	expireCachedCopy(t, cache, "acct-1")

	if _, _, err := secrets.Get(ctx, path); err != nil {
		t.Fatalf("refresh Get() error = %v", err)
	}

	presented := central.leasesPresented()
	if len(presented) != 2 {
		t.Fatalf("materialize calls = %d, want 2 (a first release and one after the window closed)", len(presented))
	}
	if presented[0] != "" {
		t.Fatalf("the first release presented %q, want no grant", presented[0])
	}
	if presented[1] != "" {
		t.Fatalf("the release after the window closed presented %q, want no grant: "+
			"an aged-out entry is evicted, so there is no grant left to present", presented[1])
	}
}

// TestMaterializingSecrets_RevocationCannotReachAFreshCopy pins the half of the
// old property that survives: a grant governs DELIVERY, and a copy already
// delivered and still inside its window is served from memory without asking.
func TestMaterializingSecrets_RevocationCannotReachAFreshCopy(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	secrets, _ := newTestMaterializingSecrets(t, central)
	ctx := context.Background()
	path := accountPath(t, "tenant-a", "acct-1")

	if _, _, err := secrets.Get(ctx, path); err != nil {
		t.Fatalf("first Get() error = %v", err)
	}
	central.revokeLease("lease_acct-1")

	if _, _, err := secrets.Get(ctx, path); err != nil {
		t.Fatalf("Get() from a still-fresh cache error = %v; the lease governs delivery, "+
			"it does not reach into a process's memory", err)
	}
	if got := central.releaseCount("acct-1"); got != 1 {
		t.Errorf("released %d times, want 1: a fresh copy must be served without asking", got)
	}
}

// TestMaterializingSecrets_AnAgedOutCopyRecoversAfterATeardown is the reason the
// eviction exists. The ordinary end of a session revokes that session's grant;
// the daemon must still be able to work afterwards.
func TestMaterializingSecrets_AnAgedOutCopyRecoversAfterATeardown(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	secrets, cache := newTestMaterializingSecrets(t, central)
	ctx := context.Background()
	path := accountPath(t, "tenant-a", "acct-1")

	if _, _, err := secrets.Get(ctx, path); err != nil {
		t.Fatalf("first Get() error = %v", err)
	}
	central.revokeLease("lease_acct-1") // the session ends
	expireCachedCopy(t, cache, "acct-1")

	if _, _, err := secrets.Get(ctx, path); err != nil {
		t.Fatalf("Get() after a teardown error = %v; one session ending must not stop the daemon", err)
	}
}

// TestMaterializingSecrets_ADisabledAccountIsStillRefused is the enforcement
// that HAS to survive. The retry carries no grant, so the control plane decides
// afresh — and an account it will not release is refused there, exactly as it
// would refuse a first release.
func TestMaterializingSecrets_ADisabledAccountIsStillRefused(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	account := central.addAccount("acct-1", "claude", "", true)
	secrets, cache := newTestMaterializingSecrets(t, central)
	ctx := context.Background()
	path := accountPath(t, "tenant-a", "acct-1")

	if _, _, err := secrets.Get(ctx, path); err != nil {
		t.Fatalf("first Get() error = %v", err)
	}

	// The operator takes the account out of service, then the window closes.
	account.refusalStatus = http.StatusConflict
	account.refusalBody = `{"error":"account not releasable: status revoked"}`
	expireCachedCopy(t, cache, "acct-1")

	if _, _, err := secrets.Get(ctx, path); err == nil {
		t.Fatal("a disabled account released a credential: evicting a stale entry must not " +
			"become a way around the control plane's own refusal")
	}
}

// TestMaterializingSecrets_NoGrantIsPresentedWhenNothingIsCached is the bad-input
// twin: a first release must present nothing, or the control plane would refuse a
// caller that legitimately holds no copy.
func TestMaterializingSecrets_NoGrantIsPresentedWhenNothingIsCached(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	secrets, _ := newTestMaterializingSecrets(t, central)

	if _, _, err := secrets.Get(context.Background(), accountPath(t, "tenant-a", "acct-1")); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	presented := central.leasesPresented()
	if len(presented) != 1 || presented[0] != "" {
		t.Fatalf("presented grants = %v, want exactly one empty presentation", presented)
	}
}

func TestAccountCredentialCache_GrantFor(t *testing.T) {
	cache := &accountCredentialCache{}

	if got := cache.grantFor("acct-1").LeaseID; got != "" {
		t.Fatalf("grantFor() on an empty cache = %q, want empty", got)
	}

	grantExpiry := time.Now().Add(30 * time.Minute).UTC()
	entry, err := newCachedAccountCredential([]byte(`{"access_token":"x"}`),
		time.Now(), time.Now().Add(time.Hour),
		accountDeliveryGrant{LeaseID: "lease_acct-1", ExpiresAt: grantExpiry})
	if err != nil {
		t.Fatalf("newCachedAccountCredential() error = %v", err)
	}
	cache.store("acct-1", entry)

	got := cache.grantFor("acct-1")
	if got.LeaseID != "lease_acct-1" {
		t.Fatalf("grantFor() lease = %q, want lease_acct-1", got.LeaseID)
	}
	// THE GRANT'S OWN EXPIRY TRAVELS WITH IT, and it is NOT the credential's:
	// the entry above is fresh for an hour while its grant lapses in thirty
	// minutes, and a report that collapsed the two would tell the control plane
	// the wrong lifetime for the delivery.
	if !got.ExpiresAt.Equal(grantExpiry) {
		t.Fatalf("grantFor() expiry = %v, want %v", got.ExpiresAt, grantExpiry)
	}

	// A copy whose window has closed is exactly the case a refresh is about to
	// be made for: the grant must still be readable, or the refresh would
	// silently become a first release.
	entry.freshUntil = time.Now().Add(-time.Hour)
	if got := cache.grantFor("acct-1").LeaseID; got != "lease_acct-1" {
		t.Fatalf("grantFor() on a stale copy = %q, want the grant to still be readable", got)
	}
}

func TestAccountCredentialCache_GrantFor_BadInput(t *testing.T) {
	cache := &accountCredentialCache{}

	t.Run("unknown account", func(t *testing.T) {
		if got := cache.grantFor("acct-nope").LeaseID; got != "" {
			t.Fatalf("grantFor() = %q, want empty", got)
		}
	})

	t.Run("wrong entry type", func(t *testing.T) {
		// The cache is a sync.Map; a value of the wrong type must not panic.
		cache.entries.Store("acct-bad", "not an entry")
		if got := cache.grantFor("acct-bad").LeaseID; got != "" {
			t.Fatalf("grantFor() = %q, want empty", got)
		}
	})
}

// TestMaterializedAccountCredential_CarriesNoSecretInItsGrantFields is the leak
// guard for the type the daemon caches: the grant fields are identifiers.
func TestMaterializedAccountCredential_CarriesNoSecretInItsGrantFields(t *testing.T) {
	released := &MaterializedAccountCredential{
		AccountID:      "acct-1",
		Credential:     &orchestration.AccountCredential{AccessToken: "oauth-access-token-value"},
		LeaseID:        "lease_acct-1",
		LeaseExpiresAt: time.Now().Add(time.Hour),
	}
	if strings.Contains(released.LeaseID, released.Credential.AccessToken) {
		t.Fatal("the grant identifier carries credential material")
	}
}

// expireCachedCopy closes a cached entry's freshness window in place.
func expireCachedCopy(t *testing.T, cache *accountCredentialCache, accountID string) {
	t.Helper()
	value, ok := cache.entries.Load(accountID)
	if !ok {
		t.Fatalf("no cached copy for %q", accountID)
	}
	entry, ok := value.(*cachedAccountCredential)
	if !ok {
		t.Fatalf("cached value for %q is %T", accountID, value)
	}
	entry.freshUntil = time.Now().Add(-time.Hour)
}
