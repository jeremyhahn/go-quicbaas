//go:build mcp

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

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// policyKMSService embeds testKMSService and keeps the last policy it was
// given, so a set followed by a get round-trips through one store.
type policyKMSService struct {
	*testKMSService
	stored   *service.KeyPolicy
	setCalls int
}

func (s *policyKMSService) SetKeyPolicy(_ context.Context, req *service.SetKeyPolicyRequest) (*service.SetKeyPolicyResponse, error) {
	s.setCalls++
	policy := *req.Policy
	policy.Version = uint64(s.setCalls)
	s.stored = &policy
	return &service.SetKeyPolicyResponse{KeyID: req.KeyID, PolicyVersion: policy.Version, Timestamp: time.Now()}, nil
}

func (s *policyKMSService) GetKeyPolicy(_ context.Context, req *service.GetKeyPolicyRequest) (*service.GetKeyPolicyResponse, error) {
	policy := s.stored
	if policy == nil {
		policy = &service.KeyPolicy{AllowedOperations: []service.KeyUsage{}}
	}
	policy.KeyID = req.KeyID
	return &service.GetKeyPolicyResponse{Policy: policy, Timestamp: time.Now()}, nil
}

func newPolicyKMSService() *policyKMSService {
	return &policyKMSService{testKMSService: &testKMSService{}}
}

func TestExecuteKMSPolicy_RoundTripsEveryField(t *testing.T) {
	svc := newPolicyKMSService()
	r := newRotationRegistry(t, svc)
	expires := time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC)

	_, err := r.executeKMSSetPolicy(context.Background(), map[string]interface{}{
		"key_id": "k1",
		"policy": map[string]interface{}{
			"allowed_operations": []interface{}{"encrypt", "decrypt"},
			"allowed_principals": []interface{}{"svc-a", "svc-b"},
			"denied_principals":  []interface{}{"svc-c"},
			"expires_at":         expires.Format(time.RFC3339),
			"rotation_required":  true,
			"max_usage_count":    float64(100),
			"require_mfa":        true,
		},
	})
	require.NoError(t, err)

	require.NotNil(t, svc.stored)
	assert.Equal(t, []string{"svc-a", "svc-b"}, svc.stored.AllowedPrincipals)
	assert.Equal(t, []string{"svc-c"}, svc.stored.DeniedPrincipals)
	require.NotNil(t, svc.stored.ExpiresAt)
	assert.True(t, expires.Equal(*svc.stored.ExpiresAt))
	require.NotNil(t, svc.stored.MaxUsageCount)
	assert.Equal(t, uint64(100), *svc.stored.MaxUsageCount)

	result, err := r.executeKMSGetPolicy(context.Background(), map[string]interface{}{"key_id": "k1"})
	require.NoError(t, err)
	policy, ok := decodeMarshalled(t, result)["policy"].(map[string]interface{})
	require.True(t, ok, "result carries a policy object")

	assert.Equal(t, "k1", policy["key_id"])
	assert.Equal(t, []interface{}{"encrypt", "decrypt"}, policy["allowed_operations"])
	assert.Equal(t, []interface{}{"svc-a", "svc-b"}, policy["allowed_principals"])
	assert.Equal(t, []interface{}{"svc-c"}, policy["denied_principals"])
	assert.Equal(t, expires.Format(time.RFC3339Nano), policy["expires_at"])
	assert.Equal(t, true, policy["rotation_required"])
	assert.Equal(t, float64(100), policy["max_usage_count"])
	assert.Equal(t, true, policy["require_mfa"])
}

func TestExecuteKMSGetPolicy_UnrestrictedFieldsArePresent(t *testing.T) {
	r := newRotationRegistry(t, newPolicyKMSService())

	result, err := r.executeKMSGetPolicy(context.Background(), map[string]interface{}{"key_id": "k1"})
	require.NoError(t, err)
	policy, ok := decodeMarshalled(t, result)["policy"].(map[string]interface{})
	require.True(t, ok, "result carries a policy object")

	assert.Equal(t, []interface{}{}, policy["allowed_principals"])
	assert.Equal(t, []interface{}{}, policy["denied_principals"])
	expires, present := policy["expires_at"]
	assert.True(t, present, "expires_at is present even when the policy never expires")
	assert.Nil(t, expires)
	maxUsage, present := policy["max_usage_count"]
	assert.True(t, present, "max_usage_count is present even when usage is unlimited")
	assert.Nil(t, maxUsage)
}

func TestExecuteKMSSetPolicy_RejectsMalformedFields(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
		value interface{}
	}{
		{"expires_at not RFC 3339", "expires_at", "next tuesday"},
		{"expires_at not a string", "expires_at", float64(1700000000)},
		{"non-string allowed principal", "allowed_principals", []interface{}{"svc-a", float64(7)}},
		{"non-string denied principal", "denied_principals", []interface{}{true}},
		{"denied_principals not a list", "denied_principals", "svc-a"},
		{"non-string operation", "allowed_operations", []interface{}{float64(1)}},
		{"rotation_required not a bool", "rotation_required", "yes"},
		{"require_mfa not a bool", "require_mfa", float64(1)},
		{"max_usage_count negative", "max_usage_count", float64(-1)},
		{"max_usage_count fractional", "max_usage_count", float64(1.5)},
		{"max_usage_count not a number", "max_usage_count", "ten"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newPolicyKMSService()
			r := newRotationRegistry(t, svc)
			_, err := r.executeKMSSetPolicy(context.Background(), map[string]interface{}{
				"key_id": "k1",
				"policy": map[string]interface{}{
					"allowed_operations": []interface{}{"encrypt"},
					tc.field:             tc.value,
				},
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.field)
			assert.Zero(t, svc.setCalls, "a malformed policy never reaches the service")
		})
	}
}

func TestExecuteKMSSetPolicy_SchemaDeclaresPolicyFields(t *testing.T) {
	r := newRotationRegistry(t, newPolicyKMSService())
	r.registerKMSTools()

	var schema map[string]interface{}
	for _, tool := range r.ListTools() {
		if tool.Name == "quicbaas_kms_set_policy" {
			schema = tool.InputSchema
		}
	}
	require.NotNil(t, schema, "quicbaas_kms_set_policy is registered")
	props, ok := schema["properties"].(map[string]interface{})
	require.True(t, ok)
	policy, ok := props["policy"].(map[string]interface{})
	require.True(t, ok)
	fields, ok := policy["properties"].(map[string]interface{})
	require.True(t, ok, "the policy schema declares its fields")
	for _, name := range []string{
		"allowed_operations", "allowed_principals", "denied_principals",
		"expires_at", "rotation_required", "max_usage_count", "require_mfa",
	} {
		assert.Contains(t, fields, name)
	}
}
