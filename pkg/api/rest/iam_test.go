//go:build rest

package rest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
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

func TestDeep_ListUserConsents_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/c", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.ListUserConsents(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_ListUserConsents_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/c", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.ListUserConsents(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetConsent_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/c", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1", "purpose": "marketing"})
	w := httptest.NewRecorder()
	h.GetConsent(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetConsent_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/c", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1", "purpose": "marketing"})
	w := httptest.NewRecorder()
	h.GetConsent(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_WithdrawConsent_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/c", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1", "purpose": "marketing"})
	w := httptest.NewRecorder()
	h.WithdrawConsent(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_WithdrawConsent_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/c", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1", "purpose": "marketing"})
	w := httptest.NewRecorder()
	h.WithdrawConsent(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_RequestDataExport_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/e", strings.NewReader(`{"format":"json"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.RequestDataExport(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_RequestDataExport_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/e", strings.NewReader(`{"format":"json"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.RequestDataExport(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetExportStatus_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/e/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1", "export_id": "exp1"})
	w := httptest.NewRecorder()
	h.GetExportStatus(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetExportStatus_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/e/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1", "export_id": "exp1"})
	w := httptest.NewRecorder()
	h.GetExportStatus(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_DownloadExport_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/e/1/d", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1", "export_id": "exp1"})
	w := httptest.NewRecorder()
	h.DownloadExport(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_DownloadExport_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/e/1/d", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1", "export_id": "exp1"})
	w := httptest.NewRecorder()
	h.DownloadExport(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_RequestDeletion_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/d", strings.NewReader(`{"reason":"test"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.RequestDeletion(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_RequestDeletion_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/d", strings.NewReader(`{"reason":"test"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.RequestDeletion(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetDeletionStatus_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/d/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1", "deletion_id": "del1"})
	w := httptest.NewRecorder()
	h.GetDeletionStatus(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetDeletionStatus_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/d/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1", "deletion_id": "del1"})
	w := httptest.NewRecorder()
	h.GetDeletionStatus(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_CancelDeletion_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/d/1/c", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1", "deletion_id": "del1"})
	w := httptest.NewRecorder()
	h.CancelDeletion(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_CancelDeletion_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/d/1/c", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1", "deletion_id": "del1"})
	w := httptest.NewRecorder()
	h.CancelDeletion(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_CreateAdminAssignment_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/a", strings.NewReader(`{"user_id":"u1","admin_role":"super_admin"}`))
	w := httptest.NewRecorder()
	h.CreateAdminAssignment(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_CreateAdminAssignment_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/a", strings.NewReader(`{"user_id":"u1","admin_role":"super_admin"}`))
	w := httptest.NewRecorder()
	h.CreateAdminAssignment(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_ListAdminAssignments_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/a", nil)
	w := httptest.NewRecorder()
	h.ListAdminAssignments(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_ListAdminAssignments_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/a", nil)
	w := httptest.NewRecorder()
	h.ListAdminAssignments(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetAdminAssignment_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/a/1", nil)
	r = withChiParams(r, map[string]string{"assignment_id": "a1"})
	w := httptest.NewRecorder()
	h.GetAdminAssignment(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetAdminAssignment_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/a/1", nil)
	r = withChiParams(r, map[string]string{"assignment_id": "a1"})
	w := httptest.NewRecorder()
	h.GetAdminAssignment(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_UpdateAdminAssignment_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPut, "/a/1", strings.NewReader(`{"admin_role":"super_admin"}`))
	r = withChiParams(r, map[string]string{"assignment_id": "a1"})
	w := httptest.NewRecorder()
	h.UpdateAdminAssignment(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_UpdateAdminAssignment_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPut, "/a/1", strings.NewReader(`{"admin_role":"super_admin"}`))
	r = withChiParams(r, map[string]string{"assignment_id": "a1"})
	w := httptest.NewRecorder()
	h.UpdateAdminAssignment(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_DeleteAdminAssignment_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/a/1", nil)
	r = withChiParams(r, map[string]string{"assignment_id": "a1"})
	w := httptest.NewRecorder()
	h.DeleteAdminAssignment(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_DeleteAdminAssignment_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/a/1", nil)
	r = withChiParams(r, map[string]string{"assignment_id": "a1"})
	w := httptest.NewRecorder()
	h.DeleteAdminAssignment(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetUserAssignments_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/a/u/1", nil)
	r = withChiParams(r, map[string]string{"user_id": "u1"})
	w := httptest.NewRecorder()
	h.GetUserAssignments(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetUserAssignments_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/a/u/1", nil)
	r = withChiParams(r, map[string]string{"user_id": "u1"})
	w := httptest.NewRecorder()
	h.GetUserAssignments(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_CheckAdminPermission_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/c", strings.NewReader(`{"user_id":"u1","permission":"manage_users"}`))
	w := httptest.NewRecorder()
	h.CheckAdminPermission(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_CheckAdminPermission_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/c", strings.NewReader(`{"user_id":"u1","permission":"manage_users"}`))
	w := httptest.NewRecorder()
	h.CheckAdminPermission(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_StartImpersonation_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/i", strings.NewReader(`{"admin_user_id":"a1","target_user_id":"u1","tenant_id":"t1","reason":"support"}`))
	w := httptest.NewRecorder()
	h.StartImpersonation(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_StartImpersonation_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/i", strings.NewReader(`{"admin_user_id":"a1","target_user_id":"u1","tenant_id":"t1","reason":"support"}`))
	w := httptest.NewRecorder()
	h.StartImpersonation(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_EndImpersonation_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/i/1", nil)
	r = withChiParams(r, map[string]string{"session_id": "s1"})
	w := httptest.NewRecorder()
	h.EndImpersonation(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_EndImpersonation_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/i/1", nil)
	r = withChiParams(r, map[string]string{"session_id": "s1"})
	w := httptest.NewRecorder()
	h.EndImpersonation(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_ListActiveImpersonations_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/i/active", nil)
	r = withChiParams(r, map[string]string{"user_id": "u1"})
	w := httptest.NewRecorder()
	h.ListActiveImpersonations(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_ListActiveImpersonations_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/i/active", nil)
	w := httptest.NewRecorder()
	h.ListActiveImpersonations(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_ListImpersonations_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/i", nil)
	r = withChiParams(r, map[string]string{"user_id": "u1"})
	w := httptest.NewRecorder()
	h.ListImpersonations(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_ListImpersonations_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/i", nil)
	r = withChiParams(r, map[string]string{"user_id": "u1"})
	w := httptest.NewRecorder()
	h.ListImpersonations(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
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
	ctx := ctxWithTestSubject(r.Context(), "u1", "t1")
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
	ctx := ctxWithTestSubject(r.Context(), "u1", "t1")
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
	ctx := ctxWithTestSubject(r.Context(), "u1", "t1")
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
	ctx := ctxWithTestSubject(r.Context(), "u1", "t1")
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
	ctx := ctxWithTestSubject(r.Context(), "u1", "t1")
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
	ctx := ctxWithTestSubject(r.Context(), "u1", "t1")
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
	ctx := ctxWithTestSubject(r.Context(), "u1", "t1")
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
	ctx := ctxWithTestSubject(r.Context(), "u1", "t1")
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
	ctx := ctxWithTestSubject(r.Context(), "u1", "t1")
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
	ctx := ctxWithTestSubject(r.Context(), "u1", "t1")
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
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
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

func TestDeep_ListGroups_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/g", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.ListGroups(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_ListGroups_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/g", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.ListGroups(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_CreateGroup_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/g", strings.NewReader(`{"name":"grp1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.CreateGroup(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_CreateGroup_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/g", strings.NewReader(`{"name":"grp1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.CreateGroup(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetGroup_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/g/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "group_id": "g1"})
	w := httptest.NewRecorder()
	h.GetGroup(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetGroup_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/g/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "group_id": "g1"})
	w := httptest.NewRecorder()
	h.GetGroup(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_UpdateGroup_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPut, "/g/1", strings.NewReader(`{"name":"updated"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "group_id": "g1"})
	w := httptest.NewRecorder()
	h.UpdateGroup(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_UpdateGroup_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPut, "/g/1", strings.NewReader(`{"name":"updated"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "group_id": "g1"})
	w := httptest.NewRecorder()
	h.UpdateGroup(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_DeleteGroup_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/g/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "group_id": "g1"})
	w := httptest.NewRecorder()
	h.DeleteGroup(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_DeleteGroup_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/g/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "group_id": "g1"})
	w := httptest.NewRecorder()
	h.DeleteGroup(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_AddMember_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/g/1/m", strings.NewReader(`{"user_id":"u1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "group_id": "g1"})
	w := httptest.NewRecorder()
	h.AddMember(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_AddMember_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/g/1/m", strings.NewReader(`{"user_id":"u1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "group_id": "g1"})
	w := httptest.NewRecorder()
	h.AddMember(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_RemoveMember_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/g/1/m/u1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "group_id": "g1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.RemoveMember(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_RemoveMember_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/g/1/m/u1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "group_id": "g1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.RemoveMember(w, r)
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
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.GetUser(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetUser_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/u/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.GetUser(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_UpdateUser_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPut, "/u/1", strings.NewReader(`{"name":"Updated"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.UpdateUser(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_UpdateUser_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPut, "/u/1", strings.NewReader(`{"name":"Updated"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.UpdateUser(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_DeleteUser_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/u/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.DeleteUser(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_DeleteUser_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/u/1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.DeleteUser(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_AssignRole_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/u/1/roles", strings.NewReader(`{"role_id":"r1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.AssignRole(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_AssignRole_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/u/1/roles", strings.NewReader(`{"role_id":"r1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.AssignRole(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_RevokeRole_NilStore(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/u/1/roles/r1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1", "role": "r1"})
	w := httptest.NewRecorder()
	h.RevokeRole(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_RevokeRole_StoreError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/u/1/roles/r1", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1", "role": "r1"})
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
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.BeginMFAEnrollment(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_BeginMFAEnrollment_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/mfa/enroll", strings.NewReader(`{"user_id":"u1","method":"totp"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.BeginMFAEnrollment(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_CompleteMFAEnrollment_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/mfa/enroll/complete", strings.NewReader(`{"enrollment_id":"e1","code":"123456"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.CompleteMFAEnrollment(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_CompleteMFAEnrollment_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/mfa/enroll/complete", strings.NewReader(`{"enrollment_id":"e1","code":"123456"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.CompleteMFAEnrollment(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_CancelMFAEnrollment_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/mfa/enroll", strings.NewReader(`{"user_id":"u1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1", "enrollment_id": "e1"})
	w := httptest.NewRecorder()
	h.CancelMFAEnrollment(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_CancelMFAEnrollment_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/mfa/enroll", strings.NewReader(`{"user_id":"u1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1", "enrollment_id": "e1"})
	w := httptest.NewRecorder()
	h.CancelMFAEnrollment(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetMFAStatus_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/mfa/status", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.GetMFAStatus(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetMFAStatus_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/mfa/status", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.GetMFAStatus(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_DisableMFA_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/mfa", strings.NewReader(`{"user_id":"u1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.DisableMFA(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_DisableMFA_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/mfa", strings.NewReader(`{"user_id":"u1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.DisableMFA(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_BeginMFAChallenge_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/mfa/challenge", strings.NewReader(`{"session_token":"tok1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.BeginMFAChallenge(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_BeginMFAChallenge_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/mfa/challenge", strings.NewReader(`{"session_token":"tok1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.BeginMFAChallenge(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_VerifyMFAChallenge_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/mfa/verify", strings.NewReader(`{"challenge_id":"ch1","code":"123456"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.VerifyMFAChallenge(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_VerifyMFAChallenge_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/mfa/verify", strings.NewReader(`{"challenge_id":"ch1","code":"123456"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.VerifyMFAChallenge(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GenerateBackupCodes_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/mfa/backup", strings.NewReader(`{"user_id":"u1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.GenerateBackupCodes(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GenerateBackupCodes_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/mfa/backup", strings.NewReader(`{"user_id":"u1"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.GenerateBackupCodes(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_GetBackupCodeStatus_NilManager(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/mfa/backup/status", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()
	h.GetBackupCodeStatus(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetBackupCodeStatus_ManagerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/mfa/backup/status", nil)
	r = withChiParams(r, map[string]string{"tenant_id": "t1", "user_id": "u1"})
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
	r = r.WithContext(iam.ContextWithTenant(r.Context(), "t1"))
	w := httptest.NewRecorder()
	h.GetAuditEvent(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeep_GetAuditEvent_LoggerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAudit{QuicBaaS: testutil.NewMockQuicBaaS(), loggerErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodGet, "/audit/ev1?tenant_id=t1", nil)
	r = withChiParams(r, map[string]string{"id": "ev1"})
	r = r.WithContext(iam.ContextWithTenant(r.Context(), "t1"))
	w := httptest.NewRecorder()
	h.GetAuditEvent(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_DeleteOldAuditEvents_NilLogger(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAudit{QuicBaaS: testutil.NewMockQuicBaaS()})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/audit?tenant_id=t1&before=2024-01-01T00:00:00Z", nil)
	r = r.WithContext(iam.ContextWithTenant(r.Context(), "t1"))
	w := httptest.NewRecorder()
	h.DeleteOldAuditEvents(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDeep_DeleteOldAuditEvents_LoggerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockServiceWithAudit{QuicBaaS: testutil.NewMockQuicBaaS(), loggerErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodDelete, "/audit?tenant_id=t1&before=2024-01-01T00:00:00Z", nil)
	r = r.WithContext(iam.ContextWithTenant(r.Context(), "t1"))
	w := httptest.NewRecorder()
	h.DeleteOldAuditEvents(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code)
}
