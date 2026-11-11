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

package embedded

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/kmstenant"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// kmsTenantCall is one KMS verb sent through the embedded transport with a
// key name or DEK path.
type kmsTenantCall struct {
	service string
	method  string
	id      string
	request func(id string) any
}

func kmsTenantCalls() []kmsTenantCall {
	key, signing, dek := kmstenant.KeyName, kmstenant.SigningKeyName, kmstenant.DEKPath
	const kms, deks = "quicbaas.KMS", "quicbaas.DEK"
	return []kmsTenantCall{
		{kms, "GetKey", key, func(id string) any { return &service.GetKeyRequest{KeyID: id} }},
		{kms, "GetPublicKey", signing, func(id string) any { return &service.GetPublicKeyRequest{KeyID: id} }},
		{kms, "RotateKey", key, func(id string) any { return &service.RotateKeyRequest{KeyID: id} }},
		{kms, "RevokeKey", key, func(id string) any { return &service.RevokeKeyRequest{KeyID: id} }},
		{kms, "DeleteKey", key, func(id string) any { return &service.DeleteKeyRequest{KeyID: id, Force: true} }},
		{kms, "GetKeyMetadata", key, func(id string) any { return &service.GetKeyMetadataRequest{KeyID: id} }},
		{kms, "UpdateKeyMetadata", key, func(id string) any {
			return &service.UpdateKeyMetadataRequest{KeyID: id, Tags: map[string]string{"env": "prod"}}
		}},
		{kms, "UpdateKeyStatus", key, func(id string) any {
			return &service.UpdateKeyStatusRequest{KeyID: id, Status: service.KeyStatusDisabled}
		}},
		{kms, "ListKeyVersions", key, func(id string) any { return &service.ListKeyVersionsRequest{KeyID: id} }},
		{kms, "SetKeyPolicy", key, func(id string) any {
			return &service.SetKeyPolicyRequest{KeyID: id, Policy: &service.KeyPolicy{
				AllowedOperations: []service.KeyUsage{service.KeyUsageEncrypt},
			}}
		}},
		{kms, "GetKeyPolicy", key, func(id string) any { return &service.GetKeyPolicyRequest{KeyID: id} }},
		{kms, "SetKeyRotationPolicy", key, func(id string) any {
			return &service.SetKeyRotationPolicyRequest{KeyID: id, Policy: &service.RotationPolicy{
				Enabled: true, AutoRotate: true, Interval: time.Hour,
			}}
		}},
		{kms, "GetKeyRotationPolicy", key, func(id string) any { return &service.GetKeyRotationPolicyRequest{KeyID: id} }},
		{kms, "DeleteKeyRotationPolicy", key, func(id string) any { return &service.DeleteKeyRotationPolicyRequest{KeyID: id} }},
		{kms, "SetKeySafety", key, func(id string) any { return &service.SetKeySafetyRequest{KeyID: id, MaxOperations: 1000} }},
		{kms, "ClearKeySafety", key, func(id string) any { return &service.ClearKeySafetyRequest{KeyID: id} }},
		{deks, "GetDEK", dek, func(id string) any { return &service.GetDEKRequest{Path: id} }},
		{deks, "RotateDEK", dek, func(id string) any { return &service.RotateDEKRequest{Path: id} }},
		{deks, "DeleteDEK", dek, func(id string) any { return &service.DeleteDEKRequest{Path: id, Force: true} }},
		{deks, "GetDEKStatus", dek, func(id string) any { return &service.GetDEKStatusRequest{Path: id} }},
		{deks, "SetDefaultDEK", dek, func(id string) any { return &service.SetDefaultDEKRequest{Path: id} }},
		{deks, "SetDEKSafety", dek, func(id string) any { return &service.SetDEKSafetyRequest{Path: id, MaxOperations: 1000} }},
		{deks, "ClearDEKSafety", dek, func(id string) any { return &service.ClearDEKSafetyRequest{Path: id} }},
	}
}

// TestKMSHandlers_TenantBGetsNotFoundForTenantAKey pins #1476 on the
// embedded transport: the tenant the caller names with
// transport.ContextWithTenant reaches the KMS service, so tenant B naming
// tenant A's key or DEK gets the same not-found a missing id gets and leaves
// tenant A's state unchanged, while tenant A still reaches its own.
func TestKMSHandlers_TenantBGetsNotFoundForTenantAKey(t *testing.T) {
	cases := []struct {
		name      string
		tenant    string
		missingID bool
		refused   bool
	}{
		{name: "tenant A reaches its own", tenant: kmstenant.OwnerTenant},
		{name: "tenant B gets not found for tenant A's id", tenant: kmstenant.OtherTenant, refused: true},
		{name: "tenant A gets not found for a missing id", tenant: kmstenant.OwnerTenant, missingID: true, refused: true},
	}

	for _, call := range kmsTenantCalls() {
		for _, tc := range cases {
			t.Run(call.method+"/"+tc.name, func(t *testing.T) {
				svc := kmstenant.NewService(t)
				tr, err := New(&mockQuicBaaS{kmsService: svc})
				require.NoError(t, err)
				ctx := auth.SubjectIntoContext(transport.ContextWithTenant(context.Background(), tc.tenant),
					interfaces.Subject{ID: "caller", TenantID: tc.tenant, Authenticated: true, Attributes: map[string]interface{}{}})
				id := call.id
				if tc.missingID {
					id = kmstenant.MissingID
				}
				before := kmstenant.ReadOwnerState(t, svc)

				_, err = tr.Request(ctx, call.service, call.method, call.request(id))

				if !tc.refused {
					require.NoError(t, err)
					return
				}
				require.Equal(t, sdkerrors.CodeNotFound, sdkerrors.Code(err), "error: %v", err)
				require.Equal(t, before, kmstenant.ReadOwnerState(t, svc), "a refused call changed tenant A's state")
			})
		}
	}
}
