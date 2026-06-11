//go:build quic

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

package quic

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// =============================================================================
// Mock Federation Service Implementation
// =============================================================================

// mockFederationService implements the federationServiceInterface for testing.
type mockFederationService struct {
	mockService

	// Provider Management
	registerProviderFn func(ctx context.Context, provider *iam.ExternalProvider) error
	getProviderFn      func(ctx context.Context, tenantID, providerID string) (*iam.ExternalProvider, error)
	updateProviderFn   func(ctx context.Context, provider *iam.ExternalProvider) error
	deleteProviderFn   func(ctx context.Context, tenantID, providerID string) error
	listProvidersFn    func(ctx context.Context, tenantID string, opts iam.ListOptions) ([]*iam.ExternalProvider, error)

	// Authentication Flow
	beginLoginFn     func(ctx context.Context, tenantID, providerName, redirectURI string) (*iam.LoginRedirect, error)
	handleCallbackFn func(ctx context.Context, stateToken, authCode string) (*iam.FederationResult, error)

	// Account Linking
	linkAccountFn   func(ctx context.Context, userID, tenantID, providerID, externalToken string) error
	unlinkAccountFn func(ctx context.Context, userID, tenantID, providerID string) error
	getUserLinksFn  func(ctx context.Context, userID, tenantID string) ([]*iam.FederatedIdentity, error)
}

// Provider Management
func (m *mockFederationService) RegisterProvider(ctx context.Context, provider *iam.ExternalProvider) error {
	if m.registerProviderFn != nil {
		return m.registerProviderFn(ctx, provider)
	}
	return nil
}

func (m *mockFederationService) GetProvider(ctx context.Context, tenantID, providerID string) (*iam.ExternalProvider, error) {
	if m.getProviderFn != nil {
		return m.getProviderFn(ctx, tenantID, providerID)
	}
	return &iam.ExternalProvider{
		ID:       providerID,
		TenantID: tenantID,
		Name:     "test-provider",
		Type:     iam.ProviderTypeOIDC,
		Enabled:  true,
	}, nil
}

func (m *mockFederationService) UpdateProvider(ctx context.Context, provider *iam.ExternalProvider) error {
	if m.updateProviderFn != nil {
		return m.updateProviderFn(ctx, provider)
	}
	return nil
}

func (m *mockFederationService) DeleteProvider(ctx context.Context, tenantID, providerID string) error {
	if m.deleteProviderFn != nil {
		return m.deleteProviderFn(ctx, tenantID, providerID)
	}
	return nil
}

func (m *mockFederationService) ListProviders(ctx context.Context, tenantID string, opts iam.ListOptions) ([]*iam.ExternalProvider, error) {
	if m.listProvidersFn != nil {
		return m.listProvidersFn(ctx, tenantID, opts)
	}
	return []*iam.ExternalProvider{
		{
			ID:       "provider-1",
			TenantID: tenantID,
			Name:     "google",
			Type:     iam.ProviderTypeOIDC,
			Enabled:  true,
		},
		{
			ID:       "provider-2",
			TenantID: tenantID,
			Name:     "github",
			Type:     iam.ProviderTypeOAuth2,
			Enabled:  true,
		},
	}, nil
}

// Authentication Flow
func (m *mockFederationService) BeginLogin(ctx context.Context, tenantID, providerName, redirectURI string) (*iam.LoginRedirect, error) {
	if m.beginLoginFn != nil {
		return m.beginLoginFn(ctx, tenantID, providerName, redirectURI)
	}
	return &iam.LoginRedirect{
		AuthURL:   "https://accounts.google.com/o/oauth2/auth?client_id=test",
		State:     "test-state-token",
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}, nil
}

func (m *mockFederationService) HandleCallback(ctx context.Context, stateToken, authCode string) (*iam.FederationResult, error) {
	if m.handleCallbackFn != nil {
		return m.handleCallbackFn(ctx, stateToken, authCode)
	}
	return &iam.FederationResult{
		User: &iam.User{
			ID:       "user-123",
			Email:    "test@example.com",
			Name:     "Test User",
			TenantID: "tenant-1",
		},
		FederatedIdentity: &iam.FederatedIdentity{
			ID:            "identity-1",
			UserID:        "user-123",
			TenantID:      "tenant-1",
			ProviderID:    "provider-1",
			ExternalID:    "external-123",
			ExternalEmail: "test@example.com",
		},
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		ExpiresIn:    3600,
		Provisioned:  false,
	}, nil
}

// Account Linking
func (m *mockFederationService) LinkAccount(ctx context.Context, userID, tenantID, providerID, externalToken string) error {
	if m.linkAccountFn != nil {
		return m.linkAccountFn(ctx, userID, tenantID, providerID, externalToken)
	}
	return nil
}

func (m *mockFederationService) UnlinkAccount(ctx context.Context, userID, tenantID, providerID string) error {
	if m.unlinkAccountFn != nil {
		return m.unlinkAccountFn(ctx, userID, tenantID, providerID)
	}
	return nil
}

func (m *mockFederationService) GetUserLinks(ctx context.Context, userID, tenantID string) ([]*iam.FederatedIdentity, error) {
	if m.getUserLinksFn != nil {
		return m.getUserLinksFn(ctx, userID, tenantID)
	}
	return []*iam.FederatedIdentity{
		{
			ID:            "identity-1",
			UserID:        userID,
			TenantID:      tenantID,
			ProviderID:    "provider-1",
			ExternalID:    "external-123",
			ExternalEmail: "test@example.com",
		},
	}, nil
}

// =============================================================================
// Error-returning Federation Service for error path testing
// =============================================================================

// errorFederationService returns errors for all operations.
type errorFederationService struct {
	mockFederationService
}

var errFederationServiceError = errors.New("federation service error")

func (s *errorFederationService) RegisterProvider(ctx context.Context, provider *iam.ExternalProvider) error {
	return errFederationServiceError
}
func (s *errorFederationService) GetProvider(ctx context.Context, tenantID, providerID string) (*iam.ExternalProvider, error) {
	return nil, errFederationServiceError
}
func (s *errorFederationService) UpdateProvider(ctx context.Context, provider *iam.ExternalProvider) error {
	return errFederationServiceError
}
func (s *errorFederationService) DeleteProvider(ctx context.Context, tenantID, providerID string) error {
	return errFederationServiceError
}
func (s *errorFederationService) ListProviders(ctx context.Context, tenantID string, opts iam.ListOptions) ([]*iam.ExternalProvider, error) {
	return nil, errFederationServiceError
}
func (s *errorFederationService) BeginLogin(ctx context.Context, tenantID, providerName, redirectURI string) (*iam.LoginRedirect, error) {
	return nil, errFederationServiceError
}
func (s *errorFederationService) HandleCallback(ctx context.Context, stateToken, authCode string) (*iam.FederationResult, error) {
	return nil, errFederationServiceError
}
func (s *errorFederationService) LinkAccount(ctx context.Context, userID, tenantID, providerID, externalToken string) error {
	return errFederationServiceError
}
func (s *errorFederationService) UnlinkAccount(ctx context.Context, userID, tenantID, providerID string) error {
	return errFederationServiceError
}
func (s *errorFederationService) GetUserLinks(ctx context.Context, userID, tenantID string) ([]*iam.FederatedIdentity, error) {
	return nil, errFederationServiceError
}

// =============================================================================
// Test Helpers
// =============================================================================

func createFederationTestLogger() *logger.Logger {
	cfg := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "quic-federation-test",
	}
	log, err := logger.New(cfg)
	if err != nil {
		panic(err)
	}
	return log
}

func createFederationTestHandler(svc *mockFederationService) *Handler {
	return NewHandler(svc, nil, nil, nil, createFederationTestLogger())
}

// encodeSubPayload creates an encoded sub-request payload for direct handler method testing.
func encodeSubPayload(payload interface{}) ([]byte, error) {
	return EncodePayload(payload)
}

// =============================================================================
// handleIAMFederationOps Tests
// =============================================================================

func TestHandleIAMFederationOps_UnknownSubOp(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	// Create an IAM sub-request with unknown sub-op
	subReq := IAMSubRequest{
		SubOp:   0xFF,
		Payload: []byte("{}"),
	}
	payload, err := EncodePayload(subReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpIAMFederation,
		Payload: payload,
	}

	resp := handler.handleIAMFederationOps(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status for unknown sub-operation")
	}
}

func TestHandleIAMFederationOps_DecodeError(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	req := &Request{
		Opcode:  OpIAMFederation,
		Payload: []byte("invalid payload"),
	}

	resp := handler.handleIAMFederationOps(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status for invalid payload")
	}
}

// =============================================================================
// handleFederationCreateProvider Tests
// =============================================================================

func TestHandleFederationCreateProvider_Success(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	provider := &iam.ExternalProvider{
		ID:        "provider-1",
		TenantID:  "tenant-1",
		Name:      "google",
		Type:      iam.ProviderTypeOIDC,
		ClientID:  "client-123",
		IssuerURL: "https://accounts.google.com",
		Enabled:   true,
	}

	createReq := &CreateProviderRequest{Provider: provider}
	payload, err := encodeSubPayload(createReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationCreateProvider,
		Payload: payload,
	}

	resp := handler.handleFederationCreateProvider(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d, payload: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleFederationCreateProvider_NilService(t *testing.T) {
	// Use base mockService which doesn't implement federationServiceInterface
	handler := NewHandler(&mockService{}, nil, nil, nil, createFederationTestLogger())

	provider := &iam.ExternalProvider{
		ID:       "provider-1",
		TenantID: "tenant-1",
		Name:     "google",
	}

	createReq := &CreateProviderRequest{Provider: provider}
	payload, err := encodeSubPayload(createReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationCreateProvider,
		Payload: payload,
	}

	resp := handler.handleFederationCreateProvider(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when federation service is not available")
	}
}

func TestHandleFederationCreateProvider_DecodeError(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	req := &Request{
		Opcode:  SubOpFederationCreateProvider,
		Payload: []byte("invalid json"),
	}

	resp := handler.handleFederationCreateProvider(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status for invalid payload")
	}
}

func TestHandleFederationCreateProvider_NilProvider(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	createReq := &CreateProviderRequest{Provider: nil}
	payload, err := encodeSubPayload(createReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationCreateProvider,
		Payload: payload,
	}

	resp := handler.handleFederationCreateProvider(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status for nil provider")
	}
}

func TestHandleFederationCreateProvider_ServiceError(t *testing.T) {
	svc := &errorFederationService{}
	handler := createFederationTestHandler(&svc.mockFederationService)
	handler.service = svc

	provider := &iam.ExternalProvider{
		ID:       "provider-1",
		TenantID: "tenant-1",
		Name:     "google",
	}

	createReq := &CreateProviderRequest{Provider: provider}
	payload, err := encodeSubPayload(createReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationCreateProvider,
		Payload: payload,
	}

	resp := handler.handleFederationCreateProvider(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when service returns error")
	}
}

// =============================================================================
// handleFederationGetProvider Tests
// =============================================================================

func TestHandleFederationGetProvider_Success(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	getReq := &GetProviderRequest{
		TenantID:   "tenant-1",
		ProviderID: "provider-1",
	}

	payload, err := encodeSubPayload(getReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationGetProvider,
		Payload: payload,
	}

	resp := handler.handleFederationGetProvider(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleFederationGetProvider_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createFederationTestLogger())

	getReq := &GetProviderRequest{
		TenantID:   "tenant-1",
		ProviderID: "provider-1",
	}

	payload, err := encodeSubPayload(getReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationGetProvider,
		Payload: payload,
	}

	resp := handler.handleFederationGetProvider(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when federation service is not available")
	}
}

func TestHandleFederationGetProvider_DecodeError(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	req := &Request{
		Opcode:  SubOpFederationGetProvider,
		Payload: []byte("not valid json{"),
	}

	resp := handler.handleFederationGetProvider(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status for invalid payload")
	}
}

func TestHandleFederationGetProvider_ServiceError(t *testing.T) {
	svc := &errorFederationService{}
	handler := createFederationTestHandler(&svc.mockFederationService)
	handler.service = svc

	getReq := &GetProviderRequest{
		TenantID:   "tenant-1",
		ProviderID: "provider-1",
	}

	payload, err := encodeSubPayload(getReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationGetProvider,
		Payload: payload,
	}

	resp := handler.handleFederationGetProvider(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when service returns error")
	}
}

// =============================================================================
// handleFederationUpdateProvider Tests
// =============================================================================

func TestHandleFederationUpdateProvider_Success(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	provider := &iam.ExternalProvider{
		ID:        "provider-1",
		TenantID:  "tenant-1",
		Name:      "google-updated",
		Type:      iam.ProviderTypeOIDC,
		ClientID:  "client-456",
		IssuerURL: "https://accounts.google.com",
		Enabled:   false,
	}

	updateReq := &UpdateProviderRequest{Provider: provider}
	payload, err := encodeSubPayload(updateReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationUpdateProvider,
		Payload: payload,
	}

	resp := handler.handleFederationUpdateProvider(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleFederationUpdateProvider_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createFederationTestLogger())

	provider := &iam.ExternalProvider{
		ID:       "provider-1",
		TenantID: "tenant-1",
		Name:     "google-updated",
	}

	updateReq := &UpdateProviderRequest{Provider: provider}
	payload, err := encodeSubPayload(updateReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationUpdateProvider,
		Payload: payload,
	}

	resp := handler.handleFederationUpdateProvider(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when federation service is not available")
	}
}

func TestHandleFederationUpdateProvider_DecodeError(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	req := &Request{
		Opcode:  SubOpFederationUpdateProvider,
		Payload: []byte("invalid json"),
	}

	resp := handler.handleFederationUpdateProvider(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status for invalid payload")
	}
}

func TestHandleFederationUpdateProvider_NilProvider(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	updateReq := &UpdateProviderRequest{Provider: nil}
	payload, err := encodeSubPayload(updateReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationUpdateProvider,
		Payload: payload,
	}

	resp := handler.handleFederationUpdateProvider(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status for nil provider")
	}
}

func TestHandleFederationUpdateProvider_ServiceError(t *testing.T) {
	svc := &errorFederationService{}
	handler := createFederationTestHandler(&svc.mockFederationService)
	handler.service = svc

	provider := &iam.ExternalProvider{
		ID:       "provider-1",
		TenantID: "tenant-1",
		Name:     "google-updated",
	}

	updateReq := &UpdateProviderRequest{Provider: provider}
	payload, err := encodeSubPayload(updateReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationUpdateProvider,
		Payload: payload,
	}

	resp := handler.handleFederationUpdateProvider(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when service returns error")
	}
}

// =============================================================================
// handleFederationDeleteProvider Tests
// =============================================================================

func TestHandleFederationDeleteProvider_Success(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	deleteReq := &DeleteProviderRequest{
		TenantID:   "tenant-1",
		ProviderID: "provider-1",
	}

	payload, err := encodeSubPayload(deleteReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationDeleteProvider,
		Payload: payload,
	}

	resp := handler.handleFederationDeleteProvider(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleFederationDeleteProvider_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createFederationTestLogger())

	deleteReq := &DeleteProviderRequest{
		TenantID:   "tenant-1",
		ProviderID: "provider-1",
	}

	payload, err := encodeSubPayload(deleteReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationDeleteProvider,
		Payload: payload,
	}

	resp := handler.handleFederationDeleteProvider(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when federation service is not available")
	}
}

func TestHandleFederationDeleteProvider_DecodeError(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	req := &Request{
		Opcode:  SubOpFederationDeleteProvider,
		Payload: []byte("invalid json"),
	}

	resp := handler.handleFederationDeleteProvider(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status for invalid payload")
	}
}

func TestHandleFederationDeleteProvider_ServiceError(t *testing.T) {
	svc := &errorFederationService{}
	handler := createFederationTestHandler(&svc.mockFederationService)
	handler.service = svc

	deleteReq := &DeleteProviderRequest{
		TenantID:   "tenant-1",
		ProviderID: "provider-1",
	}

	payload, err := encodeSubPayload(deleteReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationDeleteProvider,
		Payload: payload,
	}

	resp := handler.handleFederationDeleteProvider(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when service returns error")
	}
}

// =============================================================================
// handleFederationListProviders Tests
// =============================================================================

func TestHandleFederationListProviders_Success(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	listReq := &ListProvidersRequest{
		TenantID: "tenant-1",
		Options: iam.ListOptions{
			Limit:  10,
			Offset: 0,
		},
	}

	payload, err := encodeSubPayload(listReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationListProviders,
		Payload: payload,
	}

	resp := handler.handleFederationListProviders(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}

	// Verify response can be decoded
	var listResp ListProvidersResponse
	if err := DecodePayload(resp.Payload, &listResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(listResp.Providers) != 2 {
		t.Errorf("expected 2 providers, got %d", len(listResp.Providers))
	}
}

func TestHandleFederationListProviders_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createFederationTestLogger())

	listReq := &ListProvidersRequest{
		TenantID: "tenant-1",
	}

	payload, err := encodeSubPayload(listReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationListProviders,
		Payload: payload,
	}

	resp := handler.handleFederationListProviders(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when federation service is not available")
	}
}

func TestHandleFederationListProviders_DecodeError(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	req := &Request{
		Opcode:  SubOpFederationListProviders,
		Payload: []byte("invalid json"),
	}

	resp := handler.handleFederationListProviders(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status for invalid payload")
	}
}

func TestHandleFederationListProviders_ServiceError(t *testing.T) {
	svc := &errorFederationService{}
	handler := createFederationTestHandler(&svc.mockFederationService)
	handler.service = svc

	listReq := &ListProvidersRequest{
		TenantID: "tenant-1",
	}

	payload, err := encodeSubPayload(listReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationListProviders,
		Payload: payload,
	}

	resp := handler.handleFederationListProviders(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when service returns error")
	}
}

// =============================================================================
// handleFederationBeginLogin Tests
// =============================================================================

func TestHandleFederationBeginLogin_Success(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	loginReq := &BeginLoginRequest{
		TenantID:     "tenant-1",
		ProviderName: "google",
		RedirectURI:  "https://app.example.com/callback",
	}

	payload, err := encodeSubPayload(loginReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationBeginLogin,
		Payload: payload,
	}

	resp := handler.handleFederationBeginLogin(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}

	// Verify response can be decoded
	var loginResp iam.LoginRedirect
	if err := DecodePayload(resp.Payload, &loginResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if loginResp.AuthURL == "" {
		t.Error("expected non-empty auth URL")
	}
	if loginResp.State == "" {
		t.Error("expected non-empty state token")
	}
}

func TestHandleFederationBeginLogin_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createFederationTestLogger())

	loginReq := &BeginLoginRequest{
		TenantID:     "tenant-1",
		ProviderName: "google",
		RedirectURI:  "https://app.example.com/callback",
	}

	payload, err := encodeSubPayload(loginReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationBeginLogin,
		Payload: payload,
	}

	resp := handler.handleFederationBeginLogin(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when federation service is not available")
	}
}

func TestHandleFederationBeginLogin_DecodeError(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	req := &Request{
		Opcode:  SubOpFederationBeginLogin,
		Payload: []byte("invalid json"),
	}

	resp := handler.handleFederationBeginLogin(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status for invalid payload")
	}
}

func TestHandleFederationBeginLogin_ServiceError(t *testing.T) {
	svc := &errorFederationService{}
	handler := createFederationTestHandler(&svc.mockFederationService)
	handler.service = svc

	loginReq := &BeginLoginRequest{
		TenantID:     "tenant-1",
		ProviderName: "google",
		RedirectURI:  "https://app.example.com/callback",
	}

	payload, err := encodeSubPayload(loginReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationBeginLogin,
		Payload: payload,
	}

	resp := handler.handleFederationBeginLogin(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when service returns error")
	}
}

// =============================================================================
// handleFederationHandleCallback Tests
// =============================================================================

func TestHandleFederationHandleCallback_Success(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	callbackReq := &HandleCallbackRequest{
		StateToken: "test-state-token",
		AuthCode:   "auth-code-123",
	}

	payload, err := encodeSubPayload(callbackReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationHandleCallback,
		Payload: payload,
	}

	resp := handler.handleFederationHandleCallback(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}

	// Verify response can be decoded
	var callbackResp iam.FederationResult
	if err := DecodePayload(resp.Payload, &callbackResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if callbackResp.User == nil {
		t.Error("expected non-nil user")
	}
	if callbackResp.AccessToken == "" {
		t.Error("expected non-empty access token")
	}
}

func TestHandleFederationHandleCallback_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createFederationTestLogger())

	callbackReq := &HandleCallbackRequest{
		StateToken: "test-state-token",
		AuthCode:   "auth-code-123",
	}

	payload, err := encodeSubPayload(callbackReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationHandleCallback,
		Payload: payload,
	}

	resp := handler.handleFederationHandleCallback(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when federation service is not available")
	}
}

func TestHandleFederationHandleCallback_DecodeError(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	req := &Request{
		Opcode:  SubOpFederationHandleCallback,
		Payload: []byte("invalid json"),
	}

	resp := handler.handleFederationHandleCallback(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status for invalid payload")
	}
}

func TestHandleFederationHandleCallback_ServiceError(t *testing.T) {
	svc := &errorFederationService{}
	handler := createFederationTestHandler(&svc.mockFederationService)
	handler.service = svc

	callbackReq := &HandleCallbackRequest{
		StateToken: "test-state-token",
		AuthCode:   "auth-code-123",
	}

	payload, err := encodeSubPayload(callbackReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationHandleCallback,
		Payload: payload,
	}

	resp := handler.handleFederationHandleCallback(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when service returns error")
	}
}

// =============================================================================
// handleFederationLinkAccount Tests
// =============================================================================

func TestHandleFederationLinkAccount_Success(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	linkReq := &LinkAccountRequest{
		UserID:        "user-123",
		TenantID:      "tenant-1",
		ProviderID:    "provider-1",
		ExternalToken: "external-token-abc",
	}

	payload, err := encodeSubPayload(linkReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationLinkAccount,
		Payload: payload,
	}

	resp := handler.handleFederationLinkAccount(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleFederationLinkAccount_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createFederationTestLogger())

	linkReq := &LinkAccountRequest{
		UserID:        "user-123",
		TenantID:      "tenant-1",
		ProviderID:    "provider-1",
		ExternalToken: "external-token-abc",
	}

	payload, err := encodeSubPayload(linkReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationLinkAccount,
		Payload: payload,
	}

	resp := handler.handleFederationLinkAccount(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when federation service is not available")
	}
}

func TestHandleFederationLinkAccount_DecodeError(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	req := &Request{
		Opcode:  SubOpFederationLinkAccount,
		Payload: []byte("invalid json"),
	}

	resp := handler.handleFederationLinkAccount(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status for invalid payload")
	}
}

func TestHandleFederationLinkAccount_ServiceError(t *testing.T) {
	svc := &errorFederationService{}
	handler := createFederationTestHandler(&svc.mockFederationService)
	handler.service = svc

	linkReq := &LinkAccountRequest{
		UserID:        "user-123",
		TenantID:      "tenant-1",
		ProviderID:    "provider-1",
		ExternalToken: "external-token-abc",
	}

	payload, err := encodeSubPayload(linkReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationLinkAccount,
		Payload: payload,
	}

	resp := handler.handleFederationLinkAccount(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when service returns error")
	}
}

// =============================================================================
// handleFederationUnlinkAccount Tests
// =============================================================================

func TestHandleFederationUnlinkAccount_Success(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	unlinkReq := &UnlinkAccountRequest{
		UserID:     "user-123",
		TenantID:   "tenant-1",
		ProviderID: "provider-1",
	}

	payload, err := encodeSubPayload(unlinkReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationUnlinkAccount,
		Payload: payload,
	}

	resp := handler.handleFederationUnlinkAccount(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleFederationUnlinkAccount_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createFederationTestLogger())

	unlinkReq := &UnlinkAccountRequest{
		UserID:     "user-123",
		TenantID:   "tenant-1",
		ProviderID: "provider-1",
	}

	payload, err := encodeSubPayload(unlinkReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationUnlinkAccount,
		Payload: payload,
	}

	resp := handler.handleFederationUnlinkAccount(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when federation service is not available")
	}
}

func TestHandleFederationUnlinkAccount_DecodeError(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	req := &Request{
		Opcode:  SubOpFederationUnlinkAccount,
		Payload: []byte("invalid json"),
	}

	resp := handler.handleFederationUnlinkAccount(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status for invalid payload")
	}
}

func TestHandleFederationUnlinkAccount_ServiceError(t *testing.T) {
	svc := &errorFederationService{}
	handler := createFederationTestHandler(&svc.mockFederationService)
	handler.service = svc

	unlinkReq := &UnlinkAccountRequest{
		UserID:     "user-123",
		TenantID:   "tenant-1",
		ProviderID: "provider-1",
	}

	payload, err := encodeSubPayload(unlinkReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationUnlinkAccount,
		Payload: payload,
	}

	resp := handler.handleFederationUnlinkAccount(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when service returns error")
	}
}

// =============================================================================
// handleFederationGetUserLinks Tests
// =============================================================================

func TestHandleFederationGetUserLinks_Success(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	linksReq := &GetUserLinksRequest{
		UserID:   "user-123",
		TenantID: "tenant-1",
	}

	payload, err := encodeSubPayload(linksReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationGetUserLinks,
		Payload: payload,
	}

	resp := handler.handleFederationGetUserLinks(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}

	// Verify response can be decoded
	var linksResp GetUserLinksResponse
	if err := DecodePayload(resp.Payload, &linksResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(linksResp.Links) != 1 {
		t.Errorf("expected 1 link, got %d", len(linksResp.Links))
	}
}

func TestHandleFederationGetUserLinks_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createFederationTestLogger())

	linksReq := &GetUserLinksRequest{
		UserID:   "user-123",
		TenantID: "tenant-1",
	}

	payload, err := encodeSubPayload(linksReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationGetUserLinks,
		Payload: payload,
	}

	resp := handler.handleFederationGetUserLinks(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when federation service is not available")
	}
}

func TestHandleFederationGetUserLinks_DecodeError(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	req := &Request{
		Opcode:  SubOpFederationGetUserLinks,
		Payload: []byte("invalid json"),
	}

	resp := handler.handleFederationGetUserLinks(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status for invalid payload")
	}
}

func TestHandleFederationGetUserLinks_ServiceError(t *testing.T) {
	svc := &errorFederationService{}
	handler := createFederationTestHandler(&svc.mockFederationService)
	handler.service = svc

	linksReq := &GetUserLinksRequest{
		UserID:   "user-123",
		TenantID: "tenant-1",
	}

	payload, err := encodeSubPayload(linksReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  SubOpFederationGetUserLinks,
		Payload: payload,
	}

	resp := handler.handleFederationGetUserLinks(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error status when service returns error")
	}
}

// =============================================================================
// Integration Test - handleIAMFederationOps routing
// =============================================================================

func TestHandleIAMFederationOps_Routing(t *testing.T) {
	svc := &mockFederationService{}
	handler := createFederationTestHandler(svc)

	tests := []struct {
		name    string
		subOp   byte
		payload interface{}
	}{
		{
			name:  "CreateProvider",
			subOp: SubOpFederationCreateProvider,
			payload: &CreateProviderRequest{
				Provider: &iam.ExternalProvider{ID: "p1", TenantID: "t1", Name: "test"},
			},
		},
		{
			name:    "GetProvider",
			subOp:   SubOpFederationGetProvider,
			payload: &GetProviderRequest{TenantID: "t1", ProviderID: "p1"},
		},
		{
			name:  "UpdateProvider",
			subOp: SubOpFederationUpdateProvider,
			payload: &UpdateProviderRequest{
				Provider: &iam.ExternalProvider{ID: "p1", TenantID: "t1", Name: "updated"},
			},
		},
		{
			name:    "DeleteProvider",
			subOp:   SubOpFederationDeleteProvider,
			payload: &DeleteProviderRequest{TenantID: "t1", ProviderID: "p1"},
		},
		{
			name:    "ListProviders",
			subOp:   SubOpFederationListProviders,
			payload: &ListProvidersRequest{TenantID: "t1"},
		},
		{
			name:    "BeginLogin",
			subOp:   SubOpFederationBeginLogin,
			payload: &BeginLoginRequest{TenantID: "t1", ProviderName: "google", RedirectURI: "http://example.com"},
		},
		{
			name:    "HandleCallback",
			subOp:   SubOpFederationHandleCallback,
			payload: &HandleCallbackRequest{StateToken: "state", AuthCode: "code"},
		},
		{
			name:    "LinkAccount",
			subOp:   SubOpFederationLinkAccount,
			payload: &LinkAccountRequest{UserID: "u1", TenantID: "t1", ProviderID: "p1", ExternalToken: "token"},
		},
		{
			name:    "UnlinkAccount",
			subOp:   SubOpFederationUnlinkAccount,
			payload: &UnlinkAccountRequest{UserID: "u1", TenantID: "t1", ProviderID: "p1"},
		},
		{
			name:    "GetUserLinks",
			subOp:   SubOpFederationGetUserLinks,
			payload: &GetUserLinksRequest{UserID: "u1", TenantID: "t1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payloadBytes, err := json.Marshal(tt.payload)
			if err != nil {
				t.Fatalf("failed to marshal payload: %v", err)
			}

			subReq := IAMSubRequest{
				SubOp:   tt.subOp,
				Payload: payloadBytes,
			}

			payload, err := EncodePayload(subReq)
			if err != nil {
				t.Fatalf("failed to encode sub-request: %v", err)
			}

			req := &Request{
				Opcode:  OpIAMFederation,
				Payload: payload,
			}

			resp := handler.handleIAMFederationOps(context.Background(), req)
			if resp.Status != StatusOK {
				t.Errorf("expected StatusOK, got %d, payload: %s", resp.Status, string(resp.Payload))
			}
		})
	}
}
