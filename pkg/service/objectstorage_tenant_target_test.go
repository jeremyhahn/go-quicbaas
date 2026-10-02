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
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// callerOf is the context go-qrdb's AuthMiddleware gives a request it
// authenticated for tenantID: the tenant under go-qrdb's central key.
func callerOf(tenantID string) context.Context {
	return qrdbsdk.ContextWithTenant(context.Background(), tenantID)
}

// requireForbidden asserts err is coded ErrForbidden or ErrCrossTenantAccess,
// the two codes every transport maps to 403 / PermissionDenied.
func requireForbidden(t *testing.T, err error, msg string) {
	t.Helper()
	require.Error(t, err, msg)
	var qb *errors.QBError
	require.True(t, stderrors.As(err, &qb), "%s: want a coded error, got %v", msg, err)
	assert.Contains(t, []errors.ErrorCode{errors.ErrForbidden, errors.ErrCrossTenantAccess}, qb.Code, msg)
}

// TestObjectStorage_CallerNamedTenantIsChecked is a go-quicbaas#1150
// regression: the object-storage service scoped every operation by the tenant
// the REQUEST named (REST's X-Tenant-ID header, gRPC's tenant_id field), and
// an empty one disabled the check, so a tenant-a caller read tenant-b's
// object by naming tenant-b or by naming nobody.
func TestObjectStorage_CallerNamedTenantIsChecked(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)

	_, err := svc.PutObject(callerOf("tenant-b"), &PutObjectRequest{
		Key: "b/secret.txt", Value: []byte("tenant-b data"),
	})
	require.NoError(t, err, "tenant-b writes its own object")

	_, err = svc.GetObject(callerOf(qrdbsdk.SystemTenantID), &GetObjectRequest{Key: "b/secret.txt", TenantID: "tenant-b"})
	requireForbidden(t, err, "the system tenant naming tenant-b (no cross-tenant lift)")

	_, err = svc.GetObject(callerOf("tenant-a"), &GetObjectRequest{Key: "b/secret.txt", TenantID: "tenant-b"})
	requireForbidden(t, err, "tenant-a naming tenant-b")

	_, err = svc.GetObject(callerOf("tenant-a"), &GetObjectRequest{Key: "b/secret.txt"})
	requireForbidden(t, err, "tenant-a naming nobody")

	_, err = svc.PutObject(callerOf("tenant-a"), &PutObjectRequest{
		Key: "a/x.txt", Value: []byte("x"), TenantID: "tenant-b",
	})
	requireForbidden(t, err, "tenant-a writing into tenant-b")

	got, err := svc.GetObject(callerOf("tenant-b"), &GetObjectRequest{Key: "b/secret.txt"})
	require.NoError(t, err, "the owning tenant reads its object")
	assert.Equal(t, []byte("tenant-b data"), got.Value)
}

// TestObjectStorage_UnboundCallerIsRefused proves the fail-closed half: a
// request with no tenant bound is refused, not served as the platform scope.
func TestObjectStorage_UnboundCallerIsRefused(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	_, err := svc.GetObject(context.Background(), &GetObjectRequest{Key: "anything"})
	requireForbidden(t, err, "unbound caller")
}
