// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

func TestServer_IntrospectTokenActiveCarriesClaims(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	expires := time.Unix(1700003600, 0)
	mockSvc.On("IntrospectToken", mock.Anything, &service.IntrospectTokenRequest{Token: "live-token"}).
		Return(&service.TokenIntrospection{
			Active:       true,
			Subject:      "user-1",
			Username:     "admin",
			TokenType:    "Bearer",
			ExpiresAt:    expires,
			Audience:     []string{"quicbaas"},
			JTI:          "jti-1",
			AttestLevel:  "hardware",
			AttestAAGUID: "cb69481e8ff7403993ec0a2729a154a8",
		}, nil).Once()

	resp, err := srv.IntrospectToken(context.Background(), &pb.IntrospectTokenRequest{Token: "live-token"})
	require.NoError(t, err)
	assert.True(t, resp.GetActive())
	assert.Equal(t, "user-1", resp.GetSubject())
	assert.Equal(t, "hardware", resp.GetAttestLevel())
	assert.Equal(t, "cb69481e8ff7403993ec0a2729a154a8", resp.GetAttestAaguid())
	assert.Equal(t, expires.Unix(), resp.GetExpiresAt())
	assert.Zero(t, resp.GetIssuedAt(), "an unset time must stay 0, not the zero time's Unix value")
	assert.Equal(t, []string{"quicbaas"}, resp.GetAudience())
	mockSvc.AssertExpectations(t)
}

func TestServer_IntrospectTokenInactiveCarriesOnlyActive(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockSvc.On("IntrospectToken", mock.Anything, mock.Anything).
		Return(&service.TokenIntrospection{Active: false}, nil).Once()

	resp, err := srv.IntrospectToken(context.Background(), &pb.IntrospectTokenRequest{Token: "garbage"})
	require.NoError(t, err)
	assert.Equal(t, &pb.IntrospectTokenResponse{Active: false}, resp)
}

func TestServer_IntrospectTokenRefusesMissingToken(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockSvc.On("IntrospectToken", mock.Anything, &service.IntrospectTokenRequest{}).
		Return(nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.IntrospectToken",
			Err:  &api.RequiredFieldError{Field: "token"},
		}).Once()

	_, err := srv.IntrospectToken(context.Background(), &pb.IntrospectTokenRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = srv.IntrospectToken(context.Background(), nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestIntrospectToken_UnauthenticatedRefused mirrors REST, whose
// /oauth2/introspect refuses a caller that presents no credential: the gRPC
// interceptor must refuse it before the handler runs, and an authenticated
// caller is checked against the method's mapped grant.
func TestIntrospectToken_UnauthenticatedRefused(t *testing.T) {
	const method = "/quicbaas.v1.QuicBaaS/IntrospectToken"
	assert.False(t, isPublicGRPCMethod(method))

	authorizer := &fakeValidatorAuthorizer{
		token:       "good",
		grantedPerm: "user_authfactors:login",
		authzCtx:    &platformtoken.AuthorizedContext{Subject: "resource-server", TenantID: "system"},
	}
	interceptor := UnaryAuthorizationInterceptor(authorizer)
	handlerRan := false
	handler := func(context.Context, interface{}) (interface{}, error) {
		handlerRan = true
		return "ok", nil
	}

	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: method}, handler)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.False(t, handlerRan)

	_, err = interceptor(ctxWithBearer("good"), nil, &grpc.UnaryServerInfo{FullMethod: method}, handler)
	require.NoError(t, err)
	assert.True(t, handlerRan)
	assert.Equal(t, "user_authfactors:login", authorizer.lastPerm)
}
