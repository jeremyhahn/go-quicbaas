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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTransformResponse_Routing exercises the service dispatch in
// (*iamHandler).transformResponse including the MFA pass-through and default.
func TestTransformResponse_Routing(t *testing.T) {
	h := newTestIAMHandler()

	t.Run("MFA pass-through", func(t *testing.T) {
		in := map[string]interface{}{"enrolled": true}
		out, err := h.transformResponse("quicbaas.IAM.MFA", "GetStatus", in)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})

	t.Run("Tenant create extracts", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.IAM.Tenant", "Create", map[string]interface{}{
			"tenant": map[string]interface{}{"id": "t1", "name": "Acme"},
		})
		require.NoError(t, err)
		assert.Equal(t, "t1", out.(map[string]interface{})["id"])
	})

	t.Run("Tenant delete", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.IAM.Tenant", "Delete", map[string]interface{}{"deleted": true})
		require.NoError(t, err)
		assert.Equal(t, map[string]bool{"success": true}, out)
	})

	t.Run("Tenant delete default true", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.IAM.Tenant", "Delete", map[string]interface{}{})
		require.NoError(t, err)
		assert.Equal(t, map[string]bool{"success": true}, out)
	})

	t.Run("Tenant list", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.IAM.Tenant", "List", map[string]interface{}{
			"tenants":     []interface{}{map[string]interface{}{"id": "t1", "name": "Acme"}},
			"total_count": float64(1),
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Len(t, m["tenants"], 1)
	})

	t.Run("User get extracts", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.IAM.User", "Get", map[string]interface{}{
			"user": map[string]interface{}{"id": "u1", "email": "a@b.c", "name": "Alice"},
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "u1", m["id"])
		assert.Equal(t, "Alice", m["display_name"])
	})

	t.Run("User delete with user key", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.IAM.User", "Suspend", map[string]interface{}{
			"user": map[string]interface{}{"id": "u1"},
		})
		require.NoError(t, err)
		assert.Equal(t, map[string]bool{"success": true}, out)
	})

	t.Run("User list", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.IAM.User", "List", map[string]interface{}{
			"users":       []interface{}{map[string]interface{}{"id": "u1"}},
			"total_count": int64(1),
		})
		require.NoError(t, err)
		assert.Len(t, out.(map[string]interface{})["users"], 1)
	})

	t.Run("Group get", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.IAM.Group", "Get", map[string]interface{}{
			"group": map[string]interface{}{"id": "g1", "name": "admins"},
		})
		require.NoError(t, err)
		assert.Equal(t, "g1", out.(map[string]interface{})["id"])
	})

	t.Run("Group add member", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.IAM.Group", "AddMember", map[string]interface{}{"deleted": false})
		require.NoError(t, err)
		assert.Equal(t, map[string]bool{"success": false}, out)
	})

	t.Run("Group list", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.IAM.Group", "List", map[string]interface{}{
			"groups":      []interface{}{map[string]interface{}{"id": "g1"}},
			"total_count": int(2),
		})
		require.NoError(t, err)
		assert.Len(t, out.(map[string]interface{})["groups"], 1)
	})

	t.Run("Role get", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.IAM.Role", "Get", map[string]interface{}{
			"role": map[string]interface{}{"id": "r1", "name": "viewer"},
		})
		require.NoError(t, err)
		assert.Equal(t, "r1", out.(map[string]interface{})["id"])
	})

	t.Run("Role delete", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.IAM.Role", "Delete", map[string]interface{}{"deleted": true})
		require.NoError(t, err)
		assert.Equal(t, map[string]bool{"success": true}, out)
	})

	t.Run("Role list", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.IAM.Role", "List", map[string]interface{}{
			"roles": []interface{}{map[string]interface{}{"id": "r1"}},
		})
		require.NoError(t, err)
		assert.Len(t, out.(map[string]interface{})["roles"], 1)
	})

	t.Run("default pass-through", func(t *testing.T) {
		in := map[string]interface{}{"x": 1}
		out, err := h.transformResponse("quicbaas.IAM.Session", "Whatever", in)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})
}

// TestExtractAndFlatten covers the extract*/flatten* helpers including the
// nil, already-flat, and time.Time timestamp branches.
func TestExtractAndFlatten(t *testing.T) {
	now := time.Now()

	t.Run("extractTenant nil", func(t *testing.T) {
		assert.Nil(t, extractTenant(nil))
	})

	t.Run("extractTenant already flat", func(t *testing.T) {
		out := extractTenant(map[string]interface{}{"id": "t1", "name": "Acme"})
		assert.Equal(t, "t1", out["id"])
	})

	t.Run("extractTenant no id no tenant", func(t *testing.T) {
		in := map[string]interface{}{"other": "x"}
		assert.Equal(t, in, extractTenant(in))
	})

	t.Run("flattenTenant full", func(t *testing.T) {
		out := flattenTenant(map[string]interface{}{
			"id": "t1", "name": "Acme", "status": "active",
			"settings": map[string]interface{}{"k": "v"},
			"quotas":   map[string]interface{}{"max": 1},
			"created":  now, "updated": now,
		})
		assert.Equal(t, "t1", out["id"])
		assert.Equal(t, "Acme", out["display_name"])
		assert.Equal(t, "active", out["status"])
		assert.NotEmpty(t, out["created_at"])
		assert.NotEmpty(t, out["updated_at"])
	})

	t.Run("flattenTenant nil", func(t *testing.T) {
		assert.Nil(t, flattenTenant(nil))
	})

	t.Run("extractUser nil", func(t *testing.T) {
		assert.Nil(t, extractUser(nil))
	})

	t.Run("flattenUser full", func(t *testing.T) {
		out := flattenUser(map[string]interface{}{
			"id": "u1", "tenant_id": "t1", "email": "a@b.c", "name": "Alice",
			"mfa_enabled": true, "email_verified": true,
			"roles":      []interface{}{"r1"},
			"groups":     []interface{}{"g1"},
			"attributes": map[string]interface{}{"k": "v"},
			"created":    now, "updated": now, "last_login_at": "2021-01-01T00:00:00Z",
		})
		assert.Equal(t, "u1", out["id"])
		assert.Equal(t, "Alice", out["display_name"])
		assert.Equal(t, "Alice", out["username"]) // name used as username fallback
		assert.Equal(t, true, out["mfa_enabled"])
	})

	t.Run("flattenUser nil", func(t *testing.T) {
		assert.Nil(t, flattenUser(nil))
	})

	t.Run("extractGroup nil", func(t *testing.T) {
		assert.Nil(t, extractGroup(nil))
	})

	t.Run("flattenGroup full", func(t *testing.T) {
		out := flattenGroup(map[string]interface{}{
			"id": "g1", "tenant_id": "t1", "name": "admins",
			"display_name": "Admins", "description": "d",
			"members": []interface{}{"u1"},
			"created": now, "updated": now,
		})
		assert.Equal(t, "g1", out["id"])
		assert.Equal(t, "Admins", out["display_name"])
	})

	t.Run("flattenGroup nil", func(t *testing.T) {
		assert.Nil(t, flattenGroup(nil))
	})

	t.Run("extractRole nil", func(t *testing.T) {
		assert.Nil(t, extractRole(nil))
	})

	t.Run("flattenRole full", func(t *testing.T) {
		out := flattenRole(map[string]interface{}{
			"id": "r1", "tenant_id": "t1", "name": "viewer",
			"display_name": "Viewer", "description": "d",
			"permissions": []interface{}{"read"},
			"created":     now, "updated": now,
		})
		assert.Equal(t, "r1", out["id"])
		assert.Equal(t, []interface{}{"read"}, out["permissions"])
	})

	t.Run("flattenRole nil", func(t *testing.T) {
		assert.Nil(t, flattenRole(nil))
	})
}

// TestTransformLists covers the four *List transforms including the empty and
// missing-collection fallbacks.
func TestTransformLists(t *testing.T) {
	t.Run("tenant list empty", func(t *testing.T) {
		out := transformTenantList(map[string]interface{}{})
		assert.Equal(t, []interface{}{}, out["tenants"])
	})

	t.Run("user list float total", func(t *testing.T) {
		out := transformUserList(map[string]interface{}{
			"users":       []interface{}{map[string]interface{}{"id": "u1"}},
			"total_count": float64(1),
		})
		pg := out["pagination"].(map[string]interface{})
		assert.Equal(t, 1, pg["total_count"])
	})

	t.Run("group list empty", func(t *testing.T) {
		out := transformGroupList(map[string]interface{}{})
		assert.Equal(t, []interface{}{}, out["groups"])
	})

	t.Run("role list int64 total", func(t *testing.T) {
		out := transformRoleList(map[string]interface{}{
			"roles":       []interface{}{map[string]interface{}{"id": "r1"}},
			"total_count": int64(5),
		})
		pg := out["pagination"].(map[string]interface{})
		assert.Equal(t, 5, pg["total_count"])
	})
}
