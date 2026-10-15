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

package service

import (
	"context"
	stderrors "errors"
	"strconv"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// identityStoreGetter resolves the identity store a caller's account lives in.
// *quicBaaS satisfies it.
type identityStoreGetter interface {
	GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
}

var _ identityStoreGetter = (*quicBaaS)(nil)

const opResolveCallerAccount = "Service.ResolveCallerAccount"

// CallerAccountRefusedError reports that the authenticated caller's own
// account could not be derived from its identity: there is no authenticated
// principal, or the principal does not name a qrdb user in its tenant.
type CallerAccountRefusedError struct {
	Reason string
}

// Error implements the error interface.
func (e *CallerAccountRefusedError) Error() string { return e.Reason }

// IsCallerAccountRefused reports whether err is (or wraps) a refusal to
// derive the caller's account from its identity.
func IsCallerAccountRefused(err error) bool {
	var target *CallerAccountRefusedError
	return stderrors.As(err, &target)
}

// CallerAccountResolver resolves an authenticated subject to the qrdb user
// account it is. Transports that serve a caller's own resources (its
// sessions, its passkeys) reach it instead of reading an account id out of
// the subject themselves. *quicBaaS satisfies it.
type CallerAccountResolver interface {
	ResolveCallerAccount(ctx context.Context, subject interfaces.Subject) (uint64, error)
}

var _ CallerAccountResolver = (*quicBaaS)(nil)

// ResolveCallerAccount returns the qrdb user id of the authenticated subject,
// in the subject's own tenant. See resolveCallerAccount for how the subject's
// fields are read; a subject that names no account is refused with
// CallerAccountRefusedError under errors.ErrForbidden.
func (s *quicBaaS) ResolveCallerAccount(ctx context.Context, subject interfaces.Subject) (uint64, error) {
	store, err := s.GetIdentityStore(ctx)
	if err != nil {
		return 0, &errors.QBError{Code: errors.ErrConfiguration, Op: opResolveCallerAccount, Err: err}
	}
	return resolveCallerAccount(ctx, store, subject, opResolveCallerAccount)
}

// resolveCallerAccount finds the qrdb user the authenticated subject is, in
// the subject's own tenant, and returns that user's qrdb ID. Every path that
// acts on the caller's own account goes through it.
//
// How each transport fills the subject it reads:
//
//   - REST and MCP project the verified qrdb identity (auth.SubjectFromIdentity).
//     Over mTLS qrdb sets Identity.Subject to the USERNAME the client
//     certificate is bound to, so an administrator arrives as "admin". A
//     quicbaas-issued bearer token (REST/MCP bearer, the OIDC session
//     issuer, the platform bearer) carries the email or username the login
//     authenticated as its sub, and the user's qrdb ID as its signed uid
//     claim; those authenticators record the uid, and the subject arrives
//     with UserID set.
//   - gRPC accepts bearer tokens only; its authorization interceptor binds the
//     token's sub and uid the same way.
//   - QUIC and Unix SDK clients reach the same server handlers, so they inherit
//     one of the above.
//   - Embedded: the host binds the subject itself (auth.SubjectIntoContext) and
//     must set UserID or IDKind for the account to be resolvable.
//
// A verified UserID names the account and wins over ID. Otherwise the key is
// chosen by IDKind alone, never by what ID looks like: a username may be all
// digits, and reading "12345" as user ID 12345 would hand the caller another
// user's account. A subject whose kind is unknown, or that no user in its
// tenant matches, is refused with CallerAccountRefusedError.
func resolveCallerAccount(ctx context.Context, store *iam.IdentityStore, subject interfaces.Subject, op string) (uint64, error) {
	if store == nil {
		return 0, &errors.QBError{
			Code: errors.ErrConfiguration,
			Op:   op,
			Err:  &api.ServiceNotConfiguredError{Service: "identity store"},
		}
	}
	if subject.UserID != 0 {
		user, err := store.GetUser(ctx, subject.TenantID, subject.UserID)
		return callerAccountID(op, subject.TenantID, user, err)
	}
	key := subject.ID
	if key == "" {
		return 0, callerRefused(op, "the authenticated caller does not name a user account")
	}

	var (
		user *iam.User
		err  error
	)
	switch subject.IDKind {
	case interfaces.SubjectIDUserID:
		// A subject name of this kind is the account id rendered as text.
		userID, parseErr := strconv.ParseUint(key, 10, 64)
		if parseErr != nil || userID == 0 {
			return 0, callerRefused(op, "the authenticated caller does not name a user account")
		}
		user, err = store.GetUser(ctx, subject.TenantID, userID)
	case interfaces.SubjectIDUsername:
		user, err = store.GetUserByUsername(ctx, subject.TenantID, key)
	case interfaces.SubjectIDEmail:
		user, err = store.GetUserByEmail(ctx, subject.TenantID, key)
	default:
		return 0, callerRefused(op, "the authenticated caller does not say how it names its account")
	}
	return callerAccountID(op, subject.TenantID, user, err)
}

// callerAccountID turns a store lookup of the caller's account into its id,
// refusing a caller whose account is missing or belongs to another tenant.
func callerAccountID(op, tenantID string, user *iam.User, err error) (uint64, error) {
	if err != nil {
		var invalid *iam.ValidationError
		if iam.IsUserNotFound(err) || stderrors.As(err, &invalid) {
			return 0, callerRefused(op, "no user account in the caller's tenant matches the authenticated caller")
		}
		return 0, &errors.QBError{Code: errors.ErrInternal, Op: op, Err: err}
	}
	// The store is tenant-keyed; this guards against one that is not.
	if user.TenantID != tenantID {
		return 0, callerRefused(op, "no user account in the caller's tenant matches the authenticated caller")
	}
	return user.ID, nil
}

// callerRefused is the Forbidden error for a caller whose account cannot be
// resolved.
func callerRefused(op, reason string) error {
	return &errors.QBError{
		Code: errors.ErrForbidden,
		Op:   op,
		Err:  &CallerAccountRefusedError{Reason: reason},
	}
}
