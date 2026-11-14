//go:build rest

package rest

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

type mockSvcIdentityStore struct {
	service.QuicBaaS
	store    *iam.IdentityStore
	storeErr error
}

func (m *mockSvcIdentityStore) GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error) {
	return m.store, m.storeErr
}

type mockSvcTenantMgr struct {
	service.QuicBaaS
	mgr    iam.TenantManager
	mgrErr error
}

func (m *mockSvcTenantMgr) GetTenantManager(ctx context.Context) (iam.TenantManager, error) {
	return m.mgr, m.mgrErr
}

func setupDeepHandler(t *testing.T, svc service.QuicBaaS) (*IAMHandler, func()) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(testutil.NewMockQuicBaaS(), cfg, log)
	require.NoError(t, err)
	server.service = svc
	return &IAMHandler{server: server}, func() { server.Close() }
}

var errDeepFail = errors.New("fail")

// deepCase drives one IAM handler with its dependency absent and with its
// dependency failing.
type deepCase struct {
	name    string
	method  string
	target  string
	body    string
	header  map[string]string
	params  map[string]string
	subject bool   // true when the request carries subject 30001 of tenant t1
	tenant  string // tenant carried in the request context, when set
	handle  func(*IAMHandler, http.ResponseWriter, *http.Request)
	nilCode int  // status with the dependency absent; 0 when not exercised
	noFail  bool // true when the dependency-error case is not exercised
}

// runDeepCases serves each case through newSvc(nil), a service without the
// dependency, and through newSvc(errDeepFail), whose dependency fails. A
// failing dependency must never answer 200 OK.
func runDeepCases(t *testing.T, newSvc func(fail error) service.QuicBaaS, cases []deepCase) {
	t.Helper()
	for _, tc := range cases {
		if tc.nilCode != 0 {
			t.Run(tc.name+"/nil", func(t *testing.T) {
				assert.Equal(t, tc.nilCode, serveDeepCase(t, newSvc(nil), tc).Code)
			})
		}
		if !tc.noFail {
			t.Run(tc.name+"/error", func(t *testing.T) {
				assert.NotEqual(t, http.StatusOK, serveDeepCase(t, newSvc(errDeepFail), tc).Code)
			})
		}
	}
}

func serveDeepCase(t *testing.T, svc service.QuicBaaS, tc deepCase) *httptest.ResponseRecorder {
	t.Helper()
	h, cl := setupDeepHandler(t, svc)
	defer cl()
	var body io.Reader
	if tc.body != "" {
		body = strings.NewReader(tc.body)
	}
	r := httptest.NewRequest(tc.method, tc.target, body)
	for k, v := range tc.header {
		r.Header.Set(k, v)
	}
	if tc.params != nil {
		r = withChiParams(r, tc.params)
	}
	if tc.subject {
		r = r.WithContext(ctxWithTestSubject(r.Context(), "30001", "t1"))
	}
	if tc.tenant != "" {
		r = r.WithContext(tenantctx.WithTenant(r.Context(), tc.tenant))
	}
	w := httptest.NewRecorder()
	tc.handle(h, w, r)
	return w
}

func TestDeep_GDPRManager(t *testing.T) {
	user := map[string]string{"tenant_id": "t1", "user_id": "30001"}
	consent := map[string]string{"tenant_id": "t1", "user_id": "30001", "purpose": "marketing"}
	export := map[string]string{"tenant_id": "t1", "user_id": "30001", "export_id": "exp1"}
	deletion := map[string]string{"tenant_id": "t1", "user_id": "30001", "deletion_id": "del1"}
	newSvc := func(fail error) service.QuicBaaS {
		return &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: fail}
	}
	runDeepCases(t, newSvc, []deepCase{
		{name: "ListUserConsents", method: http.MethodGet, target: "/c", params: user,
			handle: (*IAMHandler).ListUserConsents, nilCode: http.StatusInternalServerError},
		{name: "GetConsent", method: http.MethodGet, target: "/c", params: consent,
			handle: (*IAMHandler).GetConsent, nilCode: http.StatusInternalServerError},
		{name: "WithdrawConsent", method: http.MethodDelete, target: "/c", params: consent,
			handle: (*IAMHandler).WithdrawConsent, nilCode: http.StatusInternalServerError},
		{name: "RequestDataExport", method: http.MethodPost, target: "/e", body: `{"format":"json"}`, params: user,
			handle: (*IAMHandler).RequestDataExport, nilCode: http.StatusInternalServerError},
		{name: "GetExportStatus", method: http.MethodGet, target: "/e/1", params: export,
			handle: (*IAMHandler).GetExportStatus, nilCode: http.StatusInternalServerError},
		{name: "DownloadExport", method: http.MethodGet, target: "/e/1/d", params: export,
			handle: (*IAMHandler).DownloadExport, nilCode: http.StatusInternalServerError},
		{name: "RequestDeletion", method: http.MethodPost, target: "/d", body: `{"reason":"test"}`, params: user,
			handle: (*IAMHandler).RequestDeletion, nilCode: http.StatusInternalServerError},
		{name: "GetDeletionStatus", method: http.MethodGet, target: "/d/1", params: deletion,
			handle: (*IAMHandler).GetDeletionStatus, nilCode: http.StatusInternalServerError},
		{name: "CancelDeletion", method: http.MethodPost, target: "/d/1/c", params: deletion,
			handle: (*IAMHandler).CancelDeletion, nilCode: http.StatusInternalServerError},
	})
}

func TestDeep_AdminManager(t *testing.T) {
	assignment := map[string]string{"assignment_id": "a1"}
	user := map[string]string{"user_id": "30001"}
	newSvc := func(fail error) service.QuicBaaS {
		return &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: fail}
	}
	runDeepCases(t, newSvc, []deepCase{
		{name: "CreateAdminAssignment", method: http.MethodPost, target: "/a", body: `{"user_id":"50001","admin_role":"super_admin"}`,
			handle: (*IAMHandler).CreateAdminAssignment, nilCode: http.StatusInternalServerError},
		{name: "ListAdminAssignments", method: http.MethodGet, target: "/a",
			handle: (*IAMHandler).ListAdminAssignments, nilCode: http.StatusInternalServerError},
		{name: "GetAdminAssignment", method: http.MethodGet, target: "/a/1", params: assignment,
			handle: (*IAMHandler).GetAdminAssignment, nilCode: http.StatusInternalServerError},
		{name: "UpdateAdminAssignment", method: http.MethodPut, target: "/a/1", body: `{"admin_role":"super_admin"}`, params: assignment,
			handle: (*IAMHandler).UpdateAdminAssignment, nilCode: http.StatusInternalServerError},
		{name: "DeleteAdminAssignment", method: http.MethodDelete, target: "/a/1", params: assignment,
			handle: (*IAMHandler).DeleteAdminAssignment, nilCode: http.StatusInternalServerError},
		{name: "GetUserAssignments", method: http.MethodGet, target: "/a/u/1", params: user,
			handle: (*IAMHandler).GetUserAssignments, nilCode: http.StatusInternalServerError},
		{name: "CheckAdminPermission", method: http.MethodPost, target: "/c", body: `{"user_id":"50001","permission":"manage_users"}`,
			handle: (*IAMHandler).CheckAdminPermission, nilCode: http.StatusInternalServerError},
		{name: "StartImpersonation", method: http.MethodPost, target: "/i", body: `{"target_user_id":"50001","tenant_id":"t1","reason":"support"}`,
			handle: (*IAMHandler).StartImpersonation, nilCode: http.StatusInternalServerError},
		{name: "EndImpersonation", method: http.MethodDelete, target: "/i/1", params: map[string]string{"session_id": "s1"},
			handle: (*IAMHandler).EndImpersonation, nilCode: http.StatusInternalServerError},
		{name: "ListActiveImpersonations", method: http.MethodGet, target: "/i/active", params: user,
			handle: (*IAMHandler).ListActiveImpersonations, nilCode: http.StatusInternalServerError},
		{name: "ListImpersonations", method: http.MethodGet, target: "/i", params: user,
			handle: (*IAMHandler).ListImpersonations, nilCode: http.StatusInternalServerError},
	})
}

func TestDeep_AppManager(t *testing.T) {
	tenant := map[string]string{"tenant_id": "t1"}
	app := map[string]string{"tenant_id": "t1", "client_id": "c1"}
	client := map[string]string{"client_id": "c1"}
	regToken := map[string]string{"Authorization": "Bearer reg-token"}
	newSvc := func(fail error) service.QuicBaaS {
		return &mockServiceWithApps{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: fail}
	}
	runDeepCases(t, newSvc, []deepCase{
		{name: "GetApp", method: http.MethodGet, target: "/a", params: app,
			handle: (*IAMHandler).GetApp, nilCode: http.StatusInternalServerError},
		{name: "UpdateApp", method: http.MethodPut, target: "/a", body: `{"name":"updated"}`, params: app,
			handle: (*IAMHandler).UpdateApp, nilCode: http.StatusInternalServerError},
		{name: "DeleteApp", method: http.MethodDelete, target: "/a", params: app,
			handle: (*IAMHandler).DeleteApp, nilCode: http.StatusInternalServerError},
		{name: "RotateSecret", method: http.MethodPost, target: "/a/rotate", params: app,
			handle: (*IAMHandler).RotateSecret, nilCode: http.StatusInternalServerError},
		{name: "ValidateClient", method: http.MethodPost, target: "/a/validate", body: `{"client_id":"c1","client_secret":"s1"}`,
			handle: (*IAMHandler).ValidateClient, nilCode: http.StatusInternalServerError},
		{name: "ListApps", method: http.MethodGet, target: "/a", params: tenant,
			handle: (*IAMHandler).ListApps},
		{name: "RegisterApp", method: http.MethodPost, target: "/a", body: `{"name":"app1","redirect_uris":["https://x.com"]}`, params: tenant,
			handle: (*IAMHandler).RegisterApp},
		{name: "RegisterDynamicClient", method: http.MethodPost, target: "/dcr", body: `{"grant_types":["client_credentials"],"client_name":"test"}`,
			handle: (*IAMHandler).RegisterDynamicClient},
		{name: "GetDynamicClient", method: http.MethodGet, target: "/dcr/c1", params: client, header: regToken,
			handle: (*IAMHandler).GetDynamicClient},
		{name: "UpdateDynamicClient", method: http.MethodPut, target: "/dcr/c1", body: `{"grant_types":["client_credentials"],"client_name":"updated"}`, params: client, header: regToken,
			handle: (*IAMHandler).UpdateDynamicClient},
		{name: "DeleteDynamicClient", method: http.MethodDelete, target: "/dcr/c1", params: client, header: regToken,
			handle: (*IAMHandler).DeleteDynamicClient},
	})
}

func TestDeep_PolicyEngine(t *testing.T) {
	tenant := map[string]string{"tenant_id": "t1"}
	policy := map[string]string{"tenant_id": "t1", "policy_name": "p1"}
	const rules = `"spec":{"rules":[{"name":"r1","effect":"allow","actions":["read"],"resources":["*"]}]}`
	newSvc := func(fail error) service.QuicBaaS {
		return &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS(), engineErr: fail}
	}
	runDeepCases(t, newSvc, []deepCase{
		{name: "CreatePolicy", method: http.MethodPost, target: "/p", body: `{"apiVersion":"v1","kind":"Policy","metadata":{"name":"p1"},` + rules + `}`, params: tenant,
			handle: (*IAMHandler).CreatePolicy, nilCode: http.StatusInternalServerError},
		{name: "GetPolicy", method: http.MethodGet, target: "/p/1", params: policy,
			handle: (*IAMHandler).GetPolicy, nilCode: http.StatusInternalServerError},
		{name: "UpdatePolicy", method: http.MethodPut, target: "/p/1", body: `{"apiVersion":"v1","kind":"Policy",` + rules + `}`, params: policy,
			handle: (*IAMHandler).UpdatePolicy, nilCode: http.StatusInternalServerError},
		{name: "DeletePolicy", method: http.MethodDelete, target: "/p/1", params: policy,
			handle: (*IAMHandler).DeletePolicy, nilCode: http.StatusInternalServerError},
		{name: "ValidatePolicy", method: http.MethodPost, target: "/p/validate", body: `{"name":"p","effect":"allow","actions":["read"],"resources":["*"]}`, params: tenant,
			handle: (*IAMHandler).ValidatePolicy, nilCode: http.StatusInternalServerError},
		{name: "TestPolicy", method: http.MethodPost, target: "/p/test", body: `{"policy":{"name":"p","effect":"allow","actions":["read"],"resources":["*"]},"request":{"action":"read","resource":"doc"}}`, params: tenant,
			handle: (*IAMHandler).TestPolicy, nilCode: http.StatusInternalServerError},
		{name: "CheckAccess", method: http.MethodPost, target: "/p/check", body: `{"subject":{"id":"u1"},"action":"read","resource":"doc"}`, params: tenant,
			handle: (*IAMHandler).CheckAccess, nilCode: http.StatusInternalServerError},
		{name: "BatchCheckAccess", method: http.MethodPost, target: "/p/batch", body: `{"requests":[{"subject":"u1","action":"read","resource":"doc"}]}`, params: tenant,
			handle: (*IAMHandler).BatchCheckAccess, nilCode: http.StatusInternalServerError},
		{name: "SimulatePolicy", method: http.MethodPost, target: "/p/simulate", body: `{"policy":{"apiVersion":"v1","kind":"Policy","metadata":{"name":"p1"},` + rules + `},"check_request":{"subject":{"id":"u1"},"action":"read","resource":"doc"}}`, params: tenant,
			handle: (*IAMHandler).SimulatePolicy, nilCode: http.StatusInternalServerError},
		{name: "GetEffectivePermissions", method: http.MethodGet, target: "/p/eff", body: `{"subject":{"id":"u1"},"resource_patterns":["*"]}`, params: tenant,
			handle: (*IAMHandler).GetEffectivePermissions, nilCode: http.StatusInternalServerError},
		{name: "ListPolicies", method: http.MethodGet, target: "/p", params: tenant,
			handle: (*IAMHandler).ListPolicies, nilCode: http.StatusInternalServerError, noFail: true},
	})
}

func TestDeep_SessionManager(t *testing.T) {
	session := map[string]string{"id": "s1"}
	newSvc := func(fail error) service.QuicBaaS {
		return &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: fail}
	}
	runDeepCases(t, newSvc, []deepCase{
		{name: "ListSessions", method: http.MethodGet, target: "/s", subject: true,
			handle: (*IAMHandler).ListSessions, nilCode: http.StatusInternalServerError},
		{name: "GetSession", method: http.MethodGet, target: "/s/1", params: session, subject: true,
			handle: (*IAMHandler).GetSession, nilCode: http.StatusInternalServerError},
		{name: "RevokeSession", method: http.MethodDelete, target: "/s/1", params: session, subject: true,
			handle: (*IAMHandler).RevokeSession, nilCode: http.StatusInternalServerError},
		{name: "RevokeAllSessions", method: http.MethodDelete, target: "/s", subject: true,
			handle: (*IAMHandler).RevokeAllSessions, nilCode: http.StatusInternalServerError},
		{name: "RevokeOtherSessions", method: http.MethodDelete, target: "/s/others", body: `{"keep_session_id":"current-session"}`, subject: true,
			handle: (*IAMHandler).RevokeOtherSessions, nilCode: http.StatusInternalServerError},
		{name: "ListUserSessions", method: http.MethodGet, target: "/s", params: map[string]string{"tenant_id": "t1", "user_id": "30001"},
			handle: (*IAMHandler).ListUserSessions},
	})
}

func TestDeep_OIDCProvider(t *testing.T) {
	form := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	newSvc := func(fail error) service.QuicBaaS {
		return &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS(), providerErr: fail}
	}
	runDeepCases(t, newSvc, []deepCase{
		{name: "GetJWKS", method: http.MethodGet, target: "/jwks",
			handle: (*IAMHandler).GetJWKS, nilCode: http.StatusInternalServerError},
		{name: "GetOIDCConfig", method: http.MethodGet, target: "/oidc",
			handle: (*IAMHandler).GetOIDCConfig, nilCode: http.StatusInternalServerError, noFail: true},
		{name: "Authorize", method: http.MethodGet, target: "/authorize?response_type=code&client_id=c1&redirect_uri=https://x.com&scope=openid",
			handle: (*IAMHandler).Authorize, nilCode: http.StatusInternalServerError},
		{name: "Token", method: http.MethodPost, target: "/token", body: "grant_type=authorization_code&code=abc&redirect_uri=https://x.com", header: form,
			handle: (*IAMHandler).Token, nilCode: http.StatusInternalServerError},
		{name: "UserInfo", method: http.MethodGet, target: "/userinfo", header: map[string]string{"Authorization": "Bearer tok"},
			handle: (*IAMHandler).UserInfo, nilCode: http.StatusInternalServerError},
		{name: "Revoke", method: http.MethodPost, target: "/revoke", body: "token=tok", header: form,
			handle: (*IAMHandler).Revoke, nilCode: http.StatusInternalServerError},
	})
}

func TestDeep_IdentityStore(t *testing.T) {
	tenant := map[string]string{"tenant_id": "t1"}
	role := map[string]string{"tenant_id": "t1", "role_id": "r1"}
	user := map[string]string{"tenant_id": "t1", "user_id": "30001"}
	newSvc := func(fail error) service.QuicBaaS {
		return &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: fail}
	}
	runDeepCases(t, newSvc, []deepCase{
		{name: "ListRoles", method: http.MethodGet, target: "/r", params: tenant,
			handle: (*IAMHandler).ListRoles, nilCode: http.StatusInternalServerError},
		{name: "CreateRole", method: http.MethodPost, target: "/r", body: `{"name":"role1"}`, params: tenant,
			handle: (*IAMHandler).CreateRole, nilCode: http.StatusInternalServerError},
		{name: "GetRole", method: http.MethodGet, target: "/r/1", params: role,
			handle: (*IAMHandler).GetRole, nilCode: http.StatusInternalServerError},
		{name: "UpdateRole", method: http.MethodPut, target: "/r/1", body: `{"name":"updated"}`, params: role,
			handle: (*IAMHandler).UpdateRole, nilCode: http.StatusInternalServerError},
		{name: "DeleteRole", method: http.MethodDelete, target: "/r/1", params: role,
			handle: (*IAMHandler).DeleteRole, nilCode: http.StatusInternalServerError},
		{name: "ListUsers", method: http.MethodGet, target: "/u", params: tenant,
			handle: (*IAMHandler).ListUsers, nilCode: http.StatusInternalServerError},
		{name: "CreateUser", method: http.MethodPost, target: "/u", body: `{"email":"a@b.com","username":"user1","name":"User"}`, params: tenant,
			handle: (*IAMHandler).CreateUser, nilCode: http.StatusInternalServerError},
		{name: "GetUser", method: http.MethodGet, target: "/u/1", params: user,
			handle: (*IAMHandler).GetUser, nilCode: http.StatusInternalServerError},
		{name: "UpdateUser", method: http.MethodPut, target: "/u/1", body: `{"name":"Updated"}`, params: user,
			handle: (*IAMHandler).UpdateUser, nilCode: http.StatusInternalServerError},
		{name: "DeleteUser", method: http.MethodDelete, target: "/u/1", params: user,
			handle: (*IAMHandler).DeleteUser, nilCode: http.StatusInternalServerError},
		{name: "AssignRole", method: http.MethodPost, target: "/u/1/roles", body: `{"role_id":"r1"}`, params: user,
			handle: (*IAMHandler).AssignRole, nilCode: http.StatusInternalServerError},
		{name: "RevokeRole", method: http.MethodDelete, target: "/u/1/roles/r1", params: map[string]string{"tenant_id": "t1", "user_id": "30001", "role": "r1"},
			handle: (*IAMHandler).RevokeRole, nilCode: http.StatusInternalServerError},
	})
}

func TestDeep_TenantManager(t *testing.T) {
	tenant := map[string]string{"tenant_id": "t1"}
	newSvc := func(fail error) service.QuicBaaS {
		return &mockSvcTenantMgr{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: fail}
	}
	runDeepCases(t, newSvc, []deepCase{
		{name: "ListTenants", method: http.MethodGet, target: "/t",
			handle: (*IAMHandler).ListTenants, nilCode: http.StatusInternalServerError},
		{name: "CreateTenant", method: http.MethodPost, target: "/t", body: `{"id":"t1","name":"tenant1"}`,
			handle: (*IAMHandler).CreateTenant, nilCode: http.StatusInternalServerError},
		{name: "GetTenant", method: http.MethodGet, target: "/t/1", params: tenant,
			handle: (*IAMHandler).GetTenant, nilCode: http.StatusInternalServerError},
		{name: "UpdateTenant", method: http.MethodPut, target: "/t/1", body: `{"name":"updated"}`, params: tenant,
			handle: (*IAMHandler).UpdateTenant, nilCode: http.StatusInternalServerError},
		{name: "DeleteTenant", method: http.MethodDelete, target: "/t/1", params: tenant,
			handle: (*IAMHandler).DeleteTenant, nilCode: http.StatusInternalServerError},
	})
}

func TestDeep_MFAManager(t *testing.T) {
	user := map[string]string{"tenant_id": "t1", "user_id": "30001"}
	newSvc := func(fail error) service.QuicBaaS {
		return &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: fail}
	}
	runDeepCases(t, newSvc, []deepCase{
		{name: "BeginMFAEnrollment", method: http.MethodPost, target: "/mfa/enroll", body: `{"user_id":"u1","method":"totp"}`, params: user,
			handle: (*IAMHandler).BeginMFAEnrollment, nilCode: http.StatusInternalServerError},
		{name: "CompleteMFAEnrollment", method: http.MethodPost, target: "/mfa/enroll/complete", body: `{"enrollment_id":"e1","code":"123456"}`, params: user,
			handle: (*IAMHandler).CompleteMFAEnrollment, nilCode: http.StatusInternalServerError},
		{name: "CancelMFAEnrollment", method: http.MethodDelete, target: "/mfa/enroll", body: `{"user_id":"u1"}`, params: map[string]string{"tenant_id": "t1", "user_id": "30001", "enrollment_id": "e1"},
			handle: (*IAMHandler).CancelMFAEnrollment, nilCode: http.StatusInternalServerError},
		{name: "GetMFAStatus", method: http.MethodGet, target: "/mfa/status", params: user,
			handle: (*IAMHandler).GetMFAStatus, nilCode: http.StatusInternalServerError},
		{name: "DisableMFA", method: http.MethodDelete, target: "/mfa", body: `{"user_id":"u1"}`, params: user,
			handle: (*IAMHandler).DisableMFA, nilCode: http.StatusInternalServerError},
		{name: "BeginMFAChallenge", method: http.MethodPost, target: "/mfa/challenge", body: `{"session_token":"tok1"}`, params: user,
			handle: (*IAMHandler).BeginMFAChallenge, nilCode: http.StatusInternalServerError},
		{name: "VerifyMFAChallenge", method: http.MethodPost, target: "/mfa/verify", body: `{"challenge_id":"ch1","code":"123456"}`, params: user,
			handle: (*IAMHandler).VerifyMFAChallenge, nilCode: http.StatusInternalServerError},
		{name: "GenerateBackupCodes", method: http.MethodPost, target: "/mfa/backup", body: `{"user_id":"u1"}`, params: user,
			handle: (*IAMHandler).GenerateBackupCodes, nilCode: http.StatusInternalServerError},
		{name: "GetBackupCodeStatus", method: http.MethodGet, target: "/mfa/backup/status", params: user,
			handle: (*IAMHandler).GetBackupCodeStatus, nilCode: http.StatusInternalServerError},
	})
}

func TestDeep_AuditLogger(t *testing.T) {
	newSvc := func(fail error) service.QuicBaaS {
		return &mockServiceWithAudit{QuicBaaS: testutil.NewMockQuicBaaS(), loggerErr: fail}
	}
	runDeepCases(t, newSvc, []deepCase{
		{name: "GetAuditEvent", method: http.MethodGet, target: "/audit/ev1?tenant_id=t1", params: map[string]string{"id": "ev1"}, tenant: "t1",
			handle: (*IAMHandler).GetAuditEvent, nilCode: http.StatusInternalServerError},
		// Without an admin subject the handler answers 401 before it reaches
		// the logger, so the absent-logger case would repeat this row.
		{name: "DeleteOldAuditEvents", method: http.MethodDelete, target: "/audit?tenant_id=t1&before=2024-01-01T00:00:00Z", tenant: "t1",
			handle: (*IAMHandler).DeleteOldAuditEvents},
	})
}

func TestDeep_DeleteOldAuditEvents_LoggerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAudit{QuicBaaS: testutil.NewMockQuicBaaS(), loggerErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/audit?tenant_id=t1&before=2024-01-01T00:00:00Z", nil)
	r = r.WithContext(tenantctx.WithTenant(r.Context(), "t1"))
	w := httptest.NewRecorder()
	h.DeleteOldAuditEvents(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}
