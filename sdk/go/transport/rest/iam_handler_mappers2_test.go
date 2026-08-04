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
)

// TestIAMFederationProviderMappers exercises mapProviderCreateRequest and
// mapProviderUpdateRequest field translations (provider_id->id, issuer_url->
// issuer) and the extractProvidersFromResponse passthrough branch.
func TestIAMFederationProviderMappers(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			// Non-wrapped array response exercises the passthrough branch.
			_, _ = w.Write([]byte(`[{"id":"p1"}]`))
		} else {
			_, _ = w.Write([]byte(`{"id":"p1"}`))
		}
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	t.Run("CreateProvider maps fields and defaults enabled", func(t *testing.T) {
		body = nil
		_, err := tr.Request(ctx, iamFederationServicePrefix, "CreateProvider",
			map[string]interface{}{"tenant_id": "t1", "provider": map[string]interface{}{
				"provider_id": "p1", "issuer_url": "https://idp", "name": "okta"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if body["id"] != "p1" || body["issuer"] != "https://idp" || body["enabled"] != true {
			t.Errorf("unexpected mapped create body: %v", body)
		}
	})

	t.Run("UpdateProvider maps issuer_url and skips routing fields", func(t *testing.T) {
		body = nil
		_, err := tr.Request(ctx, iamFederationServicePrefix, "UpdateProvider",
			map[string]interface{}{"tenant_id": "t1", "provider_id": "p1",
				"update": map[string]interface{}{"issuer_url": "https://idp2", "name": "x"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if body["issuer"] != "https://idp2" || body["name"] != "x" {
			t.Errorf("unexpected mapped update body: %v", body)
		}
	})

	t.Run("ListProviders passthrough non-wrapped", func(t *testing.T) {
		resp, err := tr.Request(ctx, iamFederationServicePrefix, "ListProviders",
			map[string]interface{}{"tenant_id": "t1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// A bare array response is returned as-is.
		if _, ok := resp.([]interface{}); !ok {
			t.Errorf("expected passthrough array, got %T", resp)
		}
	})
}

// TestIAMIdentityListPassthrough exercises the extractIdentitiesFromResponse
// passthrough branch for a non-wrapped array response.
func TestIAMIdentityListPassthrough(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"provider_id":"p1"}]`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	resp, err := tr.Request(context.Background(), iamIdentityServicePrefix, "ListUserIdentities",
		map[string]interface{}{"tenant_id": "t1", "user_id": "u1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := resp.([]interface{}); !ok {
		t.Errorf("expected passthrough array, got %T", resp)
	}
}

// TestIAMImpersonationDurationStringAndDefault exercises convertDuration's
// string and default branches via StartImpersonation.
func TestIAMImpersonationDurationStringAndDefault(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"session_id":"s1"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	t.Run("string duration passthrough", func(t *testing.T) {
		body = nil
		_, err := tr.Request(ctx, iamAdminServicePrefix, "StartImpersonation",
			map[string]interface{}{"impersonation": map[string]interface{}{"duration": "2h"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if body["duration"] != "2h" {
			t.Errorf("expected string duration passthrough, got %v", body["duration"])
		}
	})

	t.Run("non-duration-type yields empty", func(t *testing.T) {
		body = nil
		_, err := tr.Request(ctx, iamAdminServicePrefix, "StartImpersonation",
			map[string]interface{}{"impersonation": map[string]interface{}{"duration": true}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if body["duration"] != "" {
			t.Errorf("expected empty duration for unsupported type, got %v", body["duration"])
		}
	})
}

// TestIAMTenantUpdateSettings exercises mapTenantUpdateRequest with a settings
// block (display_name->name, settings conversion, max_users kept in settings).
func TestIAMTenantUpdateSettings(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"t1"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), iamTenantServicePrefix, "Update",
		map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{
			"display_name": "Renamed",
			"settings":     map[string]interface{}{"max_users": "10", "mfa_required": "false"},
		}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["name"] != "Renamed" {
		t.Errorf("expected display_name mapped to name, got %v", body["name"])
	}
	settings, ok := body["settings"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected settings retained, got %v", body["settings"])
	}
	if settings["max_users"] != float64(10) {
		t.Errorf("expected max_users=10 kept in settings as int, got %v", settings["max_users"])
	}
}
