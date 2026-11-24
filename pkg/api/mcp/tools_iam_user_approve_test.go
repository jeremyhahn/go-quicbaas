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

// The quicbaas_iam_user_approve and quicbaas_iam_user_reject tools set a
// pending user's lifecycle, as quicbaasd's REST approve and reject routes do,
// under the same users:enable and users:disable permissions.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func approveTestRegistry(captured **service.IAMUpdateUserRequest) *ToolRegistry {
	return newIAMTestRegistry(&mockIAMService{
		updateUserFunc: func(_ context.Context, req *service.IAMUpdateUserRequest) (*service.IAMUpdateUserResponse, error) {
			*captured = req
			return &service.IAMUpdateUserResponse{
				User:      &iam.User{ID: req.UserID, TenantID: req.TenantID},
				Timestamp: time.Now(),
			}, nil
		},
	})
}

func TestIAMUserApprove_MCPTool_Registered(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})
	want := map[string][]string{
		"quicbaas_iam_user_approve": {"tenant_id", "user_id"},
		"quicbaas_iam_user_reject":  {"tenant_id", "user_id"},
	}
	found := map[string]bool{}
	for _, tool := range reg.ListTools() {
		required, ok := want[tool.Name]
		if !ok {
			continue
		}
		found[tool.Name] = true
		assert.ElementsMatch(t, required, tool.InputSchema["required"], "%s required args", tool.Name)
		if tool.Name == "quicbaas_iam_user_reject" {
			props, _ := tool.InputSchema["properties"].(map[string]interface{})
			assert.Contains(t, props, "reason", "the reject tool takes an optional reason")
		}
	}
	for name := range want {
		assert.True(t, found[name], "%s is not registered", name)
	}
}

func TestIAMUserApprove_MCPTool_ApproveSetsActive(t *testing.T) {
	var captured *service.IAMUpdateUserRequest
	reg := approveTestRegistry(&captured)

	result, err := reg.executeIAMTool(tenantctx.WithTenant(context.Background(), "t-1"), "quicbaas_iam_user_approve",
		map[string]interface{}{"tenant_id": "t-1", "user_id": "30001"})
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "t-1", captured.TenantID)
	assert.Equal(t, uint64(30001), captured.UserID)
	require.NotNil(t, captured.Lifecycle)
	assert.Equal(t, iam.LifecycleActive, *captured.Lifecycle)
	assert.Nil(t, captured.Email, "approve changes only the lifecycle")
	assert.Nil(t, captured.Roles, "approve changes only the lifecycle")

	m, ok := result.(map[string]interface{})
	require.True(t, ok, "result is %T", result)
	assert.Equal(t, string(iam.LifecycleActive), m["lifecycle"])
	assert.Equal(t, uint64(30001), m["user_id"])
}

func TestIAMUserApprove_MCPTool_RejectSetsDisabled(t *testing.T) {
	var captured *service.IAMUpdateUserRequest
	reg := approveTestRegistry(&captured)

	result, err := reg.executeIAMTool(tenantctx.WithTenant(context.Background(), "t-1"), "quicbaas_iam_user_reject",
		map[string]interface{}{"tenant_id": "t-1", "user_id": "30001", "reason": "spam signup"})
	require.NoError(t, err)
	require.NotNil(t, captured)
	require.NotNil(t, captured.Lifecycle)
	assert.Equal(t, iam.LifecycleDisabled, *captured.Lifecycle)

	m, ok := result.(map[string]interface{})
	require.True(t, ok, "result is %T", result)
	assert.Equal(t, string(iam.LifecycleDisabled), m["lifecycle"])
}

func TestIAMUserApprove_MCPTool_RefusesMissingArgs(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"no tenant": {"user_id": "30001"},
		"no user":   {"tenant_id": "t-1"},
		"bad user":  {"tenant_id": "t-1", "user_id": "u-1"},
	}
	for _, tool := range []string{"quicbaas_iam_user_approve", "quicbaas_iam_user_reject"} {
		for name, args := range cases {
			t.Run(tool+"/"+name, func(t *testing.T) {
				var captured *service.IAMUpdateUserRequest
				reg := approveTestRegistry(&captured)
				execute := reg.executeIAMUserApprove
				if tool == "quicbaas_iam_user_reject" {
					execute = reg.executeIAMUserReject
				}
				_, err := execute(context.Background(), args)
				require.Error(t, err)
				assert.Nil(t, captured, "the service was called despite the refusal")
			})
		}
	}
}

func TestIAMUserApprove_MCPTool_RefusesAnotherTenant(t *testing.T) {
	for _, tool := range []string{"quicbaas_iam_user_approve", "quicbaas_iam_user_reject"} {
		t.Run(tool, func(t *testing.T) {
			var captured *service.IAMUpdateUserRequest
			reg := approveTestRegistry(&captured)
			_, err := reg.executeIAMTool(tenantctx.WithTenant(context.Background(), "t-1"), tool,
				map[string]interface{}{"tenant_id": "t-2", "user_id": "30001"})
			require.Error(t, err)
			assert.Nil(t, captured, "a caller bound to t-1 changed a user in t-2")
		})
	}
}

func TestIAMUserApprove_MCPTool_Permissions(t *testing.T) {
	for tool, want := range map[string][2]string{
		"quicbaas_iam_user_approve": {"users", "enable"},
		"quicbaas_iam_user_reject":  {"users", "disable"},
	} {
		resource, action, ok := getToolQRDBPermission(tool)
		require.True(t, ok, "%s has no permission mapping", tool)
		assert.Equal(t, want[0], resource, tool)
		assert.Equal(t, want[1], action, tool)
	}
}
