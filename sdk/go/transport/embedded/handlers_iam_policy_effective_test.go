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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// effectivePermsClient builds a policy client over an embedded transport whose
// policy engine authorizes through a countingAuthorizer denying deny.
func effectivePermsClient(t *testing.T, deny string) (iamsdk.PolicyClient, *countingAuthorizer) {
	t.Helper()
	authz := &countingAuthorizer{deny: deny}
	svc := newAuthorizedAdminTestService(t)
	svc.policy = iam.NewPolicyEngine(iam.PolicyEngineOptions{
		Storage:        iam.NewMemoryStorage(),
		RBACAuthorizer: authz,
	})
	return iamsdk.NewPolicyClient(newIAMVerbsTransport(t, svc)), authz
}

func TestEmbeddedPolicyGetEffectivePermissions_DefaultActionsAllowed(t *testing.T) {
	client, authz := effectivePermsClient(t, "doc/2")

	resp, err := client.GetEffectivePermissions(tenantCtx(policyConditionsTenant), policyConditionsTenant,
		&iamsdk.EffectivePermissionsRequest{Subject: iamsdk.Subject{ID: "u1"}, ResourceType: "doc/1"})
	require.NoError(t, err)
	require.Len(t, resp.Permissions, 7)
	for _, p := range resp.Permissions {
		assert.Equal(t, iamsdk.EffectAllow, p.Effect)
		assert.Equal(t, "doc/1", p.ResourcePattern)
	}
	assert.EqualValues(t, 7, authz.calls.Load())
}

func TestEmbeddedPolicyGetEffectivePermissions_ActionPattern(t *testing.T) {
	client, authz := effectivePermsClient(t, "doc/2")

	resp, err := client.GetEffectivePermissions(tenantCtx(policyConditionsTenant), policyConditionsTenant,
		&iamsdk.EffectivePermissionsRequest{Subject: iamsdk.Subject{ID: "u1"}, ResourceType: "doc/1", ActionPattern: "read"})
	require.NoError(t, err)
	require.Len(t, resp.Permissions, 1)
	assert.Equal(t, "read", resp.Permissions[0].Action)
	assert.Equal(t, iamsdk.EffectAllow, resp.Permissions[0].Effect)
	assert.EqualValues(t, 1, authz.calls.Load())
}

func TestEmbeddedPolicyGetEffectivePermissions_DeniedOnlyWithIncludeDenied(t *testing.T) {
	client, _ := effectivePermsClient(t, "doc/2")
	ctx := tenantCtx(policyConditionsTenant)

	resp, err := client.GetEffectivePermissions(ctx, policyConditionsTenant,
		&iamsdk.EffectivePermissionsRequest{Subject: iamsdk.Subject{ID: "u1"}, ResourceType: "doc/2", ActionPattern: "read"})
	require.NoError(t, err)
	assert.Empty(t, resp.Permissions)

	resp, err = client.GetEffectivePermissions(ctx, policyConditionsTenant,
		&iamsdk.EffectivePermissionsRequest{Subject: iamsdk.Subject{ID: "u1"}, ResourceType: "doc/2", ActionPattern: "read",
			IncludeDenied: true})
	require.NoError(t, err)
	require.Len(t, resp.Permissions, 1)
	assert.Equal(t, "read", resp.Permissions[0].Action)
	assert.Equal(t, iamsdk.EffectDeny, resp.Permissions[0].Effect)
}

func TestEmbeddedPolicyGetEffectivePermissions_ResourceTypeRequired(t *testing.T) {
	client, authz := effectivePermsClient(t, "doc/2")

	_, err := client.GetEffectivePermissions(tenantCtx(policyConditionsTenant), policyConditionsTenant,
		&iamsdk.EffectivePermissionsRequest{Subject: iamsdk.Subject{ID: "u1"}})
	require.Error(t, err)
	assert.True(t, errors.IsInvalidInput(err), "got %v", err)
	assert.EqualValues(t, 0, authz.calls.Load())
}

func TestEmbeddedPolicyGetEffectivePermissions_CrossTenantRefused(t *testing.T) {
	client, authz := effectivePermsClient(t, "doc/2")

	_, err := client.GetEffectivePermissions(tenantCtx("other"), policyConditionsTenant,
		&iamsdk.EffectivePermissionsRequest{Subject: iamsdk.Subject{ID: "u1"}, ResourceType: "doc/1"})
	require.Error(t, err)
	assert.EqualValues(t, 0, authz.calls.Load())
}

func TestEmbeddedPolicyGetEffectivePermissions_NotConfigured(t *testing.T) {
	client := iamsdk.NewPolicyClient(newIAMVerbsTransport(t, &mockQuicBaaS{}))

	_, err := client.GetEffectivePermissions(tenantCtx(policyConditionsTenant), policyConditionsTenant,
		&iamsdk.EffectivePermissionsRequest{Subject: iamsdk.Subject{ID: "u1"}, ResourceType: "doc/1"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "IAM.Policy")
}

func TestEmbeddedPolicyGetEffectivePermissions_DispatchKeyRegistered(t *testing.T) {
	_, ok := iamUnaryHandlers["quicbaas.IAM.Policy/GetEffectivePermissions"]
	assert.True(t, ok)
}
