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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// go-qrdb's authorization refusals are ordinary refusals: every transport
// must report them as forbidden, not as a server fault (#1769).
func TestMapQRDBRoleError_DenialsAreForbidden(t *testing.T) {
	for _, tc := range []struct {
		name     string
		src      error
		wantCode dberrors.ErrorCode
		isType   func(error) bool
	}{
		{
			name:     "permission denied",
			src:      &qrdbsdk.PermissionDeniedError{Resource: "roles", Action: "create", Subject: "acme-admin"},
			wantCode: dberrors.ErrForbidden,
			isType: func(err error) bool {
				var denied *qrdbsdk.PermissionDeniedError
				return errors.As(err, &denied)
			},
		},
		{
			name:     "cross-tenant denied",
			src:      &qrdbsdk.CrossTenantDeniedError{IdentityTenantID: "acme", RequestedTenantID: "globex", Op: "reconcile roles"},
			wantCode: dberrors.ErrForbidden,
			isType: func(err error) bool {
				var denied *qrdbsdk.CrossTenantDeniedError
				return errors.As(err, &denied)
			},
		},
		{
			name:     "unknown error",
			src:      assert.AnError,
			wantCode: dberrors.ErrInternal,
			isType:   func(err error) bool { return errors.Is(err, assert.AnError) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := mapQRDBRoleError("reconcile", tc.src)

			var qbErr *dberrors.QBError
			require.ErrorAs(t, err, &qbErr)
			assert.Equal(t, tc.wantCode, qbErr.Code)
			assert.Equal(t, "iam.role.qrdb.reconcile", qbErr.Op)
			assert.True(t, tc.isType(err), "the go-qrdb error must stay in the chain")
		})
	}
}
