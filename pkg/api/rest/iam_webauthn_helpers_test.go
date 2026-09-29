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
	cryptorand "crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// fakeWebAuthnManager returns deterministic ceremony options and a fixed user.
type fakeWebAuthnManager struct {
	user *iam.User
}

func (f *fakeWebAuthnManager) BeginRegistration(_ context.Context, _, _ string) (map[string]interface{}, error) {
	return map[string]interface{}{"challenge": "reg-challenge"}, nil
}

func (f *fakeWebAuthnManager) FinishRegistration(_ context.Context, _, _ string, _ map[string]interface{}) (*iam.AttestationResult, error) {
	return nil, nil
}

func (f *fakeWebAuthnManager) BeginLogin(_ context.Context, _, _ string) (map[string]interface{}, error) {
	return map[string]interface{}{"challenge": "login-challenge"}, nil
}

func (f *fakeWebAuthnManager) FinishLogin(_ context.Context, _ string, _ map[string]interface{}) (*iam.User, error) {
	return f.user, nil
}

type webAuthnService struct {
	*testutil.MockQuicBaaS
	mgr      WebAuthnManager
	provider *iam.OIDCProvider
	audit    iam.AuditLogger
}

func (s *webAuthnService) GetWebAuthnManager(_ context.Context) (WebAuthnManager, error) {
	return s.mgr, nil
}
func (s *webAuthnService) GetOIDCProvider(_ context.Context) (*iam.OIDCProvider, error) {
	return s.provider, nil
}
func (s *webAuthnService) GetAuditLogger(_ context.Context) (iam.AuditLogger, error) {
	return s.audit, nil
}

func setupWebAuthnHandler(t *testing.T) *IAMHandler {
	t.Helper()
	privateKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	require.NoError(t, err)
	provider, err := iam.NewOIDCProvider(&iam.OIDCConfig{
		Issuer:           "https://broker.example.com",
		SigningAlgorithm: "RS256",
		SigningKey:       privateKey,
		AccessTokenTTL:   15 * time.Minute,
		RefreshTokenTTL:  24 * time.Hour,
		IDTokenTTL:       15 * time.Minute,
		AuthCodeTTL:      10 * time.Minute,
	}, iam.OIDCDependencies{
		IdentityStore: iam.NewIdentityStore(iam.IdentityStoreOptions{
			UserStorage: iam.NewMemoryStorage(),
			RoleStorage: iam.NewMemoryStorage(),
		}),
		AppStorage:   iam.NewBackendStorage(newIntrospectMemKV()),
		TokenStorage: iam.NewBackendStorage(newIntrospectMemKV()),
	})
	require.NoError(t, err)

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := &webAuthnService{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		mgr:          &fakeWebAuthnManager{user: &iam.User{ID: "u1", TenantID: "tenant1", Email: "u1@example.com"}},
		provider:     provider,
		audit:        &captureAuditLogger{},
	}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc
	return NewIAMHandler(IAMHandlerDependencies{Server: server})
}

func webAuthnReq(method string, body interface{}, params map[string]string) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, "/x", &buf)
	return withChiParams(req, params)
}

// errWebAuthnManager fails every ceremony call.
type errWebAuthnManager struct{}

func (errWebAuthnManager) BeginRegistration(context.Context, string, string) (map[string]interface{}, error) {
	return nil, errFakeWebAuthn
}
func (errWebAuthnManager) FinishRegistration(context.Context, string, string, map[string]interface{}) (*iam.AttestationResult, error) {
	return nil, errFakeWebAuthn
}
func (errWebAuthnManager) BeginLogin(context.Context, string, string) (map[string]interface{}, error) {
	return nil, errFakeWebAuthn
}
func (errWebAuthnManager) FinishLogin(context.Context, string, map[string]interface{}) (*iam.User, error) {
	return nil, errFakeWebAuthn
}

var errFakeWebAuthn = fakeWebAuthnError{}

type fakeWebAuthnError struct{}

func (fakeWebAuthnError) Error() string { return "webauthn ceremony failed" }
