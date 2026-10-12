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
	"strconv"

	"github.com/go-webauthn/webauthn/protocol"
	xkmswebauthn "github.com/jeremyhahn/go-xkms/pkg/webauthn"
)

// WebAuthnAccountMismatchError is returned by BeginAddCredential when the
// authenticated caller does not name an existing WebAuthn account in the
// tenant: the user has no WebAuthn account record there, or the record no
// longer matches the account the caller authenticated as.
type WebAuthnAccountMismatchError struct {
	// TenantID is the tenant the account was looked up in.
	TenantID string

	// UserID is the qrdb user id of the authenticated caller.
	UserID uint64

	// Err is the underlying lookup or ceremony failure.
	Err error
}

func (e *WebAuthnAccountMismatchError) Error() string {
	return "webauthn: user " + strconv.FormatUint(e.UserID, 10) +
		" does not name a webauthn account in tenant " + strconv.Quote(e.TenantID) +
		": " + e.Err.Error()
}

func (e *WebAuthnAccountMismatchError) Unwrap() error { return e.Err }

// BeginAddCredential starts a registration ceremony that adds a passkey to
// the WebAuthn account of qrdb user userID in tenantID. The caller must have
// already authenticated as that user: the account is derived from the
// caller's identity, never from request input, so a session can only add a
// passkey to its own account.
//
// The ceremony is completed through the tenant service's FinishRegistration
// with the returned session id, exactly like a first registration; that is
// where attestation is verified and the factor row written.
func (f *WebAuthnServiceFactory) BeginAddCredential(ctx context.Context, tenantID string, userID uint64) (*protocol.CredentialCreation, string, error) {
	svc, err := f.ServiceFor(ctx, tenantID)
	if err != nil {
		return nil, "", err
	}
	handle := xkmswebauthn.HandleFromUserID(userID)
	user, err := svc.GetUser(ctx, handle)
	if err != nil {
		if xkmswebauthn.IsUserNotFound(err) {
			return nil, "", &WebAuthnAccountMismatchError{TenantID: tenantID, UserID: userID, Err: err}
		}
		return nil, "", err
	}
	creation, sessionID, err := svc.BeginAddCredential(ctx, xkmswebauthn.SessionAccount{
		UserID: handle,
		Email:  user.Email(),
	})
	if err != nil {
		if errors.Is(err, xkmswebauthn.ErrSessionAccountMismatch) {
			return nil, "", &WebAuthnAccountMismatchError{TenantID: tenantID, UserID: userID, Err: err}
		}
		return nil, "", err
	}
	return creation, sessionID, nil
}
