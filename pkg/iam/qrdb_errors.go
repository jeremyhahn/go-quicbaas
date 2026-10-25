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
	"errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// mapQRDBTenantError translates qrdb tenant provisioner and migration errors
// into the broker's typed QuicBaaS error sentinels. It is defined here (in
// pkg/iam) so that tenant.go can call it without a circular import through
// pkg/iam/qrdbadapter. It is the single canonical translator for qrdb tenant
// errors at the broker boundary.
//
// Mapping:
//
//	*EntitlementError                 → ErrForbidden  (403)
//	ErrSharedBarrierImmutable         → ErrForbidden  (403)
//	ErrTenantExists                   → ErrAlreadyExists (409)
//	ErrTenantMissing                  → ErrNotFound   (404)
//	ErrMigrationInProgress            → ErrConflict   (409)
//	ErrMigrationSourceNotNone         → ErrConflict   (409)
//	ErrMigrationInvalidMechanism      → ErrInvalidInput (400)
//	ErrMigrationInvalidAvailability   → ErrInvalidInput (400)
//	ErrMigrationInvalidTarget         → ErrInvalidInput (400)
//	ErrShamirThresholdInvalid         → ErrInvalidInput (400)
//	ErrMigrationProviderUnavailable   → ErrNotImplemented (501)
//	default                           → ErrInternal   (500)
func mapQRDBTenantError(op string, err error) error {
	if err == nil {
		return nil
	}
	var entErr *qrdbsdk.EntitlementError
	if errors.As(err, &entErr) {
		return &dberrors.QBError{Code: dberrors.ErrForbidden, Op: op, Err: err}
	}
	// A caller without the permission: an ordinary refusal, not a server fault.
	var permissionDenied *qrdbsdk.PermissionDeniedError
	if errors.As(err, &permissionDenied) {
		return &dberrors.QBError{Code: dberrors.ErrForbidden, Op: op, Err: err}
	}
	// A tenant-scoped caller naming another tenant.
	var crossTenant *qrdbsdk.CrossTenantDeniedError
	if errors.As(err, &crossTenant) {
		return &dberrors.QBError{Code: dberrors.ErrForbidden, Op: op, Err: err}
	}
	if errors.Is(err, qrdbsdk.ErrSharedBarrierImmutable) {
		return &dberrors.QBError{Code: dberrors.ErrForbidden, Op: op, Err: err}
	}
	if errors.Is(err, qrdbsdk.ErrTenantExists) {
		return &dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: op, Err: err}
	}
	if errors.Is(err, qrdbsdk.ErrTenantMissing) {
		return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: op, Err: err}
	}
	if errors.Is(err, qrdbsdk.ErrMigrationInProgress) || errors.Is(err, qrdbsdk.ErrMigrationSourceNotNone) {
		return &dberrors.QBError{Code: dberrors.ErrConflict, Op: op, Err: err}
	}
	if errors.Is(err, qrdbsdk.ErrMigrationInvalidMechanism) ||
		errors.Is(err, qrdbsdk.ErrMigrationInvalidAvailability) ||
		errors.Is(err, qrdbsdk.ErrMigrationInvalidTarget) ||
		errors.Is(err, qrdbsdk.ErrShamirThresholdInvalid) {
		return &dberrors.QBError{Code: dberrors.ErrInvalidInput, Op: op, Err: err}
	}
	if errors.Is(err, qrdbsdk.ErrMigrationProviderUnavailable) {
		return &dberrors.QBError{Code: dberrors.ErrNotImplemented, Op: op, Err: err}
	}
	return &dberrors.QBError{Code: dberrors.ErrInternal, Op: op, Err: err}
}
