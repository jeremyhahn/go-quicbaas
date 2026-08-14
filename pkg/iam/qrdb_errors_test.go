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
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TestMapQRDBTenantError asserts that every qrdb migration/barrier sentinel is
// translated to the correct QuicBaaS dberrors code, that the original error is
// preserved as the wrapped cause, and that a nil input produces nil output.
func TestMapQRDBTenantError(t *testing.T) {
	// entitlementErr is a concrete *EntitlementError (Feature-denied variant).
	entitlementErr := &qrdbsdk.EntitlementError{TenantID: "t1", Feature: "dedicated-raft"}

	// genericErr is any non-sentinel error that falls through to ErrInternal.
	genericErr := errors.New("something unexpected")

	tests := []struct {
		name         string
		input        error
		expectedCode dberrors.ErrorCode
		expectNil    bool
	}{
		{
			name:      "nil input maps to nil",
			input:     nil,
			expectNil: true,
		},
		{
			name:         "EntitlementError maps to ErrForbidden",
			input:        entitlementErr,
			expectedCode: dberrors.ErrForbidden,
		},
		{
			name:         "ErrSharedBarrierImmutable maps to ErrForbidden",
			input:        qrdbsdk.ErrSharedBarrierImmutable,
			expectedCode: dberrors.ErrForbidden,
		},
		{
			name:         "ErrTenantExists maps to ErrAlreadyExists",
			input:        qrdbsdk.ErrTenantExists,
			expectedCode: dberrors.ErrAlreadyExists,
		},
		{
			name:         "ErrTenantMissing maps to ErrNotFound",
			input:        qrdbsdk.ErrTenantMissing,
			expectedCode: dberrors.ErrNotFound,
		},
		{
			name:         "ErrMigrationInProgress maps to ErrConflict",
			input:        qrdbsdk.ErrMigrationInProgress,
			expectedCode: dberrors.ErrConflict,
		},
		{
			name:         "ErrMigrationSourceNotNone maps to ErrConflict",
			input:        qrdbsdk.ErrMigrationSourceNotNone,
			expectedCode: dberrors.ErrConflict,
		},
		{
			name:         "ErrMigrationInvalidMechanism maps to ErrInvalidInput",
			input:        qrdbsdk.ErrMigrationInvalidMechanism,
			expectedCode: dberrors.ErrInvalidInput,
		},
		{
			name:         "ErrMigrationInvalidAvailability maps to ErrInvalidInput",
			input:        qrdbsdk.ErrMigrationInvalidAvailability,
			expectedCode: dberrors.ErrInvalidInput,
		},
		{
			name:         "ErrMigrationInvalidTarget maps to ErrInvalidInput",
			input:        qrdbsdk.ErrMigrationInvalidTarget,
			expectedCode: dberrors.ErrInvalidInput,
		},
		{
			name:         "ErrShamirThresholdInvalid maps to ErrInvalidInput",
			input:        qrdbsdk.ErrShamirThresholdInvalid,
			expectedCode: dberrors.ErrInvalidInput,
		},
		{
			name:         "ErrMigrationProviderUnavailable maps to ErrNotImplemented",
			input:        qrdbsdk.ErrMigrationProviderUnavailable,
			expectedCode: dberrors.ErrNotImplemented,
		},
		{
			name:         "unknown error maps to ErrInternal",
			input:        genericErr,
			expectedCode: dberrors.ErrInternal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := mapQRDBTenantError("test-op", tc.input)

			if tc.expectNil {
				if result != nil {
					t.Fatalf("expected nil, got %v", result)
				}
				return
			}

			if result == nil {
				t.Fatal("expected non-nil error, got nil")
			}

			var qbErr *dberrors.QBError
			if !errors.As(result, &qbErr) {
				t.Fatalf("expected *dberrors.QBError, got %T: %v", result, result)
			}

			if qbErr.Code != tc.expectedCode {
				t.Errorf("Code: got %v, want %v", qbErr.Code, tc.expectedCode)
			}

			// The original error must be reachable via errors.Is so callers can
			// branch on the sentinel directly after mapping.
			if !errors.Is(result, tc.input) {
				t.Errorf("errors.Is(result, input) = false; original error not wrapped")
			}
		})
	}
}

// TestMapQRDBTenantError_OpPropagated verifies the op string is preserved in
// the returned QBError so callers get useful context in error messages.
func TestMapQRDBTenantError_OpPropagated(t *testing.T) {
	result := mapQRDBTenantError("UpdateTenantBarrier", errors.New("boom"))

	var qbErr *dberrors.QBError
	if !errors.As(result, &qbErr) {
		t.Fatalf("expected *dberrors.QBError, got %T", result)
	}
	if qbErr.Op != "UpdateTenantBarrier" {
		t.Errorf("Op: got %q, want %q", qbErr.Op, "UpdateTenantBarrier")
	}
}
