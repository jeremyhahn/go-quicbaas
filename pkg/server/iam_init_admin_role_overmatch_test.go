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

package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// roleLookupFailingStorage answers GetRole with a chosen error and delegates
// the rest of the RoleStorage contract.
type roleLookupFailingStorage struct {
	iam.RoleStorage
	err error
}

func (s *roleLookupFailingStorage) GetRole(context.Context, string, string) (*iam.Role, error) {
	return nil, s.err
}

func adminRoleStore(t *testing.T, getErr error) *iam.IdentityStore {
	t.Helper()
	mem := iam.NewMemoryStorage()
	return iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  mem,
		GroupStorage: mem,
		RoleStorage:  &roleLookupFailingStorage{RoleStorage: mem, err: getErr},
	})
}

// TestReconcileAdminRole_ForeignNotFoundIsNotAMissingRole is go-quicbaas#434
// applied to the bootstrap check.
//
// The function's own comment states the rule: "A transport/storage error (e.g.
// sealed barrier) is not a missing role -- surface it verbatim so the caller
// does not mistake it for a reconciliation gap." errors.Is against
// iam.ErrRoleNotFound compared the QBError CODE, so every not-found the role
// lookup could raise was reported as ErrAdminRoleMissing, which is exactly the
// mistake the comment forbids -- and it invites a re-bootstrap against a store
// that is merely unreachable.
func TestReconcileAdminRole_ForeignNotFoundIsNotAMissingRole(t *testing.T) {
	storeFailure := &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.role.qrdb.get",
		Err:  &iam.IAMMessageError{Message: "platform shard is unreachable"},
	}

	err := reconcileAdminRole(context.Background(), adminRoleStore(t, storeFailure))

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrAdminRoleMissing,
		"an unreachable role store was reported as an absent admin role")
	assert.ErrorIs(t, err, storeFailure, "the store failure was not surfaced verbatim")
}

// TestReconcileAdminRole_AbsentRoleStillReportsMissing is the positive half:
// the one condition the branch exists for still produces ErrAdminRoleMissing.
func TestReconcileAdminRole_AbsentRoleStillReportsMissing(t *testing.T) {
	err := reconcileAdminRole(context.Background(), adminRoleStore(t, iam.ErrRoleNotFound))

	assert.ErrorIs(t, err, ErrAdminRoleMissing)
}
