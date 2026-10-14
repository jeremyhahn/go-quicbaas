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
	"encoding/json"
	stderrors "errors"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

const opFIDO2BeginAddCredential = "FIDO2Service.BeginAddCredential" //nolint:gosec // G101: an operation name, not a credential

// BeginAddCredential starts a ceremony that adds a passkey to the account of
// the authenticated caller. The account and its tenant come from the
// caller's identity, never from the request: tenantID is only the tenant the
// transport resolved for the request, and when it is set it must be the
// caller's own. The ceremony is completed with FinishRegistration.
func (f *fido2Service) BeginAddCredential(ctx context.Context, tenantID string) (*xkmstransport.BeginRegistrationResponse, error) {
	subject, ok := auth.SubjectFromContext(ctx)
	if !ok || !subject.Authenticated {
		return nil, &errors.QBError{
			Code: errors.ErrUnauthorized,
			Op:   opFIDO2BeginAddCredential,
			Err:  &CallerAccountRefusedError{Reason: "adding a passkey requires an authenticated caller"},
		}
	}
	owner := subject.TenantID
	if owner == "" {
		return nil, ErrFIDO2TenantRequired
	}
	if tenantID != "" && tenantID != owner {
		return nil, &errors.QBError{
			Code: errors.ErrForbidden,
			Op:   opFIDO2BeginAddCredential,
			Err: &tenantctx.CrossTenantError{
				Operation:    opFIDO2BeginAddCredential,
				CallerTenant: owner,
				TargetTenant: tenantID,
			},
		}
	}
	// *quicBaaS supplies the identity store alongside the WebAuthn manager.
	stores, ok := f.manager.(identityStoreGetter)
	if !ok {
		return nil, ErrFIDO2NotConfigured
	}
	store, err := stores.GetIdentityStore(ctx)
	if err != nil {
		return nil, &errors.QBError{Code: errors.ErrConfiguration, Op: opFIDO2BeginAddCredential, Err: err}
	}
	userID, err := resolveCallerAccount(ctx, store, subject, opFIDO2BeginAddCredential)
	if err != nil {
		return nil, err
	}

	factory, err := f.webAuthnFactory(ctx)
	if err != nil {
		return nil, err
	}
	if factory == nil {
		return nil, ErrFIDO2NotConfigured
	}
	creation, sessionID, err := factory.BeginAddCredential(ctx, owner, userID)
	if err != nil {
		var mismatch *iam.WebAuthnAccountMismatchError
		if stderrors.As(err, &mismatch) {
			return nil, &errors.QBError{Code: errors.ErrForbidden, Op: opFIDO2BeginAddCredential, Err: err}
		}
		return nil, err
	}
	options, err := json.Marshal(creation)
	if err != nil {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: opFIDO2BeginAddCredential, Err: err}
	}
	return &xkmstransport.BeginRegistrationResponse{SessionID: sessionID, OptionsJSON: string(options)}, nil
}
