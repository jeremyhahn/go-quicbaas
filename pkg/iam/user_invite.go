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
	"errors"
	"net/url"
	"strings"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

const (
	// DefaultInviteTTL is how long an invite stays redeemable when the caller
	// does not say.
	DefaultInviteTTL = 24 * time.Hour

	// MaxInviteTTL caps how long any invite stays redeemable.
	MaxInviteTTL = 7 * 24 * time.Hour

	// inviteRollbackTimeout bounds the detached rollback of a failed create,
	// the detached re-read after a failed activation write, and the detached
	// revocation of a failed PIV activation's login factor.
	// It is generous because the rollback is the last chance to avoid a stranded
	// account, and short enough that a wedged store cannot hold it forever.
	inviteRollbackTimeout = 30 * time.Second
)

// ErrInviteStoreUnavailable is returned when a pending user is requested but
// no invite store is configured to issue the invite that activates it.
var ErrInviteStoreUnavailable = &dberrors.QBError{
	Code: dberrors.ErrUnavailable,
	Op:   "iam.invite",
	Err:  &IAMMessageError{Message: "invite store not available"},
}

// UserRollbackError reports a create that failed after the user was written
// and whose rollback failed too, so the user (or its invite) may survive.
//
// It unwraps to the error that failed the create: that is what the caller
// asked about, and what decides the status a transport reports.
type UserRollbackError struct {
	TenantID string
	UserID   string
	Cause    error
	Rollback error
}

// Error implements the error interface.
func (e *UserRollbackError) Error() string {
	return e.Cause.Error() + "; rollback of user " + e.UserID + " failed: " + e.Rollback.Error()
}

// Unwrap returns the error that failed the create.
func (e *UserRollbackError) Unwrap() error {
	return e.Cause
}

// EffectiveLifecycle is the state a user created with these options starts
// in. An explicit Lifecycle wins; otherwise Enabled=false means disabled and
// anything else means pending.
func (o CreateUserOptions) EffectiveLifecycle() Lifecycle {
	if o.Lifecycle != "" {
		return o.Lifecycle
	}
	if o.Enabled != nil && !*o.Enabled {
		return LifecycleDisabled
	}
	return LifecyclePending
}

// ClampInviteTTL applies DefaultInviteTTL to a non-positive ttl and caps it
// at MaxInviteTTL.
func ClampInviteTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return DefaultInviteTTL
	}
	return min(ttl, MaxInviteTTL)
}

// InviteURL is the activation link for an invite: the UI's invite view under
// origin, naming the tenant (invites are tenant-scoped, so a token alone
// cannot be resolved) and the token.
func InviteURL(origin, tenantID, token string) string {
	return strings.TrimRight(origin, "/") + "/ui/#/invite/" +
		url.PathEscape(tenantID) + "/" + url.PathEscape(token)
}

// CreateInvitedUser creates a user and, when the user starts pending, issues
// the one-time invite that activates it. Every transport creates users
// through this method, so a pending account is never handed back without the
// invite that is its only way in.
//
// The invite token is returned exactly once, here; invites issues and stores
// only its digest. A user that is not pending has nothing to activate, so it
// gets no invite and the token and record are empty.
//
// The create is atomic from the caller's view: if the invite cannot be issued
// or attached, the user is deleted and any minted token spent before the error
// is returned. A rollback that itself fails is reported as *UserRollbackError.
func (s *IdentityStore) CreateInvitedUser(ctx context.Context, tenantID string, opts CreateUserOptions,
	invites InviteStore, ttl time.Duration) (*User, string, *InviteRecord, error) {
	pending := opts.EffectiveLifecycle() == LifecyclePending
	// Checked before any write: a pending user with no invite cannot be
	// activated, and a retry cannot repair it because the identity exists.
	if pending && invites == nil {
		return nil, "", nil, ErrInviteStoreUnavailable
	}

	user, err := s.CreateUser(ctx, tenantID, opts)
	if err != nil {
		return nil, "", nil, err
	}
	if !pending {
		return user, "", nil, nil
	}

	token, record, err := invites.Create(ctx, tenantID, user.ID, ClampInviteTTL(ttl))
	if err != nil {
		// The canonical store returns no token with an error; one that does
		// must not leave it live, so it is spent with the rollback.
		return nil, "", nil, s.rollbackInvitedUser(ctx, tenantID, user.ID, invites, token, err)
	}

	// Re-attach the auth methods from the CREATED record, never the request,
	// and only when it carries some: an empty slice means "no restriction" to
	// UpdateUser, so this write can re-state a restriction but never remove one.
	updateOpts := UpdateUserOptions{}
	if len(user.AuthMethods) > 0 {
		methods := append([]string(nil), user.AuthMethods...)
		updateOpts.AuthMethods = &methods
	}
	updated, err := s.UpdateUser(ctx, tenantID, user.ID, updateOpts)
	if err != nil {
		return nil, "", nil, s.rollbackInvitedUser(ctx, tenantID, user.ID, invites, token, err)
	}
	if updated != nil {
		user = updated
	}
	return user, token, record, nil
}

// rollbackInvitedUser undoes a create that could not complete: it spends the
// minted token, if any, so it cannot outlive its user, then deletes the user.
// It returns cause, wrapped in *UserRollbackError if the rollback failed.
//
// The rollback runs detached from ctx's cancellation. Whatever failed the
// create can also have cancelled the request, and a cancelled rollback would
// leave exactly the stranded user it exists to prevent.
func (s *IdentityStore) rollbackInvitedUser(ctx context.Context, tenantID, userID string,
	invites InviteStore, token string, cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), inviteRollbackTimeout)
	defer cancel()

	var revokeErr error
	if token != "" {
		// Redeem is the store's single-use primitive; spending the token is
		// what revokes it.
		_, revokeErr = invites.Redeem(ctx, tenantID, token)
	}
	deleteErr := s.DeleteUser(ctx, tenantID, userID)

	if rollbackErr := errors.Join(revokeErr, deleteErr); rollbackErr != nil {
		return &UserRollbackError{TenantID: tenantID, UserID: userID, Cause: cause, Rollback: rollbackErr}
	}
	return cause
}

// UserReader reads one user. UserStorage and IdentityStore both satisfy it.
type UserReader interface {
	GetUser(ctx context.Context, tenantID, userID string) (*User, error)
}

// RereadUserAfterFailedWrite reads the persisted user after a write that
// reported failure. An error does not prove the write did not land: a commit
// whose response was lost, or that outlived the caller's deadline, reports
// failure on a user that changed. The read is detached from the request's
// cancellation, which may be the very reason the write failed, and bounded
// so a wedged store cannot hold the caller.
func RereadUserAfterFailedWrite(ctx context.Context, users UserReader, tenantID, userID string) (*User, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), inviteRollbackTimeout)
	defer cancel()
	return users.GetUser(ctx, tenantID, userID)
}
