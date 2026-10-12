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
// tenant matches, is refused with FIDO2CallerRefusedError.
func resolveCallerAccount(ctx context.Context, store *iam.IdentityStore, subject interfaces.Subject, op string) (uint64, error) {
	if store == nil {
		return 0, &errors.QBError{
			Code: errors.ErrConfiguration,
			Op:   op,
			Err:  &api.ServiceNotConfiguredError{Service: "identity store"},
		}
	}
	key, kind := subject.ID, subject.IDKind
	if subject.UserID != "" {
		key, kind = subject.UserID, interfaces.SubjectIDUserID
	}
	if key == "" {
		return 0, callerRefused(op, "the authenticated caller does not name a user account")
	}

	var (
		user *iam.User
		err  error
	)
	switch kind {
	case interfaces.SubjectIDUserID:
		if _, parseErr := iam.ParseUserID(key); parseErr != nil {
			return 0, callerRefused(op, "the authenticated caller does not name a user account")
		}
		user, err = store.GetUser(ctx, subject.TenantID, key)
	case interfaces.SubjectIDUsername:
		user, err = store.GetUserByUsername(ctx, subject.TenantID, key)
	case interfaces.SubjectIDEmail:
		user, err = store.GetUserByEmail(ctx, subject.TenantID, key)
	default:
		return 0, callerRefused(op, "the authenticated caller does not say how it names its account")
	}
	if err != nil {
		var invalid *iam.ValidationError
		if iam.IsUserNotFound(err) || stderrors.As(err, &invalid) {
			return 0, callerRefused(op, "no user account in the caller's tenant matches the authenticated caller")
		}
		return 0, &errors.QBError{Code: errors.ErrInternal, Op: op, Err: err}
	}
	// The store is tenant-keyed; this guards against one that is not.
	if user.TenantID != subject.TenantID {
		return 0, callerRefused(op, "no user account in the caller's tenant matches the authenticated caller")
	}

	userID, err := user.QRDBID()
	if err != nil {
		return 0, &errors.QBError{Code: errors.ErrInternal, Op: op, Err: err}
	}
	return userID, nil
}

// callerRefused is the Forbidden error for a caller whose account cannot be
// resolved.
func callerRefused(op, reason string) error {
	return &errors.QBError{
		Code: errors.ErrForbidden,
		Op:   op,
		Err:  &FIDO2CallerRefusedError{Reason: reason},
	}
}
