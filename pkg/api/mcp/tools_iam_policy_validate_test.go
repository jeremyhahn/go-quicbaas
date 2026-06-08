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
			"policy":    map[string]interface{}{"name": "p1", "description": "d"},
			"tenant_id": "t1",
		})
		require.NoError(t, err)
		require.NotNil(t, captured)
		assert.Equal(t, "p1", captured.Policy.Metadata.Name)
		assert.Equal(t, "t1", captured.Policy.Metadata.Tenant)
		out := res.(map[string]interface{})
		assert.Equal(t, true, out["valid"])
	})

	t.Run("service_error_propagated", func(t *testing.T) {
		reg := newIAMTestRegistry(&mockIAMService{
			validatePolFunc: func(ctx context.Context, req *service.ValidatePolicyRequest) (*service.ValidatePolicyResponse, error) {
				return nil, errors.New(errors.ErrInvalidInput, "ValidatePolicy", "bad policy")
			},
		})
		_, err := reg.executeIAMPolicyValidate(context.Background(), map[string]interface{}{
			"policy": map[string]interface{}{"name": "p1"},
		})
		require.Error(t, err)
	})
}
