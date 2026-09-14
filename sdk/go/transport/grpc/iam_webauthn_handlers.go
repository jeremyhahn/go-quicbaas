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

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// handleWebAuthnRequest routes WebAuthn credential administration requests onto
// the IAMService RPCs. The Unix-socket transport delegates its Request to this
// handler, so wiring it here covers both transports.
func (h *iamHandler) handleWebAuthnRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	client := iampb.NewIAMServiceClient(h.conn)

	switch method {
	case "ListWebAuthnCredentials", "List":
		return h.handleListWebAuthnCredentials(ctx, client, req)
	case "DeleteWebAuthnCredential", "Delete":
		return h.handleDeleteWebAuthnCredential(ctx, client, req)
	case "DeleteWebAuthnCredentials", "DeleteAll":
		return h.handleDeleteWebAuthnCredentials(ctx, client, req)
	default:
		return nil, errors.NewUnimplemented("iamHandler.handleWebAuthnRequest",
			"unsupported method: "+method)
	}
}

// handleListWebAuthnCredentials lists a user's registered authenticators.
//
// The response is returned as the same map shape the REST transport produces
// ({"credentials": [...], "total": n}) so the SDK client decodes one contract
// regardless of which transport carried the call.
func (h *iamHandler) handleListWebAuthnCredentials(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleListWebAuthnCredentials", "invalid request type")
	}
	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")

	resp, err := client.ListWebAuthnCredentials(ctx, &iampb.ListWebAuthnCredentialsRequest{
		TenantId: tenantID,
		UserId:   userID,
	})
	if err != nil {
		return nil, convertGRPCError(err, "handleListWebAuthnCredentials")
	}

	credentials := make([]map[string]interface{}, 0, len(resp.Credentials))
	for _, c := range resp.Credentials {
		if c == nil {
			continue
		}
		credentials = append(credentials, webAuthnCredentialFromProto(c))
	}
	return map[string]interface{}{
		"credentials": credentials,
		"total":       int(resp.Total),
	}, nil
}

// handleDeleteWebAuthnCredential removes one authenticator by credential id.
func (h *iamHandler) handleDeleteWebAuthnCredential(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleDeleteWebAuthnCredential", "invalid request type")
	}
	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")
	credentialID, _ := getString(reqMap, "credential_id")
	if credentialID == "" {
		return nil, errors.NewInvalidInput("handleDeleteWebAuthnCredential", "credential_id is required")
	}

	resp, err := client.DeleteWebAuthnCredential(ctx, &iampb.DeleteWebAuthnCredentialRequest{
		TenantId:     tenantID,
		UserId:       userID,
		CredentialId: credentialID,
	})
	if err != nil {
		return nil, convertGRPCError(err, "handleDeleteWebAuthnCredential")
	}
	return map[string]interface{}{"success": resp.Success}, nil
}

// handleDeleteWebAuthnCredentials removes every authenticator for a user.
func (h *iamHandler) handleDeleteWebAuthnCredentials(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleDeleteWebAuthnCredentials", "invalid request type")
	}
	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")

	resp, err := client.DeleteWebAuthnCredentials(ctx, &iampb.DeleteWebAuthnCredentialsRequest{
		TenantId: tenantID,
		UserId:   userID,
	})
	if err != nil {
		return nil, convertGRPCError(err, "handleDeleteWebAuthnCredentials")
	}
	return map[string]interface{}{"deleted": int(resp.Deleted)}, nil
}

// webAuthnCredentialFromProto projects the wire message onto the JSON field
// names the SDK client decodes — the json tags of cli.WebAuthnCredential.
func webAuthnCredentialFromProto(c *iampb.WebAuthnCredential) map[string]interface{} {
	m := map[string]interface{}{
		"id":              c.Id,
		"sign_count":      c.SignCount,
		"backup_eligible": c.BackupEligible,
		"backup_state":    c.BackupState,
		"clone_warning":   c.CloneWarning,
	}
	if c.Aaguid != "" {
		m["aaguid"] = c.Aaguid
	}
	if c.AttestationType != "" {
		m["attestation_type"] = c.AttestationType
	}
	if len(c.Transports) > 0 {
		m["transports"] = c.Transports
	}
	return m
}
