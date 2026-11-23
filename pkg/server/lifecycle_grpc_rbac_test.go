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
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

func TestQRDBGRPCRBACAuthorizer_NoNodeFailsClosed(t *testing.T) {
	err := (&qrdbGRPCRBACAuthorizer{server: &Server{}}).Authorize(
		context.Background(), &qrdbtransport.Identity{Subject: "alice", TenantID: "t1"}, "kv", "put")

	var unavailable *QRDBNodeClientUnavailableError
	require.ErrorAs(t, err, &unavailable)
}

func TestQRDBGRPCRBACAuthorizer_DelegatesToQRDB(t *testing.T) {
	const tenant = "acme"
	s, node := newABACTestServer(t, true)
	grant, err := policyToABAC(abacTestPolicy("grant-alice", tenant, "alice", "kv/**", iam.EffectAllow))
	require.NoError(t, err)
	require.NoError(t, node.ABACPolicyReloader().Reload([]*qrdbsdk.ABACPolicy{grant}))
	rbac := &qrdbGRPCRBACAuthorizer{server: s}
	ctx := qrdbsdk.ContextWithTenant(context.Background(), tenant)

	require.NoError(t, rbac.Authorize(ctx, &qrdbtransport.Identity{Subject: "alice", TenantID: tenant}, "kv/orders", "read"),
		"a granted identity is allowed")

	err = rbac.Authorize(ctx, &qrdbtransport.Identity{Subject: "bob", TenantID: tenant}, "kv/orders", "read")
	require.Error(t, err, "an ungranted identity is denied")
	var rbacDenied *qrdbsdk.PermissionDeniedError
	var abacDenied *qrdbsdk.ABACPolicyDeniedError
	assert.True(t, errors.As(err, &rbacDenied) || errors.As(err, &abacDenied),
		"the denial is go-qrdb's typed denial, which the gate maps to PermissionDenied: %v", err)
}

// TestGRPCAuthorizationInterceptors_InstallsQRDBAuthorizer runs the gate the
// server builds under RequireAuthz on a mapped method, on a Server with no
// qrdb node: an unaccepted token is refused before go-qrdb is asked, and no
// call reaches the handler.
func TestGRPCAuthorizationInterceptors_InstallsQRDBAuthorizer(t *testing.T) {
	provider := newTestOIDCProvider(t)
	s := &Server{
		logger:          newGRPCAuthzTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: &grpcOIDCGetterService{provider: provider},
	}
	unary, stream, err := s.grpcAuthorizationInterceptors()
	require.NoError(t, err)
	require.NotNil(t, unary)
	require.NotNil(t, stream)

	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.New(map[string]string{"authorization": "Bearer tok-test"}))
	reached := false
	_, err = unary(ctx, nil, &ggrpc.UnaryServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Put"},
		func(context.Context, interface{}) (interface{}, error) {
			reached = true
			return "ok", nil
		})
	require.Error(t, err)
	assert.False(t, reached, "the handler is not reached")
	assert.Contains(t, []codes.Code{codes.Unauthenticated, codes.Unavailable}, status.Code(err))
}
