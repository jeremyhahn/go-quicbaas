//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

const (
	mfaTenant = "tenant-mfa"
	mfaUser   = "60001"
)

// fakeMFAManager is a deterministic MFAManager for the REST handler tests.
type fakeMFAManager struct{}

func (f *fakeMFAManager) BeginEnrollment(_ context.Context, _ uint64, _ string, _ iam.MFAMethod) (*iam.MFAEnrollResponse, error) {
	return &iam.MFAEnrollResponse{
		EnrollmentID: "enr-1",
		QRCodeURI:    "otpauth://totp/x",
		Secret:       "SECRET",
		ExpiresAt:    time.Now().Add(time.Minute),
	}, nil
}

func (f *fakeMFAManager) CompleteEnrollment(_ context.Context, _, _ string) error { return nil }
func (f *fakeMFAManager) CancelEnrollment(_ context.Context, _ string) error      { return nil }

func (f *fakeMFAManager) BeginChallenge(_ context.Context, _ uint64, _, _ string) (*iam.MFAChallenge, error) {
	return &iam.MFAChallenge{ID: "chal-1", Method: iam.MFAMethodTOTP, ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (f *fakeMFAManager) VerifyChallenge(_ context.Context, _, _ string) (*iam.MFAVerifyResponse, error) {
	return &iam.MFAVerifyResponse{Success: true, Token: "full-token", BackupCodesRemaining: 5}, nil
}

func (f *fakeMFAManager) VerifyCode(_ context.Context, _ uint64, _, _ string) error { return nil }

func (f *fakeMFAManager) GetMFAStatus(_ context.Context, _ uint64, _ string) (*iam.MFAStatus, error) {
	now := time.Now()
	return &iam.MFAStatus{Enabled: true, Method: iam.MFAMethodTOTP, EnrolledAt: &now, LastVerified: &now,
		BackupCodesTotal: 10, BackupCodesRemaining: 8}, nil
}

func (f *fakeMFAManager) IsMFARequired(_ context.Context, _ uint64, _ string) (bool, error) {
	return true, nil
}
func (f *fakeMFAManager) DisableMFA(_ context.Context, _ uint64, _ string) error { return nil }

func (f *fakeMFAManager) GenerateBackupCodes(_ context.Context, _ uint64, _ string) ([]string, error) {
	return []string{"code1", "code2"}, nil
}

func (f *fakeMFAManager) VerifyBackupCode(_ context.Context, _ uint64, _, _ string) error { return nil }

func (f *fakeMFAManager) GetBackupCodeStatus(_ context.Context, _ uint64, _ string) (*iam.BackupCodeStatus, error) {
	return &iam.BackupCodeStatus{Total: 10, Remaining: 8, Used: 2}, nil
}

// mfaMockService exposes a fake MFAManager to the IAM handlers.
type mfaMockService struct {
	*testutil.MockQuicBaaS
	manager MFAManager
}

func (m *mfaMockService) GetMFAManager(_ context.Context) (MFAManager, error) {
	return m.manager, nil
}

func setupMFAHandler(t *testing.T) (*IAMHandler, func()) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mockSvc := &mfaMockService{MockQuicBaaS: testutil.NewMockQuicBaaS(), manager: &fakeMFAManager{}}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc

	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})
	return handler, func() { server.Close() }
}

func mfaRequest(t *testing.T, method string, body interface{}, params map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, "/x", &buf)
	if body != nil {
		req.ContentLength = int64(buf.Len())
	}
	return withChiParams(req, params)
}

func TestMFA_FullFlow(t *testing.T) {
	handler, cleanup := setupMFAHandler(t)
	defer cleanup()
	p := map[string]string{"tenant_id": mfaTenant, "user_id": mfaUser}

	// Begin enrollment
	w := httptest.NewRecorder()
	handler.BeginMFAEnrollment(w, mfaRequest(t, http.MethodPost, map[string]string{"method": "totp"}, p))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	// Complete enrollment
	w = httptest.NewRecorder()
	handler.CompleteMFAEnrollment(w, mfaRequest(t, http.MethodPost,
		map[string]string{"enrollment_id": "enr-1", "code": "123456"}, p))
	require.Equal(t, http.StatusOK, w.Code)

	// Cancel enrollment
	w = httptest.NewRecorder()
	handler.CancelMFAEnrollment(w, mfaRequest(t, http.MethodDelete, nil,
		map[string]string{"tenant_id": mfaTenant, "user_id": mfaUser, "enrollment_id": "enr-1"}))
	require.Equal(t, http.StatusNoContent, w.Code)

	// Status
	w = httptest.NewRecorder()
	handler.GetMFAStatus(w, mfaRequest(t, http.MethodGet, nil, p))
	require.Equal(t, http.StatusOK, w.Code)

	// Begin challenge
	w = httptest.NewRecorder()
	handler.BeginMFAChallenge(w, mfaRequest(t, http.MethodPost, map[string]string{"session_token": "st"}, p))
	require.Equal(t, http.StatusOK, w.Code)

	// Verify challenge
	w = httptest.NewRecorder()
	handler.VerifyMFAChallenge(w, mfaRequest(t, http.MethodPost,
		map[string]string{"challenge_id": "chal-1", "code": "123456"}, p))
	require.Equal(t, http.StatusOK, w.Code)

	// Generate backup codes
	w = httptest.NewRecorder()
	handler.GenerateBackupCodes(w, mfaRequest(t, http.MethodPost, nil, p))
	require.Equal(t, http.StatusOK, w.Code)

	// Backup code status
	w = httptest.NewRecorder()
	handler.GetBackupCodeStatus(w, mfaRequest(t, http.MethodGet, nil, p))
	require.Equal(t, http.StatusOK, w.Code)

	// Disable MFA
	w = httptest.NewRecorder()
	handler.DisableMFA(w, mfaRequest(t, http.MethodDelete, nil, p))
	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestMFA_Validation(t *testing.T) {
	handler, cleanup := setupMFAHandler(t)
	defer cleanup()
	p := map[string]string{"tenant_id": mfaTenant, "user_id": mfaUser}

	// Begin enrollment missing user -> 400
	w := httptest.NewRecorder()
	handler.BeginMFAEnrollment(w, mfaRequest(t, http.MethodPost, nil,
		map[string]string{"tenant_id": mfaTenant, "user_id": ""}))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Complete enrollment missing fields -> 400
	w = httptest.NewRecorder()
	handler.CompleteMFAEnrollment(w, mfaRequest(t, http.MethodPost, map[string]string{"code": "x"}, p))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Begin challenge missing session_token -> 400
	w = httptest.NewRecorder()
	handler.BeginMFAChallenge(w, mfaRequest(t, http.MethodPost, map[string]string{}, p))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Verify challenge missing fields -> 400
	w = httptest.NewRecorder()
	handler.VerifyMFAChallenge(w, mfaRequest(t, http.MethodPost, map[string]string{"code": "x"}, p))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// When the service does not expose an MFA manager, the handlers return 500.
func TestMFA_ManagerUnavailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()
	p := map[string]string{"tenant_id": mfaTenant, "user_id": mfaUser}

	w := httptest.NewRecorder()
	handler.GetMFAStatus(w, mfaRequest(t, http.MethodGet, nil, p))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// errMFAManager fails operations so the handlers' error paths run.
type errMFAManager struct{ *fakeMFAManager }

func (errMFAManager) GetMFAStatus(context.Context, uint64, string) (*iam.MFAStatus, error) {
	return nil, errFakeMFA
}
func (errMFAManager) GetBackupCodeStatus(context.Context, uint64, string) (*iam.BackupCodeStatus, error) {
	return nil, errFakeMFA
}

var errFakeMFA = fakeMFAError{}

type fakeMFAError struct{}

func (fakeMFAError) Error() string { return "mfa backend error" }

func TestMFA_StatusError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := &mfaMockService{MockQuicBaaS: testutil.NewMockQuicBaaS(), manager: errMFAManager{&fakeMFAManager{}}}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})
	defer server.Close()

	p := map[string]string{"tenant_id": mfaTenant, "user_id": mfaUser}

	w := httptest.NewRecorder()
	handler.GetMFAStatus(w, mfaRequest(t, http.MethodGet, nil, p))
	assert.NotEqual(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	handler.GetBackupCodeStatus(w, mfaRequest(t, http.MethodGet, nil, p))
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func (errMFAManager) DisableMFA(context.Context, uint64, string) error { return errFakeMFA }
func (errMFAManager) GenerateBackupCodes(context.Context, uint64, string) ([]string, error) {
	return nil, errFakeMFA
}
func (errMFAManager) CancelEnrollment(context.Context, string) error { return errFakeMFA }

func TestMFA_MoreErrors(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := &mfaMockService{MockQuicBaaS: testutil.NewMockQuicBaaS(), manager: errMFAManager{&fakeMFAManager{}}}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})
	defer server.Close()

	p := map[string]string{"tenant_id": mfaTenant, "user_id": mfaUser}

	w := httptest.NewRecorder()
	handler.DisableMFA(w, mfaRequest(t, http.MethodDelete, nil, p))
	assert.NotEqual(t, http.StatusNoContent, w.Code)

	w = httptest.NewRecorder()
	handler.GenerateBackupCodes(w, mfaRequest(t, http.MethodPost, nil, p))
	assert.NotEqual(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	handler.CancelMFAEnrollment(w, mfaRequest(t, http.MethodDelete, nil,
		map[string]string{"tenant_id": mfaTenant, "user_id": mfaUser, "enrollment_id": "e1"}))
	assert.NotEqual(t, http.StatusNoContent, w.Code)
}

func (errMFAManager) BeginEnrollment(context.Context, uint64, string, iam.MFAMethod) (*iam.MFAEnrollResponse, error) {
	return nil, errFakeMFA
}
func (errMFAManager) BeginChallenge(context.Context, uint64, string, string) (*iam.MFAChallenge, error) {
	return nil, errFakeMFA
}

func TestMFA_BeginErrors(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := &mfaMockService{MockQuicBaaS: testutil.NewMockQuicBaaS(), manager: errMFAManager{&fakeMFAManager{}}}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})
	defer server.Close()

	p := map[string]string{"tenant_id": mfaTenant, "user_id": mfaUser}

	w := httptest.NewRecorder()
	handler.BeginMFAEnrollment(w, mfaRequest(t, http.MethodPost, map[string]string{"method": "totp"}, p))
	assert.NotEqual(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	handler.BeginMFAChallenge(w, mfaRequest(t, http.MethodPost, map[string]string{"session_token": "st"}, p))
	assert.NotEqual(t, http.StatusOK, w.Code)
}
