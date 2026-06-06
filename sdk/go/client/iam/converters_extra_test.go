// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package iam

import (
	"testing"
	"time"
)

func TestConvertToUser(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if _, ok := convertToUser(nil); ok {
			t.Error("expected ok=false for nil")
		}
	})

	t.Run("already pointer", func(t *testing.T) {
		want := &User{ID: "u1"}
		got, ok := convertToUser(want)
		if !ok || got != want {
			t.Error("expected pass-through of *User")
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		if _, ok := convertToUser(12345); ok {
			t.Error("expected ok=false for non-map")
		}
	})

	t.Run("full map with nested user", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
		resp := map[string]interface{}{
			"user": map[string]interface{}{
				"id":             "u1",
				"tenant_id":      "t1",
				"username":       "alice",
				"email":          "alice@example.com",
				"name":           "Alice",
				"mfa_enabled":    true,
				"email_verified": true,
				"roles":          []interface{}{"r1", "r2"},
				"groups":         []interface{}{"g1"},
				"metadata":       map[string]interface{}{"k": "v"},
				"attributes": map[string]interface{}{
					"department":     "eng",
					"team_id":        "team1",
					"location":       "nyc",
					"cost_center":    "cc1",
					"employee_id":    "e1",
					"manager":        "m1",
					"classification": "secret",
				},
				"created_at":    now,
				"updated_at":    now,
				"last_login_at": now,
			},
		}
		got, ok := convertToUser(resp)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if got.ID != "u1" || got.Username != "alice" || got.DisplayName != "Alice" {
			t.Errorf("basic fields wrong: %+v", got)
		}
		if !got.MFAEnabled || !got.EmailVerified {
			t.Error("bool fields wrong")
		}
		if len(got.Roles) != 2 || len(got.Groups) != 1 {
			t.Errorf("slices wrong: %+v", got)
		}
		if got.Metadata["k"] != "v" {
			t.Error("metadata wrong")
		}
		if got.Attributes == nil || got.Attributes.Department != "eng" || got.Attributes.Classification != "secret" {
			t.Errorf("attributes wrong: %+v", got.Attributes)
		}
		if got.LastLoginAt == nil {
			t.Error("last login should be set")
		}
	})

	t.Run("typed slices and time.Time values", func(t *testing.T) {
		ts := time.Now().UTC()
		resp := map[string]interface{}{
			"id":         "u2",
			"roles":      []string{"a"},
			"groups":     []string{"b"},
			"metadata":   map[string]string{"x": "y"},
			"created_at": ts,
			"updated_at": ts,
		}
		got, ok := convertToUser(resp)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if len(got.Roles) != 1 || got.Roles[0] != "a" {
			t.Error("typed roles wrong")
		}
		if got.Metadata["x"] != "y" {
			t.Error("typed metadata wrong")
		}
		if !got.CreatedAt.Equal(ts) {
			t.Error("time.Time created_at wrong")
		}
	})
}

func TestIsUserSuccessResponse(t *testing.T) {
	tests := []struct {
		name string
		resp interface{}
		want bool
	}{
		{"nil", nil, true},
		{"typed struct", &deleteUserResponse{}, true},
		{"map bool true", map[string]bool{"success": true}, true},
		{"map bool false", map[string]bool{"success": false}, false},
		{"map bool no key", map[string]bool{"other": true}, true},
		{"map iface success true", map[string]interface{}{"success": true}, true},
		{"map iface success false", map[string]interface{}{"success": false}, false},
		{"map iface no key", map[string]interface{}{"other": 1}, true},
		{"unexpected", 123, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUserSuccessResponse(tt.resp); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertToGroup(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if _, ok := convertToGroup(nil); ok {
			t.Error("expected ok=false")
		}
	})
	t.Run("pointer pass-through", func(t *testing.T) {
		g := &Group{ID: "g1"}
		got, ok := convertToGroup(g)
		if !ok || got != g {
			t.Error("expected pass-through")
		}
	})
	t.Run("wrong type", func(t *testing.T) {
		if _, ok := convertToGroup(3.14); ok {
			t.Error("expected ok=false")
		}
	})
	t.Run("nested map", func(t *testing.T) {
		now := time.Now().UTC().Format(time.RFC3339)
		resp := map[string]interface{}{
			"group": map[string]interface{}{
				"id":           "g1",
				"tenant_id":    "t1",
				"name":         "admins",
				"description":  "desc",
				"member_count": float64(5),
				"roles":        []interface{}{"r1"},
				"metadata":     map[string]interface{}{"k": "v"},
				"created_at":   now,
				"updated_at":   now,
			},
		}
		got, ok := convertToGroup(resp)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if got.ID != "g1" || got.MemberCount != 5 {
			t.Errorf("wrong: %+v", got)
		}
		if got.DisplayName != "admins" {
			t.Error("display name should fall back to name")
		}
		if len(got.Roles) != 1 || got.Metadata["k"] != "v" {
			t.Error("roles/metadata wrong")
		}
	})
	t.Run("typed slices and member_count variants", func(t *testing.T) {
		got, ok := convertToGroup(map[string]interface{}{
			"id":           "g2",
			"member_count": int64(7),
			"roles":        []string{"r"},
			"metadata":     map[string]string{"a": "b"},
		})
		if !ok || got.MemberCount != 7 {
			t.Errorf("wrong: %+v", got)
		}
	})
}

func TestIsSuccessResponse(t *testing.T) {
	tests := []struct {
		name string
		resp interface{}
		want bool
	}{
		{"nil", nil, true},
		{"delete resp", &deleteGroupResponse{}, true},
		{"member resp", &groupMemberResponse{}, true},
		{"map bool true", map[string]bool{"success": true}, true},
		{"map bool false", map[string]bool{"success": false}, false},
		{"map iface false", map[string]interface{}{"success": false}, false},
		{"map iface no key", map[string]interface{}{"x": 1}, true},
		{"unexpected", "str", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSuccessResponse(tt.resp); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertToRole(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if _, ok := convertToRole(nil); ok {
			t.Error("expected ok=false")
		}
	})
	t.Run("pointer pass-through", func(t *testing.T) {
		r := &Role{ID: "r1"}
		got, ok := convertToRole(r)
		if !ok || got != r {
			t.Error("expected pass-through")
		}
	})
	t.Run("wrong type", func(t *testing.T) {
		if _, ok := convertToRole([]int{1}); ok {
			t.Error("expected ok=false")
		}
	})
	t.Run("nested map with created/updated aliases", func(t *testing.T) {
		now := time.Now().UTC().Format(time.RFC3339)
		resp := map[string]interface{}{
			"role": map[string]interface{}{
				"id":           "r1",
				"tenant_id":    "t1",
				"name":         "admin",
				"display_name": "Admin",
				"description":  "desc",
				"is_system":    true,
				"permissions":  []interface{}{"p1", "p2"},
				"metadata":     map[string]interface{}{"k": "v"},
				"created":      now,
				"updated":      now,
			},
		}
		got, ok := convertToRole(resp)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if got.ID != "r1" || !got.IsSystem || len(got.Permissions) != 2 {
			t.Errorf("wrong: %+v", got)
		}
		if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
			t.Error("created/updated aliases not parsed")
		}
	})
	t.Run("typed permissions and metadata", func(t *testing.T) {
		got, ok := convertToRole(map[string]interface{}{
			"id":          "r2",
			"permissions": []string{"p"},
			"metadata":    map[string]string{"a": "b"},
			"created_at":  time.Now().UTC().Format(time.RFC3339),
			"updated_at":  time.Now().UTC().Format(time.RFC3339),
		})
		if !ok || len(got.Permissions) != 1 {
			t.Errorf("wrong: %+v", got)
		}
	})
}

func TestConvertToPolicy(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if _, ok := convertToPolicy(nil); ok {
			t.Error("expected ok=false")
		}
	})
	t.Run("pointer pass-through", func(t *testing.T) {
		p := &Policy{Name: "p1"}
		got, ok := convertToPolicy(p)
		if !ok || got != p {
			t.Error("expected pass-through")
		}
	})
	t.Run("wrong type", func(t *testing.T) {
		if _, ok := convertToPolicy(42); ok {
			t.Error("expected ok=false")
		}
	})
	t.Run("server metadata+spec format", func(t *testing.T) {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		resp := map[string]interface{}{
			"policy": map[string]interface{}{
				"metadata": map[string]interface{}{
					"name":        "p1",
					"tenant":      "t1",
					"description": "desc",
					"priority":    float64(10),
					"labels":      map[string]interface{}{"env": "prod"},
					"created":     now,
					"updated":     now,
				},
				"spec": map[string]interface{}{
					"effect": "allow",
					"rules": []interface{}{
						map[string]interface{}{
							"name":      "rule1",
							"effect":    "allow",
							"actions":   []interface{}{"read"},
							"resources": []interface{}{"doc/*"},
						},
					},
				},
			},
		}
		got, ok := convertToPolicy(resp)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if got.Name != "p1" || got.TenantID != "t1" || got.Priority != 10 {
			t.Errorf("metadata wrong: %+v", got)
		}
		if got.Effect != EffectAllow {
			t.Error("effect should be allow")
		}
		if got.Labels["env"] != "prod" {
			t.Error("labels wrong")
		}
		if len(got.Rules) != 1 || got.Rules[0].Name != "rule1" {
			t.Errorf("rules wrong: %+v", got.Rules)
		}
		if len(got.Rules[0].Actions) != 1 || len(got.Rules[0].Resources) != 1 {
			t.Error("rule actions/resources not converted")
		}
	})
	t.Run("flat SDK format", func(t *testing.T) {
		now := time.Now().UTC().Format(time.RFC3339)
		resp := map[string]interface{}{
			"name":        "p2",
			"tenant_id":   "t2",
			"description": "d",
			"version":     float64(3),
			"enabled":     true,
			"priority":    float64(2),
			"created_by":  "admin",
			"updated_by":  "admin",
			"effect":      "ALLOW",
			"labels":      map[string]string{"a": "b"},
			"created_at":  now,
			"updated_at":  now,
		}
		got, ok := convertToPolicy(resp)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if got.Name != "p2" || got.Version != 3 || !got.Enabled {
			t.Errorf("flat wrong: %+v", got)
		}
		if got.Effect != EffectAllow {
			t.Error("effect ALLOW string not parsed")
		}
		if got.Labels["a"] != "b" {
			t.Error("typed labels wrong")
		}
	})
	t.Run("numeric effect and deny strings", func(t *testing.T) {
		got, ok := convertToPolicy(map[string]interface{}{
			"name":   "p3",
			"effect": float64(int(EffectDeny)),
		})
		if !ok || got.Effect != EffectDeny {
			t.Errorf("numeric effect wrong: %+v", got)
		}
	})
}

func TestConvertServerRulesToSDK(t *testing.T) {
	t.Run("deny effect and skip non-map", func(t *testing.T) {
		in := []interface{}{
			"not-a-map",
			map[string]interface{}{
				"name":      "r",
				"effect":    "deny",
				"actions":   []interface{}{"write"},
				"resources": []interface{}{"x"},
			},
		}
		rules := convertServerRulesToSDK(in)
		if len(rules) != 1 {
			t.Fatalf("expected 1 rule, got %d", len(rules))
		}
		if rules[0].Effect != EffectDeny {
			t.Error("expected deny effect")
		}
	})
	t.Run("empty", func(t *testing.T) {
		if rules := convertServerRulesToSDK(nil); len(rules) != 0 {
			t.Error("expected empty")
		}
	})
}

func TestConvertMapToAdminAssignment(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		resp := map[string]interface{}{"id": "a1", "admin_id": "admin1"}
		got, err := convertMapToAdminAssignment(resp, "ctx")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ID != "a1" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("wrong type", func(t *testing.T) {
		if _, err := convertMapToAdminAssignment("nope", "ctx"); err == nil {
			t.Error("expected error for non-map")
		}
	})
}
