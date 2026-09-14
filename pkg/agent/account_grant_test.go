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
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// THE RETURN JOURNEY OF A REMOTE DELIVERY GRANT.
//
// The control plane issues the grant when it releases a credential to this
// daemon; these tests pin the step that was missing — the resolution reporting
// that grant back, so the Session carries it, the durable record names it, and
// the session's conclusion can withdraw it. Without the report a remote
// session's provider credential is leased and then un-revocable.
// =============================================================================

// TestDeliveryGrantReporter_ReportsTheGrantTheReleaseWasServedUnder is the
// intended path: a cache holding a copy released under a grant reports that
// grant, with its own expiry, scoped to the delivery that asked.
func TestDeliveryGrantReporter_ReportsTheGrantTheReleaseWasServedUnder(t *testing.T) {
	cache := &accountCredentialCache{}
	grantExpiry := time.Now().Add(45 * time.Minute).UTC()
	entry, err := newCachedAccountCredential(
		[]byte(`{"access_token":"x"}`),
		time.Now(), time.Now().Add(time.Hour),
		accountDeliveryGrant{LeaseID: "lease_acct-1", ExpiresAt: grantExpiry},
	)
	if err != nil {
		t.Fatalf("newCachedAccountCredential() error = %v", err)
	}
	cache.store("acct-1", entry)

	scope := orchestration.AccountLeaseScope{
		TenantID:  "tenant-a",
		AccountID: "acct-1",
		Provider:  "claude",
		ProjectID: "proj-1",
		JobID:     "job-7",
	}
	reported, err := newDeliveryGrantReporter(cache).GrantDelivery(context.Background(), scope)
	if err != nil {
		t.Fatalf("GrantDelivery() error = %v", err)
	}
	if reported == nil {
		t.Fatal("GrantDelivery() reported no grant for a released copy")
	}
	if reported.LeaseID != "lease_acct-1" {
		t.Fatalf("GrantDelivery() lease = %q, want lease_acct-1", reported.LeaseID)
	}
	// THE GRANT'S EXPIRY IS THE GRANT'S, NOT THE CREDENTIAL'S. The entry above
	// is fresh for an hour while its grant lapses in forty-five minutes, and
	// reporting the wrong one would tell the control plane a delivery stays
	// authorised longer than it does.
	if !reported.ExpiresAt.Equal(grantExpiry) {
		t.Fatalf("GrantDelivery() expiry = %v, want %v", reported.ExpiresAt, grantExpiry)
	}
	if reported.Scope != scope {
		t.Fatalf("GrantDelivery() scope = %+v, want %+v", reported.Scope, scope)
	}
}

// TestDeliveryGrantReporter_BadInput covers every way there is nothing to
// report, and the distinction between "nothing was leased" and "this reporter
// cannot answer".
func TestDeliveryGrantReporter_BadInput(t *testing.T) {
	ctx := context.Background()
	scope := orchestration.AccountLeaseScope{TenantID: "tenant-a", AccountID: "acct-1"}

	t.Run("no copy cached", func(t *testing.T) {
		// Nothing has been released, so there is no grant. It is not an error:
		// a deployment whose control plane leases nothing resolves unleased.
		reported, err := newDeliveryGrantReporter(&accountCredentialCache{}).GrantDelivery(ctx, scope)
		if err != nil {
			t.Fatalf("GrantDelivery() error = %v, want nil", err)
		}
		if reported != nil {
			t.Fatalf("GrantDelivery() reported %+v for an empty cache, want no grant", reported)
		}
	})

	t.Run("released under no grant", func(t *testing.T) {
		cache := &accountCredentialCache{}
		entry, err := newCachedAccountCredential(
			[]byte(`{"access_token":"x"}`),
			time.Now(), time.Now().Add(time.Hour), accountDeliveryGrant{})
		if err != nil {
			t.Fatalf("newCachedAccountCredential() error = %v", err)
		}
		cache.store("acct-1", entry)

		reported, err := newDeliveryGrantReporter(cache).GrantDelivery(ctx, scope)
		if err != nil {
			t.Fatalf("GrantDelivery() error = %v, want nil", err)
		}
		if reported != nil {
			t.Fatalf("GrantDelivery() reported %+v for an unleased copy, want no grant", reported)
		}
	})

	t.Run("no cache wired", func(t *testing.T) {
		// A reporter with no cache is a WIRING fault, not an unleased
		// deployment, and it must say so rather than look like "nothing leased"
		// — otherwise a mis-wired daemon would silently stop reporting grants
		// and every remote session would become un-revocable again.
		var unwired *deliveryGrantReporter
		if _, err := unwired.GrantDelivery(ctx, scope); err == nil {
			t.Fatal("GrantDelivery() on an unwired reporter succeeded, want a refusal")
		}
		_, err := (&deliveryGrantReporter{}).GrantDelivery(ctx, scope)
		var notConfigured *orchestration.AccountLeaseNotConfiguredError
		if !errors.As(err, &notConfigured) {
			t.Fatalf("GrantDelivery() error = %v, want AccountLeaseNotConfiguredError", err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := newDeliveryGrantReporter(&accountCredentialCache{}).
			GrantDelivery(cancelled, scope); !errors.Is(err, context.Canceled) {
			t.Fatalf("GrantDelivery() error = %v, want context.Canceled", err)
		}
	})
}

// TestAccountPool_ResolutionReportsTheControlPlanesGrant is the end-to-end
// proof, through the real resolver against a control plane that leases: the
// ResolvedAccountCredential a spawn is built from names the grant the release
// was served under, which is the only channel by which the control plane's own
// session record can learn it.
func TestAccountPool_ResolutionReportsTheControlPlanesGrant(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	pool := newTestAccountPool(t, central, &staticTokenSource{})

	resolved, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
		TenantID: "tenant-a",
		Provider: "claude",
		Auto:     true,
		JobID:    "job-7",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.LeaseID != "lease_acct-1" {
		t.Fatalf("Resolve() reported grant %q, want the one the release issued", resolved.LeaseID)
	}
	if resolved.LeaseExpiresAt.IsZero() {
		t.Fatal("Resolve() reported a grant with no expiry")
	}
}

// TestAccountPool_ResolutionReportsNoGrantWhenTheControlPlaneLeasesNone is the
// bad-input twin: a control plane with no lease manager must not fail every
// remote spawn, because a sealed node has no operator remedy.
func TestAccountPool_ResolutionReportsNoGrantWhenTheControlPlaneLeasesNone(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	central.leaseNothing()
	pool := newTestAccountPool(t, central, &staticTokenSource{})

	resolved, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
		TenantID: "tenant-a",
		Provider: "claude",
		Auto:     true,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.LeaseID != "" {
		t.Fatalf("Resolve() reported grant %q against an unleased control plane", resolved.LeaseID)
	}
	if resolved.Env["CLAUDE_CODE_OAUTH_TOKEN"] == "" {
		t.Fatal("Resolve() returned no credential, so the spawn would fail closed")
	}
}
