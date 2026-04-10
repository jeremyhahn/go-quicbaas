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
	"strings"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// handleFederationRequest handles Federation service requests.
func (h *iamHandler) handleFederationRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	client := iampb.NewFederationServiceClient(h.conn)

	switch method {
	case "CreateProvider":
		return h.handleCreateProvider(ctx, client, req)
	case "GetProvider":
		return h.handleGetProvider(ctx, client, req)
	case "UpdateProvider":
		return h.handleUpdateProvider(ctx, client, req)
	case "DeleteProvider":
		return h.handleDeleteProvider(ctx, client, req)
	case "ListProviders":
		return h.handleListProviders(ctx, client, req)
	default:
		return nil, errors.NewUnimplemented("iamHandler.handleFederationRequest",
			"unsupported method: "+method)
	}
}

// handleCreateProvider handles CreateProvider requests.
func (h *iamHandler) handleCreateProvider(ctx context.Context, client iampb.FederationServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleCreateProvider", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	providerID, _ := getString(reqMap, "provider_id")
	name, _ := getString(reqMap, "name")
	providerType, _ := getString(reqMap, "type")
	clientID, _ := getString(reqMap, "client_id")
	clientSecret, _ := getString(reqMap, "client_secret")
	issuerURL, _ := getString(reqMap, "issuer_url")
	scopes, _ := getStringSlice(reqMap, "scopes")
	autoProvision, _ := getBool(reqMap, "auto_provision")
	allowLinking, _ := getBool(reqMap, "allow_linking")
	defaultRoles, _ := getStringSlice(reqMap, "default_roles")
	defaultGroups, _ := getStringSlice(reqMap, "default_groups")

	pbReq := &iampb.CreateProviderRequest{
		Id:            providerID,
		TenantId:      tenantID,
		Name:          name,
		Type:          providerTypeFromString(providerType),
		ClientId:      clientID,
		ClientSecret:  clientSecret,
		IssuerUrl:     issuerURL,
		Scopes:        scopes,
		AutoProvision: autoProvision,
		AllowLinking:  allowLinking,
		DefaultRoles:  defaultRoles,
		DefaultGroups: defaultGroups,
	}

	resp, err := client.CreateProvider(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleCreateProvider")
	}

	return convertProtoProviderToSDK(resp), nil
}

// handleGetProvider handles GetProvider requests.
func (h *iamHandler) handleGetProvider(ctx context.Context, client iampb.FederationServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleGetProvider", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	providerID, _ := getString(reqMap, "provider_id")

	pbReq := &iampb.GetProviderRequest{
		TenantId:   tenantID,
		ProviderId: providerID,
	}

	resp, err := client.GetProvider(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleGetProvider")
	}

	return convertProtoProviderToSDK(resp), nil
}

// handleUpdateProvider handles UpdateProvider requests.
// Only fields present in the request map are set on the protobuf message,
// allowing partial updates without requiring all fields.
func (h *iamHandler) handleUpdateProvider(ctx context.Context, client iampb.FederationServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleUpdateProvider", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	providerID, _ := getString(reqMap, "provider_id")

	pbReq := &iampb.UpdateProviderRequest{
		TenantId:   tenantID,
		ProviderId: providerID,
	}

	if name, ok := getString(reqMap, "name"); ok {
		pbReq.Name = name
	}
	if providerType, ok := getString(reqMap, "type"); ok {
		pbReq.Type = providerTypeFromString(providerType)
	}
	if enabled, ok := getBool(reqMap, "enabled"); ok {
		pbReq.Enabled = enabled
	}
	if clientID, ok := getString(reqMap, "client_id"); ok {
		pbReq.ClientId = clientID
	}
	if clientSecret, ok := getString(reqMap, "client_secret"); ok {
		pbReq.ClientSecret = clientSecret
	}
	if issuerURL, ok := getString(reqMap, "issuer_url"); ok {
		pbReq.IssuerUrl = issuerURL
	}
	if scopes, ok := getStringSlice(reqMap, "scopes"); ok {
		pbReq.Scopes = scopes
	}
	if autoProvision, ok := getBool(reqMap, "auto_provision"); ok {
		pbReq.AutoProvision = autoProvision
	}
	if allowLinking, ok := getBool(reqMap, "allow_linking"); ok {
		pbReq.AllowLinking = allowLinking
	}
	if defaultRoles, ok := getStringSlice(reqMap, "default_roles"); ok {
		pbReq.DefaultRoles = defaultRoles
	}
	if defaultGroups, ok := getStringSlice(reqMap, "default_groups"); ok {
		pbReq.DefaultGroups = defaultGroups
	}

	resp, err := client.UpdateProvider(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleUpdateProvider")
	}

	return convertProtoProviderToSDK(resp), nil
}

// handleDeleteProvider handles DeleteProvider requests.
func (h *iamHandler) handleDeleteProvider(ctx context.Context, client iampb.FederationServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleDeleteProvider", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	providerID, _ := getString(reqMap, "provider_id")

	pbReq := &iampb.DeleteProviderRequest{
		TenantId:   tenantID,
		ProviderId: providerID,
	}

	_, err := client.DeleteProvider(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleDeleteProvider")
	}

	return nil, nil
}

// handleListProviders handles ListProviders requests.
func (h *iamHandler) handleListProviders(ctx context.Context, client iampb.FederationServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleListProviders", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")

	pbReq := &iampb.ListProvidersRequest{
		TenantId: tenantID,
	}

	resp, err := client.ListProviders(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleListProviders")
	}

	providers := make([]*cli.Provider, 0, len(resp.Providers))
	for _, p := range resp.Providers {
		if converted := convertProtoProviderToSDK(p); converted != nil {
			providers = append(providers, converted)
		}
	}

	return providers, nil
}

// providerTypeFromString converts a string provider type to the protobuf enum.
func providerTypeFromString(s string) iampb.ProviderType {
	key := "PROVIDER_TYPE_" + strings.ToUpper(s)
	if val, ok := iampb.ProviderType_value[key]; ok {
		return iampb.ProviderType(val)
	}
	return iampb.ProviderType_PROVIDER_TYPE_UNSPECIFIED
}

// providerTypeToString converts a protobuf ProviderType enum to a lowercase string.
func providerTypeToString(pt iampb.ProviderType) string {
	switch pt {
	case iampb.ProviderType_PROVIDER_TYPE_OIDC:
		return "oidc"
	case iampb.ProviderType_PROVIDER_TYPE_SAML:
		return "saml"
	case iampb.ProviderType_PROVIDER_TYPE_OAUTH2:
		return "oauth2"
	default:
		return ""
	}
}

// convertProtoProviderToSDK converts a protobuf ExternalProvider to SDK Provider.
func convertProtoProviderToSDK(p *iampb.ExternalProvider) *cli.Provider {
	if p == nil {
		return nil
	}

	resp := &cli.Provider{
		ID:            p.Id,
		TenantID:      p.TenantId,
		Name:          p.Name,
		Type:          providerTypeToString(p.Type),
		Enabled:       p.Enabled,
		ClientID:      p.ClientId,
		IssuerURL:     p.IssuerUrl,
		Scopes:        p.Scopes,
		AutoProvision: p.AutoProvision,
		AllowLinking:  p.AllowLinking,
		DefaultRoles:  p.DefaultRoles,
		DefaultGroups: p.DefaultGroups,
	}

	if p.CreatedAt != nil {
		resp.Created = p.CreatedAt.AsTime()
	}
	if p.UpdatedAt != nil {
		resp.Updated = p.UpdatedAt.AsTime()
	}

	return resp
}
