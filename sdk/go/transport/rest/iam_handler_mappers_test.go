// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestIAMMappersAppRegisterGrantTypes exercises mapAppRegisterRequest and
// convertGrantTypes by sending integer grant-type enums in the app body.
func TestIAMMappersAppRegisterGrantTypes(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"client_id":"c1"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), iamAppServicePrefix, "Register",
		map[string]interface{}{"tenant_id": "t1", "app": map[string]interface{}{
			"name":            "myapp",
			"grant_types":     []interface{}{float64(0), float64(1), float64(2), "custom"},
			"resource_prefix": "/api",
		}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gts, ok := body["grant_types"].([]interface{})
	if !ok || len(gts) != 4 {
		t.Fatalf("expected 4 converted grant types, got %v", body["grant_types"])
	}
	if gts[0] != "authorization_code" || gts[1] != "client_credentials" || gts[3] != "custom" {
		t.Errorf("unexpected grant type conversion: %v", gts)
	}
}

// TestIAMMappersImpersonationDuration exercises mapImpersonationRequest,
// convertDuration and formatDuration with a nanosecond duration value.
func TestIAMMappersImpersonationDuration(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"session_id":"s1"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)

	cases := []struct {
		name    string
		nanos   float64
		wantDur string
	}{
		{"hours and minutes", float64(90 * time.Minute), "1h30m"},
		{"whole hours", float64(2 * time.Hour), "2h"},
		{"minutes only", float64(45 * time.Minute), "45m"},
		{"seconds only", float64(30 * time.Second), "30s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body = nil
			_, err := tr.Request(context.Background(), iamAdminServicePrefix, "StartImpersonation",
				map[string]interface{}{"impersonation": map[string]interface{}{
					"target_user_id": "u2", "duration": tc.nanos}})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if body["duration"] != tc.wantDur {
				t.Errorf("duration: want %q, got %v", tc.wantDur, body["duration"])
			}
		})
	}
}

// TestIAMMappersTenantSettings exercises mapTenantCreateRequest,
// mapTenantSettings, convertSettingValue and extractQuotasFromSettings.
func TestIAMMappersTenantSettings(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"t1"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), iamTenantServicePrefix, "Create",
		map[string]interface{}{"request": map[string]interface{}{
			"display_name": "Acme",
			"settings": map[string]interface{}{
				"max_users":    "50",
				"mfa_required": "true",
				"theme":        "dark",
			},
		}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["name"] != "Acme" {
		t.Errorf("expected display_name fallback to name, got %v", body["name"])
	}
	quotas, ok := body["quotas"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected quotas extracted, got %v", body["quotas"])
	}
	if quotas["max_users"] != float64(50) {
		t.Errorf("expected max_users=50 in quotas, got %v", quotas["max_users"])
	}
	settings, ok := body["settings"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected settings retained, got %v", body["settings"])
	}
	if settings["mfa_required"] != true {
		t.Errorf("expected mfa_required converted to bool, got %v", settings["mfa_required"])
	}
	if _, present := settings["max_users"]; present {
		t.Error("max_users should have been moved out of settings into quotas")
	}
}

// TestIAMMappersAdminCreateAssignment exercises mapAdminCreateAssignmentRequest
// with the full field set including a zero-time expiry that must be skipped.
func TestIAMMappersAdminCreateAssignment(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"assignment_id":"a1"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), iamAdminServicePrefix, "CreateAssignment",
		map[string]interface{}{
			"user_id":     "u1",
			"role_id":     "tenant_admin",
			"tenant_ids":  []interface{}{"t1", "t2"},
			"permissions": []interface{}{"read", "write"},
			"granted_by":  "admin",
			"expires_at":  "0001-01-01T00:00:00Z",
			"metadata":    map[string]interface{}{"note": "x"},
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["admin_role"] != "tenant_admin" {
		t.Errorf("expected role_id mapped to admin_role, got %v", body["admin_role"])
	}
	if _, present := body["expires_at"]; present {
		t.Error("zero-time expires_at should have been skipped")
	}
	if body["granted_by"] != "admin" {
		t.Errorf("expected granted_by copied, got %v", body["granted_by"])
	}
}

// TestIAMMappersPolicyServerFormat exercises mapServerPolicyToSDK,
// transformServerRulesToSDK and mapServerPolicyListToSDK by returning a
// server-format policy from Create and List.
func TestIAMMappersPolicyServerFormat(t *testing.T) {
	serverPolicy := `{
		"apiVersion": "iam/v1",
		"kind": "Policy",
		"metadata": {"name":"p1","tenant":"t1","description":"d","priority":5,"created":"2025-01-01T00:00:00Z","updated":"2025-01-02T00:00:00Z"},
		"spec": {
			"effect": "allow",
			"enabled": true,
			"targets": ["svc"],
			"conditions": {"k":"v"},
			"rules": [{
				"actions": ["read","write"],
				"resources": ["doc/*"],
				"subjects": {"authenticated": true, "users": ["u1","u2"]}
			}]
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(serverPolicy))
		} else {
			_, _ = w.Write([]byte(`{"policies":[` + serverPolicy + `],"total":1}`))
		}
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	t.Run("Create transforms server policy", func(t *testing.T) {
		resp, err := tr.Request(ctx, iamPolicyServicePrefix, "Create",
			map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"name": "p1"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m, ok := resp.(map[string]interface{})
		if !ok {
			t.Fatalf("expected mapped policy, got %T", resp)
		}
		if m["name"] != "p1" || m["tenant_id"] != "t1" || m["effect"] != "allow" {
			t.Errorf("unexpected mapped policy: %v", m)
		}
		rules, ok := m["rules"].([]interface{})
		if !ok || len(rules) != 1 {
			t.Fatalf("expected 1 transformed rule, got %v", m["rules"])
		}
		rule := rules[0].(map[string]interface{})
		actions := rule["actions"].([]interface{})
		if actions[0].(map[string]interface{})["pattern"] != "read" {
			t.Errorf("expected action pattern matcher, got %v", actions)
		}
		subjects := rule["subjects"].([]interface{})
		if len(subjects) != 3 {
			t.Errorf("expected 3 subject matchers (authenticated + 2 users), got %v", subjects)
		}
	})

	t.Run("List transforms server policies", func(t *testing.T) {
		resp, err := tr.Request(ctx, iamPolicyServicePrefix, "List",
			map[string]interface{}{"tenant_id": "t1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m, ok := resp.(map[string]interface{})
		if !ok {
			t.Fatalf("expected mapped list, got %T", resp)
		}
		if m["total_count"] != float64(1) {
			t.Errorf("expected total_count=1, got %v", m["total_count"])
		}
		policies, ok := m["policies"].([]interface{})
		if !ok || len(policies) != 1 {
			t.Fatalf("expected 1 mapped policy, got %v", m["policies"])
		}
	})
}
