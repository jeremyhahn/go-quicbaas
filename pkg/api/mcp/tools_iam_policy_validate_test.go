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

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestExecuteIAMPolicyValidate(t *testing.T) {
	t.Run("iam_unavailable", func(t *testing.T) {
		// mockService does not implement iamServiceAccessor.
		reg := NewToolRegistry(&mockService{}, newTestLogger(t))
		_, err := reg.executeIAMPolicyValidate(context.Background(), map[string]interface{}{
			"policy": map[string]interface{}{"name": "p"},
		})
		require.Error(t, err)
	})

	t.Run("missing_policy_object", func(t *testing.T) {
		reg := newIAMTestRegistry(&mockIAMService{})
		_, err := reg.executeIAMPolicyValidate(context.Background(), map[string]interface{}{})
		require.Error(t, err)
	})

	t.Run("policy_wrong_type", func(t *testing.T) {
		reg := newIAMTestRegistry(&mockIAMService{})
		_, err := reg.executeIAMPolicyValidate(context.Background(), map[string]interface{}{
			"policy": "not-an-object",
		})
		require.Error(t, err)
	})

	t.Run("sdk_format_policy_valid", func(t *testing.T) {
		var captured *service.ValidatePolicyRequest
		reg := newIAMTestRegistry(&mockIAMService{
			validatePolFunc: func(ctx context.Context, req *service.ValidatePolicyRequest) (*service.ValidatePolicyResponse, error) {
				captured = req
				return &service.ValidatePolicyResponse{Valid: true, Timestamp: time.Now()}, nil
			},
		})
		// No apiVersion -> routes through buildPolicyFromSDKFormat.
		res, err := reg.executeIAMPolicyValidate(context.Background(), map[string]interface{}{
			"policy":    map[string]interface{}{"name": "p1", "description": "d", "spec_yaml": "rules: []"},
			"tenant_id": "t1",
		})
		require.NoError(t, err)
		require.NotNil(t, captured)
		assert.Equal(t, "p1", captured.Policy.Metadata.Name)
		assert.Equal(t, "t1", captured.Policy.Metadata.Tenant)
		out := res.(map[string]interface{})
		assert.Equal(t, true, out["valid"])
	})

	t.Run("sdk_policy_record_metadata_accepted", func(t *testing.T) {
		// An SDK iam.Policy always serializes tenant_id, created_at and
		// updated_at; validation checks the document and ignores them.
		var captured *service.ValidatePolicyRequest
		reg := newIAMTestRegistry(&mockIAMService{
			validatePolFunc: func(ctx context.Context, req *service.ValidatePolicyRequest) (*service.ValidatePolicyResponse, error) {
				captured = req
				return &service.ValidatePolicyResponse{Valid: true, Timestamp: time.Now()}, nil
			},
		})
		_, err := reg.executeIAMPolicyValidate(context.Background(), map[string]interface{}{
			"policy": map[string]interface{}{
				"name": "p1", "spec_yaml": "rules: []", "tenant_id": "t1",
				"created_at": "0001-01-01T00:00:00Z", "updated_at": "0001-01-01T00:00:00Z",
			},
			"tenant_id": "t1",
		})
		require.NoError(t, err)
		require.NotNil(t, captured)
		assert.Equal(t, "p1", captured.Policy.Metadata.Name)
	})

	t.Run("sdk_policy_unknown_key_refused", func(t *testing.T) {
		reg := newIAMTestRegistry(&mockIAMService{})
		_, err := reg.executeIAMPolicyValidate(context.Background(), map[string]interface{}{
			"policy": map[string]interface{}{"name": "p1", "spec_yaml": "rules: []", "tenant_id": "t1", "owner": "ops"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "policy.owner")
	})

	t.Run("name_forwarded", func(t *testing.T) {
		for _, name := range []string{"p1", "other", ""} {
			var captured *service.ValidatePolicyRequest
			reg := newIAMTestRegistry(&mockIAMService{
				validatePolFunc: func(ctx context.Context, req *service.ValidatePolicyRequest) (*service.ValidatePolicyResponse, error) {
					captured = req
					return &service.ValidatePolicyResponse{Valid: true, Timestamp: time.Now()}, nil
				},
			})
			_, err := reg.executeIAMPolicyValidate(context.Background(), map[string]interface{}{
				"policy": map[string]interface{}{"name": "p1", "spec_yaml": "rules: []"},
				"name":   name,
			})
			require.NoError(t, err)
			require.NotNil(t, captured)
			assert.Equal(t, name, captured.Name)
		}
	})

	t.Run("schema_declares_name", func(t *testing.T) {
		reg := newIAMTestRegistry(&mockIAMService{})
		var def *ToolDefinition
		for _, tool := range reg.ListTools() {
			if tool.Name == "quicbaas_iam_policy_validate" {
				def = &tool
				break
			}
		}
		require.NotNil(t, def, "quicbaas_iam_policy_validate is not registered")
		props, ok := def.InputSchema["properties"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, props, "name")
		assert.Contains(t, props, "policy")
	})

	t.Run("service_error_propagated", func(t *testing.T) {
		reg := newIAMTestRegistry(&mockIAMService{
			validatePolFunc: func(ctx context.Context, req *service.ValidatePolicyRequest) (*service.ValidatePolicyResponse, error) {
				return nil, errors.New(errors.ErrInvalidInput, "ValidatePolicy", "bad policy")
			},
		})
		_, err := reg.executeIAMPolicyValidate(context.Background(), map[string]interface{}{
			"policy": map[string]interface{}{"name": "p1", "spec_yaml": "rules: []"},
		})
		require.Error(t, err)
	})
}

// TestExecuteIAMPolicyValidate_DocumentName pins that the MCP validate tool
// forwards both the requested name and the document's metadata.name, so the
// service's iam.CheckPolicyName (mirrored by the double) reports a mismatch
// as Valid=false with its message.
func TestExecuteIAMPolicyValidate_DocumentName(t *testing.T) {
	want := iam.CheckPolicyName(&iam.Policy{Metadata: iam.PolicyMetadata{Name: "other"}}, "p1").Error()
	tests := []struct {
		docName   string
		wantValid bool
	}{
		{"other", false},
		{"p1", true},
	}
	for _, tt := range tests {
		reg := newIAMTestRegistry(&mockIAMService{
			validatePolFunc: func(ctx context.Context, req *service.ValidatePolicyRequest) (*service.ValidatePolicyResponse, error) {
				resp := &service.ValidatePolicyResponse{Valid: true, Timestamp: time.Now()}
				if err := iam.CheckPolicyName(req.Policy, req.Name); err != nil {
					resp.Valid = false
					resp.Errors = []string{err.Error()}
				}
				return resp, nil
			},
		})
		res, err := reg.executeIAMPolicyValidate(context.Background(), map[string]interface{}{
			"tenant_id": "t1",
			"name":      "p1",
			"policy": map[string]interface{}{
				"apiVersion": "iam.quicbaas.io/v1",
				"kind":       "Policy",
				"metadata":   map[string]interface{}{"name": tt.docName},
				"spec": map[string]interface{}{
					"rules": []interface{}{map[string]interface{}{
						"name": "r1", "effect": "allow", "actions": []interface{}{"read"}, "resources": []interface{}{"documents"},
					}},
				},
			},
		})
		require.NoError(t, err, tt.docName)
		out := res.(map[string]interface{})
		assert.Equal(t, tt.wantValid, out["valid"], tt.docName)
		if !tt.wantValid {
			assert.Contains(t, out["errors"], want)
		}
	}
}
