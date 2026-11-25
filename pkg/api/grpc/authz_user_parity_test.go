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

// userGrants is the grant of each quicbaas.iam.v1.IAMService user verb the
// CLI calls over gRPC that is mapped by parity with the other gRPC user
// surface. Unmapped, RequireAuthz refuses each one for every caller, so
// `iam user create`, `iam user list` and `iam user delete` over gRPC fail
// with "method not authorized".
var userGrants = map[string]methodPermission{
	// Mirrors /qrdb.v1.QRDBService/CreateUser (users:create).
	"/quicbaas.iam.v1.IAMService/CreateUser": {"users", "create"},
	// Mirrors /qrdb.v1.QRDBService/ListUsers (users:list).
	"/quicbaas.iam.v1.IAMService/ListUsers": {"users", "list"},
	// Mirrors /qrdb.v1.QRDBService/DeleteUser (users:delete).
	"/quicbaas.iam.v1.IAMService/DeleteUser": {"users", "delete"},
}

func TestUserGRPCResourceAndAction(t *testing.T) {
	requireGRPCGrants(t, userGrants)
}

func TestUserGRPCAuthorization(t *testing.T) {
	requireGRPCGrantEnforced(t, userGrants)
}

// grantSetAuthorizer grants every permission in granted to the bearer token
// and records each requiredPerm it is asked, in order.
// grantEnforcedRequest is the request requireGRPCGrantEnforced sends for
// method: a typed request whose fields demand nothing beyond the method's
// grant when the method has a grpcRequestPermissions entry (which refuses a
// nil request), and nil otherwise. CreateUser and UpdateUser get a typed
// request with no roles, so each needs only its method's grant.
func grantEnforcedRequest(method string) interface{} {
	switch method {
	case "/quicbaas.iam.v1.IAMService/CreateUser":
		return &iampb.CreateUserRequest{}
	case "/quicbaas.iam.v1.IAMService/UpdateUser":
		return &iampb.UpdateUserRequest{}
	}
	return nil
}

type grantSetAuthorizer struct {
	token   string
	granted map[string]bool
	asked   []string
}

func (g *grantSetAuthorizer) Authorize(_ context.Context, token, requiredPerm, _, _, _ string) (*platformtoken.AuthorizedContext, error) {
	g.asked = append(g.asked, requiredPerm)
	if token != g.token {
		return nil, &platformtoken.InvalidTokenError{Detail: "signature did not verify"}
	}
	if !g.granted[requiredPerm] {
		return nil, &platformtoken.ScopeDeniedError{Required: requiredPerm}
	}
	return &platformtoken.AuthorizedContext{}, nil
}

// TestUserGRPCCreateWithRolesRequiresRolesAssign pins go-qrdb's native
// CreateUser rule (AuthorizeRoleAssignmentWriteOnCreate) on the IAMService
// CreateUser path: a request naming roles also takes roles:assign, and a
// request with no roles takes only users:create.
func TestUserGRPCCreateWithRolesRequiresRolesAssign(t *testing.T) {
	const method = "/quicbaas.iam.v1.IAMService/CreateUser"
	info := &grpc.UnaryServerInfo{FullMethod: method}

	run := func(t *testing.T, granted []string, req interface{}) (*grantSetAuthorizer, bool, error) {
		t.Helper()
		authz := &grantSetAuthorizer{token: "tok", granted: map[string]bool{}}
		for _, p := range granted {
			authz.granted[p] = true
		}
		called := false
		handler := func(context.Context, interface{}) (interface{}, error) {
			called = true
			return "ok", nil
		}
		_, err := UnaryAuthorizationInterceptor(authz)(ctxWithBearer("tok"), req, info, handler)
		return authz, called, err
	}

	t.Run("users:create only, roles named", func(t *testing.T) {
		authz, called, err := run(t, []string{"users:create"},
			&iampb.CreateUserRequest{Roles: []string{"admin"}})
		require.Error(t, err)
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
		assert.False(t, called, "handler must not run without roles:assign")
		assert.Equal(t, []string{"users:create", "roles:assign"}, authz.asked)
	})

	t.Run("users:create only, no roles", func(t *testing.T) {
		authz, called, err := run(t, []string{"users:create"}, &iampb.CreateUserRequest{})
		require.NoError(t, err)
		assert.True(t, called)
		assert.Equal(t, []string{"users:create"}, authz.asked)
	})

	t.Run("users:create and roles:assign, roles named", func(t *testing.T) {
		authz, called, err := run(t, []string{"users:create", "roles:assign"},
			&iampb.CreateUserRequest{Roles: []string{"admin"}})
		require.NoError(t, err)
		assert.True(t, called)
		assert.Equal(t, []string{"users:create", "roles:assign"}, authz.asked)
	})

	t.Run("nil request", func(t *testing.T) {
		_, called, err := run(t, []string{"users:create", "roles:assign"}, nil)
		require.Error(t, err)
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
		assert.False(t, called)
	})

	t.Run("request of another type", func(t *testing.T) {
		_, called, err := run(t, []string{"users:create", "roles:assign"}, &iampb.GetUserRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
		assert.False(t, called)
	})
}

// TestUserGRPCUpdateWithRolesRequiresRolesAssign pins that an UpdateUser
// request naming roles takes roles:assign on top of its users:update
// grant (the users grant GetUser, ListUsers and DeleteUser also take), and that one without roles takes only users:update.
func TestUserGRPCUpdateWithRolesRequiresRolesAssign(t *testing.T) {
	const method = "/quicbaas.iam.v1.IAMService/UpdateUser"
	info := &grpc.UnaryServerInfo{FullMethod: method}

	run := func(t *testing.T, granted []string, req interface{}) (*grantSetAuthorizer, bool, error) {
		t.Helper()
		authz := &grantSetAuthorizer{token: "tok", granted: map[string]bool{}}
		for _, p := range granted {
			authz.granted[p] = true
		}
		called := false
		handler := func(context.Context, interface{}) (interface{}, error) {
			called = true
			return "ok", nil
		}
		_, err := UnaryAuthorizationInterceptor(authz)(ctxWithBearer("tok"), req, info, handler)
		return authz, called, err
	}

	t.Run("users:update only, roles named", func(t *testing.T) {
		authz, called, err := run(t, []string{"users:update"},
			&iampb.UpdateUserRequest{Roles: []string{"admin"}})
		require.Error(t, err)
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
		assert.False(t, called, "handler must not run without roles:assign")
		assert.Equal(t, []string{"users:update", "roles:assign"}, authz.asked)
	})

	t.Run("users:update only, no roles", func(t *testing.T) {
		authz, called, err := run(t, []string{"users:update"}, &iampb.UpdateUserRequest{Name: "n"})
		require.NoError(t, err)
		assert.True(t, called)
		assert.Equal(t, []string{"users:update"}, authz.asked)
	})

	t.Run("users:update and roles:assign, roles named", func(t *testing.T) {
		authz, called, err := run(t, []string{"users:update", "roles:assign"},
			&iampb.UpdateUserRequest{Roles: []string{"admin"}})
		require.NoError(t, err)
		assert.True(t, called)
		assert.Equal(t, []string{"users:update", "roles:assign"}, authz.asked)
	})

	t.Run("nil request", func(t *testing.T) {
		_, called, err := run(t, []string{"users:update", "roles:assign"}, nil)
		require.Error(t, err)
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
		assert.False(t, called)
	})

	t.Run("request of another type", func(t *testing.T) {
		_, called, err := run(t, []string{"users:update", "roles:assign"}, &iampb.GetUserRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
		assert.False(t, called)
	})
}
