//go:build !integration

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// TestIAMUserLookupGRPCAuthorization pins the user lookups' authorization.
// GetUserByUsername carries GetUser's authorization: neither is mapped to a
// permission nor public, so under RequireAuthz both are default-denied until
// IAMService user reads are mapped, even for a caller holding users:read.
func TestIAMUserLookupGRPCAuthorization(t *testing.T) {
	methods := []string{
		iampb.IAMService_GetUser_FullMethodName,
		iampb.IAMService_GetUserByUsername_FullMethodName,
	}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			_, _, ok := grpcResourceAndAction(method)
			assert.False(t, ok, "%s must stay unmapped", method)
			assert.False(t, publicGRPCMethods[method], "%s must not be public", method)

			authorizer := &fakeValidatorAuthorizer{
				token:       "good",
				grantedPerm: "users:read",
				authzCtx:    &platformtoken.AuthorizedContext{Subject: "u1", TenantID: "t1"},
			}
			called := false
			handler := func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil }
			info := &grpc.UnaryServerInfo{FullMethod: method}

			_, err := UnaryAuthorizationInterceptor(authorizer)(ctxWithBearer("good"), nil, info, handler)
			require.Error(t, err)
			assert.Equal(t, codes.PermissionDenied, status.Code(err))
			assert.Equal(t, (&UnmappedMethodError{Method: method}).Error(), status.Convert(err).Message())
			assert.False(t, called, "handler must not run for an unmapped method")
		})
	}
}
