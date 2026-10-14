//go:build rest

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
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
var tenantTargetRefusals = []struct {
	name   string
	caller func(context.Context) context.Context
}{
	{"other tenant", func(ctx context.Context) context.Context { return tenantctx.WithTenant(ctx, "tenant-a") }},
	{"system tenant", tenantctx.WithSystem},
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

// TestWebAuthnCredentials_RefuseANamedTenantThatIsNotTheCaller covers the
// credential routes, which resolve the tenant through their own subject helper.
func TestWebAuthnCredentials_RefuseANamedTenantThatIsNotTheCaller(t *testing.T) {
	store := &credentialStoreDouble{credentials: testCredentialIDs()}
	h := mountCredentialRoutes(t, store)

	for _, refusal := range tenantTargetRefusals {
		t.Run(refusal.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, credentialPath(""), nil)
			rec := httptest.NewRecorder()

			// credentialPath names tenant acme; tenant-a, system and tenantless
			// callers all differ from it.
			h.ServeHTTP(rec, req.WithContext(refusal.caller(req.Context())))

			assert.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
			assert.Len(t, store.credentials, 2, "a refused request deleted credentials")
		})
	}
}
