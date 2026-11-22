//go:build !integration

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// TestIAMUserLookupGRPCAuthorization pins the user lookups' authorization.
// GetUser and GetUserByUsername both take users:read, the grant QRDBService's
// user reads take: under RequireAuthz a caller holding users:read passes the
// gate and a caller without it is denied before the handler runs.
func TestIAMUserLookupGRPCAuthorization(t *testing.T) {
	methods := []string{
		iampb.IAMService_GetUser_FullMethodName,
		iampb.IAMService_GetUserByUsername_FullMethodName,
	}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			resource, action, ok := grpcResourceAndAction(method)
			require.True(t, ok, "%s must be mapped", method)
			assert.Equal(t, "users", resource)
			assert.Equal(t, "read", action)
			assert.False(t, publicGRPCMethods[method], "%s must not be public", method)

			info := &grpc.UnaryServerInfo{FullMethod: method}
			for _, tc := range []struct {
				granted string
				allowed bool
			}{
				{granted: "users:read", allowed: true},
				{granted: "users:list", allowed: false},
			} {
				t.Run(tc.granted, func(t *testing.T) {
					authorizer := &fakeValidatorAuthorizer{
						token:       "good",
						grantedPerm: tc.granted,
						authzCtx:    &platformtoken.AuthorizedContext{Subject: "u1", TenantID: "t1"},
					}
					called := false
					handler := func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil }

					_, err := UnaryAuthorizationInterceptor(authorizer)(ctxWithBearer("good"), nil, info, handler)
					assert.Equal(t, "users:read", authorizer.lastPerm)
					assert.Equal(t, tc.allowed, called)
					if tc.allowed {
						require.NoError(t, err)
						return
					}
					assert.Equal(t, codes.PermissionDenied, status.Code(err))
				})
			}
		})
	}
}

// TestIAMUserLookupGRPC_ThroughGate drives the real handlers behind the real
// authorization interceptor: a users:read caller of its own tenant gets the
// user back, and a caller of another tenant is refused without a lookup.
func TestIAMUserLookupGRPC_ThroughGate(t *testing.T) {
	gate := func(tenant string) (grpc.UnaryServerInterceptor, context.Context) {
		authorizer := &fakeValidatorAuthorizer{
			token:       "good",
			grantedPerm: "users:read",
			authzCtx:    &platformtoken.AuthorizedContext{Subject: "u1", TenantID: tenant},
		}
		return UnaryAuthorizationInterceptor(authorizer), ctxWithBearer("good")
	}

	t.Run("GetUser", func(t *testing.T) {
		srv, svc := newTestIAMServer(t)
		svc.On("GetUser", mock.Anything, "t1", "alice@test.com").
			Return(&iam.User{ID: 30001, TenantID: "t1", Username: "alice", Email: "alice@test.com"}, nil)
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			return srv.GetUser(ctx, req.(*iampb.GetUserRequest))
		}
		interceptor, ctx := gate("t1")
		info := &grpc.UnaryServerInfo{FullMethod: iampb.IAMService_GetUser_FullMethodName}

		resp, err := interceptor(ctx, &iampb.GetUserRequest{TenantId: "t1", UserId: "alice@test.com"}, info, handler)
		require.NoError(t, err)
		assert.Equal(t, "alice@test.com", resp.(*iampb.User).Email)
		svc.AssertExpectations(t)
	})

	t.Run("GetUserByUsername", func(t *testing.T) {
		srv, svc := newTestIAMServer(t)
		svc.On("GetUserByUsername", mock.Anything, "t1", "alice").
			Return(&iam.User{ID: 30001, TenantID: "t1", Username: "alice", Email: "alice@test.com"}, nil)
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			return srv.GetUserByUsername(ctx, req.(*iampb.GetUserByUsernameRequest))
		}
		interceptor, ctx := gate("t1")
		info := &grpc.UnaryServerInfo{FullMethod: iampb.IAMService_GetUserByUsername_FullMethodName}

		resp, err := interceptor(ctx, &iampb.GetUserByUsernameRequest{TenantId: "t1", Username: "alice"}, info, handler)
		require.NoError(t, err)
		assert.Equal(t, "alice", resp.(*iampb.User).Username)
		svc.AssertExpectations(t)
	})

	t.Run("foreign tenant refused", func(t *testing.T) {
		srv, svc := newTestIAMServer(t)
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			return srv.GetUserByUsername(ctx, req.(*iampb.GetUserByUsernameRequest))
		}
		interceptor, ctx := gate("t2")
		info := &grpc.UnaryServerInfo{FullMethod: iampb.IAMService_GetUserByUsername_FullMethodName}

		_, err := interceptor(ctx, &iampb.GetUserByUsernameRequest{TenantId: "t1", Username: "alice"}, info, handler)
		require.Error(t, err)
		assert.NotEqual(t, codes.OK, status.Code(err))
		svc.AssertNotCalled(t, "GetUserByUsername", mock.Anything, mock.Anything, mock.Anything)
	})
}
