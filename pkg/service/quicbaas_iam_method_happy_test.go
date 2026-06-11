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

package service

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// iamServiceWrapper App + Policy happy paths.
//
// The backend-resolution guards for these methods are covered elsewhere; this
// file drives the success branch of every App and Policy verb against a real
// MemoryStorage-backed AppManager / PolicyEngine so the response-mapping code
// (which previously sat at 44-55%) is exercised end to end.
// =============================================================================

func newHappyAppProvider() (*mockIAMServiceProvider, *iam.AppManager) {
	storage := iam.NewMemoryStorage()
	am := iam.NewAppManager(storage, nil)
	return &mockIAMServiceProvider{appMgr: am}, am
}

func newHappyPolicyProvider() (*mockIAMServiceProvider, *iam.PolicyEngine) {
	storage := iam.NewMemoryStorage()
	pe := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: storage})
	return &mockIAMServiceProvider{policyEngine: pe}, pe
}

func happyPolicy(tenantID, name string) *iam.Policy {
	return &iam.Policy{
		APIVersion: "iam.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata: iam.PolicyMetadata{
			Name:        name,
			Tenant:      tenantID,
			Description: "happy-path policy",
			Priority:    100,
		},
		Spec: iam.PolicySpec{
			Subjects: iam.SubjectSelector{Authenticated: true},
			Rules: []iam.Rule{
				{
					Name:      "allow-read",
					Effect:    iam.EffectAllow,
					Priority:  100,
					Actions:   []string{"read"},
					Resources: []string{"/test/*"},
				},
			},
		},
	}
}

// registerHappyApp creates an application and returns its client ID.
func registerHappyApp(t *testing.T, w *iamServiceWrapper, tenantID, name string) string {
	t.Helper()
	resp, err := w.RegisterApp(context.Background(), &RegisterAppRequest{
		TenantID:     tenantID,
		Name:         name,
		RedirectURIs: []string{"https://app.example.com/callback"},
		GrantTypes:   []string{iam.GrantTypeAuthorizationCode},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.ClientSecret)
	require.NotNil(t, resp.App)
	return resp.App.ClientID
}

func TestIAMServiceWrapper_AppLifecycle_HappyPath(t *testing.T) {
	provider, _ := newHappyAppProvider()
	w := &iamServiceWrapper{provider: provider}
	ctx := context.Background()
	const tenantID = "tenant-apps"

	clientID := registerHappyApp(t, w, tenantID, "billing-service")

	t.Run("GetApp", func(t *testing.T) {
		resp, err := w.GetApp(ctx, &GetAppRequest{ClientID: clientID})
		require.NoError(t, err)
		require.NotNil(t, resp.App)
		assert.Equal(t, clientID, resp.App.ClientID)
	})

	t.Run("UpdateApp", func(t *testing.T) {
		newName := "billing-service-v2"
		uris := []string{"https://app.example.com/cb2"}
		grants := []string{iam.GrantTypeClientCredentials}
		resp, err := w.UpdateApp(ctx, &UpdateAppRequest{
			ClientID:     clientID,
			Name:         &newName,
			RedirectURIs: &uris,
			GrantTypes:   &grants,
		})
		require.NoError(t, err)
		require.NotNil(t, resp.App)
		assert.Equal(t, newName, resp.App.Name)
	})

	t.Run("ListApps", func(t *testing.T) {
		resp, err := w.ListApps(ctx, &ListAppsRequest{TenantID: tenantID, Limit: 10})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, resp.TotalCount, int64(1))
		assert.NotEmpty(t, resp.Apps)
	})

	t.Run("RotateAppSecret", func(t *testing.T) {
		resp, err := w.RotateAppSecret(ctx, &RotateAppSecretRequest{ClientID: clientID})
		require.NoError(t, err)
		assert.NotEmpty(t, resp.ClientSecret)
	})

	t.Run("DeleteApp", func(t *testing.T) {
		resp, err := w.DeleteApp(ctx, &DeleteAppRequest{ClientID: clientID})
		require.NoError(t, err)
		assert.True(t, resp.Deleted)

		_, err = w.GetApp(ctx, &GetAppRequest{ClientID: clientID})
		require.Error(t, err)
	})
}

// Error paths: invalid input flows through the manager and is propagated.
func TestIAMServiceWrapper_App_BackendErrors(t *testing.T) {
	provider, _ := newHappyAppProvider()
	w := &iamServiceWrapper{provider: provider}
	ctx := context.Background()

	t.Run("RegisterApp_InvalidOptions", func(t *testing.T) {
		// Empty name fails AppManager.validateRegisterOptions.
		_, err := w.RegisterApp(ctx, &RegisterAppRequest{
			TenantID:     "t1",
			RedirectURIs: []string{"https://x/cb"},
			GrantTypes:   []string{iam.GrantTypeAuthorizationCode},
		})
		require.Error(t, err)
	})

	t.Run("GetApp_NotFound", func(t *testing.T) {
		_, err := w.GetApp(ctx, &GetAppRequest{ClientID: "missing"})
		require.Error(t, err)
	})

	t.Run("UpdateApp_NotFound", func(t *testing.T) {
		name := "x"
		_, err := w.UpdateApp(ctx, &UpdateAppRequest{ClientID: "missing", Name: &name})
		require.Error(t, err)
	})

	t.Run("DeleteApp_NotFound", func(t *testing.T) {
		_, err := w.DeleteApp(ctx, &DeleteAppRequest{ClientID: "missing"})
		require.Error(t, err)
	})

	t.Run("RotateAppSecret_NotFound", func(t *testing.T) {
		_, err := w.RotateAppSecret(ctx, &RotateAppSecretRequest{ClientID: "missing"})
		require.Error(t, err)
	})
}

func TestIAMServiceWrapper_PolicyLifecycle_HappyPath(t *testing.T) {
	provider, _ := newHappyPolicyProvider()
	w := &iamServiceWrapper{provider: provider}
	ctx := context.Background()
	const tenantID = "tenant-policies"

	policy := happyPolicy(tenantID, "read-policy")

	t.Run("CreatePolicy", func(t *testing.T) {
		resp, err := w.CreatePolicy(ctx, &CreatePolicyRequest{TenantID: tenantID, Policy: policy})
		require.NoError(t, err)
		require.NotNil(t, resp.Policy)
		assert.Equal(t, "read-policy", resp.Policy.Metadata.Name)
	})

	t.Run("GetPolicy", func(t *testing.T) {
		resp, err := w.GetPolicy(ctx, &GetPolicyRequest{TenantID: tenantID, Name: "read-policy"})
		require.NoError(t, err)
		require.NotNil(t, resp.Policy)
		assert.Equal(t, "read-policy", resp.Policy.Metadata.Name)
	})

	t.Run("UpdatePolicy", func(t *testing.T) {
		updated := happyPolicy(tenantID, "read-policy")
		updated.Metadata.Description = "updated description"
		resp, err := w.UpdatePolicy(ctx, &UpdatePolicyRequest{
			TenantID: tenantID,
			Name:     "read-policy",
			Policy:   updated,
		})
		require.NoError(t, err)
		require.NotNil(t, resp.Policy)
		assert.Equal(t, "updated description", resp.Policy.Metadata.Description)
	})

	t.Run("ListPolicies", func(t *testing.T) {
		resp, err := w.ListPolicies(ctx, &ListPoliciesRequest{TenantID: tenantID, Limit: 10})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, resp.TotalCount, int64(1))
	})

	t.Run("ValidatePolicy_Valid", func(t *testing.T) {
		resp, err := w.ValidatePolicy(ctx, &ValidatePolicyRequest{Policy: happyPolicy(tenantID, "v")})
		require.NoError(t, err)
		assert.True(t, resp.Valid)
		assert.Empty(t, resp.Errors)
	})

	t.Run("ValidatePolicy_Invalid", func(t *testing.T) {
		// Missing rules => Policy.Validate fails; wrapper reports invalid.
		bad := happyPolicy(tenantID, "bad")
		bad.Spec.Rules = nil
		resp, err := w.ValidatePolicy(ctx, &ValidatePolicyRequest{Policy: bad})
		require.NoError(t, err)
		assert.False(t, resp.Valid)
		assert.NotEmpty(t, resp.Errors)
	})

	t.Run("TestPolicy", func(t *testing.T) {
		resp, err := w.TestPolicy(ctx, &TestPolicyRequest{
			Check: &iam.CheckRequest{
				Subject:  iam.Subject{ID: "u1", TenantID: tenantID},
				Action:   "read",
				Resource: "/test/object",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
	})

	t.Run("DeletePolicy", func(t *testing.T) {
		resp, err := w.DeletePolicy(ctx, &DeletePolicyRequest{TenantID: tenantID, Name: "read-policy"})
		require.NoError(t, err)
		assert.True(t, resp.Deleted)
	})
}

func TestIAMServiceWrapper_Policy_BackendErrors(t *testing.T) {
	provider, _ := newHappyPolicyProvider()
	w := &iamServiceWrapper{provider: provider}
	ctx := context.Background()

	t.Run("GetPolicy_NotFound", func(t *testing.T) {
		_, err := w.GetPolicy(ctx, &GetPolicyRequest{TenantID: "t1", Name: "missing"})
		require.Error(t, err)
	})

	t.Run("DeletePolicy_NotFound", func(t *testing.T) {
		_, err := w.DeletePolicy(ctx, &DeletePolicyRequest{TenantID: "t1", Name: "missing"})
		require.Error(t, err)
	})
}
