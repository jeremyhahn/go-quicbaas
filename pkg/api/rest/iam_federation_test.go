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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// mockFederationManager implements iam.FederationManager interface for testing.
type mockFederationManager struct {
	providers           map[string]*iam.ExternalProvider
	registerProviderErr error
	listProvidersErr    error
	getProviderErr      error
	updateProviderErr   error
	deleteProviderErr   error
	beginLoginErr       error
	handleCallbackErr   error
	linkAccountErr      error
	unlinkAccountErr    error
	getUserLinksErr     error
	beginLoginRedirect  *iam.LoginRedirect
	callbackResult      *iam.FederationResult
}

func newMockFederationManager() *mockFederationManager {
	return &mockFederationManager{
		providers: make(map[string]*iam.ExternalProvider),
	}
}

func (m *mockFederationManager) RegisterProvider(ctx context.Context, provider *iam.ExternalProvider) error {
	if m.registerProviderErr != nil {
		return m.registerProviderErr
	}
	if provider.ID == "" {
		provider.ID = "prov_" + provider.Name
	}
	provider.Created = time.Now().UTC()
	provider.Updated = time.Now().UTC()
	m.providers[provider.ID] = provider
	return nil
}

func (m *mockFederationManager) ListProviders(ctx context.Context, tenantID string, opts iam.ListOptions) ([]*iam.ExternalProvider, error) {
	if m.listProvidersErr != nil {
		return nil, m.listProvidersErr
	}
	// Extract type filter if present
	typeFilter := ""
	if opts.Filter != nil {
		if t, ok := opts.Filter["type"].(string); ok {
			typeFilter = t
		}
	}
	var result []*iam.ExternalProvider
	for _, p := range m.providers {
		if p.TenantID == tenantID {
			if typeFilter != "" && string(p.Type) != typeFilter {
				continue
			}
			result = append(result, p)
		}
	}
	// Apply pagination
	start := opts.Offset
	if start > len(result) {
		start = len(result)
	}
	end := start + opts.Limit
	if opts.Limit > 0 && end > len(result) {
		end = len(result)
	}
	if opts.Limit <= 0 {
		end = len(result)
	}
	return result[start:end], nil
}

func (m *mockFederationManager) GetProvider(ctx context.Context, tenantID, providerID string) (*iam.ExternalProvider, error) {
	if m.getProviderErr != nil {
		return nil, m.getProviderErr
	}
	provider, ok := m.providers[providerID]
	if !ok || provider.TenantID != tenantID {
		return nil, errors.New("provider not found")
	}
	return provider, nil
}

func (m *mockFederationManager) GetProviderByName(ctx context.Context, tenantID, providerName string) (*iam.ExternalProvider, error) {
	for _, p := range m.providers {
		if p.TenantID == tenantID && p.Name == providerName {
			return p, nil
		}
	}
	return nil, errors.New("provider not found")
}

func (m *mockFederationManager) UpdateProvider(ctx context.Context, provider *iam.ExternalProvider) error {
	if m.updateProviderErr != nil {
		return m.updateProviderErr
	}
	existing, ok := m.providers[provider.ID]
	if !ok {
		return errors.New("provider not found")
	}
	_ = existing
	m.providers[provider.ID] = provider
	return nil
}

func (m *mockFederationManager) DeleteProvider(ctx context.Context, tenantID, providerID string) error {
	if m.deleteProviderErr != nil {
		return m.deleteProviderErr
	}
	provider, ok := m.providers[providerID]
	if !ok || provider.TenantID != tenantID {
		return errors.New("provider not found")
	}
	delete(m.providers, providerID)
	return nil
}

func (m *mockFederationManager) BeginLogin(ctx context.Context, tenantID, providerName, redirectURI string) (*iam.LoginRedirect, error) {
	if m.beginLoginErr != nil {
		return nil, m.beginLoginErr
	}
	if m.beginLoginRedirect != nil {
		return m.beginLoginRedirect, nil
	}
	return &iam.LoginRedirect{
		AuthURL: "https://idp.example.com/authorize?client_id=test&redirect_uri=" + redirectURI,
		State:   "test-state",
	}, nil
}

func (m *mockFederationManager) HandleCallback(ctx context.Context, stateToken, authCode string) (*iam.FederationResult, error) {
	if m.handleCallbackErr != nil {
		return nil, m.handleCallbackErr
	}
	if m.callbackResult != nil {
		return m.callbackResult, nil
	}
	return &iam.FederationResult{
		User: &iam.User{
			ID:    10123,
			Email: "user@example.com",
			Name:  "Federated User",
		},
		AccessToken:  "access_token_xyz",
		RefreshToken: "refresh_token_xyz",
		ExpiresIn:    3600,
		Provisioned:  false,
	}, nil
}

func (m *mockFederationManager) LinkAccount(ctx context.Context, userID uint64, tenantID, providerID, externalToken string) error {
	if m.linkAccountErr != nil {
		return m.linkAccountErr
	}
	return nil
}

func (m *mockFederationManager) UnlinkAccount(ctx context.Context, userID uint64, tenantID, providerID string) error {
	if m.unlinkAccountErr != nil {
		return m.unlinkAccountErr
	}
	return nil
}

func (m *mockFederationManager) GetUserLinks(ctx context.Context, userID uint64, tenantID string) ([]*iam.FederatedIdentity, error) {
	if m.getUserLinksErr != nil {
		return nil, m.getUserLinksErr
	}
	return []*iam.FederatedIdentity{}, nil
}

// mockServiceWithFederation implements service interface with federation manager support.
type mockServiceWithFederation struct {
	service.QuicBaaS
	fedMgr      *mockFederationManager
	fedMgrErr   error
	auditLogger *mockAuditLogger
}

func (m *mockServiceWithFederation) GetFederationManager(ctx context.Context) (iam.FederationManager, error) {
	if m.fedMgrErr != nil {
		return nil, m.fedMgrErr
	}
	return m.fedMgr, nil
}

func (m *mockServiceWithFederation) GetAuditLogger(ctx context.Context) (iam.AuditLogger, error) {
	if m.auditLogger != nil {
		return m.auditLogger, nil
	}
	return nil, nil
}

// mockAuditLogger implements iam.AuditLogger for testing.
type mockAuditLogger struct {
	events []*iam.AuditEvent
}

func (m *mockAuditLogger) Log(ctx context.Context, event *iam.AuditEvent) error {
	m.events = append(m.events, event)
	return nil
}

func (m *mockAuditLogger) LogAsync(event *iam.AuditEvent) {
	m.events = append(m.events, event)
}

func (m *mockAuditLogger) Query(ctx context.Context, filter *iam.AuditFilter) ([]*iam.AuditEvent, int, error) {
	return m.events, len(m.events), nil
}

func (m *mockAuditLogger) GetEvent(ctx context.Context, tenantID, eventID string) (*iam.AuditEvent, error) {
	for _, e := range m.events {
		if e.ID == eventID {
			return e, nil
		}
	}
	return nil, errors.New("event not found")
}

func (m *mockAuditLogger) DeleteOlderThan(ctx context.Context, tenantID string, before time.Time) (int, error) {
	return 0, nil
}

func (m *mockAuditLogger) Close() error {
	return nil
}

// Helper to create test handler with mock federation service.
func createFederationTestHandler(fedMgr *mockFederationManager) (*IAMHandler, *Server) {
	mockSvc := &mockServiceWithFederation{
		fedMgr:      fedMgr,
		auditLogger: &mockAuditLogger{},
	}
	server := &Server{
		service: mockSvc,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})
	return handler, server
}

// federationHandlerFunc is an IAMHandler method expression such as
// (*IAMHandler).RegisterProvider.
type federationHandlerFunc func(*IAMHandler, http.ResponseWriter, *http.Request)

// federationErrorCase is one error path of a federation handler.
type federationErrorCase struct {
	name string
	// params are the chi URL params; target carries any query string.
	params map[string]string
	target string
	body   io.Reader
	// configure sets the mock federation manager's injected errors.
	configure func(*mockFederationManager)
	// fedMgrErr makes GetFederationManager fail; noService leaves the
	// server without a service, so no federation manager is available.
	fedMgrErr error
	noService bool
	wantCode  int
	// wantDesc, when set, must appear in the IAMAPIError description.
	wantDesc string
}

// jsonBody marshals v into a request body.
func jsonBody(t *testing.T, v any) io.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return bytes.NewReader(b)
}

// runFederationErrorCases serves each case through fn and checks the status
// and, when the case names one, the error description.
func runFederationErrorCases(t *testing.T, method string, fn federationHandlerFunc, cases []federationErrorCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fedMgr := newMockFederationManager()
			if tc.configure != nil {
				tc.configure(fedMgr)
			}
			server := &Server{}
			if !tc.noService {
				server.service = &mockServiceWithFederation{
					fedMgr:      fedMgr,
					fedMgrErr:   tc.fedMgrErr,
					auditLogger: &mockAuditLogger{},
				}
			}
			handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

			target := tc.target
			if target == "" {
				target = "/"
			}
			req := httptest.NewRequest(method, target, tc.body)
			req.Header.Set("Content-Type", "application/json")
			rctx := chi.NewRouteContext()
			for k, v := range tc.params {
				rctx.URLParams.Add(k, v)
			}
			req = req.WithContext(callerRouteContext(req.Context(), rctx))

			rec := httptest.NewRecorder()
			fn(handler, rec, req)

			assert.Equal(t, tc.wantCode, rec.Code)
			if tc.wantDesc != "" {
				var resp IAMAPIError
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				assert.Contains(t, resp.ErrorDescription, tc.wantDesc)
			}
		})
	}
}

// TestRegisterProvider_Success tests successful provider registration.
func TestRegisterProvider_Success(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	body := RegisterProviderOptions{
		Name:         "google",
		Type:         "oidc",
		Enabled:      true,
		ClientID:     "google-client-id",
		ClientSecret: "google-secret",
		Issuer:       "https://accounts.google.com",
		Scopes:       []string{"openid", "email", "profile"},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant123/providers", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// Set up chi context with route params
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.RegisterProvider(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp FederatedProvider
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "google", resp.Name)
	assert.Equal(t, string(ProviderTypeOIDC), string(resp.Type))
	assert.Equal(t, "tenant123", resp.TenantID)
	assert.Equal(t, "google-client-id", resp.ClientID)
}

// TestRegisterProvider_Errors covers every RegisterProvider error path.
func TestRegisterProvider_Errors(t *testing.T) {
	tenant := map[string]string{"tenant_id": "tenant123"}
	valid := RegisterProviderOptions{Name: "google", Type: "oidc", ClientID: "google-client-id"}
	runFederationErrorCases(t, http.MethodPost, (*IAMHandler).RegisterProvider, []federationErrorCase{
		{name: "missing tenant ID", body: jsonBody(t, valid), wantCode: http.StatusBadRequest, wantDesc: "tenant ID is required"},
		{name: "invalid JSON", params: tenant, body: bytes.NewReader([]byte("invalid json")), wantCode: http.StatusBadRequest, wantDesc: "invalid JSON body"},
		{name: "body read error", params: tenant, body: &fedErrorReader{}, wantCode: http.StatusBadRequest, wantDesc: "failed to read request body"},
		{name: "missing name", params: tenant, body: jsonBody(t, RegisterProviderOptions{Type: "oidc", ClientID: "google-client-id"}), wantCode: http.StatusBadRequest, wantDesc: "provider name is required"},
		{name: "missing type", params: tenant, body: jsonBody(t, RegisterProviderOptions{Name: "google", ClientID: "google-client-id"}), wantCode: http.StatusBadRequest, wantDesc: "provider type is required"},
		{name: "invalid type", params: tenant, body: jsonBody(t, RegisterProviderOptions{Name: "google", Type: "invalid", ClientID: "google-client-id"}), wantCode: http.StatusBadRequest, wantDesc: "invalid provider type"},
		{name: "missing client ID", params: tenant, body: jsonBody(t, RegisterProviderOptions{Name: "google", Type: "oidc"}), wantCode: http.StatusBadRequest, wantDesc: "client_id is required"},
		{name: "federation manager not available", params: tenant, body: jsonBody(t, valid), noService: true, wantCode: http.StatusInternalServerError},
		{name: "federation manager error", params: tenant, body: jsonBody(t, valid), fedMgrErr: errors.New("federation manager unavailable"), wantCode: http.StatusInternalServerError},
		{name: "service error", params: tenant, body: jsonBody(t, valid), configure: func(m *mockFederationManager) { m.registerProviderErr = errors.New("service error") }, wantCode: http.StatusInternalServerError},
	})
}

// TestListProviders_Success tests successful provider listing.
func TestListProviders_Success(t *testing.T) {
	fedMgr := newMockFederationManager()
	// Pre-populate with providers
	fedMgr.providers["prov_google"] = &iam.ExternalProvider{
		ID:       "prov_google",
		TenantID: "tenant123",
		Name:     "google",
		Type:     "oidc",
		Enabled:  true,
	}
	fedMgr.providers["prov_github"] = &iam.ExternalProvider{
		ID:       "prov_github",
		TenantID: "tenant123",
		Name:     "github",
		Type:     "oauth2",
		Enabled:  true,
	}

	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant123/providers", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.ListProviders(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp, "providers")
	assert.Contains(t, resp, "limit")
	assert.Contains(t, resp, "offset")
}

// TestListProviders_WithTypeFilter tests provider listing with type filter.
func TestListProviders_WithTypeFilter(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.providers["prov_google"] = &iam.ExternalProvider{
		ID:       "prov_google",
		TenantID: "tenant123",
		Name:     "google",
		Type:     "oidc",
	}
	fedMgr.providers["prov_github"] = &iam.ExternalProvider{
		ID:       "prov_github",
		TenantID: "tenant123",
		Name:     "github",
		Type:     "oauth2",
	}
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant123/providers?type=oidc", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.ListProviders(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	providers := resp["providers"].([]interface{})
	assert.Equal(t, 1, len(providers))
}

// TestListProviders_Errors covers every ListProviders error path.
func TestListProviders_Errors(t *testing.T) {
	tenant := map[string]string{"tenant_id": "tenant123"}
	runFederationErrorCases(t, http.MethodGet, (*IAMHandler).ListProviders, []federationErrorCase{
		{name: "missing tenant ID", wantCode: http.StatusBadRequest},
		{name: "federation manager not available", params: tenant, noService: true, wantCode: http.StatusInternalServerError},
		{name: "federation manager error", params: tenant, fedMgrErr: errors.New("federation manager unavailable"), wantCode: http.StatusInternalServerError},
		{name: "service error", params: tenant, configure: func(m *mockFederationManager) { m.listProvidersErr = errors.New("service error") }, wantCode: http.StatusInternalServerError},
	})
}

// TestGetProvider_Success tests successful provider retrieval.
func TestGetProvider_Success(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.providers["prov_google"] = &iam.ExternalProvider{
		ID:        "prov_google",
		TenantID:  "tenant123",
		Name:      "google",
		Type:      "oidc",
		Enabled:   true,
		ClientID:  "client-id",
		IssuerURL: "https://accounts.google.com",
		Created:   time.Now().UTC(),
		Updated:   time.Now().UTC(),
	}

	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant123/providers/prov_google", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.GetProvider(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp FederatedProvider
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "prov_google", resp.ID)
	assert.Equal(t, "google", resp.Name)
	assert.Equal(t, string(ProviderTypeOIDC), string(resp.Type))
}

// TestGetProvider_Errors covers every GetProvider error path.
func TestGetProvider_Errors(t *testing.T) {
	both := map[string]string{"tenant_id": "tenant123", "provider_id": "prov_google"}
	runFederationErrorCases(t, http.MethodGet, (*IAMHandler).GetProvider, []federationErrorCase{
		{name: "missing tenant ID", params: map[string]string{"provider_id": "prov_google"}, wantCode: http.StatusBadRequest},
		{name: "missing provider ID", params: map[string]string{"tenant_id": "tenant123"}, wantCode: http.StatusBadRequest},
		{name: "federation manager not available", params: both, noService: true, wantCode: http.StatusInternalServerError},
		{name: "federation manager error", params: both, fedMgrErr: errors.New("federation manager unavailable"), wantCode: http.StatusInternalServerError},
		{name: "service error", params: both, configure: func(m *mockFederationManager) { m.getProviderErr = errors.New("service error") }, wantCode: http.StatusInternalServerError},
	})
}

// TestUpdateProvider_Errors covers every UpdateProvider error path.
func TestUpdateProvider_Errors(t *testing.T) {
	both := map[string]string{"tenant_id": "tenant123", "provider_id": "prov_google"}
	fedMgrErr := errors.New("federation manager unavailable")
	runFederationErrorCases(t, http.MethodPut, (*IAMHandler).UpdateProvider, []federationErrorCase{
		{name: "missing tenant ID", params: map[string]string{"provider_id": "prov_google"}, body: jsonBody(t, UpdateProviderOptions{}), wantCode: http.StatusBadRequest},
		{name: "missing provider ID", params: map[string]string{"tenant_id": "tenant123"}, body: jsonBody(t, UpdateProviderOptions{}), wantCode: http.StatusBadRequest},
		{name: "invalid JSON", params: both, body: bytes.NewReader([]byte("invalid")), wantCode: http.StatusBadRequest},
		{name: "body read error", params: both, body: &fedErrorReader{}, wantCode: http.StatusBadRequest, wantDesc: "failed to read request body"},
		{name: "federation manager not available", params: both, body: jsonBody(t, UpdateProviderOptions{}), noService: true, wantCode: http.StatusInternalServerError},
		{name: "federation manager error", params: both, body: jsonBody(t, UpdateProviderOptions{}), fedMgrErr: fedMgrErr, wantCode: http.StatusInternalServerError},
		{name: "service error", params: both, body: jsonBody(t, UpdateProviderOptions{}), configure: func(m *mockFederationManager) { m.updateProviderErr = errors.New("service error") }, wantCode: http.StatusInternalServerError},
	})
}

// TestDeleteProvider_Success tests successful provider deletion.
func TestDeleteProvider_Success(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.providers["prov_google"] = &iam.ExternalProvider{
		ID:       "prov_google",
		TenantID: "tenant123",
		Name:     "google",
	}

	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant123/providers/prov_google", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.DeleteProvider(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, fedMgr.providers)
}

// TestDeleteProvider_Errors covers every DeleteProvider error path.
func TestDeleteProvider_Errors(t *testing.T) {
	both := map[string]string{"tenant_id": "tenant123", "provider_id": "prov_google"}
	runFederationErrorCases(t, http.MethodDelete, (*IAMHandler).DeleteProvider, []federationErrorCase{
		{name: "missing tenant ID", params: map[string]string{"provider_id": "prov_google"}, wantCode: http.StatusBadRequest},
		{name: "missing provider ID", params: map[string]string{"tenant_id": "tenant123"}, wantCode: http.StatusBadRequest},
		{name: "federation manager not available", params: both, noService: true, wantCode: http.StatusInternalServerError},
		{name: "federation manager error", params: both, fedMgrErr: errors.New("federation manager unavailable"), wantCode: http.StatusInternalServerError},
		{name: "service error", params: both, configure: func(m *mockFederationManager) { m.deleteProviderErr = errors.New("service error") }, wantCode: http.StatusInternalServerError},
	})
}

// TestBeginFederatedLogin_Success tests successful federated login initiation.
func TestBeginFederatedLogin_Success(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.beginLoginRedirect = &iam.LoginRedirect{
		AuthURL: "https://idp.example.com/authorize?client_id=test",
		State:   "random_state",
		// Nonce:       "random_nonce",
	}
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers/tenant123/google/login?redirect_uri=https://app.example.com/callback&state=mystate", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_name", "google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.BeginFederatedLogin(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "https://idp.example.com/authorize")
}

// TestBeginFederatedLogin_Errors covers every BeginFederatedLogin error path.
func TestBeginFederatedLogin_Errors(t *testing.T) {
	both := map[string]string{"tenant_id": "tenant123", "provider_name": "google"}
	target := "/?redirect_uri=https://app.example.com/callback"
	runFederationErrorCases(t, http.MethodGet, (*IAMHandler).BeginFederatedLogin, []federationErrorCase{
		{name: "missing tenant ID", params: map[string]string{"provider_name": "google"}, target: target, wantCode: http.StatusBadRequest},
		{name: "missing provider name", params: map[string]string{"tenant_id": "tenant123"}, target: target, wantCode: http.StatusBadRequest},
		{name: "missing redirect URI", params: both, wantCode: http.StatusBadRequest, wantDesc: "redirect_uri is required"},
		{name: "federation manager not available", params: both, target: target, noService: true, wantCode: http.StatusInternalServerError},
		{name: "federation manager error", params: both, target: target, fedMgrErr: errors.New("federation manager unavailable"), wantCode: http.StatusInternalServerError},
		{name: "service error", params: both, target: target, configure: func(m *mockFederationManager) { m.beginLoginErr = errors.New("service error") }, wantCode: http.StatusInternalServerError},
	})
}

// TestHandleFederatedCallback_Success tests successful callback handling.
func TestHandleFederatedCallback_Success(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.callbackResult = &iam.FederationResult{
		User: &iam.User{
			ID:       10123,
			Email:    "user@example.com",
			Name:     "Test User",
			TenantID: "tenant123",
		},
		AccessToken:  "access_token_123",
		RefreshToken: "refresh_token_123",
		// IDToken:      "id_token_123",
		ExpiresIn:   3600,
		Provisioned: true,
	}
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers/tenant123/google/callback?code=auth_code_123&state=mystate", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_name", "google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.HandleFederatedCallback(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp FederatedAuthResult
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "access_token_123", resp.AccessToken)
	assert.Equal(t, "Bearer", resp.TokenType)
	assert.True(t, resp.IsNewUser)
	assert.Equal(t, "user@example.com", resp.User.Email)
}

// TestHandleFederatedCallback_Errors covers the HandleFederatedCallback
// error paths that answer with an IAMAPIError.
func TestHandleFederatedCallback_Errors(t *testing.T) {
	both := map[string]string{"tenant_id": "tenant123", "provider_name": "google"}
	target := "/?code=auth_code_123"
	runFederationErrorCases(t, http.MethodGet, (*IAMHandler).HandleFederatedCallback, []federationErrorCase{
		{name: "missing tenant ID", params: map[string]string{"provider_name": "google"}, target: target, wantCode: http.StatusBadRequest},
		{name: "missing provider name", params: map[string]string{"tenant_id": "tenant123"}, target: target, wantCode: http.StatusBadRequest},
		{name: "missing code", params: both, target: "/?state=mystate", wantCode: http.StatusBadRequest, wantDesc: "authorization code is required"},
		{name: "federation manager not available", params: both, target: target, noService: true, wantCode: http.StatusInternalServerError},
		{name: "federation manager error", params: both, target: target, fedMgrErr: errors.New("federation manager unavailable"), wantCode: http.StatusInternalServerError},
		{name: "service error", params: both, target: target, configure: func(m *mockFederationManager) { m.handleCallbackErr = errors.New("service error") }, wantCode: http.StatusInternalServerError},
	})
}

// TestHandleFederatedCallback_IdPError tests callback with IdP error.
func TestHandleFederatedCallback_IdPError(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers/tenant123/google/callback?error=access_denied&error_description=User%20denied%20access", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_name", "google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.HandleFederatedCallback(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp OAuthError
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "access_denied", resp.Code)
}

// TestLinkAccount_Success tests successful account linking.
func TestLinkAccount_Success(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	body := LinkAccountOptions{
		ProviderID: "prov_google",
		ExternalID: "google_user_123",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant123/users/user123/links", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("user_id", "10123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.LinkAccount(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp AccountLink
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, uint64(10123), resp.UserID)
	assert.Equal(t, "prov_google", resp.ProviderID)
	assert.Equal(t, "google_user_123", resp.ExternalID)
}

// TestLinkAccount_Errors covers every LinkAccount error path.
func TestLinkAccount_Errors(t *testing.T) {
	both := map[string]string{"tenant_id": "tenant123", "user_id": "10123"}
	valid := LinkAccountOptions{ProviderID: "prov_google", ExternalID: "google_user_123"}
	runFederationErrorCases(t, http.MethodPost, (*IAMHandler).LinkAccount, []federationErrorCase{
		{name: "missing tenant ID", params: map[string]string{"user_id": "10123"}, body: jsonBody(t, valid), wantCode: http.StatusBadRequest},
		{name: "missing user ID", params: map[string]string{"tenant_id": "tenant123"}, body: jsonBody(t, valid), wantCode: http.StatusBadRequest},
		{name: "invalid JSON", params: both, body: bytes.NewReader([]byte("invalid")), wantCode: http.StatusBadRequest},
		{name: "body read error", params: both, body: &fedErrorReader{}, wantCode: http.StatusBadRequest, wantDesc: "failed to read request body"},
		{name: "missing provider ID", params: both, body: jsonBody(t, LinkAccountOptions{ExternalID: "google_user_123"}), wantCode: http.StatusBadRequest, wantDesc: "provider_id is required"},
		{name: "missing external ID", params: both, body: jsonBody(t, LinkAccountOptions{ProviderID: "prov_google"}), wantCode: http.StatusBadRequest, wantDesc: "external_id is required"},
		{name: "federation manager not available", params: both, body: jsonBody(t, valid), noService: true, wantCode: http.StatusInternalServerError},
		{name: "federation manager error", params: both, body: jsonBody(t, valid), fedMgrErr: errors.New("federation manager unavailable"), wantCode: http.StatusInternalServerError},
		{name: "service error", params: both, body: jsonBody(t, valid), configure: func(m *mockFederationManager) { m.linkAccountErr = errors.New("service error") }, wantCode: http.StatusInternalServerError},
	})
}

// TestUnlinkAccount_Success tests successful account unlinking.
func TestUnlinkAccount_Success(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant123/users/user123/links/prov_google", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("user_id", "10123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.UnlinkAccount(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	// Verify unlink succeeded (mock always succeeds)
}

// TestUnlinkAccount_Errors covers every UnlinkAccount error path.
func TestUnlinkAccount_Errors(t *testing.T) {
	all := map[string]string{"tenant_id": "tenant123", "user_id": "10123", "provider_id": "prov_google"}
	runFederationErrorCases(t, http.MethodDelete, (*IAMHandler).UnlinkAccount, []federationErrorCase{
		{name: "missing tenant ID", params: map[string]string{"user_id": "10123", "provider_id": "prov_google"}, wantCode: http.StatusBadRequest},
		{name: "missing user ID", params: map[string]string{"tenant_id": "tenant123", "provider_id": "prov_google"}, wantCode: http.StatusBadRequest},
		{name: "missing provider ID", params: map[string]string{"tenant_id": "tenant123", "user_id": "10123"}, wantCode: http.StatusBadRequest},
		{name: "federation manager not available", params: all, noService: true, wantCode: http.StatusInternalServerError},
		{name: "federation manager error", params: all, fedMgrErr: errors.New("federation manager unavailable"), wantCode: http.StatusInternalServerError},
		{name: "service error", params: all, configure: func(m *mockFederationManager) { m.unlinkAccountErr = errors.New("service error") }, wantCode: http.StatusInternalServerError},
	})
}

// TestGetUserLinks_Success tests successful user links retrieval.
func TestGetUserLinks_Success(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant123/users/user123/links", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("user_id", "10123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.GetUserLinks(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp, "links")
	assert.Contains(t, resp, "limit")
	assert.Contains(t, resp, "offset")
}

// TestGetUserLinks_Errors covers every GetUserLinks error path.
func TestGetUserLinks_Errors(t *testing.T) {
	both := map[string]string{"tenant_id": "tenant123", "user_id": "10123"}
	runFederationErrorCases(t, http.MethodGet, (*IAMHandler).GetUserLinks, []federationErrorCase{
		{name: "missing tenant ID", params: map[string]string{"user_id": "10123"}, wantCode: http.StatusBadRequest},
		{name: "missing user ID", params: map[string]string{"tenant_id": "tenant123"}, wantCode: http.StatusBadRequest},
		{name: "federation manager not available", params: both, noService: true, wantCode: http.StatusInternalServerError},
		{name: "federation manager error", params: both, fedMgrErr: errors.New("federation manager unavailable"), wantCode: http.StatusInternalServerError},
		{name: "service error", params: both, configure: func(m *mockFederationManager) { m.getUserLinksErr = errors.New("service error") }, wantCode: http.StatusInternalServerError},
	})
}

// TestIsValidProviderType tests provider type validation.
func TestIsValidProviderType(t *testing.T) {
	tests := []struct {
		name     string
		ptype    ProviderType
		expected bool
	}{
		{
			name:     "OIDC is valid",
			ptype:    "oidc",
			expected: true,
		},
		{
			name:     "SAML is valid",
			ptype:    "saml",
			expected: true,
		},
		{
			name:     "OAuth2 is valid",
			ptype:    "oauth2",
			expected: true,
		},
		{
			name:     "LDAP is valid",
			ptype:    ProviderTypeLDAP,
			expected: true,
		},
		{
			name:     "empty is invalid",
			ptype:    ProviderType(""),
			expected: false,
		},
		{
			name:     "random is invalid",
			ptype:    ProviderType("random"),
			expected: false,
		},
		{
			name:     "kerberos is invalid",
			ptype:    ProviderType("kerberos"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidProviderType(tt.ptype)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestRegisterFederationRoutes tests route registration.
func TestRegisterFederationRoutes(t *testing.T) {
	router := chi.NewRouter()
	handler := NewIAMHandler(IAMHandlerDependencies{Server: &Server{}})
	router.Route("/tenants/{tenant_id}", func(r chi.Router) {
		RegisterFederationRoutes(r, handler)
	})
	RegisterFederationAuthRoutes(router, handler)

	routes := []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodPost, "/tenants/test/providers/", true},
		{http.MethodGet, "/tenants/test/providers/", true},
		{http.MethodGet, "/tenants/test/providers/prov1", true},
		{http.MethodPut, "/tenants/test/providers/prov1", true},
		{http.MethodDelete, "/tenants/test/providers/prov1", true},
		{http.MethodPost, "/tenants/test/links/", true},
		{http.MethodGet, "/tenants/test/links/", true},
		{http.MethodDelete, "/tenants/test/links/prov1", true},
		{http.MethodGet, "/api/v1/auth/providers/tenant1/google/login", true},
		{http.MethodGet, "/api/v1/auth/providers/tenant1/google/callback", true},
		{http.MethodPatch, "/tenants/test/providers/prov1", false},
		{http.MethodPost, "/api/v1/auth/providers/tenant1/google/login", false},
	}
	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			assert.Equal(t, route.want, router.Match(chi.NewRouteContext(), route.method, route.path))
		})
	}
}

// TestAccountLinkJSONSerialization tests JSON marshaling of AccountLink.
func TestAccountLinkJSONSerialization(t *testing.T) {
	now := time.Now().UTC()
	link := &AccountLink{
		ID:           "link_123",
		UserID:       1<<60 + 7, // above 2^53: a float64 would round it
		ProviderID:   "prov_google",
		ProviderName: "google",
		ExternalID:   "google_user_456",
		Email:        "user@gmail.com",
		// Metadata:     map[string]interface{}{"avatar": "https://example.com/avatar.png"},
		CreatedAt: now,
		UpdatedAt: now,
	}

	data, err := json.Marshal(link)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	assert.Contains(t, string(data), `"user_id":"1152921504606846983"`)

	var decoded AccountLink
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, link.ID, decoded.ID)
	assert.Equal(t, link.UserID, decoded.UserID)
	assert.Equal(t, link.ProviderID, decoded.ProviderID)
	assert.Equal(t, link.ProviderName, decoded.ProviderName)
	assert.Equal(t, link.ExternalID, decoded.ExternalID)
	assert.Equal(t, link.Email, decoded.Email)
}

// TestRegisterProvider_AllProviderTypes tests registering all provider types.
func TestRegisterProvider_AllProviderTypes(t *testing.T) {
	types := []ProviderType{
		"oidc",
		"saml",
		"oauth2",
		ProviderTypeLDAP,
	}

	for _, ptype := range types {
		t.Run(string(ptype), func(t *testing.T) {
			fedMgr := newMockFederationManager()
			handler, _ := createFederationTestHandler(fedMgr)

			body := RegisterProviderOptions{
				Name:     string(ptype) + "-provider",
				Type:     ptype,
				Enabled:  true,
				ClientID: "client-id",
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant123/providers", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("tenant_id", "tenant123")
			req = req.WithContext(callerRouteContext(req.Context(), rctx))

			rec := httptest.NewRecorder()
			handler.RegisterProvider(rec, req)

			assert.Equal(t, http.StatusCreated, rec.Code)

			var resp FederatedProvider
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Equal(t, ptype, resp.Type)
		})
	}
}

// TestListProviders_InvalidPagination tests pagination with invalid values.
func TestListProviders_InvalidPagination(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		expectedLimit int
		expectedOff   int
	}{
		{
			name:          "negative limit uses default",
			query:         "?limit=-1",
			expectedLimit: 100, // default
			expectedOff:   0,
		},
		{
			name:          "very large limit is capped",
			query:         "?limit=5000",
			expectedLimit: 100, // default since > 1000
			expectedOff:   0,
		},
		{
			name:          "negative offset uses default",
			query:         "?offset=-10",
			expectedLimit: 100,
			expectedOff:   0, // default
		},
		{
			name:          "non-numeric limit uses default",
			query:         "?limit=abc",
			expectedLimit: 100, // default
			expectedOff:   0,
		},
		{
			name:          "non-numeric offset uses default",
			query:         "?offset=abc",
			expectedLimit: 100,
			expectedOff:   0, // default
		},
		{
			name:          "valid limit and offset",
			query:         "?limit=50&offset=10",
			expectedLimit: 50,
			expectedOff:   10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fedMgr := newMockFederationManager()
			handler, _ := createFederationTestHandler(fedMgr)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant123/providers"+tt.query, nil)

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("tenant_id", "tenant123")
			req = req.WithContext(callerRouteContext(req.Context(), rctx))

			rec := httptest.NewRecorder()
			handler.ListProviders(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)

			var resp map[string]interface{}
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Equal(t, float64(tt.expectedLimit), resp["limit"])
			assert.Equal(t, float64(tt.expectedOff), resp["offset"])
		})
	}
}

// TestUpdateProvider_AllFields tests updating all provider fields.
func TestUpdateProvider_AllFields(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.providers["prov_google"] = &iam.ExternalProvider{
		ID:       "prov_google",
		TenantID: "tenant123",
		Name:     "google",
		Type:     "oidc",
		Enabled:  true,
		ClientID: "old-client-id",
		Created:  time.Now().UTC(),
		Updated:  time.Now().UTC(),
	}

	handler, _ := createFederationTestHandler(fedMgr)

	newName := "google-updated"
	enabled := false
	newClientID := "new-client-id"
	newClientSecret := "new-secret"
	newIssuer := "https://new-issuer.com"
	newAuthURL := "https://new-issuer.com/auth"
	newTokenURL := "https://new-issuer.com/token"
	newUserInfoURL := "https://new-issuer.com/userinfo"
	newScopes := []string{"openid", "email"}

	body := UpdateProviderOptions{
		Name:         &newName,
		Enabled:      &enabled,
		ClientID:     &newClientID,
		ClientSecret: &newClientSecret,
		Issuer:       &newIssuer,
		AuthURL:      &newAuthURL,
		TokenURL:     &newTokenURL,
		UserInfoURL:  &newUserInfoURL,
		Scopes:       &newScopes,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant123/providers/prov_google", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.UpdateProvider(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp iam.ExternalProvider
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "google-updated", resp.Name)
	assert.False(t, resp.Enabled)
	assert.Equal(t, "new-client-id", resp.ClientID)
	assert.Empty(t, resp.ClientSecret, "the response must not carry the upstream client secret")
	assert.NotContains(t, rec.Body.String(), "new-secret")
	stored, err := fedMgr.GetProvider(context.Background(), "tenant123", "prov_google")
	require.NoError(t, err)
	assert.Equal(t, "new-secret", stored.ClientSecret, "the stored provider must keep the secret")
	assert.Equal(t, "https://new-issuer.com", resp.IssuerURL)
	assert.Equal(t, "https://new-issuer.com/auth", resp.AuthURL)
	assert.Equal(t, "https://new-issuer.com/token", resp.TokenURL)
	assert.Equal(t, "https://new-issuer.com/userinfo", resp.UserInfoURL)
	assert.Equal(t, newScopes, resp.Scopes)
}

// assertNoProviderSecret fails when a provider response body carries the
// upstream client secret or any non-empty client_secret value.
func assertNoProviderSecret(t *testing.T, body []byte, secret string) {
	t.Helper()
	assert.NotContains(t, string(body), secret)
	assert.NotRegexp(t, `"client_secret":\s*"[^"]`, string(body))
}

func TestProviderResponses_RedactClientSecret(t *testing.T) {
	const secret = "upstream-idp-secret"
	storedProvider := func() *iam.ExternalProvider {
		return &iam.ExternalProvider{
			ID:           "prov_google",
			TenantID:     "tenant123",
			Name:         "google",
			Type:         "oidc",
			Enabled:      true,
			ClientID:     "client-id",
			ClientSecret: secret,
			IssuerURL:    "https://accounts.google.com",
		}
	}
	routeRequest := func(method, target string, body []byte, providerID string) *http.Request {
		req := httptest.NewRequest(method, target, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant123")
		if providerID != "" {
			rctx.URLParams.Add("provider_id", providerID)
		}
		return req.WithContext(callerRouteContext(req.Context(), rctx))
	}

	t.Run("create", func(t *testing.T) {
		fedMgr := newMockFederationManager()
		handler, _ := createFederationTestHandler(fedMgr)
		body, err := json.Marshal(RegisterProviderOptions{
			Name:         "google",
			Type:         "oidc",
			Enabled:      true,
			ClientID:     "client-id",
			ClientSecret: secret,
			Issuer:       "https://accounts.google.com",
		})
		require.NoError(t, err)

		rec := httptest.NewRecorder()
		handler.RegisterProvider(rec, routeRequest(http.MethodPost, "/api/v1/tenants/tenant123/providers", body, ""))

		require.Equal(t, http.StatusCreated, rec.Code)
		assertNoProviderSecret(t, rec.Body.Bytes(), secret)
		require.Len(t, fedMgr.providers, 1)
		for _, stored := range fedMgr.providers {
			assert.Equal(t, secret, stored.ClientSecret, "the stored provider must keep the secret")
		}
	})

	t.Run("get", func(t *testing.T) {
		fedMgr := newMockFederationManager()
		fedMgr.providers["prov_google"] = storedProvider()
		handler, _ := createFederationTestHandler(fedMgr)

		rec := httptest.NewRecorder()
		handler.GetProvider(rec, routeRequest(http.MethodGet, "/api/v1/tenants/tenant123/providers/prov_google", nil, "prov_google"))

		require.Equal(t, http.StatusOK, rec.Code)
		assertNoProviderSecret(t, rec.Body.Bytes(), secret)
		assert.Equal(t, secret, fedMgr.providers["prov_google"].ClientSecret)
	})

	t.Run("list", func(t *testing.T) {
		fedMgr := newMockFederationManager()
		fedMgr.providers["prov_google"] = storedProvider()
		handler, _ := createFederationTestHandler(fedMgr)

		rec := httptest.NewRecorder()
		handler.ListProviders(rec, routeRequest(http.MethodGet, "/api/v1/tenants/tenant123/providers", nil, ""))

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "prov_google")
		assertNoProviderSecret(t, rec.Body.Bytes(), secret)
		assert.Equal(t, secret, fedMgr.providers["prov_google"].ClientSecret)
	})

	t.Run("update", func(t *testing.T) {
		fedMgr := newMockFederationManager()
		fedMgr.providers["prov_google"] = storedProvider()
		handler, _ := createFederationTestHandler(fedMgr)
		newName := "google-renamed"
		body, err := json.Marshal(UpdateProviderOptions{Name: &newName})
		require.NoError(t, err)

		rec := httptest.NewRecorder()
		handler.UpdateProvider(rec, routeRequest(http.MethodPut, "/api/v1/tenants/tenant123/providers/prov_google", body, "prov_google"))

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), newName)
		assertNoProviderSecret(t, rec.Body.Bytes(), secret)
		assert.Equal(t, secret, fedMgr.providers["prov_google"].ClientSecret)
	})
}

// TestBeginFederatedLogin_WithOptionalParams tests login with optional parameters.
func TestBeginFederatedLogin_WithOptionalParams(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/providers/tenant123/google/login?redirect_uri=https://app.example.com/callback&state=mystate&nonce=mynonce&scopes=openid%20email", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_name", "google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.BeginFederatedLogin(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)
	// The redirect URL should contain state and nonce from the mock
	location := rec.Header().Get("Location")
	assert.NotEmpty(t, location)
}

// fedErrorReader is a reader that always returns an error.
type fedErrorReader struct{}

func (e *fedErrorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("simulated read error")
}

// TestGetUserLinks_InvalidPagination tests pagination with invalid values.
func TestGetUserLinks_InvalidPagination(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		expectedLimit int
		expectedOff   int
	}{
		{
			name:          "negative limit uses default",
			query:         "?limit=-1",
			expectedLimit: 100,
			expectedOff:   0,
		},
		{
			name:          "very large limit is capped",
			query:         "?limit=5000",
			expectedLimit: 100,
			expectedOff:   0,
		},
		{
			name:          "negative offset uses default",
			query:         "?offset=-10",
			expectedLimit: 100,
			expectedOff:   0,
		},
		{
			name:          "non-numeric limit uses default",
			query:         "?limit=abc",
			expectedLimit: 100,
			expectedOff:   0,
		},
		{
			name:          "non-numeric offset uses default",
			query:         "?offset=abc",
			expectedLimit: 100,
			expectedOff:   0,
		},
		{
			name:          "valid limit within range",
			query:         "?limit=500",
			expectedLimit: 500,
			expectedOff:   0,
		},
		{
			name:          "valid limit and offset",
			query:         "?limit=25&offset=5",
			expectedLimit: 25,
			expectedOff:   5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fedMgr := newMockFederationManager()
			handler, _ := createFederationTestHandler(fedMgr)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant123/users/user123/links"+tt.query, nil)

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("tenant_id", "tenant123")
			rctx.URLParams.Add("user_id", "10123")
			req = req.WithContext(callerRouteContext(req.Context(), rctx))

			rec := httptest.NewRecorder()
			handler.GetUserLinks(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)

			var resp map[string]interface{}
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Equal(t, float64(tt.expectedLimit), resp["limit"])
			assert.Equal(t, float64(tt.expectedOff), resp["offset"])
		})
	}
}
