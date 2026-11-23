//go:build rest

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

package rest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// SelfRegistrationTenantInUseError reports an unfinished self-registration
// whose tenant holds more than the registrant's pending account. The tenant is
// not discarded: what else it holds is not the registration's to delete.
type SelfRegistrationTenantInUseError struct {
	TenantID string
}

// Error implements the error interface.
func (e *SelfRegistrationTenantInUseError) Error() string {
	return "iam.self_registration: tenant " + e.TenantID +
		" holds more than its registrant's pending account; it is not discarded"
}

// registrationTimer is the part of *time.Timer the sweeper keeps, so a test
// can fire a timer instead of waiting for it.
type registrationTimer interface {
	Stop() bool
}

// registrationSweeper discards the self-registrations a restart left
// unfinished. A session begun by this process is reaped by its own timer; a
// restart loses that timer, and the broker's record on the tenant
// (iam.Tenant.SelfRegistration) is what survives it. The sweep at startup
// discards each such tenant already past its expiry and arms a timer for each
// that is not, so a registration still inside its expiry at the restart is
// discarded when it expires rather than at the next restart. stop, called on
// server shutdown, cancels every armed timer. A tenant that a session of this
// process still registers is that session's reaper's, and is left to it.
type registrationSweeper struct {
	tenants  iam.TenantManager
	store    *iam.IdentityStore
	sessions *registrationSessionStore
	logger   *logger.Logger

	now       func() time.Time
	afterFunc func(time.Duration, func()) registrationTimer

	timers  sync.Map // tenant ID -> registrationTimer
	stopped atomic.Bool
}

// newRegistrationSweeper builds a sweeper over the resolved stores, on the
// wall clock.
func newRegistrationSweeper(tenants iam.TenantManager, store *iam.IdentityStore, log *logger.Logger) *registrationSweeper {
	return &registrationSweeper{
		tenants:  tenants,
		store:    store,
		sessions: globalRegSessionStore,
		logger:   log,
		now:      time.Now,
		afterFunc: func(d time.Duration, f func()) registrationTimer {
			return time.AfterFunc(d, f)
		},
	}
}

// sweep settles every tenant that records an unfinished registration. A
// failure in one tenant does not stop the rest, and every failure is returned.
func (s *registrationSweeper) sweep(ctx context.Context) error {
	if s.tenants == nil || s.store == nil {
		// A server without them provisioned no registration to sweep.
		return nil
	}
	list, err := s.tenants.ListTenants(ctx, iam.ListOptions{})
	if err != nil {
		return err
	}
	var errs []error
	for _, tenant := range list {
		if tenant.SelfRegistration == nil {
			continue
		}
		if err := s.settle(ctx, tenant); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// settle discards the tenant's registration once it has expired, and until
// then arms a timer that settles it again at its expiry.
func (s *registrationSweeper) settle(ctx context.Context, tenant *iam.Tenant) error {
	if s.sessions.holdsTenant(tenant.ID) {
		return nil
	}
	if wait := tenant.SelfRegistration.Expires.Sub(s.now()); wait > 0 {
		s.arm(ctx, tenant.ID, wait)
		return nil
	}
	err := discardUnfinishedTenant(ctx, s.tenants, s.store, tenant.ID, "")
	var held *SelfRegistrationClaimHeldError
	if errors.As(err, &held) {
		// A finish or cleanup elsewhere holds the registration. Settle it
		// again once that claim lapses: by then it has completed, or its
		// holder died and the registration is still to be discarded.
		wait := held.LeaseUntil.Sub(s.now())
		if wait < time.Second {
			wait = time.Second
		}
		s.arm(ctx, tenant.ID, wait)
		return nil
	}
	return err
}

// arm schedules the tenant to be settled again after wait. Nothing is armed
// once the sweeper has stopped; a timer armed while stop runs is cancelled
// here, so none outlives the server.
func (s *registrationSweeper) arm(ctx context.Context, tenantID string, wait time.Duration) {
	if s.stopped.Load() {
		return
	}
	timer := s.afterFunc(wait, func() {
		s.timers.Delete(tenantID)
		if err := s.expire(ctx, tenantID); err != nil {
			s.logger.WithError(err).Error("iam.self_registration: discarding an expired registration failed")
		}
	})
	if previous, loaded := s.timers.Swap(tenantID, timer); loaded {
		if prev, ok := previous.(registrationTimer); ok {
			prev.Stop()
		}
	}
	if s.stopped.Load() && s.timers.CompareAndDelete(tenantID, timer) {
		timer.Stop()
	}
}

// expire settles the tenant when its timer fires. It re-reads the tenant
// first: one that is gone, or whose registration was finished meanwhile, is
// left alone.
func (s *registrationSweeper) expire(ctx context.Context, tenantID string) error {
	if s.stopped.Load() {
		return nil
	}
	tenant, err := s.tenants.GetTenant(ctx, tenantID)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if tenant.SelfRegistration == nil {
		return nil
	}
	return s.settle(ctx, tenant)
}

// stop cancels every armed timer and arms no more. It is called on server
// shutdown.
func (s *registrationSweeper) stop() {
	s.stopped.Store(true)
	s.timers.Range(func(key, value any) bool {
		if timer, ok := value.(registrationTimer); ok {
			timer.Stop()
		}
		s.timers.Delete(key)
		return true
	})
}

// registrationClaimLease is how long a finish or cleanup holds its claim on a
// registration. It bounds one finish: the ceremony, the activation and the
// clearing of the registration record. A holder that dies loses the claim
// when the lease lapses, so it never pins the registration.
const registrationClaimLease = 5 * time.Minute

// registrationClaimHolderBytes is the size of a claim holder's random name.
const registrationClaimHolderBytes = 16

// SelfRegistrationClaimHeldError reports a registration that cleanup did not
// discard because a finish or another cleanup holds a live claim on it.
type SelfRegistrationClaimHeldError struct {
	TenantID   string
	LeaseUntil time.Time
}

// Error implements the error interface.
func (e *SelfRegistrationClaimHeldError) Error() string {
	return "iam.self_registration: tenant " + e.TenantID +
		" is claimed by a finish or cleanup until " + e.LeaseUntil.UTC().Format(time.RFC3339) + "; it is not discarded"
}

// newRegistrationClaimHolder names one finish or cleanup attempt. It is
// random, and never derived from the session ID, which is a bearer secret and
// must not be written to the tenant record.
func newRegistrationClaimHolder() string {
	b := make([]byte, registrationClaimHolderBytes)
	// crypto/rand.Read never returns an error; it aborts the program when
	// the system's randomness fails.
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// claimRegistration takes the claim on the tenant's unfinished registration
// through the tenant manager. A manager that cannot take it fails closed with
// an "unsupported" claim error.
func claimRegistration(ctx context.Context, tenants iam.TenantManager, tenantID, state, holder string) error {
	claimer, ok := tenants.(iam.SelfRegistrationClaimer)
	if !ok {
		return &iam.SelfRegistrationClaimError{TenantID: tenantID, Reason: iam.SelfRegistrationClaimUnsupported}
	}
	_, err := claimer.ClaimSelfRegistration(context.WithoutCancel(ctx), tenantID, iam.SelfRegistrationClaim{
		State:  state,
		Holder: holder,
		Lease:  registrationClaimLease,
	})
	return err
}

// claimRegistrationForFinish claims the session's registration as finishing.
// refused is true when the claim was refused because cleanup holds it or the
// registration is already finished: the session is then spent, and there is
// nothing for this finish to discard. Any other error is a failure to claim.
func (h *IAMHandler) claimRegistrationForFinish(ctx context.Context, tenants iam.TenantManager, sess *registrationSession) (refused bool, err error) {
	sess.claimHolder = newRegistrationClaimHolder()
	err = claimRegistration(ctx, tenants, sess.tenantID, iam.SelfRegistrationFinishing, sess.claimHolder)
	var claimErr *iam.SelfRegistrationClaimError
	if errors.As(err, &claimErr) &&
		(claimErr.Reason == iam.SelfRegistrationClaimFinished || claimErr.Reason == iam.SelfRegistrationClaimHeld) {
		return true, err
	}
	return false, err
}

// discardUnfinishedTenant deletes an expired registration's account and
// tenant, after verifying the tenant is the registration's own: the tenant's
// record says self-registration created it, and it holds nothing but the
// registrant's still-pending account. A tenant holding anything more is
// reported and kept.
//
// It first claims the registration as discarding, under holder (a fresh one
// when empty). The claim is a conditional write on the tenant record that a
// finish on any node also takes before its ceremony, and it is what makes the
// user check and the delete below one decision across nodes: a finish that
// holds the claim is not deleted from under it, and a finish that comes later
// is refused. A registration already finished is left alone; one held by a
// live claim elsewhere is reported with *SelfRegistrationClaimHeldError; and a
// store that cannot take the claim deletes nothing.
func discardUnfinishedTenant(ctx context.Context, tenants iam.TenantManager, store *iam.IdentityStore, tenantID, holder string) error {
	if holder == "" {
		holder = newRegistrationClaimHolder()
	}
	if err := claimRegistration(ctx, tenants, tenantID, iam.SelfRegistrationDiscarding, holder); err != nil {
		var claimErr *iam.SelfRegistrationClaimError
		if errors.As(err, &claimErr) {
			switch claimErr.Reason {
			case iam.SelfRegistrationClaimFinished:
				return nil
			case iam.SelfRegistrationClaimHeld:
				return &SelfRegistrationClaimHeldError{TenantID: tenantID, LeaseUntil: claimErr.LeaseUntil}
			}
		}
		return err
	}
	// Two is enough to tell "only the registrant" from "more than that".
	users, err := store.ListUsers(ctx, tenantID, iam.ListOptions{Limit: 2})
	if err != nil {
		return err
	}
	sess := &registrationSession{tenantID: tenantID}
	switch {
	case len(users) > 1:
		return &SelfRegistrationTenantInUseError{TenantID: tenantID}
	case len(users) == 1:
		if users[0].Lifecycle != iam.LifecyclePending {
			return &SelfRegistrationTenantInUseError{TenantID: tenantID}
		}
		sess.userID = users[0].ID
	}
	return removeRegistration(ctx, tenants, store, sess)
}
