//go:build mcp

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

package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// newRotationPolicyRegistry returns a registry with the secrets tools
// registered over mockSvc.
func newRotationPolicyRegistry(t *testing.T, mockSvc *testutil.MockQuicBaaS) *ToolRegistry {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)
	registry := &ToolRegistry{service: mockSvc, logger: log}
	registry.registerSecretsTools()
	return registry
}

// TestSecretsSetRotationPolicy_RoundTrip proves every policy field the tool
// schema declares reaches the service on set and comes back on get.
func TestSecretsSetRotationPolicy_RoundTrip(t *testing.T) {
	mockSvc := testutil.NewMockQuicBaaS()
	setResp := &service.SetRotationPolicyResponse{SecretID: "secret-123", Timestamp: time.Now()}
	getResp := &service.GetRotationPolicyResponse{SecretID: "secret-123", Timestamp: time.Now()}

	mockSvc.On("SetRotationPolicy", mock.Anything, mock.MatchedBy(func(req *service.SetRotationPolicyRequest) bool {
		return req.SecretID == "secret-123"
	})).Run(func(args mock.Arguments) {
		stored := args.Get(1).(*service.SetRotationPolicyRequest).Policy
		setResp.Policy = stored
		getResp.Policy = stored
	}).Return(setResp, nil).Once()
	mockSvc.On("GetRotationPolicy", mock.Anything, mock.MatchedBy(func(req *service.GetRotationPolicyRequest) bool {
		return req.SecretID == "secret-123"
	})).Return(getResp, nil).Once()

	registry := newRotationPolicyRegistry(t, mockSvc)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_secrets_set_rotation_policy", map[string]interface{}{
		"secret_id": "secret-123",
		"policy": map[string]interface{}{
			"enabled":         true,
			"interval":        "36h30m",
			"retain_versions": float64(5),
			"rotation_lambda": "arn:aws:lambda:us-east-1:000000000000:function:rotate",
		},
	})
	require.NoError(t, err)

	result, err := registry.ExecuteTool(ctx, "quicbaas_secrets_get_rotation_policy", map[string]interface{}{
		"secret_id": "secret-123",
	})
	require.NoError(t, err)
	mockSvc.AssertExpectations(t)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result = %T", result)
	policy, ok := resultMap["policy"].(map[string]interface{})
	require.True(t, ok, "policy = %v", resultMap["policy"])

	assert.Equal(t, true, policy["enabled"])
	assert.Equal(t, float64(36*time.Hour+30*time.Minute), policy["interval"])
	assert.Equal(t, float64(5), policy["retain_versions"])
	assert.Equal(t, "arn:aws:lambda:us-east-1:000000000000:function:rotate", policy["rotation_lambda"])
}

// TestSecretsSetRotationPolicy_Refused proves a malformed request is refused
// with a typed error before the service is called.
func TestSecretsSetRotationPolicy_Refused(t *testing.T) {
	tests := []struct {
		name  string
		args  map[string]interface{}
		check func(t *testing.T, err error)
	}{
		{
			name: "bad interval",
			args: map[string]interface{}{
				"secret_id": "secret-123",
				"policy":    map[string]interface{}{"interval": "7 days"},
			},
			check: func(t *testing.T, err error) {
				var target *api.InvalidFormatError
				require.True(t, errors.As(err, &target), "err = %v", err)
				assert.Equal(t, "policy.interval", target.Field)
			},
		},
		{
			name: "wrong-typed enabled",
			args: map[string]interface{}{
				"secret_id": "secret-123",
				"policy":    map[string]interface{}{"enabled": "yes"},
			},
			check: func(t *testing.T, err error) {
				var target *api.InvalidTypeError
				require.True(t, errors.As(err, &target), "err = %v", err)
				assert.Equal(t, "policy.enabled", target.Field)
			},
		},
		{
			name: "fractional retain_versions",
			args: map[string]interface{}{
				"secret_id": "secret-123",
				"policy":    map[string]interface{}{"retain_versions": float64(2.5)},
			},
			check: func(t *testing.T, err error) {
				var target *api.InvalidTypeError
				require.True(t, errors.As(err, &target), "err = %v", err)
				assert.Equal(t, "policy.retain_versions", target.Field)
			},
		},
		{
			name: "wrong-typed interval",
			args: map[string]interface{}{
				"secret_id": "secret-123",
				"policy":    map[string]interface{}{"interval": float64(3600)},
			},
			check: func(t *testing.T, err error) {
				var target *api.InvalidTypeError
				require.True(t, errors.As(err, &target), "err = %v", err)
				assert.Equal(t, "policy.interval", target.Field)
			},
		},
		{
			name: "unknown field",
			args: map[string]interface{}{
				"secret_id": "secret-123",
				"policy":    map[string]interface{}{"next_rotation": "2026-10-20T00:00:00Z"},
			},
			check: func(t *testing.T, err error) {
				var target *api.UnsupportedValueError
				require.True(t, errors.As(err, &target), "err = %v", err)
				assert.Equal(t, "next_rotation", target.Value)
			},
		},
		{
			name: "policy not an object",
			args: map[string]interface{}{
				"secret_id": "secret-123",
				"policy":    "enabled",
			},
			check: func(t *testing.T, err error) {
				var target *api.InvalidTypeError
				require.True(t, errors.As(err, &target), "err = %v", err)
				assert.Equal(t, "policy", target.Field)
			},
		},
		{
			name: "missing secret_id",
			args: map[string]interface{}{
				"policy": map[string]interface{}{"enabled": true},
			},
			check: func(t *testing.T, err error) {
				var target *api.RequiredFieldError
				require.True(t, errors.As(err, &target), "err = %v", err)
				assert.Equal(t, "secret_id", target.Field)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := testutil.NewMockQuicBaaS()
			registry := newRotationPolicyRegistry(t, mockSvc)

			_, err := registry.ExecuteTool(context.Background(), "quicbaas_secrets_set_rotation_policy", tt.args)
			require.Error(t, err)
			tt.check(t, err)
			mockSvc.AssertNotCalled(t, "SetRotationPolicy", mock.Anything, mock.Anything)
		})
	}
}
