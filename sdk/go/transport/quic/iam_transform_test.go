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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestIAMHandler() *iamHandler {
	return newIAMHandler(nil)
}

func TestStructToMap(t *testing.T) {
	t.Run("already a map", func(t *testing.T) {
		in := map[string]interface{}{"a": 1}
		out, err := structToMap(in)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})
	t.Run("struct", func(t *testing.T) {
		out, err := structToMap(&iam.CreateTenantRequest{ID: "t1", Name: "Acme"})
		require.NoError(t, err)
		assert.Equal(t, "t1", out["id"])
		assert.Equal(t, "Acme", out["name"])
	})
}

// TestTransformRequest_Routing drives the service-name dispatch in
// (*iamHandler).transformRequest, covering each sub-service branch plus the
// default pass-through.
func TestTransformRequest_Routing(t *testing.T) {
	h := newTestIAMHandler()

	t.Run("MFA", func(t *testing.T) {
		out, err := h.transformRequest("quicbaas.IAM.MFA", "GetMFAStatus", map[string]interface{}{"user_id": "u"})
		require.NoError(t, err)
		sub := out.(*IAMSubRequest)
		assert.Equal(t, SubOpMFAGetStatus, sub.SubOp)
	})

	t.Run("MFA unknown method", func(t *testing.T) {
		out, err := h.transformRequest("quicbaas.IAM.MFA", "Nope", nil)
		require.NoError(t, err)
		assert.Equal(t, byte(0xFF), out.(*IAMSubRequest).SubOp)
	})

	t.Run("Tenant create", func(t *testing.T) {
		out, err := h.transformRequest("quicbaas.IAM.Tenant", "Create", &iam.CreateTenantRequest{ID: "t1", Name: "Acme"})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "t1", m["id"])
		assert.Equal(t, "Acme", m["name"])
	})

	t.Run("Tenant get renames tenant_id", func(t *testing.T) {
		out, err := h.transformRequest("quicbaas.IAM.Tenant", "Get", map[string]interface{}{"tenant_id": "t1"})
		require.NoError(t, err)
		assert.Equal(t, "t1", out.(map[string]interface{})["id"])
	})

	t.Run("User", func(t *testing.T) {
		out, err := h.transformRequest("quicbaas.IAM.User", "Create", map[string]interface{}{
			"tenant_id": "t1",
			"request":   map[string]interface{}{"username": "alice", "email": "a@b.c", "display_name": "Alice"},
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "alice", m["username"])
		assert.Equal(t, "Alice", m["name"])
	})

	t.Run("Group", func(t *testing.T) {
		out, err := h.transformRequest("quicbaas.IAM.Group", "Create", map[string]interface{}{
			"tenant_id": "t1",
			"request":   map[string]interface{}{"name": "admins", "description": "d"},
		})
		require.NoError(t, err)
		assert.Equal(t, "admins", out.(map[string]interface{})["name"])
	})

	t.Run("Role", func(t *testing.T) {
		out, err := h.transformRequest("quicbaas.IAM.Role", "Create", map[string]interface{}{
			"tenant_id": "t1",
			"request":   map[string]interface{}{"name": "viewer", "permissions": []interface{}{"read"}},
		})
		require.NoError(t, err)
		assert.Equal(t, "viewer", out.(map[string]interface{})["name"])
	})

	t.Run("Policy", func(t *testing.T) {
		out, err := h.transformRequest("quicbaas.IAM.Policy", "Create", map[string]interface{}{
			"tenant_id": "t1",
			"request":   map[string]interface{}{"name": "p1", "description": "d"},
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.NotNil(t, m["policy"])
	})

	t.Run("App", func(t *testing.T) {
		out, err := h.transformRequest("quicbaas.IAM.App", "Register", map[string]interface{}{
			"tenant_id": "t1",
			"request":   map[string]interface{}{"name": "myapp", "type": "web"},
		})
		require.NoError(t, err)
		assert.Equal(t, "myapp", out.(map[string]interface{})["name"])
	})

	t.Run("default pass-through", func(t *testing.T) {
		in := map[string]interface{}{"x": 1}
		out, err := h.transformRequest("quicbaas.IAM.Session", "Whatever", in)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})
}

// TestTransformGetRequest covers the field-rename helper used by Get/Delete.
func TestTransformGetRequest(t *testing.T) {
	t.Run("renames present field", func(t *testing.T) {
		out, err := transformGetRequest(map[string]interface{}{"tenant_id": "t", "policy_name": "p"}, "policy_name", "name")
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "p", m["name"])
		assert.NotContains(t, m, "policy_name")
	})

	t.Run("target already present", func(t *testing.T) {
		out, err := transformGetRequest(map[string]interface{}{"name": "p"}, "policy_name", "name")
		require.NoError(t, err)
		assert.Equal(t, "p", out.(map[string]interface{})["name"])
	})

	t.Run("struct input", func(t *testing.T) {
		out, err := transformGetRequest(&iam.CreateTenantRequest{ID: "x"}, "id", "id")
		require.NoError(t, err)
		assert.Equal(t, "x", out.(map[string]interface{})["id"])
	})
}

// TestTransformUpdateRequest covers flattening of the nested request object.
func TestTransformUpdateRequest(t *testing.T) {
	out, err := transformUpdateRequest(map[string]interface{}{
		"tenant_id": "t1",
		"request":   map[string]interface{}{"name": "New", "settings": map[string]interface{}{"k": "v"}},
		"extra":     "kept",
	}, "tenant_id", "id")
	require.NoError(t, err)
	m := out.(map[string]interface{})
	assert.Equal(t, "t1", m["id"])
	assert.Equal(t, "New", m["name"])
	assert.Equal(t, "kept", m["extra"])
}

func TestGetStringAndInterfaceField(t *testing.T) {
	m := map[string]interface{}{"lower": "a", "Upper": "b", "iface": 5}

	v, ok := getStringField(m, "lower", "Lower")
	assert.True(t, ok)
	assert.Equal(t, "a", v)

	v, ok = getStringField(m, "upper", "Upper")
	assert.True(t, ok)
	assert.Equal(t, "b", v)

	_, ok = getStringField(m, "missing", "Missing")
	assert.False(t, ok)

	iv, ok := getInterfaceField(m, "iface", "Iface")
	assert.True(t, ok)
	assert.Equal(t, 5, iv)

	_, ok = getInterfaceField(m, "missing", "Missing")
	assert.False(t, ok)
}

// TestTransformUserCreateRequest covers both the nested and flat formats and
// the display_name->name and metadata->attributes mappings.
func TestTransformUserCreateRequest(t *testing.T) {
	t.Run("nested", func(t *testing.T) {
		out, err := transformUserCreateRequest(map[string]interface{}{
			"tenant_id": "t1",
			"request": map[string]interface{}{
				"username": "bob", "display_name": "Bob", "email": "b@x.y",
				"roles": []interface{}{"r1"}, "groups": []interface{}{"g1"},
				"metadata": map[string]interface{}{"k": "v"},
			},
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "bob", m["username"])
		assert.Equal(t, "Bob", m["name"])
		assert.Equal(t, "b@x.y", m["email"])
		assert.Equal(t, map[string]interface{}{"k": "v"}, m["attributes"])
	})

	t.Run("flat with name fallback", func(t *testing.T) {
		out, err := transformUserCreateRequest(map[string]interface{}{
			"tenant_id": "t1", "username": "carol", "name": "Carol", "email": "c@x.y",
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "Carol", m["name"])
	})
}

func TestTransformUserUpdateRequest(t *testing.T) {
	out, err := transformUserUpdateRequest(map[string]interface{}{
		"tenant_id": "t1", "user_id": "u1",
		"request": map[string]interface{}{
			"username": "dan", "display_name": "Dan", "email": "d@x.y",
			"metadata": map[string]interface{}{"k": "v"},
		},
	})
	require.NoError(t, err)
	m := out.(map[string]interface{})
	assert.Equal(t, "u1", m["user_id"])
	require.NotNil(t, m["username"])
	assert.Equal(t, "dan", *m["username"].(*string))
	assert.Equal(t, "Dan", *m["name"].(*string))
}

func TestTransformUserListRequest(t *testing.T) {
	out, err := transformUserListRequest(map[string]interface{}{
		"tenant_id": "t1",
		"request": map[string]interface{}{
			"page_size": float64(20), "role_id": "r1", "group_id": "g1",
		},
	})
	require.NoError(t, err)
	m := out.(map[string]interface{})
	assert.Equal(t, "t1", m["tenant_id"])
	assert.Equal(t, 20, m["limit"])
	f := m["filter"].(map[string]interface{})
	assert.Equal(t, "r1", f["role_id"])
	assert.Equal(t, "g1", f["group_id"])
}

func TestTransformGroupRequests(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		out, err := transformGroupCreateRequest(map[string]interface{}{
			"tenant_id": "t1",
			"request":   map[string]interface{}{"id": "g1", "name": "admins", "description": "d", "metadata": map[string]interface{}{"k": "v"}},
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "g1", m["id"])
		assert.Equal(t, "admins", m["name"])
	})

	t.Run("update", func(t *testing.T) {
		out, err := transformGroupUpdateRequest(map[string]interface{}{
			"tenant_id": "t1", "group_id": "g1",
			"request": map[string]interface{}{"name": "newname", "description": "newdesc"},
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "newname", *m["name"].(*string))
		assert.Equal(t, "newdesc", *m["description"].(*string))
	})

	t.Run("list", func(t *testing.T) {
		out, err := transformGroupListRequest(map[string]interface{}{
			"tenant_id": "t1",
			"request":   map[string]interface{}{"page_size": float64(5), "filter": "x"},
		})
		require.NoError(t, err)
		assert.Equal(t, 5, out.(map[string]interface{})["limit"])
	})
}

func TestTransformRoleRequests(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		out, err := transformRoleCreateRequest(map[string]interface{}{
			"tenant_id": "t1",
			"request":   map[string]interface{}{"id": "r1", "name": "viewer", "description": "d", "permissions": []interface{}{"read"}},
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "viewer", m["name"])
		assert.Equal(t, []interface{}{"read"}, m["permissions"])
	})

	t.Run("update", func(t *testing.T) {
		out, err := transformRoleUpdateRequest(map[string]interface{}{
			"tenant_id": "t1", "role_id": "r1",
			"request": map[string]interface{}{"name": "n", "description": "d", "permissions": []interface{}{"write"}},
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "n", *m["name"].(*string))
	})

	t.Run("list", func(t *testing.T) {
		out, err := transformRoleListRequest(map[string]interface{}{
			"tenant_id": "t1",
			"request":   map[string]interface{}{"limit": float64(7)},
		})
		require.NoError(t, err)
		assert.Equal(t, 7, out.(map[string]interface{})["limit"])
	})
}

func TestTransformPolicyRequests(t *testing.T) {
	t.Run("create from complete policy", func(t *testing.T) {
		out, err := transformPolicyCreateRequest(map[string]interface{}{
			"tenant_id": "t1",
			"policy":    map[string]interface{}{"name": "p1"},
		})
		require.NoError(t, err)
		assert.NotNil(t, out.(map[string]interface{})["policy"])
	})

	t.Run("create from nested request", func(t *testing.T) {
		out, err := transformPolicyCreateRequest(map[string]interface{}{
			"tenant_id": "t1",
			"request": map[string]interface{}{
				"name": "p1", "description": "d", "enabled": true,
				"priority": float64(3), "rules": []interface{}{"r"}, "conditions": []interface{}{"c"},
			},
		})
		require.NoError(t, err)
		p := out.(map[string]interface{})["policy"].(map[string]interface{})
		assert.Equal(t, "p1", p["name"])
		assert.Equal(t, true, p["enabled"])
		assert.Equal(t, 3, p["priority"])
	})

	t.Run("update", func(t *testing.T) {
		out, err := transformPolicyUpdateRequest(map[string]interface{}{
			"tenant_id": "t1", "policy_name": "p1",
			"request": map[string]interface{}{"description": "d"},
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "p1", m["name"])
		assert.NotNil(t, m["policy"])
	})

	t.Run("list", func(t *testing.T) {
		out, err := transformPolicyListRequest(map[string]interface{}{
			"tenant_id": "t1",
			"request":   map[string]interface{}{"page_size": float64(10), "page_token": "tok", "filter": "f"},
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, 10, m["limit"])
		assert.Equal(t, "tok", m["offset"])
	})

	t.Run("test", func(t *testing.T) {
		out, err := transformPolicyTestRequest(map[string]interface{}{
			"tenant_id": "t1",
			"request":   map[string]interface{}{"policy_name": "p1", "test_cases": []interface{}{"tc"}},
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "p1", m["policy_name"])
		assert.Equal(t, []interface{}{"tc"}, m["test_cases"])
	})
}

func TestTransformAppRequests(t *testing.T) {
	t.Run("register", func(t *testing.T) {
		out, err := transformAppRegisterRequest(map[string]interface{}{
			"tenant_id": "t1",
			"request": map[string]interface{}{
				"name": "app", "description": "d", "type": "web",
				"redirect_uris": []interface{}{"http://x"}, "scopes": []interface{}{"s"},
			},
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "app", m["name"])
		assert.Equal(t, "web", m["type"])
	})

	t.Run("update", func(t *testing.T) {
		out, err := transformAppUpdateRequest(map[string]interface{}{
			"tenant_id": "t1", "app_id": "a1",
			"request": map[string]interface{}{"name": "newapp"},
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "a1", m["id"])
		assert.Equal(t, "newapp", m["name"])
	})

	t.Run("list", func(t *testing.T) {
		out, err := transformAppListRequest(map[string]interface{}{
			"tenant_id": "t1",
			"request":   map[string]interface{}{"page_size": float64(4), "page_token": "p", "filter": "f"},
		})
		require.NoError(t, err)
		assert.Equal(t, 4, out.(map[string]interface{})["limit"])
	})
}

// TestTransformUppercaseKeyFallbacks feeds the transforms a struct without JSON
// tags so structToMap produces PascalCase keys, exercising the uppercase-key
// fallback branches (TenantID/Request/Name/...).
func TestTransformUppercaseKeyFallbacks(t *testing.T) {
	type inner struct {
		Username    string
		DisplayName string
		Email       string
		Name        string
		Description string
		Permissions []string
		PageSize    float64
		PageToken   string
		Filter      string
		Enabled     bool
		Priority    float64
	}
	type outer struct {
		TenantID   string
		UserID     string
		GroupID    string
		RoleID     string
		AppID      string
		PolicyName string
		Request    inner
	}

	t.Run("user create uppercase", func(t *testing.T) {
		out, err := transformUserCreateRequest(outer{
			TenantID: "t1",
			Request:  inner{Username: "u", DisplayName: "U", Email: "e@x.y"},
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "u", m["username"])
		assert.Equal(t, "U", m["name"])
	})

	t.Run("user update uppercase", func(t *testing.T) {
		out, err := transformUserUpdateRequest(outer{
			TenantID: "t1", UserID: "u1",
			Request: inner{Username: "u", DisplayName: "U", Email: "e@x.y"},
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "u1", m["user_id"])
	})

	t.Run("user list uppercase", func(t *testing.T) {
		out, err := transformUserListRequest(outer{
			TenantID: "t1", Request: inner{PageSize: 5, Filter: "f"},
		})
		require.NoError(t, err)
		assert.Equal(t, 5, out.(map[string]interface{})["limit"])
	})

	t.Run("group create uppercase", func(t *testing.T) {
		out, err := transformGroupCreateRequest(outer{
			TenantID: "t1", Request: inner{Name: "g", Description: "d"},
		})
		require.NoError(t, err)
		assert.Equal(t, "g", out.(map[string]interface{})["name"])
	})

	t.Run("group update uppercase", func(t *testing.T) {
		out, err := transformGroupUpdateRequest(outer{
			TenantID: "t1", GroupID: "g1", Request: inner{Name: "g", Description: "d"},
		})
		require.NoError(t, err)
		assert.Equal(t, "g1", out.(map[string]interface{})["group_id"])
	})

	t.Run("group list uppercase", func(t *testing.T) {
		out, err := transformGroupListRequest(outer{TenantID: "t1", Request: inner{PageSize: 3, Filter: "f"}})
		require.NoError(t, err)
		assert.Equal(t, 3, out.(map[string]interface{})["limit"])
	})

	t.Run("role create uppercase", func(t *testing.T) {
		out, err := transformRoleCreateRequest(outer{
			TenantID: "t1", Request: inner{Name: "r", Description: "d", Permissions: []string{"read"}},
		})
		require.NoError(t, err)
		assert.Equal(t, "r", out.(map[string]interface{})["name"])
	})

	t.Run("role update uppercase", func(t *testing.T) {
		out, err := transformRoleUpdateRequest(outer{
			TenantID: "t1", RoleID: "r1", Request: inner{Name: "r", Description: "d", Permissions: []string{"x"}},
		})
		require.NoError(t, err)
		assert.Equal(t, "r1", out.(map[string]interface{})["role_id"])
	})

	t.Run("role list uppercase", func(t *testing.T) {
		out, err := transformRoleListRequest(outer{TenantID: "t1", Request: inner{PageSize: 2, Filter: "f"}})
		require.NoError(t, err)
		assert.Equal(t, 2, out.(map[string]interface{})["limit"])
	})

	t.Run("policy create uppercase", func(t *testing.T) {
		out, err := transformPolicyCreateRequest(outer{
			TenantID: "t1", Request: inner{Name: "p", Description: "d", Enabled: true, Priority: 1},
		})
		require.NoError(t, err)
		assert.NotNil(t, out.(map[string]interface{})["policy"])
	})

	t.Run("policy update uppercase", func(t *testing.T) {
		out, err := transformPolicyUpdateRequest(outer{
			TenantID: "t1", PolicyName: "p1", Request: inner{Description: "d"},
		})
		require.NoError(t, err)
		assert.Equal(t, "p1", out.(map[string]interface{})["name"])
	})

	t.Run("policy list uppercase", func(t *testing.T) {
		out, err := transformPolicyListRequest(outer{TenantID: "t1", Request: inner{PageSize: 4, PageToken: "tok", Filter: "f"}})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, 4, m["limit"])
		assert.Equal(t, "tok", m["offset"])
	})

	t.Run("app register uppercase", func(t *testing.T) {
		out, err := transformAppRegisterRequest(outer{TenantID: "t1", Request: inner{Name: "a", Description: "d"}})
		require.NoError(t, err)
		assert.Equal(t, "a", out.(map[string]interface{})["name"])
	})

	t.Run("app update uppercase", func(t *testing.T) {
		out, err := transformAppUpdateRequest(outer{TenantID: "t1", AppID: "a1", Request: inner{Name: "a"}})
		require.NoError(t, err)
		assert.Equal(t, "a1", out.(map[string]interface{})["id"])
	})

	t.Run("app list uppercase", func(t *testing.T) {
		out, err := transformAppListRequest(outer{TenantID: "t1", Request: inner{PageSize: 6, PageToken: "p", Filter: "f"}})
		require.NoError(t, err)
		assert.Equal(t, 6, out.(map[string]interface{})["limit"])
	})
}

func TestExtractMethodName_Empty(t *testing.T) {
	assert.Equal(t, "", extractMethodName(""))
}
