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

package embedded

import (
	"context"
	"reflect"
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// inviteActivationProvider is the structural accessor for the invite
// activation service. It is satisfied by the concrete *quicBaaS, matching
// webAuthnCredentialsProvider.
type inviteActivationProvider interface {
	InviteActivation() service.InviteActivationService
}

// inviteActivationFor resolves the invite activation service. The activation
// logic lives once in pkg/service, so the embedded transport and the REST and
// gRPC servers redeem an invite identically.
func inviteActivationFor(svc service.QuicBaaS) (service.InviteActivationService, error) {
	p, ok := svc.(inviteActivationProvider)
	if !ok {
		return nil, NewServiceNotConfiguredError("IAM.Invite")
	}
	activation := p.InviteActivation()
	if activation == nil {
		return nil, NewServiceNotConfiguredError("IAM.Invite")
	}
	return activation, nil
}

// iamRequestMapField returns the map[string]interface{} field called name on
// req via reflection, mirroring iamRequestField for the FIDO2 credential the
// invite client sends.
func iamRequestMapField(req interface{}, name string) (map[string]interface{}, bool) {
	v := reflect.ValueOf(req)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, false
	}
	f := v.FieldByName(name)
	if !f.IsValid() || f.Kind() != reflect.Map {
		return nil, false
	}
	m, ok := f.Interface().(map[string]interface{})
	return m, ok
}

func handleIAMInviteGet(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	activation, err := inviteActivationFor(svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	token, _ := iamRequestField(req, "Token")
	meta, err := activation.GetInvite(ctx, tenantID, token)
	if err != nil {
		return nil, err
	}
	// The SDK client decodes the REST InviteMetadataResponse shape; return
	// the same json names so both transports share one wire contract.
	return map[string]interface{}{
		"user_id":      strconv.FormatUint(meta.UserID, 10),
		"email":        meta.Email,
		"name":         meta.Name,
		"auth_methods": meta.AuthMethods,
		"expires_at":   meta.ExpiresAt,
	}, nil
}

func handleIAMInviteFIDO2Begin(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	activation, err := inviteActivationFor(svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	token, _ := iamRequestField(req, "Token")
	return activation.ActivateFIDO2Begin(ctx, tenantID, token)
}

func handleIAMInviteFIDO2Finish(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	activation, err := inviteActivationFor(svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	token, _ := iamRequestField(req, "Token")
	credential, _ := iamRequestMapField(req, "Credential")
	result, err := activation.ActivateFIDO2Finish(ctx, tenantID, token, credential)
	if err != nil {
		return nil, err
	}
	return activationResponse(result, "activation successful"), nil
}

func handleIAMInvitePIV(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	activation, err := inviteActivationFor(svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	token, _ := iamRequestField(req, "Token")
	certPEM, _ := iamRequestField(req, "CertPEM")
	result, err := activation.ActivatePIV(ctx, tenantID, token, []byte(certPEM))
	if err != nil {
		return nil, err
	}
	return activationResponse(result, "PIV activation successful"), nil
}

// activationResponse projects an activation onto the REST ActivationResponse
// shape the SDK client decodes.
func activationResponse(a *service.InviteActivation, message string) map[string]interface{} {
	out := map[string]interface{}{
		"user_id": strconv.FormatUint(a.UserID, 10),
		"message": message,
	}
	if a.Warning != "" {
		out["warning"] = a.Warning
	}
	return out
}
