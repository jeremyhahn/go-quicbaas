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

package fleet

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// THE DAEMON REPORTS THE GRANT IT WAS SERVED UNDER; IT DOES NOT ISSUE ONE.
//
// A worker daemon holds no lease store, so it has no authority to create a
// delivery grant. It does not need one: a daemon's credential release IS the
// control plane's materialize verb, and that verb issues the grant and returns
// its identifier with the envelope (pkg/service/account_materialize.go). The
// grant already exists by the time the resolution that triggered the release
// finishes.
//
// WHAT WAS MISSING IS THE RETURN JOURNEY. Without this reporter the grant
// stopped at the daemon's credential cache: the resolution reported no grant, so
// the Session carried none, so the durable SessionRecord named none, so the
// session's conclusion had nothing to withdraw. A remote session's provider
// credential was leased and then un-revocable — worse than not leasing it,
// because the grant claimed governance it did not have and merely aged out on
// its own TTL.
//
// So this closes the loop, and it is the whole of the daemon's part in it:
// resolution -> grant reported -> Session.CredentialLeaseID ->
// SessionRecord.CredentialLeaseIDs -> conclusion revokes.
//
// WHY A GRANT IS SHARED, AND WHY THAT IS SAFE. The daemon's credential cache is
// keyed by account, by design (pkg/orchestration/fleet/account_secrets.go): a burst of spawns
// onto one account collapses onto one release, and therefore onto one grant. Two
// concurrent sessions on this host using the same account are consequently
// served under the SAME grant and both record it. That is why the control
// plane's conclusion withdraws a grant only once no other live session still
// names it (pkg/orchestration/session_conclusion.go) — otherwise the first
// session to end would cut the other's refresh off. The alternative, a grant per
// session, would mean a credential release per session and a second copy of the
// secret in this process's memory for each, which is the more expensive answer
// to a problem the control plane can settle with one record scan.
// =============================================================================

// deliveryGrantReporter is the daemon's AccountDeliveryGrantor: it names the
// grant the control plane issued for this daemon's release of an account's
// credential, reading it from the credential cache the release populated.
type deliveryGrantReporter struct {
	// cache holds one entry per account this daemon has been released a
	// credential for, each recording the grant it was released under.
	cache *accountCredentialCache
}

// Compile-time interface satisfaction check.
var _ orchestration.AccountDeliveryGrantor = (*deliveryGrantReporter)(nil)

// newDeliveryGrantReporter returns a reporter over cache.
func newDeliveryGrantReporter(cache *accountCredentialCache) *deliveryGrantReporter {
	return &deliveryGrantReporter{cache: cache}
}

// GrantDelivery returns the grant this daemon's release of scope.AccountID was
// served under.
//
// It is called by the account resolver AFTER the credential has been resolved,
// which is what makes reading the cache the right answer: the resolution either
// hit a cached copy (whose grant is the one it is being delivered under) or
// caused a release that stored a fresh copy and a fresh grant. Either way the
// entry exists and names the grant covering this delivery.
//
// A zero grant — the control plane wired no leaser, so nothing was leased — is
// reported as no grant rather than as a failure. Refusing here would take pooled
// accounts away from a deployment whose control plane cannot lease, which is the
// sealed-window case and has no operator remedy.
func (r *deliveryGrantReporter) GrantDelivery(
	ctx context.Context,
	scope orchestration.AccountLeaseScope,
) (*orchestration.AccountCredentialLease, error) {
	if r == nil || r.cache == nil {
		return nil, &orchestration.AccountLeaseNotConfiguredError{
			Operation: "report the delivery grant for account " + scope.AccountID,
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// THE RESOLUTION'S OWN GRANT FIRST. The cache holds the latest copy of the
	// account, which a concurrent spawn may have replaced since this
	// resolution's lookup; the slot holds the copy this resolution was served.
	grant := r.cache.grantFor(scope.AccountID)
	if leaseID, expiresAt, recorded := orchestration.DeliveryGrantSlotFrom(ctx).Grant(); recorded {
		grant = accountDeliveryGrant{LeaseID: leaseID, ExpiresAt: expiresAt}
	}
	if grant.LeaseID == "" {
		return nil, nil
	}
	return &orchestration.AccountCredentialLease{
		LeaseID:   grant.LeaseID,
		ExpiresAt: grant.ExpiresAt,
		Scope:     scope,
	}, nil
}
