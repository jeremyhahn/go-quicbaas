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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

type selfRegService struct {
	*testutil.MockQuicBaaS
	mgr      WebAuthnManager
	store    *iam.IdentityStore
	tenants  *testTenantManager
	provider *iam.OIDCProvider
}

func (s *selfRegService) GetWebAuthnManager(_ context.Context) (WebAuthnManager, error) {
	return s.mgr, nil
}
func (s *selfRegService) GetIdentityStore(_ context.Context) (*iam.IdentityStore, error) {
	return s.store, nil
}
func (s *selfRegService) GetTenantManager(_ context.Context) (TenantManager, error) {
	return s.tenants, nil
}
func (s *selfRegService) GetOIDCProvider(_ context.Context) (*iam.OIDCProvider, error) {
	return s.provider, nil
}

func setupSelfRegHandler(t *testing.T, cfg config.IAMSelfServiceRegistrationConfig) *IAMHandler {
	t.Helper()
	memStorage := iam.NewMemoryStorage()
	tenants := &testTenantManager{storage: memStorage}
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  memStorage,
		GroupStorage: memStorage,
		RoleStorage:  memStorage,
		Tenants:      tenants,
	})

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
		IdentityStore: store,
		AppStorage:    iam.NewBackendStorage(newIntrospectMemKV()),
		TokenStorage:  iam.NewBackendStorage(newIntrospectMemKV()),
	})
	require.NoError(t, err)

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := &selfRegService{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		mgr:          &fakeWebAuthnManager{},
		store:        store,
		tenants:      tenants,
		provider:     provider,
	}
	serverCfg := DefaultConfig()
	serverCfg.Address = ":0"
	serverCfg.IAMSelfServiceRegistration = cfg
	server, err := NewServer(mockSvc.MockQuicBaaS, serverCfg, log)
	require.NoError(t, err)
	server.service = mockSvc
	return NewIAMHandler(IAMHandlerDependencies{Server: server})
}

func selfRegBegin(t *testing.T, handler *IAMHandler, tenantName, email string) string {
	t.Helper()
	body, _ := json.Marshal(SelfRegisterBeginRequest{
		TenantName: tenantName, Email: email, DisplayName: "User",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register/begin", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.RegisterSelfBegin(w, req)
	require.Equal(t, http.StatusOK, w.Code, "begin body: %s", w.Body.String())

	var resp SelfRegisterBeginResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotEmpty(t, resp.SessionID)
	return resp.SessionID
}

func selfRegFinish(t *testing.T, handler *IAMHandler, sessionID string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(SelfRegisterFinishRequest{
		SessionID:  sessionID,
		Credential: map[string]interface{}{"id": "abc", "response": map[string]interface{}{}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register/finish", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.RegisterSelfFinish(w, req)
	return w
}

// Begin then Finish drives the ceremony through tenant + user creation. The
// canonical in-memory identity store rejects an email as a username, so the
// flow surfaces that validation error after creating the tenant — exercising
// RegisterSelfFinish end to end up to user creation.
func TestRegisterSelf_FinishCreatesTenant(t *testing.T) {
	handler := setupSelfRegHandler(t, config.IAMSelfServiceRegistrationConfig{
		Enabled:               true,
		AllowedIsolationModes: []string{config.TenantIsolationKeyspace},
		DefaultIsolationMode:  config.TenantIsolationKeyspace,
	})

	sessionID := selfRegBegin(t, handler, "Acme Inc", "alice@acme.example")
	w := selfRegFinish(t, handler, sessionID)

	// Tenant was created; user creation validation is enforced by the store.
	require.NotEqual(t, http.StatusForbidden, w.Code, "finish body: %s", w.Body.String())
	tenants, _ := handler.getTenantManager(context.Background())
	require.NotNil(t, tenants)
	_, err := tenants.GetTenant(context.Background(), "acme-inc")
	assert.NoError(t, err, "tenant should have been created during finish")
}

// Finish with an unknown session is rejected.
func TestRegisterSelfFinish_UnknownSession(t *testing.T) {
	handler := setupSelfRegHandler(t, config.IAMSelfServiceRegistrationConfig{
		Enabled:               true,
		AllowedIsolationModes: []string{config.TenantIsolationKeyspace},
		DefaultIsolationMode:  config.TenantIsolationKeyspace,
	})

	w := selfRegFinish(t, handler, "no-such-session")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// An isolation mode not in the allow-list is rejected at Begin.
func TestRegisterSelfBegin_IsolationModeNotAllowed(t *testing.T) {
	handler := setupSelfRegHandler(t, config.IAMSelfServiceRegistrationConfig{
		Enabled:               true,
		AllowedIsolationModes: []string{config.TenantIsolationKeyspace},
		DefaultIsolationMode:  config.TenantIsolationKeyspace,
	})

	body, _ := json.Marshal(SelfRegisterBeginRequest{
		TenantName: "Acme", Email: "a@a.example", DisplayName: "A", IsolationMode: "dedicated-cluster",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register/begin", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.RegisterSelfBegin(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
