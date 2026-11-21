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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

func TestQRDBDataTenantGuard_SetMatchesMapping(t *testing.T) {
	want := make(map[string]bool, len(qrdbDataSurfaceMethods))
	for name := range qrdbDataSurfaceMethods {
		want["/qrdb.v1.QRDBService/"+name] = true
	}
	assert.Equal(t, want, qrdbRegistryDataMethods)
	for method := range qrdbRegistryDataMethods {
		_, _, mapped := grpcResourceAndAction(method)
		assert.True(t, mapped, method)
	}
}

// qrdbDataGuardFullChain runs the authorization interceptor with a token
// carrying authzCtx, then the tenant guard, for the qrdb Put method.
func qrdbDataGuardFullChain(t *testing.T, authzCtx *platformtoken.AuthorizedContext) (bool, string, error) {
	t.Helper()
	authz := UnaryAuthorizationInterceptor(&fakeValidatorAuthorizer{token: "good", grantedPerm: "kv:put", authzCtx: authzCtx})
	guard := UnaryQRDBDataTenantInterceptor()
	const method = "/qrdb.v1.QRDBService/Put"
	info := &grpc.UnaryServerInfo{FullMethod: method}
	ran := false
	var tenant string
	_, err := authz(ctxWithBearer("good"), nil, info, func(ctx context.Context, req interface{}) (interface{}, error) {
		return guard(ctx, req, info, func(ctx context.Context, _ interface{}) (interface{}, error) {
			ran = true
			tid, terr := tenantctx.FromContext(ctx, method)
			if terr == nil {
				tenant = tid
			}
			return nil, nil
		})
	})
	return ran, tenant, err
}

func TestQRDBDataTenantGuard_FullChain_TokenWithoutTenantDenied(t *testing.T) {
	ran, _, err := qrdbDataGuardFullChain(t, &platformtoken.AuthorizedContext{Subject: "u1"})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, ran, "the data handler must not run without a bound tenant")
}

func TestQRDBDataTenantGuard_FullChain_TenantTokenAdmitted(t *testing.T) {
	ran, tenant, err := qrdbDataGuardFullChain(t, &platformtoken.AuthorizedContext{Subject: "u1", TenantID: "t1652"})
	require.NoError(t, err)
	assert.True(t, ran)
	assert.Equal(t, "t1652", tenant)
}

func TestQRDBDataTenantGuard_NonDataMethodPassesThrough(t *testing.T) {
	ran := false
	_, err := UnaryQRDBDataTenantInterceptor()(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/qrdb.v1.QRDBService/Status"},
		func(context.Context, interface{}) (interface{}, error) { ran = true; return nil, nil })
	require.NoError(t, err)
	assert.True(t, ran)
}

func TestStreamQRDBDataTenantGuard_DeniesUnbound(t *testing.T) {
	ran := false
	err := StreamQRDBDataTenantInterceptor()(nil, &fakeServerStream{ctx: context.Background()},
		&grpc.StreamServerInfo{FullMethod: "/qrdb.v1.QRDBService/BatchImport", IsClientStream: true},
		func(interface{}, grpc.ServerStream) error { ran = true; return nil })
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, ran)
}
