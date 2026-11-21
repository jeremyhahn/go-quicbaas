//go:build rest

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/iamtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// roleAssignFixture is an IAMHandler over a real in-memory identity store
// holding tenant "acme", its Admin role and one user, with a scope
// authorizer whose decision the test sets.
type roleAssignFixture struct {
	handler  *IAMHandler
	identity *iam.IdentityStore
	authz    *scopeAuthorizerStub
	user     *iam.User
}

const roleAssignAdminRole = "acme-admin"

func newRoleAssignFixture(t *testing.T, deny bool) *roleAssignFixture {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	ctx := context.Background()
	mem := iam.NewMemoryStorage()
	tenants := iam.NewDefaultTenantManager(mem)
	_, err = tenants.CreateTenant(ctx, iam.CreateTenantOptions{ID: "acme", Name: "Acme"})
	require.NoError(t, err)
	identity := iam.NewIdentityStore(iam.IdentityStoreOptions{UserStorage: mem, RoleStorage: mem, Tenants: tenants})
	_, err = identity.CreateRole(ctx, "acme", iam.CreateRoleOptions{ID: roleAssignAdminRole, Name: "Admin"})
	require.NoError(t, err)
	user, err := identity.CreateUser(ctx, "acme", iam.CreateUserOptions{Email: "mallory@acme.com", Username: "mallory"})
	require.NoError(t, err)

	mock := testutil.NewMockQuicBaaS()
	mock.InviteStoreOverride = iamtest.NewMemoryInviteStore()
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(&rollbackTestService{MockQuicBaaS: mock, identity: identity}, cfg, log)
	require.NoError(t, err)
	t.Cleanup(func() { server.Close() })

	authz := &scopeAuthorizerStub{}
	if deny {
		authz.err = &ForbiddenError{Reason: "roles:assign denied"}
	}
	server.SetBarrierScopeAuthorizer(authz)
	return &roleAssignFixture{handler: &IAMHandler{server: server}, identity: identity, authz: authz, user: user}
}

// request builds an authenticated request for the acme tenant.
func (f *roleAssignFixture) request(method, body string, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	req := httptest.NewRequest(method, "/api/v1/tenants/acme/users/", strings.NewReader(body))
	ctx := qrdbsdk.WithIdentity(req.Context(), &qrdbsdk.Identity{Subject: "mallory", TenantID: "acme"})
	req = req.WithContext(callerRouteContext(ctx, rctx))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// asRoleAssigner authenticates req as a caller the handler's authorizer
// grants roles:assign, for tests whose subject is not that check.
func asRoleAssigner(h *IAMHandler, req *http.Request) *http.Request {
	h.server.SetBarrierScopeAuthorizer(&scopeAuthorizerStub{})
	ctx := qrdbsdk.WithIdentity(req.Context(), &qrdbsdk.Identity{Subject: "admin", TenantID: "tenant1"})
	return req.WithContext(ctx)
}

func (f *roleAssignFixture) updateUser(body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.handler.UpdateUser(w, f.request(http.MethodPut, body,
		map[string]string{"tenant_id": "acme", "user_id": strconv.FormatUint(f.user.ID, 10)}))
	return w
}

func (f *roleAssignFixture) createUser(body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.handler.CreateUser(w, f.request(http.MethodPost, body, map[string]string{"tenant_id": "acme"}))
	return w
}

func (f *roleAssignFixture) storedRoles(t *testing.T) []string {
	t.Helper()
	user, err := f.identity.GetUser(context.Background(), "acme", f.user.ID)
	require.NoError(t, err)
	return user.Roles
}

func TestUpdateUser_RolesWithoutRolesAssignIsForbidden(t *testing.T) {
	f := newRoleAssignFixture(t, true)

	w := f.updateUser(`{"roles":["` + roleAssignAdminRole + `"]}`)

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Empty(t, f.storedRoles(t), "a caller without roles:assign gave itself the Admin role")
	decisions := f.authz.made()
	require.Len(t, decisions, 1)
	assert.Equal(t, scopeDecision{tenant: "acme", resource: "roles", action: "assign"}, decisions[0])
}

// TestUpdateUser_UnchangedRolesWithoutRolesAssignIsForbidden is the regression
// for Codex 8269: resending the roles the user holds now still needs
// roles:assign. An exemption decided on an earlier read would forward the
// roles to UpdateUser, so an administrator's revocation landing between that
// read and the write would be undone without roles:assign ever being held.
func TestUpdateUser_UnchangedRolesWithoutRolesAssignIsForbidden(t *testing.T) {
	f := newRoleAssignFixture(t, true)
	require.NoError(t, f.identity.AssignRole(context.Background(), "acme", f.user.ID, roleAssignAdminRole))

	w := f.updateUser(`{"roles":["` + roleAssignAdminRole + `"]}`)

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	decisions := f.authz.made()
	require.Len(t, decisions, 1)
	assert.Equal(t, scopeDecision{tenant: "acme", resource: "roles", action: "assign"}, decisions[0])
}

func TestCreateUser_RolesWithoutRolesAssignIsForbidden(t *testing.T) {
	f := newRoleAssignFixture(t, true)

	w := f.createUser(`{"email":"eve@acme.com","username":"eve","roles":["` + roleAssignAdminRole + `"]}`)

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	_, err := f.identity.GetUserByEmail(context.Background(), "acme", "eve@acme.com")
	assert.Error(t, err, "a caller without roles:assign created a user holding the Admin role")
}

func TestUserRoles_UnwiredAuthorizerIsForbidden(t *testing.T) {
	f := newRoleAssignFixture(t, false)
	f.handler.server.SetBarrierScopeAuthorizer(nil)

	w := f.updateUser(`{"roles":["` + roleAssignAdminRole + `"]}`)

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Empty(t, f.storedRoles(t))
}

func TestUserRoles_WithRolesAssignSucceeds(t *testing.T) {
	f := newRoleAssignFixture(t, false)

	w := f.updateUser(`{"roles":["` + roleAssignAdminRole + `"]}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, []string{roleAssignAdminRole}, f.storedRoles(t))

	w = f.createUser(`{"email":"eve@acme.com","username":"eve","roles":["` + roleAssignAdminRole + `"]}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	created, err := f.identity.GetUserByEmail(context.Background(), "acme", "eve@acme.com")
	require.NoError(t, err)
	assert.Equal(t, []string{roleAssignAdminRole}, created.Roles)
}

func TestUpdateUser_WithoutRolesNeedsNoRolesAssign(t *testing.T) {
	f := newRoleAssignFixture(t, true)

	w := f.updateUser(`{"name":"Mallory"}`)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Empty(t, f.authz.made(), "an update that assigns no role consulted roles:assign")
}
