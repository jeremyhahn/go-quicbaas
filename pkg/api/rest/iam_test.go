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
	params  map[string]string
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
	if tc.params != nil {
		r = withChiParams(r, tc.params)
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

func TestDeep_GetApp_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithApps{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/a", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "client_id": "c1"})
	w := httptest.NewRecorder()
	h.GetApp(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetApp_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithApps{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/a", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "client_id": "c1"})
	w := httptest.NewRecorder()
	h.GetApp(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_UpdateApp_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithApps{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPut, "/a", strings.NewReader(`{"name":"updated"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "client_id": "c1"})
	w := httptest.NewRecorder()
	h.UpdateApp(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_UpdateApp_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithApps{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPut, "/a", strings.NewReader(`{"name":"updated"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "client_id": "c1"})
	w := httptest.NewRecorder()
	h.UpdateApp(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_DeleteApp_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithApps{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/a", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "client_id": "c1"})
	w := httptest.NewRecorder()
	h.DeleteApp(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_DeleteApp_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithApps{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/a", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "client_id": "c1"})
	w := httptest.NewRecorder()
	h.DeleteApp(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_RotateSecret_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithApps{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/a/rotate", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "client_id": "c1"})
	w := httptest.NewRecorder()
	h.RotateSecret(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_RotateSecret_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithApps{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/a/rotate", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "client_id": "c1"})
	w := httptest.NewRecorder()
	h.RotateSecret(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_ValidateClient_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithApps{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/a/validate", strings.NewReader(`{"client_id":"c1","client_secret":"s1"}`))
	w := httptest.NewRecorder()
	h.ValidateClient(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_ValidateClient_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithApps{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/a/validate", strings.NewReader(`{"client_id":"c1","client_secret":"s1"}`))
	w := httptest.NewRecorder()
	h.ValidateClient(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_ListApps_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithApps{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/a", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.ListApps(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_RegisterApp_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithApps{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/a", strings.NewReader(`{"name":"app1","redirect_uris":["https://x.com"]}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.RegisterApp(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_CreatePolicy_NilEngine(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/p", strings.NewReader(`{"apiVersion":"v1","kind":"Policy","metadata":{"name":"p1"},"spec":{"rules":[{"name":"r1","effect":"allow","actions":["read"],"resources":["*"]}]}}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.CreatePolicy(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_CreatePolicy_EngineError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS(), engineErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/p", strings.NewReader(`{"apiVersion":"v1","kind":"Policy","metadata":{"name":"p1"},"spec":{"rules":[{"name":"r1","effect":"allow","actions":["read"],"resources":["*"]}]}}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.CreatePolicy(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetPolicy_NilEngine(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/p/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "policy_name": "p1"})
	w := httptest.NewRecorder()
	h.GetPolicy(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetPolicy_EngineError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS(), engineErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/p/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "policy_name": "p1"})
	w := httptest.NewRecorder()
	h.GetPolicy(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_UpdatePolicy_NilEngine(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPut, "/p/1", strings.NewReader(`{"apiVersion":"v1","kind":"Policy","spec":{"rules":[{"name":"r1","effect":"allow","actions":["read"],"resources":["*"]}]}}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "policy_name": "p1"})
	w := httptest.NewRecorder()
	h.UpdatePolicy(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_UpdatePolicy_EngineError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS(), engineErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPut, "/p/1", strings.NewReader(`{"apiVersion":"v1","kind":"Policy","spec":{"rules":[{"name":"r1","effect":"allow","actions":["read"],"resources":["*"]}]}}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "policy_name": "p1"})
	w := httptest.NewRecorder()
	h.UpdatePolicy(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_DeletePolicy_NilEngine(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/p/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "policy_name": "p1"})
	w := httptest.NewRecorder()
	h.DeletePolicy(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_DeletePolicy_EngineError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS(), engineErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/p/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "policy_name": "p1"})
	w := httptest.NewRecorder()
	h.DeletePolicy(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_ValidatePolicy_NilEngine(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/p/validate", strings.NewReader(`{"name":"p","effect":"allow","actions":["read"],"resources":["*"]}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.ValidatePolicy(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_ValidatePolicy_EngineError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS(), engineErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/p/validate", strings.NewReader(`{"name":"p","effect":"allow","actions":["read"],"resources":["*"]}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.ValidatePolicy(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_TestPolicy_NilEngine(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/p/test", strings.NewReader(`{"policy":{"name":"p","effect":"allow","actions":["read"],"resources":["*"]},"request":{"action":"read","resource":"doc"}}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.TestPolicy(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_TestPolicy_EngineError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS(), engineErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/p/test", strings.NewReader(`{"policy":{"name":"p","effect":"allow","actions":["read"],"resources":["*"]},"request":{"action":"read","resource":"doc"}}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.TestPolicy(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_CheckAccess_NilEngine(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/p/check", strings.NewReader(`{"subject":{"id":"u1"},"action":"read","resource":"doc"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.CheckAccess(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_CheckAccess_EngineError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS(), engineErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/p/check", strings.NewReader(`{"subject":{"id":"u1"},"action":"read","resource":"doc"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.CheckAccess(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_BatchCheckAccess_NilEngine(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/p/batch", strings.NewReader(`{"requests":[{"subject":"u1","action":"read","resource":"doc"}]}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.BatchCheckAccess(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_BatchCheckAccess_EngineError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS(), engineErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/p/batch", strings.NewReader(`{"requests":[{"subject":"u1","action":"read","resource":"doc"}]}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.BatchCheckAccess(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_SimulatePolicy_NilEngine(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/p/simulate", strings.NewReader(`{"policy":{"apiVersion":"v1","kind":"Policy","metadata":{"name":"p1"},"spec":{"rules":[{"name":"r1","effect":"allow","actions":["read"],"resources":["*"]}]}},"check_request":{"subject":{"id":"u1"},"action":"read","resource":"doc"}}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.SimulatePolicy(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_SimulatePolicy_EngineError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS(), engineErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/p/simulate", strings.NewReader(`{"policy":{"apiVersion":"v1","kind":"Policy","metadata":{"name":"p1"},"spec":{"rules":[{"name":"r1","effect":"allow","actions":["read"],"resources":["*"]}]}},"check_request":{"subject":{"id":"u1"},"action":"read","resource":"doc"}}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.SimulatePolicy(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetEffectivePermissions_NilEngine(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/p/eff", strings.NewReader(`{"subject":{"id":"u1"},"resource_patterns":["*"]}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.GetEffectivePermissions(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetEffectivePermissions_EngineError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS(), engineErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/p/eff", strings.NewReader(`{"subject":{"id":"u1"},"resource_patterns":["*"]}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.GetEffectivePermissions(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_ListPolicies_NilEngine(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/p", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.ListPolicies(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_ListSessions_NilManager(t *testing.T) {
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS()}
	h, cl := setupDeepHandler(t, svc)
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/s", nil)
	ctx := ctxWithTestSubject(r.Context(), "30001", "t1")
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.ListSessions(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_ListSessions_ManagerError(t *testing.T) {
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail}
	h, cl := setupDeepHandler(t, svc)
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/s", nil)
	ctx := ctxWithTestSubject(r.Context(), "30001", "t1")
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.ListSessions(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetSession_NilManager(t *testing.T) {
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS()}
	h, cl := setupDeepHandler(t, svc)
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/s/1", nil)
	r = withChiParams(r, map[string]string{"id": "s1"})
	ctx := ctxWithTestSubject(r.Context(), "30001", "t1")
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.GetSession(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetSession_ManagerError(t *testing.T) {
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail}
	h, cl := setupDeepHandler(t, svc)
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/s/1", nil)
	r = withChiParams(r, map[string]string{"id": "s1"})
	ctx := ctxWithTestSubject(r.Context(), "30001", "t1")
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.GetSession(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_RevokeSession_NilManager(t *testing.T) {
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS()}
	h, cl := setupDeepHandler(t, svc)
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/s/1", nil)
	r = withChiParams(r, map[string]string{"id": "s1"})
	ctx := ctxWithTestSubject(r.Context(), "30001", "t1")
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.RevokeSession(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_RevokeSession_ManagerError(t *testing.T) {
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail}
	h, cl := setupDeepHandler(t, svc)
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/s/1", nil)
	r = withChiParams(r, map[string]string{"id": "s1"})
	ctx := ctxWithTestSubject(r.Context(), "30001", "t1")
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.RevokeSession(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_RevokeAllSessions_NilManager(t *testing.T) {
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS()}
	h, cl := setupDeepHandler(t, svc)
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/s", nil)
	ctx := ctxWithTestSubject(r.Context(), "30001", "t1")
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.RevokeAllSessions(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_RevokeAllSessions_ManagerError(t *testing.T) {
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail}
	h, cl := setupDeepHandler(t, svc)
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/s", nil)
	ctx := ctxWithTestSubject(r.Context(), "30001", "t1")
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.RevokeAllSessions(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_RevokeOtherSessions_NilManager(t *testing.T) {
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS()}
	h, cl := setupDeepHandler(t, svc)
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/s/others",
		strings.NewReader(`{"keep_session_id":"current-session"}`))
	ctx := ctxWithTestSubject(r.Context(), "30001", "t1")
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.RevokeOtherSessions(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_RevokeOtherSessions_ManagerError(t *testing.T) {
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail}
	h, cl := setupDeepHandler(t, svc)
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/s/others",
		strings.NewReader(`{"keep_session_id":"current-session"}`))
	ctx := ctxWithTestSubject(r.Context(), "30001", "t1")
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.RevokeOtherSessions(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_ListUserSessions_ManagerError(t *testing.T) {
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail}
	h, cl := setupDeepHandler(t, svc)
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/s", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.ListUserSessions(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetJWKS_NilProvider(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/jwks", nil)
	w := httptest.NewRecorder()
	h.GetJWKS(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetJWKS_ProviderError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS(), providerErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/jwks", nil)
	w := httptest.NewRecorder()
	h.GetJWKS(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetOIDCConfig_NilProvider(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/oidc", nil)
	w := httptest.NewRecorder()
	h.GetOIDCConfig(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_Authorize_NilProvider(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/authorize?response_type=code&client_id=c1&redirect_uri=https://x.com&scope=openid", nil)
	w := httptest.NewRecorder()
	h.Authorize(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_Authorize_ProviderError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS(), providerErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/authorize?response_type=code&client_id=c1&redirect_uri=https://x.com&scope=openid", nil)
	w := httptest.NewRecorder()
	h.Authorize(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_Token_NilProvider(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader("grant_type=authorization_code&code=abc&redirect_uri=https://x.com"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.Token(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_Token_ProviderError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS(), providerErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader("grant_type=authorization_code&code=abc&redirect_uri=https://x.com"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.Token(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_UserInfo_NilProvider(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/userinfo", nil)
	r.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	h.UserInfo(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_UserInfo_ProviderError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS(), providerErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/userinfo", nil)
	r.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	h.UserInfo(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_Revoke_NilProvider(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/revoke", strings.NewReader("token=tok"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.Revoke(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_Revoke_ProviderError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS(), providerErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/revoke", strings.NewReader("token=tok"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.Revoke(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_ListRoles_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/r", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.ListRoles(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_ListRoles_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/r", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.ListRoles(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_CreateRole_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/r", strings.NewReader(`{"name":"role1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.CreateRole(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_CreateRole_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/r", strings.NewReader(`{"name":"role1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.CreateRole(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetRole_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/r/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "role_id": "r1"})
	w := httptest.NewRecorder()
	h.GetRole(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetRole_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/r/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "role_id": "r1"})
	w := httptest.NewRecorder()
	h.GetRole(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_UpdateRole_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPut, "/r/1", strings.NewReader(`{"name":"updated"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "role_id": "r1"})
	w := httptest.NewRecorder()
	h.UpdateRole(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_UpdateRole_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPut, "/r/1", strings.NewReader(`{"name":"updated"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "role_id": "r1"})
	w := httptest.NewRecorder()
	h.UpdateRole(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_DeleteRole_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/r/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "role_id": "r1"})
	w := httptest.NewRecorder()
	h.DeleteRole(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_DeleteRole_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/r/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "role_id": "r1"})
	w := httptest.NewRecorder()
	h.DeleteRole(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_ListUsers_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/u", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.ListUsers(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_ListUsers_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/u", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.ListUsers(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_CreateUser_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/u", strings.NewReader(`{"email":"a@b.com","username":"user1","name":"User"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.CreateUser(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_CreateUser_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/u", strings.NewReader(`{"email":"a@b.com","username":"user1","name":"User"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.CreateUser(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetUser_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/u/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.GetUser(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetUser_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/u/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.GetUser(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_UpdateUser_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPut, "/u/1", strings.NewReader(`{"name":"Updated"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.UpdateUser(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_UpdateUser_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPut, "/u/1", strings.NewReader(`{"name":"Updated"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.UpdateUser(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_DeleteUser_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/u/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.DeleteUser(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_DeleteUser_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/u/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.DeleteUser(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_AssignRole_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/u/1/roles", strings.NewReader(`{"role_id":"r1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.AssignRole(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_AssignRole_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/u/1/roles", strings.NewReader(`{"role_id":"r1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.AssignRole(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_RevokeRole_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/u/1/roles/r1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001", "role": "r1"})
	w := httptest.NewRecorder()
	h.RevokeRole(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_RevokeRole_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/u/1/roles/r1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001", "role": "r1"})
	w := httptest.NewRecorder()
	h.RevokeRole(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_ListTenants_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcTenantMgr{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/t", nil)
	w := httptest.NewRecorder()
	h.ListTenants(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_ListTenants_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcTenantMgr{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/t", nil)
	w := httptest.NewRecorder()
	h.ListTenants(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_CreateTenant_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcTenantMgr{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/t", strings.NewReader(`{"id":"t1","name":"tenant1"}`))
	w := httptest.NewRecorder()
	h.CreateTenant(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_CreateTenant_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcTenantMgr{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/t", strings.NewReader(`{"id":"t1","name":"tenant1"}`))
	w := httptest.NewRecorder()
	h.CreateTenant(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetTenant_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcTenantMgr{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/t/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.GetTenant(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetTenant_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcTenantMgr{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/t/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.GetTenant(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_UpdateTenant_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcTenantMgr{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPut, "/t/1", strings.NewReader(`{"name":"updated"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.UpdateTenant(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_UpdateTenant_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcTenantMgr{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPut, "/t/1", strings.NewReader(`{"name":"updated"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.UpdateTenant(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_DeleteTenant_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcTenantMgr{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/t/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.DeleteTenant(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_DeleteTenant_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcTenantMgr{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/t/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.DeleteTenant(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_BeginMFAEnrollment_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/mfa/enroll", strings.NewReader(`{"user_id":"u1","method":"totp"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.BeginMFAEnrollment(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_BeginMFAEnrollment_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/mfa/enroll", strings.NewReader(`{"user_id":"u1","method":"totp"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.BeginMFAEnrollment(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_CompleteMFAEnrollment_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/mfa/enroll/complete", strings.NewReader(`{"enrollment_id":"e1","code":"123456"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.CompleteMFAEnrollment(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_CompleteMFAEnrollment_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/mfa/enroll/complete", strings.NewReader(`{"enrollment_id":"e1","code":"123456"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.CompleteMFAEnrollment(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_CancelMFAEnrollment_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/mfa/enroll", strings.NewReader(`{"user_id":"u1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001", "enrollment_id": "e1"})
	w := httptest.NewRecorder()
	h.CancelMFAEnrollment(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_CancelMFAEnrollment_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/mfa/enroll", strings.NewReader(`{"user_id":"u1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001", "enrollment_id": "e1"})
	w := httptest.NewRecorder()
	h.CancelMFAEnrollment(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetMFAStatus_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/mfa/status", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.GetMFAStatus(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetMFAStatus_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/mfa/status", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.GetMFAStatus(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_DisableMFA_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/mfa", strings.NewReader(`{"user_id":"u1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.DisableMFA(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_DisableMFA_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/mfa", strings.NewReader(`{"user_id":"u1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.DisableMFA(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_BeginMFAChallenge_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/mfa/challenge", strings.NewReader(`{"session_token":"tok1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.BeginMFAChallenge(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_BeginMFAChallenge_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/mfa/challenge", strings.NewReader(`{"session_token":"tok1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.BeginMFAChallenge(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_VerifyMFAChallenge_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/mfa/verify", strings.NewReader(`{"challenge_id":"ch1","code":"123456"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.VerifyMFAChallenge(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_VerifyMFAChallenge_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/mfa/verify", strings.NewReader(`{"challenge_id":"ch1","code":"123456"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.VerifyMFAChallenge(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GenerateBackupCodes_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/mfa/backup", strings.NewReader(`{"user_id":"u1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.GenerateBackupCodes(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GenerateBackupCodes_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/mfa/backup", strings.NewReader(`{"user_id":"u1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.GenerateBackupCodes(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetBackupCodeStatus_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/mfa/backup/status", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.GetBackupCodeStatus(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetBackupCodeStatus_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/mfa/backup/status", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	h.GetBackupCodeStatus(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_RegisterDynamicClient_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithApps{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/dcr", strings.NewReader(`{"grant_types":["client_credentials"],"client_name":"test"}`))
	w := httptest.NewRecorder()
	h.RegisterDynamicClient(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetDynamicClient_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithApps{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/dcr/c1", nil)
	r = withChiParams(r, map[string]string{"client_id": "c1"})
	r.Header.Set("Authorization", "Bearer reg-token")
	w := httptest.NewRecorder()
	h.GetDynamicClient(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_UpdateDynamicClient_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithApps{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPut, "/dcr/c1", strings.NewReader(`{"grant_types":["client_credentials"],"client_name":"updated"}`))
	r = withChiParams(r, map[string]string{"client_id": "c1"})
	r.Header.Set("Authorization", "Bearer reg-token")
	w := httptest.NewRecorder()
	h.UpdateDynamicClient(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_DeleteDynamicClient_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithApps{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/dcr/c1", nil)
	r = withChiParams(r, map[string]string{"client_id": "c1"})
	r.Header.Set("Authorization", "Bearer reg-token")
	w := httptest.NewRecorder()
	h.DeleteDynamicClient(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetAuditEvent_NilLogger(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAudit{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/audit/ev1?tenant_id=t1", nil)
	r = withChiParams(r, map[string]string{"id": "ev1"})
	r = r.WithContext(tenantctx.WithTenant(r.Context(), "t1"))
	w := httptest.NewRecorder()
	h.GetAuditEvent(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetAuditEvent_LoggerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAudit{QuicBaaS: testutil.NewMockQuicBaaS(), loggerErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/audit/ev1?tenant_id=t1", nil)
	r = withChiParams(r, map[string]string{"id": "ev1"})
	r = r.WithContext(tenantctx.WithTenant(r.Context(), "t1"))
	w := httptest.NewRecorder()
	h.GetAuditEvent(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_DeleteOldAuditEvents_NilLogger(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAudit{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/audit?tenant_id=t1&before=2024-01-01T00:00:00Z", nil)
	r = r.WithContext(tenantctx.WithTenant(r.Context(), "t1"))
	w := httptest.NewRecorder()
	h.DeleteOldAuditEvents(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
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
