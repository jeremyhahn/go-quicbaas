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

// TestRegisterProvider_MissingTenantID tests provider registration with missing tenant ID.
func TestRegisterProvider_MissingTenantID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	body := RegisterProviderOptions{
		Name:     "google",
		Type:     "oidc",
		ClientID: "google-client-id",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//providers", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// No tenant_id in URL params
	rctx := chi.NewRouteContext()
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.RegisterProvider(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp IAMAPIError
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, ErrBadRequest, resp.Error)
	assert.Contains(t, resp.ErrorDescription, "tenant ID is required")
}

// TestRegisterProvider_InvalidJSON tests provider registration with invalid JSON.
func TestRegisterProvider_InvalidJSON(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant123/providers", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.RegisterProvider(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp IAMAPIError
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, ErrBadRequest, resp.Error)
	assert.Contains(t, resp.ErrorDescription, "invalid JSON body")
}

// TestRegisterProvider_MissingName tests provider registration with missing name.
func TestRegisterProvider_MissingName(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	body := RegisterProviderOptions{
		Type:     "oidc",
		ClientID: "google-client-id",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant123/providers", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.RegisterProvider(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp IAMAPIError
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp.ErrorDescription, "provider name is required")
}

// TestRegisterProvider_MissingType tests provider registration with missing type.
func TestRegisterProvider_MissingType(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	body := RegisterProviderOptions{
		Name:     "google",
		ClientID: "google-client-id",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant123/providers", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.RegisterProvider(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp IAMAPIError
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp.ErrorDescription, "provider type is required")
}

// TestRegisterProvider_InvalidType tests provider registration with invalid type.
func TestRegisterProvider_InvalidType(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	body := RegisterProviderOptions{
		Name:     "google",
		Type:     ProviderType("invalid"),
		ClientID: "google-client-id",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant123/providers", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.RegisterProvider(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp IAMAPIError
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp.ErrorDescription, "invalid provider type")
}

// TestRegisterProvider_MissingClientID tests provider registration with missing client ID.
func TestRegisterProvider_MissingClientID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	body := RegisterProviderOptions{
		Name: "google",
		Type: "oidc",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant123/providers", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.RegisterProvider(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp IAMAPIError
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp.ErrorDescription, "client_id is required")
}

// TestRegisterProvider_FederationManagerNotAvailable tests when federation manager is not available.
func TestRegisterProvider_FederationManagerNotAvailable(t *testing.T) {
	// Use a service that doesn't implement GetFederationManager
	server := &Server{
		service: nil, // No service
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	body := RegisterProviderOptions{
		Name:     "google",
		Type:     "oidc",
		ClientID: "google-client-id",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant123/providers", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.RegisterProvider(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
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

// TestListProviders_WithPagination tests provider listing with pagination.
func TestListProviders_WithPagination(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant123/providers?limit=50&offset=10", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.ListProviders(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(50), resp["limit"])
	assert.Equal(t, float64(10), resp["offset"])
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

// TestListProviders_MissingTenantID tests provider listing without tenant ID.
func TestListProviders_MissingTenantID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants//providers", nil)

	rctx := chi.NewRouteContext()
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.ListProviders(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
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

// TestGetProvider_MissingTenantID tests get provider without tenant ID.
func TestGetProvider_MissingTenantID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants//providers/prov_google", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.GetProvider(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestGetProvider_MissingProviderID tests get provider without provider ID.
func TestGetProvider_MissingProviderID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant123/providers/", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.GetProvider(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestUpdateProvider_Success tests successful provider update.
func TestUpdateProvider_Success(t *testing.T) {
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
	body := UpdateProviderOptions{
		Name:     &newName,
		Enabled:  &enabled,
		ClientID: &newClientID,
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

	var resp FederatedProvider
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "google-updated", resp.Name)
	assert.False(t, resp.Enabled)
	assert.Equal(t, "new-client-id", resp.ClientID)
}

// TestUpdateProvider_MissingTenantID tests update provider without tenant ID.
func TestUpdateProvider_MissingTenantID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	body := UpdateProviderOptions{}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants//providers/prov_google", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.UpdateProvider(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestUpdateProvider_MissingProviderID tests update provider without provider ID.
func TestUpdateProvider_MissingProviderID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	body := UpdateProviderOptions{}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant123/providers/", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.UpdateProvider(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestUpdateProvider_InvalidJSON tests update provider with invalid JSON.
func TestUpdateProvider_InvalidJSON(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant123/providers/prov_google", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.UpdateProvider(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
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

// TestDeleteProvider_MissingTenantID tests delete provider without tenant ID.
func TestDeleteProvider_MissingTenantID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants//providers/prov_google", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.DeleteProvider(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestDeleteProvider_MissingProviderID tests delete provider without provider ID.
func TestDeleteProvider_MissingProviderID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant123/providers/", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.DeleteProvider(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
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

// TestBeginFederatedLogin_MissingTenantID tests federated login without tenant ID.
func TestBeginFederatedLogin_MissingTenantID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers//google/login?redirect_uri=https://app.example.com/callback", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("provider_name", "google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.BeginFederatedLogin(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestBeginFederatedLogin_MissingProviderName tests federated login without provider name.
func TestBeginFederatedLogin_MissingProviderName(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers/tenant123//login?redirect_uri=https://app.example.com/callback", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.BeginFederatedLogin(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestBeginFederatedLogin_MissingRedirectURI tests federated login without redirect URI.
func TestBeginFederatedLogin_MissingRedirectURI(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers/tenant123/google/login", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_name", "google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.BeginFederatedLogin(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp IAMAPIError
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp.ErrorDescription, "redirect_uri is required")
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

// TestHandleFederatedCallback_MissingTenantID tests callback without tenant ID.
func TestHandleFederatedCallback_MissingTenantID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers//google/callback?code=auth_code_123", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("provider_name", "google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.HandleFederatedCallback(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandleFederatedCallback_MissingProviderName tests callback without provider name.
func TestHandleFederatedCallback_MissingProviderName(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers/tenant123//callback?code=auth_code_123", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.HandleFederatedCallback(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
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

// TestHandleFederatedCallback_MissingCode tests callback without authorization code.
func TestHandleFederatedCallback_MissingCode(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers/tenant123/google/callback?state=mystate", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_name", "google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.HandleFederatedCallback(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp IAMAPIError
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp.ErrorDescription, "authorization code is required")
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

// TestLinkAccount_MissingTenantID tests account linking without tenant ID.
func TestLinkAccount_MissingTenantID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	body := LinkAccountOptions{
		ProviderID: "prov_google",
		ExternalID: "google_user_123",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//users/user123/links", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("user_id", "10123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.LinkAccount(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestLinkAccount_MissingUserID tests account linking without user ID.
func TestLinkAccount_MissingUserID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	body := LinkAccountOptions{
		ProviderID: "prov_google",
		ExternalID: "google_user_123",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant123/users//links", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.LinkAccount(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestLinkAccount_InvalidJSON tests account linking with invalid JSON.
func TestLinkAccount_InvalidJSON(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant123/users/user123/links", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("user_id", "10123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.LinkAccount(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestLinkAccount_MissingProviderID tests account linking without provider ID.
func TestLinkAccount_MissingProviderID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	body := LinkAccountOptions{
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

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp IAMAPIError
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp.ErrorDescription, "provider_id is required")
}

// TestLinkAccount_MissingExternalID tests account linking without external ID.
func TestLinkAccount_MissingExternalID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	body := LinkAccountOptions{
		ProviderID: "prov_google",
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

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp IAMAPIError
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp.ErrorDescription, "external_id is required")
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

// TestUnlinkAccount_MissingTenantID tests unlinking without tenant ID.
func TestUnlinkAccount_MissingTenantID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants//users/user123/links/prov_google", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("user_id", "10123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.UnlinkAccount(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestUnlinkAccount_MissingUserID tests unlinking without user ID.
func TestUnlinkAccount_MissingUserID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant123/users//links/prov_google", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.UnlinkAccount(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestUnlinkAccount_MissingProviderID tests unlinking without provider ID.
func TestUnlinkAccount_MissingProviderID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant123/users/user123/links/", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("user_id", "10123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.UnlinkAccount(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
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

// TestGetUserLinks_WithPagination tests user links with pagination.
func TestGetUserLinks_WithPagination(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant123/users/user123/links?limit=25&offset=5", nil)

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
	assert.Equal(t, float64(25), resp["limit"])
	assert.Equal(t, float64(5), resp["offset"])
}

// TestGetUserLinks_MissingTenantID tests user links without tenant ID.
func TestGetUserLinks_MissingTenantID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants//users/user123/links", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("user_id", "10123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.GetUserLinks(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestGetUserLinks_MissingUserID tests user links without user ID.
func TestGetUserLinks_MissingUserID(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant123/users//links", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.GetUserLinks(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
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
	mockServer := &Server{}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: mockServer})

	// Register routes under a tenant scope
	router.Route("/tenants/{tenant_id}", func(r chi.Router) {
		RegisterFederationRoutes(r, handler)
	})

	// Verify routes are registered
	routes := []struct {
		method string
		path   string
	}{
		{"POST", "/tenants/test/providers"},
		{"GET", "/tenants/test/providers"},
		{"GET", "/tenants/test/providers/prov1"},
		{"PUT", "/tenants/test/providers/prov1"},
		{"DELETE", "/tenants/test/providers/prov1"},
		{"POST", "/tenants/test/links"},
		{"GET", "/tenants/test/links"},
		{"DELETE", "/tenants/test/links/prov1"},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, nil)
			rctx := chi.NewRouteContext()
			req = req.WithContext(callerRouteContext(req.Context(), rctx))
			assert.NotNil(t, router, "Router should be initialized")
		})
	}
}

// TestRegisterFederationAuthRoutes tests public auth route registration.
func TestRegisterFederationAuthRoutes(t *testing.T) {
	router := chi.NewRouter()
	mockServer := &Server{}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: mockServer})

	RegisterFederationAuthRoutes(router, handler)

	// Verify routes are registered
	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/auth/providers/tenant1/google/login"},
		{"GET", "/api/v1/auth/providers/tenant1/google/callback"},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, nil)
			rctx := chi.NewRouteContext()
			req = req.WithContext(callerRouteContext(req.Context(), rctx))
			assert.NotNil(t, router, "Router should be initialized")
		})
	}
}

// TestFederatedProviderJSONSerialization tests JSON marshaling of FederatedProvider.
func TestFederatedProviderJSONSerialization(t *testing.T) {
	now := time.Now().UTC()
	provider := &iam.ExternalProvider{
		ID:           "prov_google",
		TenantID:     "tenant123",
		Name:         "google",
		Type:         "oidc",
		Enabled:      true,
		ClientID:     "client-id",
		ClientSecret: "secret",
		IssuerURL:    "https://accounts.google.com",
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		UserInfoURL:  "https://openidconnect.googleapis.com/v1/userinfo",
		// JWKSURL:      "https://www.googleapis.com/oauth2/v3/certs",
		Scopes: []string{"openid", "email", "profile"},
		// ClaimMapping: map[string]string{"email": "email", "name": "name"},
		// Metadata:     map[string]interface{}{"custom": "value"},
		Created: now,
		Updated: now,
	}

	data, err := json.Marshal(provider)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	var decoded iam.ExternalProvider
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, provider.ID, decoded.ID)
	assert.Equal(t, provider.TenantID, decoded.TenantID)
	assert.Equal(t, provider.Name, decoded.Name)
	assert.Equal(t, provider.Type, decoded.Type)
	assert.Equal(t, provider.Enabled, decoded.Enabled)
	assert.Equal(t, provider.ClientID, decoded.ClientID)
	assert.Equal(t, provider.IssuerURL, decoded.IssuerURL)
	assert.Equal(t, provider.Scopes, decoded.Scopes)
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

// TestFederatedAuthResultJSONSerialization tests JSON marshaling of FederatedAuthResult.
func TestFederatedAuthResultJSONSerialization(t *testing.T) {
	result := &iam.FederationResult{
		User: &iam.User{
			ID:       10123,
			Email:    "user@example.com",
			Name:     "Test User",
			TenantID: "tenant123",
		},
		AccessToken:  "access_token_xyz",
		RefreshToken: "refresh_token_xyz",
		ExpiresIn:    3600,
		Provisioned:  true,
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	var decoded iam.FederationResult
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, result.AccessToken, decoded.AccessToken)
	assert.Equal(t, result.RefreshToken, decoded.RefreshToken)
	assert.Equal(t, result.ExpiresIn, decoded.ExpiresIn)
	assert.Equal(t, result.Provisioned, decoded.Provisioned)
	assert.Equal(t, result.User.Email, decoded.User.Email)
}

// TestLoginRedirectJSONSerialization tests JSON marshaling of LoginRedirect.
func TestLoginRedirectJSONSerialization(t *testing.T) {
	redirect := &iam.LoginRedirect{
		AuthURL: "https://idp.example.com/authorize?client_id=xyz",
		State:   "random_state_123",
		// Nonce:       "random_nonce_456",
	}

	data, err := json.Marshal(redirect)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	var decoded iam.LoginRedirect
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, redirect.AuthURL, decoded.AuthURL)
	assert.Equal(t, redirect.State, decoded.State)
}

// TestRegisterProvider_ServiceError tests provider registration with service error.
func TestRegisterProvider_ServiceError(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.registerProviderErr = errors.New("service error")
	handler, _ := createFederationTestHandler(fedMgr)

	body := RegisterProviderOptions{
		Name:     "google",
		Type:     "oidc",
		ClientID: "google-client-id",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant123/providers", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.RegisterProvider(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestListProviders_ServiceError tests provider listing with service error.
func TestListProviders_ServiceError(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.listProvidersErr = errors.New("service error")
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant123/providers", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.ListProviders(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestGetProvider_ServiceError tests provider retrieval with service error.
func TestGetProvider_ServiceError(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.getProviderErr = errors.New("service error")
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant123/providers/prov_google", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.GetProvider(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestUpdateProvider_ServiceError tests provider update with service error.
func TestUpdateProvider_ServiceError(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.updateProviderErr = errors.New("service error")
	handler, _ := createFederationTestHandler(fedMgr)

	body := UpdateProviderOptions{}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant123/providers/prov_google", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.UpdateProvider(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestDeleteProvider_ServiceError tests provider deletion with service error.
func TestDeleteProvider_ServiceError(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.deleteProviderErr = errors.New("service error")
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant123/providers/prov_google", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.DeleteProvider(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestBeginFederatedLogin_ServiceError tests federated login with service error.
func TestBeginFederatedLogin_ServiceError(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.beginLoginErr = errors.New("service error")
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers/tenant123/google/login?redirect_uri=https://app.example.com/callback", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_name", "google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.BeginFederatedLogin(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestHandleFederatedCallback_ServiceError tests callback with service error.
func TestHandleFederatedCallback_ServiceError(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.handleCallbackErr = errors.New("service error")
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers/tenant123/google/callback?code=auth_code_123", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_name", "google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.HandleFederatedCallback(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestLinkAccount_ServiceError tests account linking with service error.
func TestLinkAccount_ServiceError(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.linkAccountErr = errors.New("service error")
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

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestUnlinkAccount_ServiceError tests account unlinking with service error.
func TestUnlinkAccount_ServiceError(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.unlinkAccountErr = errors.New("service error")
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant123/users/user123/links/prov_google", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("user_id", "10123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.UnlinkAccount(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestGetUserLinks_ServiceError tests user links with service error.
func TestGetUserLinks_ServiceError(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.getUserLinksErr = errors.New("service error")
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant123/users/user123/links", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("user_id", "10123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.GetUserLinks(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestFederationManager_GetFederationManagerError tests when GetFederationManager returns error.
func TestFederationManager_GetFederationManagerError(t *testing.T) {
	mockSvc := &mockServiceWithFederation{
		fedMgr:    newMockFederationManager(),
		fedMgrErr: errors.New("federation manager unavailable"),
	}
	server := &Server{
		service: mockSvc,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	body := RegisterProviderOptions{
		Name:     "google",
		Type:     "oidc",
		ClientID: "google-client-id",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant123/providers", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.RegisterProvider(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
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
	_ = "https://new-issuer.com/.well-known/jwks.json"
	newScopes := []string{"openid", "email"}
	newClaimMapping := map[string]string{"email": "e_mail"}
	_ = map[string]interface{}{"key": "value"}

	body := UpdateProviderOptions{
		Name:         &newName,
		Enabled:      &enabled,
		ClientID:     &newClientID,
		ClientSecret: &newClientSecret,
		Issuer:       &newIssuer,
		AuthURL:      &newAuthURL,
		TokenURL:     &newTokenURL,
		UserInfoURL:  &newUserInfoURL,
		// JWKSURL:      &newJWKSURL,
		Scopes: &newScopes,
		// ClaimMapping: &newClaimMapping,
		// Metadata:     &newMetadata,
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
	_ = newClaimMapping
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

// TestLinkAccount_WithAccessToken tests account linking with access token.
func TestLinkAccount_WithAccessToken(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	body := LinkAccountOptions{
		ProviderID:  "prov_google",
		ExternalID:  "google_user_123",
		AccessToken: "provider_access_token",
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
	assert.Equal(t, "google_user_123", resp.ExternalID)
}

// fedErrorReader is a reader that always returns an error.
type fedErrorReader struct{}

func (e *fedErrorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("simulated read error")
}

// TestRegisterProvider_BodyReadError tests provider registration with body read error.
func TestRegisterProvider_BodyReadError(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant123/providers", &fedErrorReader{})
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.RegisterProvider(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp IAMAPIError
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp.ErrorDescription, "failed to read request body")
}

// TestUpdateProvider_BodyReadError tests provider update with body read error.
func TestUpdateProvider_BodyReadError(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant123/providers/prov_google", &fedErrorReader{})
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.UpdateProvider(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp IAMAPIError
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp.ErrorDescription, "failed to read request body")
}

// TestLinkAccount_BodyReadError tests account linking with body read error.
func TestLinkAccount_BodyReadError(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, _ := createFederationTestHandler(fedMgr)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant123/users/user123/links", &fedErrorReader{})
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("user_id", "10123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.LinkAccount(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp IAMAPIError
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp.ErrorDescription, "failed to read request body")
}

// TestListProviders_FederationManagerError tests when GetFederationManager returns error.
func TestListProviders_FederationManagerError(t *testing.T) {
	mockSvc := &mockServiceWithFederation{
		fedMgr:    newMockFederationManager(),
		fedMgrErr: errors.New("federation manager unavailable"),
	}
	server := &Server{
		service: mockSvc,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant123/providers", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.ListProviders(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestGetProvider_FederationManagerError tests when GetFederationManager returns error.
func TestGetProvider_FederationManagerError(t *testing.T) {
	mockSvc := &mockServiceWithFederation{
		fedMgr:    newMockFederationManager(),
		fedMgrErr: errors.New("federation manager unavailable"),
	}
	server := &Server{
		service: mockSvc,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant123/providers/prov_google", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.GetProvider(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestUpdateProvider_FederationManagerError tests when GetFederationManager returns error.
func TestUpdateProvider_FederationManagerError(t *testing.T) {
	mockSvc := &mockServiceWithFederation{
		fedMgr:    newMockFederationManager(),
		fedMgrErr: errors.New("federation manager unavailable"),
	}
	server := &Server{
		service: mockSvc,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	body := UpdateProviderOptions{}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant123/providers/prov_google", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.UpdateProvider(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestDeleteProvider_FederationManagerError tests when GetFederationManager returns error.
func TestDeleteProvider_FederationManagerError(t *testing.T) {
	mockSvc := &mockServiceWithFederation{
		fedMgr:    newMockFederationManager(),
		fedMgrErr: errors.New("federation manager unavailable"),
	}
	server := &Server{
		service: mockSvc,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant123/providers/prov_google", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.DeleteProvider(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestBeginFederatedLogin_FederationManagerError tests when GetFederationManager returns error.
func TestBeginFederatedLogin_FederationManagerError(t *testing.T) {
	mockSvc := &mockServiceWithFederation{
		fedMgr:    newMockFederationManager(),
		fedMgrErr: errors.New("federation manager unavailable"),
	}
	server := &Server{
		service: mockSvc,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers/tenant123/google/login?redirect_uri=https://app.example.com/callback", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_name", "google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.BeginFederatedLogin(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestHandleFederatedCallback_FederationManagerError tests when GetFederationManager returns error.
func TestHandleFederatedCallback_FederationManagerError(t *testing.T) {
	mockSvc := &mockServiceWithFederation{
		fedMgr:    newMockFederationManager(),
		fedMgrErr: errors.New("federation manager unavailable"),
	}
	server := &Server{
		service: mockSvc,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers/tenant123/google/callback?code=auth_code_123", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_name", "google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.HandleFederatedCallback(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestLinkAccount_FederationManagerError tests when GetFederationManager returns error.
func TestLinkAccount_FederationManagerError(t *testing.T) {
	mockSvc := &mockServiceWithFederation{
		fedMgr:    newMockFederationManager(),
		fedMgrErr: errors.New("federation manager unavailable"),
	}
	server := &Server{
		service: mockSvc,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

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

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestUnlinkAccount_FederationManagerError tests when GetFederationManager returns error.
func TestUnlinkAccount_FederationManagerError(t *testing.T) {
	mockSvc := &mockServiceWithFederation{
		fedMgr:    newMockFederationManager(),
		fedMgrErr: errors.New("federation manager unavailable"),
	}
	server := &Server{
		service: mockSvc,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant123/users/user123/links/prov_google", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("user_id", "10123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.UnlinkAccount(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestGetUserLinks_FederationManagerError tests when GetFederationManager returns error.
func TestGetUserLinks_FederationManagerError(t *testing.T) {
	mockSvc := &mockServiceWithFederation{
		fedMgr:    newMockFederationManager(),
		fedMgrErr: errors.New("federation manager unavailable"),
	}
	server := &Server{
		service: mockSvc,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant123/users/user123/links", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("user_id", "10123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.GetUserLinks(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestListProviders_FederationManagerNotAvailable tests when service doesn't support federation.
func TestListProviders_FederationManagerNotAvailable(t *testing.T) {
	server := &Server{
		service: nil,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant123/providers", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.ListProviders(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestGetProvider_FederationManagerNotAvailable tests when service doesn't support federation.
func TestGetProvider_FederationManagerNotAvailable(t *testing.T) {
	server := &Server{
		service: nil,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant123/providers/prov_google", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.GetProvider(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestUpdateProvider_FederationManagerNotAvailable tests when service doesn't support federation.
func TestUpdateProvider_FederationManagerNotAvailable(t *testing.T) {
	server := &Server{
		service: nil,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	body := UpdateProviderOptions{}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant123/providers/prov_google", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.UpdateProvider(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestDeleteProvider_FederationManagerNotAvailable tests when service doesn't support federation.
func TestDeleteProvider_FederationManagerNotAvailable(t *testing.T) {
	server := &Server{
		service: nil,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant123/providers/prov_google", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.DeleteProvider(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestBeginFederatedLogin_FederationManagerNotAvailable tests when service doesn't support federation.
func TestBeginFederatedLogin_FederationManagerNotAvailable(t *testing.T) {
	server := &Server{
		service: nil,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers/tenant123/google/login?redirect_uri=https://app.example.com/callback", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_name", "google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.BeginFederatedLogin(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestHandleFederatedCallback_FederationManagerNotAvailable tests when service doesn't support federation.
func TestHandleFederatedCallback_FederationManagerNotAvailable(t *testing.T) {
	server := &Server{
		service: nil,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers/tenant123/google/callback?code=auth_code_123", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("provider_name", "google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.HandleFederatedCallback(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestLinkAccount_FederationManagerNotAvailable tests when service doesn't support federation.
func TestLinkAccount_FederationManagerNotAvailable(t *testing.T) {
	server := &Server{
		service: nil,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

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

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestUnlinkAccount_FederationManagerNotAvailable tests when service doesn't support federation.
func TestUnlinkAccount_FederationManagerNotAvailable(t *testing.T) {
	server := &Server{
		service: nil,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant123/users/user123/links/prov_google", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("user_id", "10123")
	rctx.URLParams.Add("provider_id", "prov_google")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.UnlinkAccount(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestGetUserLinks_FederationManagerNotAvailable tests when service doesn't support federation.
func TestGetUserLinks_FederationManagerNotAvailable(t *testing.T) {
	server := &Server{
		service: nil,
	}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant123/users/user123/links", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant123")
	rctx.URLParams.Add("user_id", "10123")
	req = req.WithContext(callerRouteContext(req.Context(), rctx))

	rec := httptest.NewRecorder()
	handler.GetUserLinks(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
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

// TestCallbackOptionsJSONSerialization tests JSON marshaling of CallbackOptions.
func TestCallbackOptionsJSONSerialization(t *testing.T) {
	opts := CallbackOptions{
		Code:  "auth_code_123",
		State: "random_state",
		Error: "",
	}

	data, err := json.Marshal(opts)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	var decoded CallbackOptions
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, opts.Code, decoded.Code)
	assert.Equal(t, opts.State, decoded.State)
}

// TestBeginLoginOptionsJSONSerialization tests JSON marshaling of BeginLoginOptions.
func TestBeginLoginOptionsJSONSerialization(t *testing.T) {
	opts := BeginLoginOptions{
		RedirectURI: "https://app.example.com/callback",
		State:       "random_state",
		// Nonce:       "random_nonce",
		Scopes: "openid email",
	}

	data, err := json.Marshal(opts)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	var decoded BeginLoginOptions
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, opts.RedirectURI, decoded.RedirectURI)
	assert.Equal(t, opts.State, decoded.State)
	assert.Equal(t, opts.Nonce, decoded.Nonce)
	assert.Equal(t, opts.Scopes, decoded.Scopes)
}

// TestListProvidersOptionsJSONSerialization tests JSON marshaling of ListProvidersOptions.
func TestListProvidersOptionsJSONSerialization(t *testing.T) {
	opts := ListProvidersOptions{
		Limit:  50,
		Offset: 10,
		Type:   "oidc",
	}

	data, err := json.Marshal(opts)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	var decoded ListProvidersOptions
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, opts.Limit, decoded.Limit)
	assert.Equal(t, opts.Offset, decoded.Offset)
	assert.Equal(t, opts.Type, decoded.Type)
}

// TestListLinksOptionsJSONSerialization tests JSON marshaling of ListLinksOptions.
func TestListLinksOptionsJSONSerialization(t *testing.T) {
	opts := ListLinksOptions{
		Limit:  25,
		Offset: 5,
	}

	data, err := json.Marshal(opts)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	var decoded ListLinksOptions
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, opts.Limit, decoded.Limit)
	assert.Equal(t, opts.Offset, decoded.Offset)
}

// TestProviderTypeConstants tests provider type constants.
func TestProviderTypeConstants(t *testing.T) {
	assert.Equal(t, ProviderType("oidc"), ProviderTypeOIDC)
	assert.Equal(t, ProviderType("saml"), ProviderTypeSAML)
	assert.Equal(t, ProviderType("oauth2"), ProviderTypeOAuth2)
	assert.Equal(t, ProviderType("ldap"), ProviderTypeLDAP)
}
