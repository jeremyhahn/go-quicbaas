//go:build rest

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

package rest

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

const claimRaceTenant = "acme-inc"

// claimFor takes a claim on the begun registration the way a finish or a
// cleanup on another node would.
func claimFor(t *testing.T, svc *selfRegService, state, holder string, now time.Time) {
	t.Helper()
	_, err := svc.tenants.ClaimSelfRegistration(context.Background(), claimRaceTenant, iam.SelfRegistrationClaim{
		State:  state,
		Holder: holder,
		Lease:  registrationClaimLease,
		Now:    now,
	})
	require.NoError(t, err)
}

func boundCount(c *xkmsCeremony) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.bound)
}

// A cleanup that reaches a registration a finish has claimed leaves it alone,
// so the finish completes and the account survives.
func TestRegistrationClaim_CleanupAfterFinishCompletedKeepsTenant(t *testing.T) {
	ctx := context.Background()
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	svc := handler.server.service.(*selfRegService)
	newXKMSCeremony(svc, true)
	restart(t, selfRegBegin(t, handler, "Acme Inc", "jane@acme.example"))
	users := usersIn(t, handler, claimRaceTenant)
	require.Len(t, users, 1)

	// The finish claims the registration on one node...
	claimFor(t, svc, iam.SelfRegistrationFinishing, "finish-A", time.Time{})

	// ...cleanup on another node reaches it before the finish completes...
	err := discardUnfinishedTenant(ctx, svc.tenants, svc.store, claimRaceTenant, "")
	var held *SelfRegistrationClaimHeldError
	require.True(t, errors.As(err, &held), "cleanup must report the registration held, got %v", err)

	// ...and the finish then activates the account and clears the record.
	active := iam.LifecycleActive
	_, err = svc.store.UpdateUser(ctx, claimRaceTenant, users[0].ID, iam.UpdateUserOptions{Lifecycle: &active})
	require.NoError(t, err)
	_, err = svc.tenants.UpdateTenant(ctx, claimRaceTenant, iam.UpdateTenantOptions{FinishSelfRegistration: true})
	require.NoError(t, err)

	assert.Equal(t, []string{claimRaceTenant}, tenantIDs(t, handler), "the finished tenant is kept")
	kept := usersIn(t, handler, claimRaceTenant)
	require.Len(t, kept, 1, "the finished account is kept")
	assert.Equal(t, iam.LifecycleActive, kept[0].Lifecycle)

	// Once finished, a later cleanup finds nothing to discard.
	require.NoError(t, discardUnfinishedTenant(ctx, svc.tenants, svc.store, claimRaceTenant, ""))
	assert.Equal(t, []string{claimRaceTenant}, tenantIDs(t, handler))
}

// A finish that reaches a registration cleanup has claimed is refused before
// the ceremony binds a credential to an account about to be deleted.
func TestRegistrationClaim_FinishAfterCleanupClaimedIsExpired(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	svc := handler.server.service.(*selfRegService)
	ceremony := newXKMSCeremony(svc, true)
	sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")

	claimFor(t, svc, iam.SelfRegistrationDiscarding, "cleanup-B", time.Time{})

	w := selfRegFinish(t, handler, sessionID)
	require.Equal(t, http.StatusBadRequest, w.Code, "finish body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), ErrRegistrationSessionExpired)
	assert.Zero(t, boundCount(ceremony), "no credential is bound to a registration cleanup owns")
	users := usersIn(t, handler, claimRaceTenant)
	require.Len(t, users, 1, "the refused finish leaves the registration to its cleanup")
	assert.Equal(t, iam.LifecyclePending, users[0].Lifecycle)
}

// A finish whose ceremony fails discards its registration under its own claim.
func TestRegistrationClaim_FinishFailureDiscardsUnderItsOwnClaim(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	svc := handler.server.service.(*selfRegService)
	ceremony := newXKMSCeremony(svc, true)
	ceremony.failFinish = true
	sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")

	w := selfRegFinish(t, handler, sessionID)
	require.NotEqual(t, http.StatusOK, w.Code, "finish body: %s", w.Body.String())

	assert.Empty(t, tenantIDs(t, handler), "the failed finish discards its tenant")
	assert.Empty(t, usersIn(t, handler, claimRaceTenant), "the failed finish discards its account")
}

// claimlessTenants hides the claim of the tenant manager it wraps.
type claimlessTenants struct {
	iam.TenantManager
}

// claimlessService serves a tenant manager that cannot take a claim.
type claimlessService struct {
	*selfRegService
}

func (s *claimlessService) GetTenantManager(context.Context) (iam.TenantManager, error) {
	return claimlessTenants{TenantManager: s.tenants}, nil
}

// Without a claim neither finish nor cleanup can act safely, so both refuse
// and nothing is deleted.
func TestRegistrationClaim_ManagerWithoutClaimFailsClosed(t *testing.T) {
	ctx := context.Background()
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	svc := handler.server.service.(*selfRegService)
	ceremony := newXKMSCeremony(svc, true)
	sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")
	handler.server.service = &claimlessService{selfRegService: svc}

	w := selfRegFinish(t, handler, sessionID)
	require.Equal(t, http.StatusInternalServerError, w.Code, "finish body: %s", w.Body.String())
	assert.Zero(t, boundCount(ceremony), "no credential is bound without a claim")

	err := discardUnfinishedTenant(ctx, claimlessTenants{TenantManager: svc.tenants}, svc.store, claimRaceTenant, "")
	var claimErr *iam.SelfRegistrationClaimError
	require.True(t, errors.As(err, &claimErr), "cleanup without a claim must refuse, got %v", err)
	assert.Equal(t, iam.SelfRegistrationClaimUnsupported, claimErr.Reason)

	handler.server.service = svc
	assert.Equal(t, []string{claimRaceTenant}, tenantIDs(t, handler), "nothing is deleted without a claim")
	assert.Len(t, usersIn(t, handler, claimRaceTenant), 1)
}

// A sweep that finds an expired registration held by a finish keeps it and
// settles it again once the claim lapses.
func TestRegistrationSweeper_RearmsAHeldRegistration(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	svc := handler.server.service.(*selfRegService)
	newXKMSCeremony(svc, true)
	restart(t, selfRegBegin(t, handler, "Acme Inc", "jane@acme.example"))
	clock := &testClock{at: afterExpiry()}
	// The claim is live well past the sweeper's clock.
	claimFor(t, svc, iam.SelfRegistrationFinishing, "finish-X", clock.at)
	sweeper, timers := newTestSweeper(handler, clock)

	require.NoError(t, sweeper.expire(context.Background(), claimRaceTenant))

	assert.Equal(t, []string{claimRaceTenant}, tenantIDs(t, handler), "a held registration is kept")
	assert.Len(t, usersIn(t, handler, claimRaceTenant), 1)
	armed := timers.all()
	require.Len(t, armed, 1, "the sweep re-arms a timer for the held registration")
	assert.GreaterOrEqual(t, armed[0].wait, time.Second)
	assert.LessOrEqual(t, armed[0].wait, registrationClaimLease)
}
