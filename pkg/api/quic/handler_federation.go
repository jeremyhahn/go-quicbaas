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

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// federationServiceInterface defines federation operations for type assertion.
type federationServiceInterface interface {
	// Provider Management
	RegisterProvider(ctx context.Context, provider *iam.ExternalProvider) error
	GetProvider(ctx context.Context, tenantID, providerID string) (*iam.ExternalProvider, error)
	UpdateProvider(ctx context.Context, provider *iam.ExternalProvider) error
	DeleteProvider(ctx context.Context, tenantID, providerID string) error
	ListProviders(ctx context.Context, tenantID string, opts iam.ListOptions) ([]*iam.ExternalProvider, error)

	// Authentication Flow
	BeginLogin(ctx context.Context, tenantID, providerName, redirectURI string) (*iam.LoginRedirect, error)
	HandleCallback(ctx context.Context, stateToken, authCode string) (*iam.FederationResult, error)

	// Account Linking
	LinkAccount(ctx context.Context, userID, tenantID, providerID, externalToken string) error
	UnlinkAccount(ctx context.Context, userID, tenantID, providerID string) error
	GetUserLinks(ctx context.Context, userID, tenantID string) ([]*iam.FederatedIdentity, error)
}

// =============================================================================
// Federation Request/Response Types
// =============================================================================

// CreateProviderRequest represents a request to create an external provider.
type CreateProviderRequest struct {
	Provider *iam.ExternalProvider `json:"provider" msgpack:"provider"`
}

// GetProviderRequest represents a request to get an external provider.
type GetProviderRequest struct {
	TenantID   string `json:"tenant_id" msgpack:"tenant_id"`
	ProviderID string `json:"provider_id" msgpack:"provider_id"`
}

// UpdateProviderRequest represents a request to update an external provider.
type UpdateProviderRequest struct {
	Provider *iam.ExternalProvider `json:"provider" msgpack:"provider"`
}

// DeleteProviderRequest represents a request to delete an external provider.
type DeleteProviderRequest struct {
	TenantID   string `json:"tenant_id" msgpack:"tenant_id"`
	ProviderID string `json:"provider_id" msgpack:"provider_id"`
}

// ListProvidersRequest represents a request to list external providers.
type ListProvidersRequest struct {
	TenantID string          `json:"tenant_id" msgpack:"tenant_id"`
	Options  iam.ListOptions `json:"options" msgpack:"options"`
}

// ListProvidersResponse represents a response containing external providers.
type ListProvidersResponse struct {
	Providers []*iam.ExternalProvider `json:"providers" msgpack:"providers"`
}

// BeginLoginRequest represents a request to begin OAuth login flow.
type BeginLoginRequest struct {
	TenantID     string `json:"tenant_id" msgpack:"tenant_id"`
	ProviderName string `json:"provider_name" msgpack:"provider_name"`
	RedirectURI  string `json:"redirect_uri" msgpack:"redirect_uri"`
}

// HandleCallbackRequest represents a request to handle OAuth callback.
type HandleCallbackRequest struct {
	StateToken string `json:"state_token" msgpack:"state_token"`
	AuthCode   string `json:"auth_code" msgpack:"auth_code"`
}

// LinkAccountRequest represents a request to link a user account to an external provider.
type LinkAccountRequest struct {
	UserID        string `json:"user_id" msgpack:"user_id"`
	TenantID      string `json:"tenant_id" msgpack:"tenant_id"`
	ProviderID    string `json:"provider_id" msgpack:"provider_id"`
	ExternalToken string `json:"external_token" msgpack:"external_token"`
}

// UnlinkAccountRequest represents a request to unlink a user account from an external provider.
type UnlinkAccountRequest struct {
	UserID     string `json:"user_id" msgpack:"user_id"`
	TenantID   string `json:"tenant_id" msgpack:"tenant_id"`
	ProviderID string `json:"provider_id" msgpack:"provider_id"`
}

// GetUserLinksRequest represents a request to get a user's linked identities.
type GetUserLinksRequest struct {
	UserID   string `json:"user_id" msgpack:"user_id"`
	TenantID string `json:"tenant_id" msgpack:"tenant_id"`
}

// GetUserLinksResponse represents a response containing federated identities.
type GetUserLinksResponse struct {
	Links []*iam.FederatedIdentity `json:"links" msgpack:"links"`
}

// =============================================================================
// Federation Operations Router
// =============================================================================

// handleIAMFederationOps routes federation sub-operations.
func (h *Handler) handleIAMFederationOps(ctx context.Context, req *Request) *Response {
	var subReq IAMSubRequest
	if err := DecodePayload(req.Payload, &subReq); err != nil {
		return h.errorResponse(err)
	}

	// Create a new request with the sub-payload.
	subRequest := &Request{
		Opcode:  subReq.SubOp,
		Payload: subReq.Payload,
	}

	switch subReq.SubOp {
	// Provider management.
	case SubOpFederationCreateProvider:
		return h.handleFederationCreateProvider(ctx, subRequest)
	case SubOpFederationGetProvider:
		return h.handleFederationGetProvider(ctx, subRequest)
	case SubOpFederationUpdateProvider:
		return h.handleFederationUpdateProvider(ctx, subRequest)
	case SubOpFederationDeleteProvider:
		return h.handleFederationDeleteProvider(ctx, subRequest)
	case SubOpFederationListProviders:
		return h.handleFederationListProviders(ctx, subRequest)

	// Authentication flow.
	case SubOpFederationBeginLogin:
		return h.handleFederationBeginLogin(ctx, subRequest)
	case SubOpFederationHandleCallback:
		return h.handleFederationHandleCallback(ctx, subRequest)

	// Account linking.
	case SubOpFederationLinkAccount:
		return h.handleFederationLinkAccount(ctx, subRequest)
	case SubOpFederationUnlinkAccount:
		return h.handleFederationUnlinkAccount(ctx, subRequest)
	case SubOpFederationGetUserLinks:
		return h.handleFederationGetUserLinks(ctx, subRequest)

	default:
		return h.errorResponse(api.NewUnknownFederationSubOpError(
			"quic.handleIAMFederationOps", subReq.SubOp))
	}
}

// =============================================================================
// Provider Management Handlers
// =============================================================================

func (h *Handler) handleFederationCreateProvider(ctx context.Context, req *Request) *Response {
	fedSvc, ok := h.service.(federationServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("federation"))
	}

	var createReq CreateProviderRequest
	if err := DecodePayload(req.Payload, &createReq); err != nil {
		return h.errorResponse(err)
	}

	if createReq.Provider == nil {
		return h.errorResponse(api.NewRequiredFieldError("provider"))
	}

	if err := fedSvc.RegisterProvider(ctx, createReq.Provider); err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(createReq.Provider)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleFederationGetProvider(ctx context.Context, req *Request) *Response {
	fedSvc, ok := h.service.(federationServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("federation"))
	}

	var getReq GetProviderRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	provider, err := fedSvc.GetProvider(ctx, getReq.TenantID, getReq.ProviderID)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(provider)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleFederationUpdateProvider(ctx context.Context, req *Request) *Response {
	fedSvc, ok := h.service.(federationServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("federation"))
	}

	var updateReq UpdateProviderRequest
	if err := DecodePayload(req.Payload, &updateReq); err != nil {
		return h.errorResponse(err)
	}

	if updateReq.Provider == nil {
		return h.errorResponse(api.NewRequiredFieldError("provider"))
	}

	if err := fedSvc.UpdateProvider(ctx, updateReq.Provider); err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(updateReq.Provider)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleFederationDeleteProvider(ctx context.Context, req *Request) *Response {
	fedSvc, ok := h.service.(federationServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("federation"))
	}

	var deleteReq DeleteProviderRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	if err := fedSvc.DeleteProvider(ctx, deleteReq.TenantID, deleteReq.ProviderID); err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: nil}
}

func (h *Handler) handleFederationListProviders(ctx context.Context, req *Request) *Response {
	fedSvc, ok := h.service.(federationServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("federation"))
	}

	var listReq ListProvidersRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	providers, err := fedSvc.ListProviders(ctx, listReq.TenantID, listReq.Options)
	if err != nil {
		return h.errorResponse(err)
	}

	resp := &ListProvidersResponse{
		Providers: providers,
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Authentication Flow Handlers
// =============================================================================

func (h *Handler) handleFederationBeginLogin(ctx context.Context, req *Request) *Response {
	fedSvc, ok := h.service.(federationServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("federation"))
	}

	var loginReq BeginLoginRequest
	if err := DecodePayload(req.Payload, &loginReq); err != nil {
		return h.errorResponse(err)
	}

	redirect, err := fedSvc.BeginLogin(ctx, loginReq.TenantID, loginReq.ProviderName, loginReq.RedirectURI)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(redirect)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleFederationHandleCallback(ctx context.Context, req *Request) *Response {
	fedSvc, ok := h.service.(federationServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("federation"))
	}

	var callbackReq HandleCallbackRequest
	if err := DecodePayload(req.Payload, &callbackReq); err != nil {
		return h.errorResponse(err)
	}

	result, err := fedSvc.HandleCallback(ctx, callbackReq.StateToken, callbackReq.AuthCode)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(result)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Account Linking Handlers
// =============================================================================

func (h *Handler) handleFederationLinkAccount(ctx context.Context, req *Request) *Response {
	fedSvc, ok := h.service.(federationServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("federation"))
	}

	var linkReq LinkAccountRequest
	if err := DecodePayload(req.Payload, &linkReq); err != nil {
		return h.errorResponse(err)
	}

	if err := fedSvc.LinkAccount(ctx, linkReq.UserID, linkReq.TenantID, linkReq.ProviderID, linkReq.ExternalToken); err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: nil}
}

func (h *Handler) handleFederationUnlinkAccount(ctx context.Context, req *Request) *Response {
	fedSvc, ok := h.service.(federationServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("federation"))
	}

	var unlinkReq UnlinkAccountRequest
	if err := DecodePayload(req.Payload, &unlinkReq); err != nil {
		return h.errorResponse(err)
	}

	if err := fedSvc.UnlinkAccount(ctx, unlinkReq.UserID, unlinkReq.TenantID, unlinkReq.ProviderID); err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: nil}
}

func (h *Handler) handleFederationGetUserLinks(ctx context.Context, req *Request) *Response {
	fedSvc, ok := h.service.(federationServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("federation"))
	}

	var linksReq GetUserLinksRequest
	if err := DecodePayload(req.Payload, &linksReq); err != nil {
		return h.errorResponse(err)
	}

	links, err := fedSvc.GetUserLinks(ctx, linksReq.UserID, linksReq.TenantID)
	if err != nil {
		return h.errorResponse(err)
	}

	resp := &GetUserLinksResponse{
		Links: links,
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
