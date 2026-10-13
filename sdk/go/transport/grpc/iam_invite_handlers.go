// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"encoding/json"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// handleInviteRequest routes the public invite activation verbs onto the IAM
// gRPC service. The FIDO2 options and credential travel as JSON strings, the
// form the browser's WebAuthn API produces and consumes.
func (h *iamHandler) handleInviteRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	const op = "iamHandler.handleInviteRequest"

	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	tenantID, _ := getString(reqMap, "tenant_id")
	if tenantID == "" {
		return nil, errors.NewInvalidInput(op, "tenant_id is required")
	}
	token, _ := getString(reqMap, "token")
	if token == "" {
		return nil, errors.NewInvalidInput(op, "token is required")
	}
	client := iampb.NewIAMServiceClient(h.conn)

	switch method {
	case "GetInvite":
		resp, err := client.GetInvite(ctx, &iampb.GetInviteRequest{TenantId: tenantID, Token: token})
		if err != nil {
			return nil, convertGRPCError(err, op)
		}
		return map[string]interface{}{
			"user_id":      resp.UserId,
			"email":        resp.Email,
			"name":         resp.Name,
			"auth_methods": resp.AuthMethods,
			"expires_at":   resp.ExpiresAt.AsTime(),
		}, nil

	case "ActivateFIDO2Begin":
		resp, err := client.ActivateFIDO2Begin(ctx, &iampb.ActivateInviteRequest{TenantId: tenantID, Token: token})
		if err != nil {
			return nil, convertGRPCError(err, op)
		}
		var options map[string]interface{}
		if err := json.Unmarshal([]byte(resp.OptionsJson), &options); err != nil {
			return nil, errors.NewInternal(op, "server returned malformed credential creation options")
		}
		return options, nil

	case "ActivateFIDO2Finish":
		credentialJSON, err := json.Marshal(reqMap["credential"])
		if err != nil {
			return nil, errors.NewInvalidInput(op, "credential is not JSON-encodable")
		}
		resp, err := client.ActivateFIDO2Finish(ctx, &iampb.ActivateFIDO2FinishRequest{
			TenantId: tenantID, Token: token, CredentialJson: string(credentialJSON),
		})
		if err != nil {
			return nil, convertGRPCError(err, op)
		}
		return activationFromProto(resp), nil

	case "ActivatePIV":
		certPEM, _ := getString(reqMap, "cert_pem")
		resp, err := client.ActivatePIV(ctx, &iampb.ActivatePIVRequest{
			TenantId: tenantID, Token: token, CertPem: certPEM,
		})
		if err != nil {
			return nil, convertGRPCError(err, op)
		}
		return activationFromProto(resp), nil

	default:
		return nil, errors.NewUnimplemented(op, "unsupported method: "+method)
	}
}

// activationFromProto projects an activation result onto the JSON shape the
// SDK invite client decodes.
func activationFromProto(resp *iampb.ActivateInviteResponse) map[string]interface{} {
	return map[string]interface{}{
		"user_id": resp.UserId,
		"message": resp.Message,
		"warning": resp.Warning,
	}
}
