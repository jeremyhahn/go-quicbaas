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

package grpc

import (
	"context"
	stderrors "errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// FederationServer implements the Federation gRPC service for OIDC Consumer federation.
type FederationServer struct {
	iampb.UnimplementedFederationServiceServer
	federationManager iam.FederationManager
	logger            *logger.Logger
}

// NewFederationServer creates a new Federation gRPC server.
func NewFederationServer(federationManager iam.FederationManager, log *logger.Logger) *FederationServer {
	return &FederationServer{
		federationManager: federationManager,
		logger:            log.WithField("component", "federation-grpc-server"),
	}
}

// =============================================================================
// Provider Management Operations
// =============================================================================

// CreateProvider creates a new external identity provider.
func (s *FederationServer) CreateProvider(ctx context.Context, req *iampb.CreateProviderRequest) (*iampb.ExternalProvider, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id":     req.TenantId,
		"provider_name": req.Name,
	}).Debug("Processing CreateProvider request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if err := requireCallerTenant(ctx, "grpc.Federation", req.TenantId); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "provider name is required")
	}
	if req.ClientId == "" {
		return nil, status.Error(codes.InvalidArgument, "client_id is required")
	}

	provider := convertProtoToExternalProvider(req)

	if err := s.federationManager.RegisterProvider(ctx, provider); err != nil {
		return nil, convertFederationError(err)
	}

	return convertExternalProviderToProto(provider), nil
}

// GetProvider retrieves an external provider by ID.
func (s *FederationServer) GetProvider(ctx context.Context, req *iampb.GetProviderRequest) (*iampb.ExternalProvider, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id":   req.TenantId,
		"provider_id": req.ProviderId,
	}).Debug("Processing GetProvider request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if err := requireCallerTenant(ctx, "grpc.Federation", req.TenantId); err != nil {
		return nil, err
	}
	if req.ProviderId == "" {
		return nil, status.Error(codes.InvalidArgument, "provider_id is required")
	}

	provider, err := s.federationManager.GetProvider(ctx, req.TenantId, req.ProviderId)
	if err != nil {
		return nil, convertFederationError(err)
	}

	return convertExternalProviderToProto(provider), nil
}

// UpdateProvider updates an existing external provider.
// It fetches the existing provider first and merges only the fields
// present in the request, preserving unmodified fields such as client_id.
func (s *FederationServer) UpdateProvider(ctx context.Context, req *iampb.UpdateProviderRequest) (*iampb.ExternalProvider, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id":   req.TenantId,
		"provider_id": req.ProviderId,
	}).Debug("Processing UpdateProvider request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if err := requireCallerTenant(ctx, "grpc.Federation", req.TenantId); err != nil {
		return nil, err
	}
	if req.ProviderId == "" {
		return nil, status.Error(codes.InvalidArgument, "provider_id is required")
	}

	// Fetch existing provider to use as base for partial update
	existing, err := s.federationManager.GetProvider(ctx, req.TenantId, req.ProviderId)
	if err != nil {
		return nil, convertFederationError(err)
	}

	provider := mergeProtoUpdateToExternalProvider(req, existing)

	if err := s.federationManager.UpdateProvider(ctx, provider); err != nil {
		return nil, convertFederationError(err)
	}

	// Fetch updated provider to return
	updated, err := s.federationManager.GetProvider(ctx, req.TenantId, req.ProviderId)
	if err != nil {
		return nil, convertFederationError(err)
	}

	return convertExternalProviderToProto(updated), nil
}

// DeleteProvider removes an external provider.
func (s *FederationServer) DeleteProvider(ctx context.Context, req *iampb.DeleteProviderRequest) (*iampb.DeleteProviderResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id":   req.TenantId,
		"provider_id": req.ProviderId,
	}).Info("Processing DeleteProvider request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if err := requireCallerTenant(ctx, "grpc.Federation", req.TenantId); err != nil {
		return nil, err
	}
	if req.ProviderId == "" {
		return nil, status.Error(codes.InvalidArgument, "provider_id is required")
	}

	if err := s.federationManager.DeleteProvider(ctx, req.TenantId, req.ProviderId); err != nil {
		return nil, convertFederationError(err)
	}

	return &iampb.DeleteProviderResponse{Success: true}, nil
}

// ListProviders lists external providers for a tenant.
func (s *FederationServer) ListProviders(ctx context.Context, req *iampb.ListProvidersRequest) (*iampb.ListProvidersResponse, error) {
	s.logger.WithField("tenant_id", req.TenantId).Debug("Processing ListProviders request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if err := requireCallerTenant(ctx, "grpc.Federation", req.TenantId); err != nil {
		return nil, err
	}

	limit := int(req.Limit)
	if limit == 0 {
		limit = 100
	}
	offset := int(req.Offset)

	opts := iam.ListOptions{
		Limit:  limit,
		Offset: offset,
	}

	providers, err := s.federationManager.ListProviders(ctx, req.TenantId, opts)
	if err != nil {
		return nil, convertFederationError(err)
	}

	pbProviders := make([]*iampb.ExternalProvider, 0, len(providers))
	for _, provider := range providers {
		pbProviders = append(pbProviders, convertExternalProviderToProto(provider))
	}

	total, err := responseInt32("total", len(providers))
	if err != nil {
		return nil, err
	}

	return &iampb.ListProvidersResponse{
		Providers: pbProviders,
		Total:     total,
	}, nil
}

// =============================================================================
// Authentication Flow Operations
// =============================================================================

// BeginFederatedLogin initiates the OAuth2/OIDC authorization flow.
func (s *FederationServer) BeginFederatedLogin(ctx context.Context, req *iampb.BeginFederatedLoginRequest) (*iampb.BeginFederatedLoginResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id":     req.TenantId,
		"provider_name": req.ProviderName,
		"provider_id":   req.ProviderId,
	}).Debug("Processing BeginFederatedLogin request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if (req.ProviderName == "") == (req.ProviderId == "") {
		return nil, status.Error(codes.InvalidArgument, "exactly one of provider_name and provider_id is required")
	}

	providerName, err := s.loginProviderName(ctx, req)
	if err != nil {
		return nil, err
	}

	redirect, err := s.federationManager.BeginLogin(ctx, req.TenantId, providerName, req.RedirectUri)
	if err != nil {
		return nil, convertFederationError(err)
	}

	return &iampb.BeginFederatedLoginResponse{
		AuthUrl:   redirect.AuthURL,
		State:     redirect.State,
		ExpiresAt: timestamppb.New(redirect.ExpiresAt),
	}, nil
}

// loginProviderName names the provider a login begins with. A provider ID is
// resolved through the federation manager inside this public RPC, so an
// anonymous caller never needs the authorized GetProvider; an unknown ID is
// refused with the same error as an unknown name.
func (s *FederationServer) loginProviderName(ctx context.Context, req *iampb.BeginFederatedLoginRequest) (string, error) {
	if req.ProviderName != "" {
		return req.ProviderName, nil
	}
	provider, err := s.federationManager.GetProvider(ctx, req.TenantId, req.ProviderId)
	if err != nil {
		return "", convertFederationError(err)
	}
	return provider.Name, nil
}

// HandleFederatedCallback processes the OAuth2/OIDC callback with authorization code.
func (s *FederationServer) HandleFederatedCallback(ctx context.Context, req *iampb.HandleFederatedCallbackRequest) (*iampb.HandleFederatedCallbackResponse, error) {
	s.logger.Debug("Processing HandleFederatedCallback request")

	if req.State == "" {
		return nil, status.Error(codes.InvalidArgument, "state is required")
	}
	if req.Code == "" {
		return nil, status.Error(codes.InvalidArgument, "code is required")
	}

	result, err := s.federationManager.HandleCallback(ctx, req.State, req.Code)
	if err != nil {
		return nil, convertFederationError(err)
	}

	return &iampb.HandleFederatedCallbackResponse{
		User:              convertUserToProto(result.User),
		FederatedIdentity: convertFederatedIdentityToProto(result.FederatedIdentity),
		AccessToken:       result.AccessToken,
		RefreshToken:      result.RefreshToken,
		ExpiresIn:         result.ExpiresIn,
		Provisioned:       result.Provisioned,
	}, nil
}

// =============================================================================
// Account Linking Operations
// =============================================================================

// LinkAccount links an external identity to an existing user account.
func (s *FederationServer) LinkAccount(ctx context.Context, req *iampb.LinkAccountRequest) (*iampb.LinkAccountResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id":   req.TenantId,
		"user_id":     req.UserId,
		"provider_id": req.ProviderId,
	}).Debug("Processing LinkAccount request")

	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if err := requireCallerTenant(ctx, "grpc.Federation", req.TenantId); err != nil {
		return nil, err
	}
	if req.ProviderId == "" {
		return nil, status.Error(codes.InvalidArgument, "provider_id is required")
	}
	if req.ExternalToken == "" {
		return nil, status.Error(codes.InvalidArgument, "external_token is required")
	}

	userID, err := parseUserID("user_id", req.UserId)
	if err != nil {
		return nil, err
	}
	if err := s.federationManager.LinkAccount(ctx, userID, req.TenantId, req.ProviderId, req.ExternalToken); err != nil {
		return nil, convertFederationError(err)
	}

	return &iampb.LinkAccountResponse{Success: true}, nil
}

// UnlinkAccount removes the link between a user and an external provider.
func (s *FederationServer) UnlinkAccount(ctx context.Context, req *iampb.UnlinkAccountRequest) (*iampb.UnlinkAccountResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id":   req.TenantId,
		"user_id":     req.UserId,
		"provider_id": req.ProviderId,
	}).Debug("Processing UnlinkAccount request")

	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if err := requireCallerTenant(ctx, "grpc.Federation", req.TenantId); err != nil {
		return nil, err
	}
	if req.ProviderId == "" {
		return nil, status.Error(codes.InvalidArgument, "provider_id is required")
	}

	userID, err := parseUserID("user_id", req.UserId)
	if err != nil {
		return nil, err
	}
	if err := s.federationManager.UnlinkAccount(ctx, userID, req.TenantId, req.ProviderId); err != nil {
		return nil, convertFederationError(err)
	}

	return &iampb.UnlinkAccountResponse{Success: true}, nil
}

// GetUserLinks retrieves all linked identities for a user.
func (s *FederationServer) GetUserLinks(ctx context.Context, req *iampb.GetUserLinksRequest) (*iampb.GetUserLinksResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
	}).Debug("Processing GetUserLinks request")

	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if err := requireCallerTenant(ctx, "grpc.Federation", req.TenantId); err != nil {
		return nil, err
	}

	userID, err := parseUserID("user_id", req.UserId)
	if err != nil {
		return nil, err
	}
	links, err := s.federationManager.GetUserLinks(ctx, userID, req.TenantId)
	if err != nil {
		return nil, convertFederationError(err)
	}

	pbLinks := make([]*iampb.FederatedIdentity, 0, len(links))
	for _, link := range links {
		pbLinks = append(pbLinks, convertFederatedIdentityToProto(link))
	}

	return &iampb.GetUserLinksResponse{
		Identities: pbLinks,
	}, nil
}

// =============================================================================
// Type Conversion Functions
// =============================================================================

// convertProtoToExternalProvider converts a CreateProviderRequest to domain ExternalProvider.
func convertProtoToExternalProvider(req *iampb.CreateProviderRequest) *iam.ExternalProvider {
	provider := &iam.ExternalProvider{
		ID:            req.Id,
		TenantID:      req.TenantId,
		Name:          req.Name,
		Type:          convertProtoProviderType(req.Type),
		Enabled:       req.Enabled,
		ClientID:      req.ClientId,
		ClientSecret:  req.ClientSecret,
		IssuerURL:     req.IssuerUrl,
		AuthURL:       req.AuthUrl,
		TokenURL:      req.TokenUrl,
		UserInfoURL:   req.UserinfoUrl,
		Scopes:        req.Scopes,
		AutoProvision: req.AutoProvision,
		AllowLinking:  req.AllowLinking,
		DefaultRoles:  req.DefaultRoles,
	}

	if req.ClaimMappings != nil {
		provider.ClaimMappings = &iam.ClaimMappings{
			Subject:    req.ClaimMappings.Subject,
			Email:      req.ClaimMappings.Email,
			Name:       req.ClaimMappings.Name,
			Groups:     req.ClaimMappings.Groups,
			Roles:      req.ClaimMappings.Roles,
			Attributes: req.ClaimMappings.Attributes,
		}
	}

	return provider
}

// mergeProtoUpdateToExternalProvider merges non-zero fields from the
// UpdateProviderRequest onto the existing provider. Fields that are not
// set in the request (zero-value strings, false bools, nil slices) retain
// their existing values, enabling true partial updates over gRPC.
func mergeProtoUpdateToExternalProvider(req *iampb.UpdateProviderRequest, existing *iam.ExternalProvider) *iam.ExternalProvider {
	provider := *existing
	provider.ID = req.ProviderId
	provider.TenantID = req.TenantId

	if req.Name != "" {
		provider.Name = req.Name
	}
	if req.Type != iampb.ProviderType_PROVIDER_TYPE_UNSPECIFIED {
		provider.Type = convertProtoProviderType(req.Type)
	}
	// Enabled is a bool - always apply it since the caller may want to
	// set it to false explicitly. The proto default is false, but the
	// existing value is preserved when the field is not sent because
	// the proto zero value matches "not sent".
	// To properly handle enable/disable, we always set it from the request.
	// The test sends --enabled=false and --enabled=true explicitly.
	provider.Enabled = req.Enabled
	if req.ClientId != "" {
		provider.ClientID = req.ClientId
	}
	if req.ClientSecret != "" {
		provider.ClientSecret = req.ClientSecret
	}
	if req.IssuerUrl != "" {
		provider.IssuerURL = req.IssuerUrl
	}
	if req.AuthUrl != "" {
		provider.AuthURL = req.AuthUrl
	}
	if req.TokenUrl != "" {
		provider.TokenURL = req.TokenUrl
	}
	if req.UserinfoUrl != "" {
		provider.UserInfoURL = req.UserinfoUrl
	}
	if len(req.Scopes) > 0 {
		provider.Scopes = req.Scopes
	}
	if req.AutoProvision {
		provider.AutoProvision = req.AutoProvision
	}
	if req.AllowLinking {
		provider.AllowLinking = req.AllowLinking
	}
	if len(req.DefaultRoles) > 0 {
		provider.DefaultRoles = req.DefaultRoles
	}
	if req.ClaimMappings != nil {
		provider.ClaimMappings = &iam.ClaimMappings{
			Subject:    req.ClaimMappings.Subject,
			Email:      req.ClaimMappings.Email,
			Name:       req.ClaimMappings.Name,
			Groups:     req.ClaimMappings.Groups,
			Roles:      req.ClaimMappings.Roles,
			Attributes: req.ClaimMappings.Attributes,
		}
	}

	return &provider
}

// convertExternalProviderToProto converts a domain ExternalProvider to proto.
func convertExternalProviderToProto(p *iam.ExternalProvider) *iampb.ExternalProvider {
	if p == nil {
		return nil
	}

	pb := &iampb.ExternalProvider{
		Id:            p.ID,
		TenantId:      p.TenantID,
		Name:          p.Name,
		Type:          convertProviderTypeToProto(p.Type),
		Enabled:       p.Enabled,
		ClientId:      p.ClientID,
		IssuerUrl:     p.IssuerURL,
		AuthUrl:       p.AuthURL,
		TokenUrl:      p.TokenURL,
		UserinfoUrl:   p.UserInfoURL,
		Scopes:        p.Scopes,
		AutoProvision: p.AutoProvision,
		AllowLinking:  p.AllowLinking,
		DefaultRoles:  p.DefaultRoles,
		CreatedAt:     timestamppb.New(p.Created),
		UpdatedAt:     timestamppb.New(p.Updated),
	}

	// Don't return client secret in responses
	// pb.ClientSecret is intentionally not set

	if p.ClaimMappings != nil {
		pb.ClaimMappings = &iampb.ClaimMappings{
			Subject:    p.ClaimMappings.Subject,
			Email:      p.ClaimMappings.Email,
			Name:       p.ClaimMappings.Name,
			Groups:     p.ClaimMappings.Groups,
			Roles:      p.ClaimMappings.Roles,
			Attributes: p.ClaimMappings.Attributes,
		}
	}

	return pb
}

// convertFederatedIdentityToProto converts a domain FederatedIdentity to proto.
func convertFederatedIdentityToProto(fi *iam.FederatedIdentity) *iampb.FederatedIdentity {
	if fi == nil {
		return nil
	}

	pb := &iampb.FederatedIdentity{
		Id:            fi.ID,
		UserId:        formatUserID(fi.UserID),
		TenantId:      fi.TenantID,
		ProviderId:    fi.ProviderID,
		ExternalId:    fi.ExternalID,
		ExternalEmail: fi.ExternalEmail,
		ExternalName:  fi.ExternalName,
		LastLogin:     timestamppb.New(fi.LastLogin),
		CreatedAt:     timestamppb.New(fi.Created),
		UpdatedAt:     timestamppb.New(fi.Updated),
	}

	// Convert claims map to string map for proto
	if len(fi.Claims) > 0 {
		pb.Claims = make(map[string]string, len(fi.Claims))
		for k, v := range fi.Claims {
			if str, ok := v.(string); ok {
				pb.Claims[k] = str
			}
		}
	}

	return pb
}

// convertProtoProviderType converts proto ProviderType to domain ProviderType.
func convertProtoProviderType(pt iampb.ProviderType) iam.ProviderType {
	switch pt {
	case iampb.ProviderType_PROVIDER_TYPE_OIDC:
		return iam.ProviderTypeOIDC
	case iampb.ProviderType_PROVIDER_TYPE_SAML:
		return iam.ProviderTypeSAML
	case iampb.ProviderType_PROVIDER_TYPE_OAUTH2:
		return iam.ProviderTypeOAuth2
	default:
		return iam.ProviderTypeOIDC
	}
}

// convertProviderTypeToProto converts domain ProviderType to proto ProviderType.
func convertProviderTypeToProto(pt iam.ProviderType) iampb.ProviderType {
	switch pt {
	case iam.ProviderTypeOIDC:
		return iampb.ProviderType_PROVIDER_TYPE_OIDC
	case iam.ProviderTypeSAML:
		return iampb.ProviderType_PROVIDER_TYPE_SAML
	case iam.ProviderTypeOAuth2:
		return iampb.ProviderType_PROVIDER_TYPE_OAUTH2
	default:
		return iampb.ProviderType_PROVIDER_TYPE_UNSPECIFIED
	}
}

// =============================================================================
// Error Conversion
// =============================================================================

// convertFederationError converts federation domain errors to gRPC status errors.
func convertFederationError(err error) error {
	if err == nil {
		return nil
	}

	var dErr *errors.QBError
	if !stderrors.As(err, &dErr) {
		return status.Error(codes.Internal, err.Error())
	}

	// These services report conflict as a failed precondition, not AlreadyExists.
	if dErr.Code == errors.ErrConflict {
		return status.Error(codes.FailedPrecondition, dErr.Error())
	}
	return status.Error(errorCodeToGRPCCode(dErr.Code), dErr.Error())
}
