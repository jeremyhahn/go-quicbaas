package grpc

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// allowAllRoleAuthorizer grants every permission to the authenticated caller
// on ctx, for UpdateUser tests that name roles but test something other than
// the roles:assign check.
type allowAllRoleAuthorizer struct{}

func (allowAllRoleAuthorizer) Authorize(ctx context.Context, _, _, _, _, _ string) (*platformtoken.AuthorizedContext, error) {
	caller, _ := auth.SubjectFromContext(ctx)
	return &platformtoken.AuthorizedContext{Subject: caller.ID, TenantID: caller.TenantID}, nil
}

const (
	updateUserAuthzToken   = "update-user-token"
	updateUserAuthzSubject = "admin-ada"
)

// updateUserAuthzContext is a call by the authenticated principal
// updateUserAuthzSubject in tenantID, carrying updateUserAuthzToken.
func updateUserAuthzContext(tenantID string) context.Context {
	ctx := auth.SubjectIntoContext(callerContext(tenantID), interfaces.Subject{
		ID: updateUserAuthzSubject, TenantID: tenantID, Authenticated: true,
	})
	md := metadata.New(map[string]string{"authorization": "Bearer " + updateUserAuthzToken})
	return metadata.NewIncomingContext(ctx, md)
}

// certCallerContext is a call authenticated by certificate as subject in
// tenantID (the qrdb identity projected as the unified listener does), with
// an optional bearer token attached.
func certCallerContext(subject, tenantID, bearer string) context.Context {
	ctx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: subject, TenantID: tenantID})
	ctx = auth.ContextWithIdentitySubject(ctx)
	if bearer != "" {
		ctx = metadata.NewIncomingContext(ctx, metadata.New(map[string]string{"authorization": "Bearer " + bearer}))
	}
	return ctx
}

// seedUpdateUserAuthz creates the roles viewer and admin and a user holding
// viewer in tenant acme, returning the user's id.
func seedUpdateUserAuthz(t *testing.T, srv *IAMServer, mem *iam.MemoryStorage) (string, uint64) {
	t.Helper()
	ctx := callerContext("acme")
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{UserStorage: mem, RoleStorage: mem})
	for _, id := range []string{"viewer", "admin"} {
		_, err := store.CreateRole(ctx, "acme", iam.CreateRoleOptions{
			ID: id, Name: id, Permissions: []string{"users:read"},
		})
		require.NoError(t, err)
	}
	created, err := srv.CreateUser(ctx, &iampb.CreateUserRequest{
		TenantId:  "acme",
		Email:     "ada@acme.test",
		Username:  "ada",
		Name:      "Ada One",
		Lifecycle: string(iam.LifecycleActive),
		Roles:     []string{"viewer"},
	})
	require.NoError(t, err)
	userID, err := strconv.ParseUint(created.Id, 10, 64)
	require.NoError(t, err)
	return created.Id, userID
}

func TestGRPC_UpdateUserRolesWithoutRolesAssignIsDenied(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	id, userID := seedUpdateUserAuthz(t, srv, mem)
	authz := &grantSetAuthorizer{token: updateUserAuthzToken, subject: updateUserAuthzSubject, tenantID: "acme", granted: map[string]bool{"users:update": true}}
	srv.SetRoleAuthorizer(authz)

	_, err := srv.UpdateUser(updateUserAuthzContext("acme"), &iampb.UpdateUserRequest{
		TenantId: "acme", UserId: id, Name: "Ada Two", Roles: []string{"admin"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Equal(t, []string{"roles:assign"}, authz.asked)

	stored, err := mem.GetUser(context.Background(), "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, []string{"viewer"}, stored.Roles, "a denied update changed the user's roles")
	assert.Equal(t, "Ada One", stored.Name, "a denied update wrote the profile")
}

func TestGRPC_UpdateUserRolesWithRolesAssignSucceeds(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	id, userID := seedUpdateUserAuthz(t, srv, mem)
	authz := &grantSetAuthorizer{token: updateUserAuthzToken, subject: updateUserAuthzSubject, tenantID: "acme", granted: map[string]bool{"users:update": true, "roles:assign": true}}
	srv.SetRoleAuthorizer(authz)

	_, err := srv.UpdateUser(updateUserAuthzContext("acme"), &iampb.UpdateUserRequest{
		TenantId: "acme", UserId: id, Roles: []string{"admin"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"roles:assign"}, authz.asked)

	stored, err := mem.GetUser(context.Background(), "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, []string{"admin"}, stored.Roles)
}

// TestGRPC_UpdateUserAttributesWithoutRolesAssignIsDenied pins that an
// attributes-only update by a users:update holder is refused before any write:
// attributes are authorization data and need roles:assign.
func TestGRPC_UpdateUserAttributesWithoutRolesAssignIsDenied(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	id, userID := seedUpdateUserAuthz(t, srv, mem)
	authz := &grantSetAuthorizer{token: updateUserAuthzToken, subject: updateUserAuthzSubject, tenantID: "acme", granted: map[string]bool{"users:update": true}}
	srv.SetRoleAuthorizer(authz)

	_, err := srv.UpdateUser(updateUserAuthzContext("acme"), &iampb.UpdateUserRequest{
		TenantId: "acme", UserId: id, Name: "Ada Two", Attributes: map[string]string{"department": "finance"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Equal(t, []string{"roles:assign"}, authz.asked)

	stored, err := mem.GetUser(context.Background(), "acme", userID)
	require.NoError(t, err)
	assert.NotContains(t, stored.Attributes, "department", "a denied update wrote the attributes")
	assert.Equal(t, "Ada One", stored.Name, "a denied update wrote the profile")
}

// TestGRPC_UpdateUserAttributesWithRolesAssignSucceeds pins that a caller
// holding roles:assign can write attributes.
func TestGRPC_UpdateUserAttributesWithRolesAssignSucceeds(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	id, userID := seedUpdateUserAuthz(t, srv, mem)
	authz := &grantSetAuthorizer{token: updateUserAuthzToken, subject: updateUserAuthzSubject, tenantID: "acme", granted: map[string]bool{"users:update": true, "roles:assign": true}}
	srv.SetRoleAuthorizer(authz)

	resp, err := srv.UpdateUser(updateUserAuthzContext("acme"), &iampb.UpdateUserRequest{
		TenantId: "acme", UserId: id, Attributes: map[string]string{"department": "finance"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"roles:assign"}, authz.asked)
	assert.Equal(t, "finance", resp.GetAttributes()["department"])

	stored, err := mem.GetUser(context.Background(), "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, "finance", stored.Attributes["department"])
}

// TestGRPC_UpdateUserReturnsThePersistedRoles pins that replacing viewer with
// admin answers with admin: the response is read after the role sync, not
// before it.
func TestGRPC_UpdateUserReturnsThePersistedRoles(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	id, userID := seedUpdateUserAuthz(t, srv, mem)
	srv.SetRoleAuthorizer(&grantSetAuthorizer{token: updateUserAuthzToken, subject: updateUserAuthzSubject, tenantID: "acme", granted: map[string]bool{"users:update": true, "roles:assign": true}})

	resp, err := srv.UpdateUser(updateUserAuthzContext("acme"), &iampb.UpdateUserRequest{
		TenantId: "acme", UserId: id, Roles: []string{"admin"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"admin"}, resp.GetRoles(), "UpdateUser answered with the pre-sync roles")

	stored, err := mem.GetUser(context.Background(), "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, []string{"admin"}, stored.Roles)
}

func TestGRPC_UpdateUserRolesWithoutAuthorizerIsRefused(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	id, userID := seedUpdateUserAuthz(t, srv, mem)

	_, err := srv.UpdateUser(updateUserAuthzContext("acme"), &iampb.UpdateUserRequest{
		TenantId: "acme", UserId: id, Roles: []string{"admin"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))

	stored, err := mem.GetUser(context.Background(), "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, []string{"viewer"}, stored.Roles, "an update with no authorizer changed the user's roles")
}

func TestGRPC_UpdateUserWithoutRolesSkipsRolesAssign(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	id, userID := seedUpdateUserAuthz(t, srv, mem)
	authz := &grantSetAuthorizer{token: updateUserAuthzToken, subject: updateUserAuthzSubject, tenantID: "acme", granted: map[string]bool{"users:update": true}}
	srv.SetRoleAuthorizer(authz)

	_, err := srv.UpdateUser(updateUserAuthzContext("acme"), &iampb.UpdateUserRequest{
		TenantId: "acme", UserId: id, Name: "Ada Two",
	})
	require.NoError(t, err)
	assert.Empty(t, authz.asked, "an update naming no roles asked for a permission")

	// With no authorizer at all, an update naming no roles is unchanged too.
	srv.SetRoleAuthorizer(nil)
	_, err = srv.UpdateUser(updateUserAuthzContext("acme"), &iampb.UpdateUserRequest{
		TenantId: "acme", UserId: id, Name: "Ada Three",
	})
	require.NoError(t, err)

	stored, err := mem.GetUser(context.Background(), "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, "Ada Three", stored.Name)
	assert.Equal(t, []string{"viewer"}, stored.Roles)
}

// TestGRPC_UpdateUserRoleAssignIsBoundToTheCaller pins that roles:assign is
// decided for the caller authenticated on the call, in the request's tenant:
// a caller authenticated by certificate in acme (which satisfies users:update
// and the tenant check) cannot borrow roles:assign from a bearer token naming
// another principal, or the same principal in another tenant.
func TestGRPC_UpdateUserRoleAssignIsBoundToTheCaller(t *testing.T) {
	cases := []struct {
		name          string
		tokenSubject  string
		tokenTenantID string
	}{
		{name: "token of another principal", tokenSubject: "mallory", tokenTenantID: "acme"},
		{name: "token of the caller in another tenant", tokenSubject: updateUserAuthzSubject, tokenTenantID: "globex"},
		{name: "token of another principal in another tenant", tokenSubject: "mallory", tokenTenantID: "globex"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, mem := newRealIAMGRPCStack(t)
			id, userID := seedUpdateUserAuthz(t, srv, mem)
			authz := &grantSetAuthorizer{
				token:    updateUserAuthzToken,
				subject:  tc.tokenSubject,
				tenantID: tc.tokenTenantID,
				granted:  map[string]bool{"roles:assign": true},
			}
			srv.SetRoleAuthorizer(authz)

			ctx := certCallerContext(updateUserAuthzSubject, "acme", updateUserAuthzToken)
			for _, req := range []*iampb.UpdateUserRequest{
				{TenantId: "acme", UserId: id, Name: "Ada Two", Roles: []string{"admin"}},
				{TenantId: "acme", UserId: id, Name: "Ada Two", Attributes: map[string]string{"department": "finance"}},
			} {
				_, err := srv.UpdateUser(ctx, req)
				require.Error(t, err)
				assert.Equal(t, codes.PermissionDenied, status.Code(err))
			}

			stored, err := mem.GetUser(context.Background(), "acme", userID)
			require.NoError(t, err)
			assert.Equal(t, []string{"viewer"}, stored.Roles, "a borrowed roles:assign changed the user's roles")
			assert.NotContains(t, stored.Attributes, "department", "a borrowed roles:assign wrote the attributes")
			assert.Equal(t, "Ada One", stored.Name, "a denied update wrote the profile")
		})
	}
}

// TestGRPC_UpdateUserRoleAssignHeldByTheCertCallerSucceeds pins that the
// binding refuses only a borrowed grant: the certificate caller's own token,
// holding roles:assign in the request's tenant, still assigns roles.
func TestGRPC_UpdateUserRoleAssignHeldByTheCertCallerSucceeds(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	id, userID := seedUpdateUserAuthz(t, srv, mem)
	srv.SetRoleAuthorizer(&grantSetAuthorizer{
		token: updateUserAuthzToken, subject: updateUserAuthzSubject, tenantID: "acme",
		granted: map[string]bool{"roles:assign": true},
	})

	_, err := srv.UpdateUser(certCallerContext(updateUserAuthzSubject, "acme", updateUserAuthzToken), &iampb.UpdateUserRequest{
		TenantId: "acme", UserId: id, Roles: []string{"admin"},
	})
	require.NoError(t, err)

	stored, err := mem.GetUser(context.Background(), "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, []string{"admin"}, stored.Roles)
}

// TestGRPC_UpdateUserRoleAssignWithoutBearerIsRefused pins that a caller
// authenticated only by certificate, with no bearer token, cannot assign
// roles, even with an authorizer that would grant anything.
func TestGRPC_UpdateUserRoleAssignWithoutBearerIsRefused(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	id, userID := seedUpdateUserAuthz(t, srv, mem)
	srv.SetRoleAuthorizer(allowAllRoleAuthorizer{})

	_, err := srv.UpdateUser(certCallerContext(updateUserAuthzSubject, "acme", ""), &iampb.UpdateUserRequest{
		TenantId: "acme", UserId: id, Roles: []string{"admin"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))

	stored, err := mem.GetUser(context.Background(), "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, []string{"viewer"}, stored.Roles, "an update with no bearer token changed the user's roles")
}

// userIDRoleAuthorizer resolves updateUserAuthzToken to a principal carrying
// an immutable user ID, as the platform token validator does (uid claim).
type userIDRoleAuthorizer struct {
	subject  string
	userID   uint64
	tenantID string
}

func (a userIDRoleAuthorizer) Authorize(_ context.Context, token, requiredPerm, _, _, _ string) (*platformtoken.AuthorizedContext, error) {
	if token != updateUserAuthzToken {
		return nil, &platformtoken.InvalidTokenError{Detail: "signature did not verify"}
	}
	if requiredPerm != "roles:assign" {
		return nil, &platformtoken.ScopeDeniedError{Required: requiredPerm}
	}
	return &platformtoken.AuthorizedContext{Subject: a.subject, UserID: a.userID, TenantID: a.tenantID}, nil
}

// TestGRPC_UpdateUserRoleAssignMatchesTheCallerByUserID pins that a
// certificate caller, named by its username, is matched to its own bearer
// token by the immutable user ID, whatever subject string the token carries.
func TestGRPC_UpdateUserRoleAssignMatchesTheCallerByUserID(t *testing.T) {
	cases := []struct {
		name    string
		subject func(userID uint64) string
	}{
		{name: "token subject is the email", subject: func(uint64) string { return "ada@acme.test" }},
		{name: "token subject is the decimal user id", subject: func(userID uint64) string { return strconv.FormatUint(userID, 10) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, mem := newRealIAMGRPCStack(t)
			id, userID := seedUpdateUserAuthz(t, srv, mem)
			srv.SetRoleAuthorizer(userIDRoleAuthorizer{subject: tc.subject(userID), userID: userID, tenantID: "acme"})

			_, err := srv.UpdateUser(certCallerContext("ada", "acme", updateUserAuthzToken), &iampb.UpdateUserRequest{
				TenantId: "acme", UserId: id, Roles: []string{"admin"}, Attributes: map[string]string{"department": "finance"},
			})
			require.NoError(t, err)

			stored, err := mem.GetUser(context.Background(), "acme", userID)
			require.NoError(t, err)
			assert.Equal(t, []string{"admin"}, stored.Roles)
			assert.Equal(t, "finance", stored.Attributes["department"])
		})
	}
}

// TestGRPC_UpdateUserRoleAssignRefusesAnotherUserID pins that a token whose
// user ID is not the caller's is refused, even when its subject string equals
// the caller's name.
func TestGRPC_UpdateUserRoleAssignRefusesAnotherUserID(t *testing.T) {
	for _, subject := range []string{"ada", "mallory@acme.test"} {
		t.Run(subject, func(t *testing.T) {
			srv, mem := newRealIAMGRPCStack(t)
			id, userID := seedUpdateUserAuthz(t, srv, mem)
			srv.SetRoleAuthorizer(userIDRoleAuthorizer{subject: subject, userID: userID + 1000, tenantID: "acme"})

			ctx := certCallerContext("ada", "acme", updateUserAuthzToken)
			for _, req := range []*iampb.UpdateUserRequest{
				{TenantId: "acme", UserId: id, Name: "Ada Two", Roles: []string{"admin"}},
				{TenantId: "acme", UserId: id, Name: "Ada Two", Attributes: map[string]string{"department": "finance"}},
			} {
				_, err := srv.UpdateUser(ctx, req)
				require.Error(t, err)
				assert.Equal(t, codes.PermissionDenied, status.Code(err))
			}

			stored, err := mem.GetUser(context.Background(), "acme", userID)
			require.NoError(t, err)
			assert.Equal(t, []string{"viewer"}, stored.Roles)
			assert.NotContains(t, stored.Attributes, "department")
			assert.Equal(t, "Ada One", stored.Name)
		})
	}
}

// TestGRPC_UpdateUserRoleAssignByUserIDKeepsTheTenantCheck pins that a
// matching user ID does not lift the tenant check: the caller's own token in
// another tenant is still refused.
func TestGRPC_UpdateUserRoleAssignByUserIDKeepsTheTenantCheck(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	id, userID := seedUpdateUserAuthz(t, srv, mem)
	srv.SetRoleAuthorizer(userIDRoleAuthorizer{subject: "ada@acme.test", userID: userID, tenantID: "globex"})

	_, err := srv.UpdateUser(certCallerContext("ada", "acme", updateUserAuthzToken), &iampb.UpdateUserRequest{
		TenantId: "acme", UserId: id, Roles: []string{"admin"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))

	stored, err := mem.GetUser(context.Background(), "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, []string{"viewer"}, stored.Roles)
}
