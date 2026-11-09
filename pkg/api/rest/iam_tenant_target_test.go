//go:build rest

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// callerRouteContext attaches rctx to ctx and binds the tenant its tenant_id
// parameter names as the caller's own tenant, so a handler test exercises the
// handler rather than the cross-tenant refusal urlTenant applies.
func callerRouteContext(ctx context.Context, rctx *chi.Context) context.Context {
	if tenantID := rctx.URLParam("tenant_id"); tenantID != "" {
		ctx = tenantctx.WithTenant(ctx, tenantID)
	}
	return context.WithValue(ctx, chi.RouteCtxKey, rctx)
}

// tenantTargetRefusals are the callers that must not reach another tenant's
// IAM data by naming it in the URL (go-quicbaas#1150): a tenant naming a
// different tenant, the system tenant (which gets no cross-tenant lift), and a
// request with no tenant bound at all.
// The system tenant is not among them: the system admin acts in the tenant
// the URL names (go-quicbaas#2212).
var tenantTargetRefusals = []struct {
	name   string
	caller func(context.Context) context.Context
}{
	{"other tenant", func(ctx context.Context) context.Context { return tenantctx.WithTenant(ctx, "tenant-a") }},
	{"no tenant", func(ctx context.Context) context.Context { return ctx }},
}

// iamTenantScopedHandlers lists every IAM handler that reads the tenant from
// its tenant_id URL parameter.
func iamTenantScopedHandlers(h *IAMHandler) map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"ListApps": h.ListApps, "RegisterApp": h.RegisterApp, "GetApp": h.GetApp,
		"UpdateApp": h.UpdateApp, "DeleteApp": h.DeleteApp, "RotateSecret": h.RotateSecret,

		"RegisterProvider": h.RegisterProvider, "ListProviders": h.ListProviders,
		"GetProvider": h.GetProvider, "UpdateProvider": h.UpdateProvider,
		"DeleteProvider": h.DeleteProvider, "LinkAccount": h.LinkAccount,
		"UnlinkAccount": h.UnlinkAccount, "GetUserLinks": h.GetUserLinks,

		"GrantConsent": h.GrantConsent, "ListUserConsents": h.ListUserConsents,
		"GetConsent": h.GetConsent, "WithdrawConsent": h.WithdrawConsent,
		"RequestDataExport": h.RequestDataExport, "GetExportStatus": h.GetExportStatus,
		"DownloadExport": h.DownloadExport, "RequestDeletion": h.RequestDeletion,
		"GetDeletionStatus": h.GetDeletionStatus, "CancelDeletion": h.CancelDeletion,

		"BeginMFAEnrollment": h.BeginMFAEnrollment, "CompleteMFAEnrollment": h.CompleteMFAEnrollment,
		"CancelMFAEnrollment": h.CancelMFAEnrollment, "GetMFAStatus": h.GetMFAStatus,
		"DisableMFA": h.DisableMFA, "BeginMFAChallenge": h.BeginMFAChallenge,
		"VerifyMFAChallenge": h.VerifyMFAChallenge, "GenerateBackupCodes": h.GenerateBackupCodes,
		"GetBackupCodeStatus": h.GetBackupCodeStatus,

		"ListPolicies": h.ListPolicies, "CreatePolicy": h.CreatePolicy, "GetPolicy": h.GetPolicy,
		"UpdatePolicy": h.UpdatePolicy, "DeletePolicy": h.DeletePolicy,
		"ValidatePolicy": h.ValidatePolicy, "TestPolicy": h.TestPolicy,
		"SimulatePolicy": h.SimulatePolicy, "GetEffectivePermissions": h.GetEffectivePermissions,

		"ListRoles": h.ListRoles, "CreateRole": h.CreateRole, "GetRole": h.GetRole,
		"UpdateRole": h.UpdateRole, "DeleteRole": h.DeleteRole, "ReconcileRoles": h.ReconcileRoles,

		"CreateSession": h.CreateSession, "ListUserSessions": h.ListUserSessions,
		"RevokeAllUserSessions": h.RevokeAllUserSessions,

		"ListUsers": h.ListUsers, "CreateUser": h.CreateUser, "GetUser": h.GetUser,
		"UpdateUser": h.UpdateUser, "DeleteUser": h.DeleteUser,
		"AssignRole": h.AssignRole, "RevokeRole": h.RevokeRole,
	}
}

// TestIAMHandlers_RefuseANamedTenantThatIsNotTheCaller pins go-quicbaas#1150
// for every tenant-scoped IAM handler: naming tenant-b is a 403 unless the
// caller is tenant-b.
func TestIAMHandlers_RefuseANamedTenantThatIsNotTheCaller(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	for name, serve := range iamTenantScopedHandlers(handler) {
		for _, refusal := range tenantTargetRefusals {
			t.Run(name+"/"+refusal.name, func(t *testing.T) {
				rctx := chi.NewRouteContext()
				for _, param := range []string{"user_id", "app_id", "provider_id", "role_id",
					"policy_id", "consent_id", "export_id", "request_id", "method", "challenge_id", "enrollment_id"} {
					rctx.URLParams.Add(param, "x-1")
				}
				rctx.URLParams.Add("tenant_id", "tenant-b")
				req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-b/x", nil)
				ctx := context.WithValue(refusal.caller(req.Context()), chi.RouteCtxKey, rctx)
				w := httptest.NewRecorder()

				serve(w, req.WithContext(ctx))

				assert.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
			})
		}
	}
}

// TestIAMHandlers_CallerNamingItsOwnTenantSucceeds is the positive half: the
// check must not refuse the caller the tenant it is bound to.
func TestIAMHandlers_CallerNamingItsOwnTenantSucceeds(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()
	require.NoError(t, memStorage.CreateTenant(context.Background(), &iam.Tenant{ID: "tenant-b", Name: "B"}))

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant-b")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-b/roles", nil)
	req = req.WithContext(context.WithValue(tenantctx.WithTenant(req.Context(), "tenant-b"), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.ListRoles(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

func TestURLTenant_SystemCallerActsInTheNamedTenant(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()
	require.NoError(t, memStorage.CreateTenant(context.Background(), &iam.Tenant{ID: "acme", Name: "Acme"}))

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "acme")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/acme/roles", nil)
	req = req.WithContext(context.WithValue(tenantctx.WithSystem(req.Context()), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.ListRoles(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var s Server
	ctx, tenantID, ok := s.urlTenant(w, req, "rest.IAM", "tenant_id")
	require.True(t, ok)
	assert.Equal(t, "acme", tenantID)
	resolved, err := tenantctx.FromContext(ctx, "rest.IAM")
	require.NoError(t, err)
	assert.Equal(t, "acme", resolved, "urlTenant did not return the URL's tenant in its context")
	bound, err := tenantctx.FromContext(req.Context(), "rest.IAM")
	require.NoError(t, err)
	assert.Equal(t, "acme", bound, "urlTenant did not bind the URL's tenant into the request")
}

func TestURLTenant_RefusesAnUnboundRequest(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "acme")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/acme/roles", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.ListRoles(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "no tenant is bound")
}

// TestWebAuthnCredentials_RefuseANamedTenantThatIsNotTheCaller covers the
// credential routes, which resolve the tenant through their own subject helper.
func TestWebAuthnCredentials_RefuseANamedTenantThatIsNotTheCaller(t *testing.T) {
	store := &credentialStoreDouble{credentials: testCredentialIDs()}
	h := mountCredentialRoutes(t, store)

	for _, refusal := range tenantTargetRefusals {
		t.Run(refusal.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, credentialPath(""), nil)
			rec := httptest.NewRecorder()

			// credentialPath names tenant acme; tenant-a and tenantless
			// callers both differ from it.
			h.ServeHTTP(rec, req.WithContext(refusal.caller(req.Context())))

			assert.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
			assert.Len(t, store.credentials, 2, "a refused request deleted credentials")
		})
	}
}

// ownerFollowingRoleStorage stands in for QRDBRoleStorage on the role update
// path: it ignores the tenantID argument and finds a role by its globally
// unique ID, then admits the read only when the context's tenant owns the role
// or is the system tenant, as go-qrdb's OwningTenant does. It records the
// tenant of every context it receives.
type ownerFollowingRoleStorage struct {
	*iam.MemoryStorage
	owners map[string]string
	seen   []string
}

func (s *ownerFollowingRoleStorage) contextTenant(ctx context.Context) string {
	tenantID, err := tenantctx.FromContext(ctx, "test")
	if err != nil {
		return ""
	}
	s.seen = append(s.seen, tenantID)
	return tenantID
}

func (s *ownerFollowingRoleStorage) GetRole(ctx context.Context, _, roleID string) (*iam.Role, error) {
	caller := s.contextTenant(ctx)
	owner, ok := s.owners[roleID]
	if !ok || (caller != owner && !tenantctx.IsSystem(caller)) {
		return nil, iam.ErrRoleNotFound
	}
	return s.MemoryStorage.GetRole(ctx, owner, roleID)
}

func (s *ownerFollowingRoleStorage) UpdateRole(ctx context.Context, role *iam.Role) error {
	s.contextTenant(ctx)
	return s.MemoryStorage.UpdateRole(ctx, role)
}

// TestUpdateRole_SystemAdminCannotReachAnotherTenantsRole pins the handler
// passing urlTenant's resolved context to the store: a system-admin PUT to
// /tenants/acme/roles/<id> reaches storage bound to acme, so a role Globex
// owns is refused and left unchanged, while an Acme role updates
// (go-quicbaas#2212).
func TestUpdateRole_SystemAdminCannotReachAnotherTenantsRole(t *testing.T) {
	_, mockService, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()
	ctx := context.Background()
	for _, tenant := range []*iam.Tenant{{ID: "acme", Name: "Acme"}, {ID: "globex", Name: "Globex"}} {
		require.NoError(t, memStorage.CreateTenant(ctx, tenant))
	}
	require.NoError(t, memStorage.CreateRole(ctx, &iam.Role{ID: "role-globex", Name: "globex-ops", TenantID: "globex"}))
	require.NoError(t, memStorage.CreateRole(ctx, &iam.Role{ID: "role-acme", Name: "acme-ops", TenantID: "acme"}))

	roles := &ownerFollowingRoleStorage{
		MemoryStorage: memStorage,
		owners:        map[string]string{"role-globex": "globex", "role-acme": "acme"},
	}
	mockService.identityStore = iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: memStorage,
		RoleStorage: roles,
		Tenants:     mockService.tenantManager,
	})

	update := func(roleID string) *httptest.ResponseRecorder {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "acme")
		rctx.URLParams.Add("role_id", roleID)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/acme/roles/"+roleID,
			strings.NewReader(`{"name":"renamed"}`))
		req = req.WithContext(context.WithValue(tenantctx.WithSystem(req.Context()), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		handler.UpdateRole(w, req)
		return w
	}

	t.Run("another tenant's role is refused", func(t *testing.T) {
		roles.seen = nil
		w := update("role-globex")

		assert.False(t, w.Code >= 200 && w.Code < 300, "status %d, body: %s", w.Code, w.Body.String())
		require.NotEmpty(t, roles.seen, "the store was never called")
		for _, tenantID := range roles.seen {
			assert.Equal(t, "acme", tenantID, "the store received a context not bound to the URL's tenant")
		}
		role, err := memStorage.GetRole(ctx, "globex", "role-globex")
		require.NoError(t, err)
		assert.Equal(t, "globex-ops", role.Name, "Globex's role was modified")
	})

	t.Run("the named tenant's role updates", func(t *testing.T) {
		roles.seen = nil
		w := update("role-acme")

		assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
		require.NotEmpty(t, roles.seen, "the store was never called")
		for _, tenantID := range roles.seen {
			assert.Equal(t, "acme", tenantID, "the store received a context not bound to the URL's tenant")
		}
		role, err := memStorage.GetRole(ctx, "acme", "role-acme")
		require.NoError(t, err)
		assert.Equal(t, "renamed", role.Name)
	})
}
