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

package quic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTransformTenantRequest_AllMethods exercises every method branch of the
// tenant request router.
func TestTransformTenantRequest_AllMethods(t *testing.T) {
	h := newTestIAMHandler()

	t.Run("Get with tenant_id map", func(t *testing.T) {
		out, err := h.transformTenantRequest("Get", map[string]interface{}{"tenant_id": "t1"})
		require.NoError(t, err)
		assert.Equal(t, "t1", out.(map[string]interface{})["id"])
	})

	t.Run("Get fallback to generic rename", func(t *testing.T) {
		out, err := h.transformTenantRequest("Get", map[string]interface{}{"other": "x"})
		require.NoError(t, err)
		assert.NotNil(t, out)
	})

	t.Run("Update", func(t *testing.T) {
		out, err := h.transformTenantRequest("Update", map[string]interface{}{
			"tenant_id": "t1", "request": map[string]interface{}{"name": "New"},
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "t1", m["id"])
		assert.Equal(t, "New", m["name"])
	})

	t.Run("Delete", func(t *testing.T) {
		out, err := h.transformTenantRequest("Delete", map[string]interface{}{"tenant_id": "t1"})
		require.NoError(t, err)
		assert.Equal(t, "t1", out.(map[string]interface{})["id"])
	})

	t.Run("List pass-through", func(t *testing.T) {
		in := map[string]interface{}{"limit": 10}
		out, err := h.transformTenantRequest("List", in)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})

	t.Run("default pass-through", func(t *testing.T) {
		in := map[string]interface{}{"a": 1}
		out, err := h.transformTenantRequest("Bogus", in)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})
}

// TestTransformUserRequest_AllMethods exercises the user request router's
// pass-through and transform branches.
func TestTransformUserRequest_AllMethods(t *testing.T) {
	h := newTestIAMHandler()
	passThrough := []string{"Get", "GetByEmail", "FindByUsername", "Delete",
		"AssignRole", "RevokeRole", "AddToGroup", "RemoveFromGroup", "Suspend", "Unsuspend", "Bogus"}
	for _, m := range passThrough {
		t.Run(m, func(t *testing.T) {
			in := map[string]interface{}{"tenant_id": "t1"}
			out, err := h.transformUserRequest(m, in)
			require.NoError(t, err)
			assert.Equal(t, in, out)
		})
	}

	t.Run("Update", func(t *testing.T) {
		out, err := h.transformUserRequest("Update", map[string]interface{}{
			"tenant_id": "t1", "user_id": "u1",
			"request": map[string]interface{}{"username": "x"},
		})
		require.NoError(t, err)
		assert.Equal(t, "u1", out.(map[string]interface{})["user_id"])
	})

	t.Run("List", func(t *testing.T) {
		out, err := h.transformUserRequest("List", map[string]interface{}{
			"tenant_id": "t1", "request": map[string]interface{}{"page_size": float64(5)},
		})
		require.NoError(t, err)
		assert.Equal(t, 5, out.(map[string]interface{})["limit"])
	})
}

// TestTransformGroupRequest_AllMethods exercises the group request router.
func TestTransformGroupRequest_AllMethods(t *testing.T) {
	h := newTestIAMHandler()
	passThrough := []string{"Get", "Delete", "AddMember", "RemoveMember", "Bogus"}
	for _, m := range passThrough {
		t.Run(m, func(t *testing.T) {
			in := map[string]interface{}{"tenant_id": "t1"}
			out, err := h.transformGroupRequest(m, in)
			require.NoError(t, err)
			assert.Equal(t, in, out)
		})
	}

	t.Run("Update", func(t *testing.T) {
		out, err := h.transformGroupRequest("Update", map[string]interface{}{
			"tenant_id": "t1", "group_id": "g1", "request": map[string]interface{}{"name": "n"},
		})
		require.NoError(t, err)
		assert.Equal(t, "g1", out.(map[string]interface{})["group_id"])
	})

	t.Run("List", func(t *testing.T) {
		out, err := h.transformGroupRequest("List", map[string]interface{}{
			"tenant_id": "t1", "request": map[string]interface{}{"limit": float64(3)},
		})
		require.NoError(t, err)
		assert.Equal(t, 3, out.(map[string]interface{})["limit"])
	})
}

// TestTransformRoleRequest_AllMethods exercises the role request router.
func TestTransformRoleRequest_AllMethods(t *testing.T) {
	h := newTestIAMHandler()
	passThrough := []string{"Get", "Delete", "Bogus"}
	for _, m := range passThrough {
		t.Run(m, func(t *testing.T) {
			in := map[string]interface{}{"tenant_id": "t1"}
			out, err := h.transformRoleRequest(m, in)
			require.NoError(t, err)
			assert.Equal(t, in, out)
		})
	}

	t.Run("Update", func(t *testing.T) {
		out, err := h.transformRoleRequest("Update", map[string]interface{}{
			"tenant_id": "t1", "role_id": "r1", "request": map[string]interface{}{"name": "n"},
		})
		require.NoError(t, err)
		assert.Equal(t, "r1", out.(map[string]interface{})["role_id"])
	})

	t.Run("List", func(t *testing.T) {
		out, err := h.transformRoleRequest("List", map[string]interface{}{
			"tenant_id": "t1", "request": map[string]interface{}{"limit": float64(2)},
		})
		require.NoError(t, err)
		assert.Equal(t, 2, out.(map[string]interface{})["limit"])
	})
}

// TestTransformPolicyRequest_AllMethods exercises the policy request router.
func TestTransformPolicyRequest_AllMethods(t *testing.T) {
	h := newTestIAMHandler()

	t.Run("Get renames policy_name", func(t *testing.T) {
		out, err := h.transformPolicyRequest("Get", map[string]interface{}{"policy_name": "p1"})
		require.NoError(t, err)
		assert.Equal(t, "p1", out.(map[string]interface{})["name"])
	})

	t.Run("Update", func(t *testing.T) {
		out, err := h.transformPolicyRequest("Update", map[string]interface{}{
			"tenant_id": "t1", "policy_name": "p1", "request": map[string]interface{}{"description": "d"},
		})
		require.NoError(t, err)
		assert.Equal(t, "p1", out.(map[string]interface{})["name"])
	})

	t.Run("Delete", func(t *testing.T) {
		out, err := h.transformPolicyRequest("Delete", map[string]interface{}{"policy_name": "p1"})
		require.NoError(t, err)
		assert.Equal(t, "p1", out.(map[string]interface{})["name"])
	})

	t.Run("List", func(t *testing.T) {
		out, err := h.transformPolicyRequest("List", map[string]interface{}{
			"tenant_id": "t1", "request": map[string]interface{}{"page_size": float64(4)},
		})
		require.NoError(t, err)
		assert.Equal(t, 4, out.(map[string]interface{})["limit"])
	})

	t.Run("Validate pass-through", func(t *testing.T) {
		in := map[string]interface{}{"policy": map[string]interface{}{}}
		out, err := h.transformPolicyRequest("Validate", in)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})

	t.Run("Test", func(t *testing.T) {
		out, err := h.transformPolicyRequest("Test", map[string]interface{}{
			"tenant_id": "t1", "request": map[string]interface{}{"policy_name": "p1"},
		})
		require.NoError(t, err)
		assert.Equal(t, "p1", out.(map[string]interface{})["policy_name"])
	})

	t.Run("default pass-through", func(t *testing.T) {
		in := map[string]interface{}{"a": 1}
		out, err := h.transformPolicyRequest("Bogus", in)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})
}

// TestTransformAppRequest_AllMethods exercises the app request router.
func TestTransformAppRequest_AllMethods(t *testing.T) {
	h := newTestIAMHandler()

	t.Run("Get renames app_id", func(t *testing.T) {
		out, err := h.transformAppRequest("Get", map[string]interface{}{"app_id": "a1"})
		require.NoError(t, err)
		assert.Equal(t, "a1", out.(map[string]interface{})["id"])
	})

	t.Run("Update", func(t *testing.T) {
		out, err := h.transformAppRequest("Update", map[string]interface{}{
			"tenant_id": "t1", "app_id": "a1", "request": map[string]interface{}{"name": "n"},
		})
		require.NoError(t, err)
		assert.Equal(t, "a1", out.(map[string]interface{})["id"])
	})

	t.Run("Delete", func(t *testing.T) {
		out, err := h.transformAppRequest("Delete", map[string]interface{}{"app_id": "a1"})
		require.NoError(t, err)
		assert.Equal(t, "a1", out.(map[string]interface{})["id"])
	})

	t.Run("List", func(t *testing.T) {
		out, err := h.transformAppRequest("List", map[string]interface{}{
			"tenant_id": "t1", "request": map[string]interface{}{"page_size": float64(6)},
		})
		require.NoError(t, err)
		assert.Equal(t, 6, out.(map[string]interface{})["limit"])
	})

	t.Run("RotateSecret", func(t *testing.T) {
		out, err := h.transformAppRequest("RotateSecret", map[string]interface{}{"app_id": "a1"})
		require.NoError(t, err)
		assert.Equal(t, "a1", out.(map[string]interface{})["id"])
	})

	t.Run("default pass-through", func(t *testing.T) {
		in := map[string]interface{}{"a": 1}
		out, err := h.transformAppRequest("Bogus", in)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})
}

// TestTransformResponse_FlatVariants covers the "already flat" extract paths
// (response without the nested entity key) that the wrapped-key tests do not.
func TestTransformResponse_FlatVariants(t *testing.T) {
	h := newTestIAMHandler()

	t.Run("tenant flat", func(t *testing.T) {
		out, err := h.transformTenantResponse("Get", map[string]interface{}{"id": "t1", "name": "Acme"})
		require.NoError(t, err)
		assert.Equal(t, "t1", out.(map[string]interface{})["id"])
	})

	t.Run("user flat", func(t *testing.T) {
		out, err := h.transformUserResponse("Get", map[string]interface{}{"id": "u1", "username": "alice"})
		require.NoError(t, err)
		assert.Equal(t, "u1", out.(map[string]interface{})["id"])
	})

	t.Run("user delete deleted=false", func(t *testing.T) {
		out, err := h.transformUserResponse("Delete", map[string]interface{}{"deleted": false})
		require.NoError(t, err)
		assert.Equal(t, map[string]bool{"success": false}, out)
	})

	t.Run("user default pass-through", func(t *testing.T) {
		in := map[string]interface{}{"x": 1}
		out, err := h.transformUserResponse("Bogus", in)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})

	t.Run("group flat", func(t *testing.T) {
		out, err := h.transformGroupResponse("Get", map[string]interface{}{"id": "g1", "name": "admins"})
		require.NoError(t, err)
		assert.Equal(t, "g1", out.(map[string]interface{})["id"])
	})

	t.Run("group delete deleted true", func(t *testing.T) {
		out, err := h.transformGroupResponse("Delete", map[string]interface{}{"deleted": true})
		require.NoError(t, err)
		assert.Equal(t, map[string]bool{"success": true}, out)
	})

	t.Run("group default pass-through", func(t *testing.T) {
		in := map[string]interface{}{"x": 1}
		out, err := h.transformGroupResponse("Bogus", in)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})

	t.Run("role flat", func(t *testing.T) {
		out, err := h.transformRoleResponse("Get", map[string]interface{}{"id": "r1", "name": "viewer"})
		require.NoError(t, err)
		assert.Equal(t, "r1", out.(map[string]interface{})["id"])
	})

	t.Run("role delete default true", func(t *testing.T) {
		out, err := h.transformRoleResponse("Delete", map[string]interface{}{})
		require.NoError(t, err)
		assert.Equal(t, map[string]bool{"success": true}, out)
	})

	t.Run("role default pass-through", func(t *testing.T) {
		in := map[string]interface{}{"x": 1}
		out, err := h.transformRoleResponse("Bogus", in)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})

	t.Run("tenant default pass-through", func(t *testing.T) {
		in := map[string]interface{}{"x": 1}
		out, err := h.transformTenantResponse("Bogus", in)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})
}
