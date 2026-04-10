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

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// handleIdentityRequest handles Identity service requests.
// Identity operations map to FederationService RPCs (LinkAccount, UnlinkAccount, GetUserLinks).
func (h *iamHandler) handleIdentityRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	client := iampb.NewFederationServiceClient(h.conn)

	switch method {
	case "LinkIdentity":
		return h.handleLinkIdentity(ctx, client, req)
	case "UnlinkIdentity":
		return h.handleUnlinkIdentity(ctx, client, req)
	case "ListUserIdentities", "GetLinkedIdentities":
		return h.handleListUserIdentities(ctx, client, req)
	default:
		return nil, errors.NewUnimplemented("iamHandler.handleIdentityRequest",
			"unsupported method: "+method)
	}
}

// handleLinkIdentity handles LinkIdentity requests by mapping to FederationService.LinkAccount.
func (h *iamHandler) handleLinkIdentity(ctx context.Context, client iampb.FederationServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleLinkIdentity", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")
	providerID, _ := getString(reqMap, "provider_id")
	authCode, _ := getString(reqMap, "auth_code")

	pbReq := &iampb.LinkAccountRequest{
		UserId:        userID,
		TenantId:      tenantID,
		ProviderId:    providerID,
		ExternalToken: authCode,
	}

	resp, err := client.LinkAccount(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleLinkIdentity")
	}

	if !resp.Success {
		return nil, errors.NewInternal("handleLinkIdentity", "link account operation was not successful")
	}

	// After a successful link, retrieve the user's identities to return
	// the newly created federated identity.
	linksResp, err := client.GetUserLinks(ctx, &iampb.GetUserLinksRequest{
		UserId:   userID,
		TenantId: tenantID,
	})
	if err != nil {
		return nil, convertGRPCError(err, "handleLinkIdentity")
	}

	// Find the identity matching the provider that was just linked.
	for _, identity := range linksResp.Identities {
		if identity.ProviderId == providerID {
			return convertProtoFederatedIdentityToSDK(identity), nil
		}
	}

	// If no matching identity found, return a minimal response.
	return &cli.FederatedIdentity{
		UserID:     userID,
		TenantID:   tenantID,
		ProviderID: providerID,
	}, nil
}

// handleUnlinkIdentity handles UnlinkIdentity requests by mapping to FederationService.UnlinkAccount.
func (h *iamHandler) handleUnlinkIdentity(ctx context.Context, client iampb.FederationServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleUnlinkIdentity", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")
	providerID, _ := getString(reqMap, "provider_id")

	pbReq := &iampb.UnlinkAccountRequest{
		UserId:     userID,
		TenantId:   tenantID,
		ProviderId: providerID,
	}

	_, err := client.UnlinkAccount(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleUnlinkIdentity")
	}

	return nil, nil
}

// handleListUserIdentities handles ListUserIdentities requests by mapping to FederationService.GetUserLinks.
func (h *iamHandler) handleListUserIdentities(ctx context.Context, client iampb.FederationServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleListUserIdentities", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")

	pbReq := &iampb.GetUserLinksRequest{
		UserId:   userID,
		TenantId: tenantID,
	}

	resp, err := client.GetUserLinks(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleListUserIdentities")
	}

	identities := make([]*cli.FederatedIdentity, 0, len(resp.Identities))
	for _, identity := range resp.Identities {
		if converted := convertProtoFederatedIdentityToSDK(identity); converted != nil {
			identities = append(identities, converted)
		}
	}

	return identities, nil
}

// convertProtoFederatedIdentityToSDK converts a protobuf FederatedIdentity to SDK FederatedIdentity.
func convertProtoFederatedIdentityToSDK(fi *iampb.FederatedIdentity) *cli.FederatedIdentity {
	if fi == nil {
		return nil
	}

	resp := &cli.FederatedIdentity{
		ID:            fi.Id,
		UserID:        fi.UserId,
		TenantID:      fi.TenantId,
		ProviderID:    fi.ProviderId,
		ExternalID:    fi.ExternalId,
		ExternalEmail: fi.ExternalEmail,
		ExternalName:  fi.ExternalName,
	}

	if fi.CreatedAt != nil {
		resp.Created = fi.CreatedAt.AsTime()
	}
	if fi.LastLogin != nil {
		resp.LastLogin = fi.LastLogin.AsTime()
	}

	return resp
}
