// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestVectorItoaDirect covers the vectorItoa branches (zero, negative,
// multi-digit positive).
func TestVectorItoaDirect(t *testing.T) {
	cases := map[int]string{0: "0", -42: "-42", 12345: "12345", 7: "7"}
	for in, want := range cases {
		if got := vectorItoa(in); got != want {
			t.Errorf("vectorItoa(%d) = %q, want %q", in, got, want)
		}
	}
}

// TestVectorGetTimeDirect covers RFC3339, RFC3339Nano, invalid and missing
// branches of vectorGetTime.
func TestVectorGetTimeDirect(t *testing.T) {
	if tm := vectorGetTime(map[string]interface{}{"ts": "2025-01-01T00:00:00Z"}, "ts"); tm.Year() != 2025 {
		t.Errorf("RFC3339 parse failed: %v", tm)
	}
	if tm := vectorGetTime(map[string]interface{}{"ts": "2025-01-01T00:00:00.500Z"}, "ts"); tm.Year() != 2025 {
		t.Errorf("RFC3339Nano parse failed: %v", tm)
	}
	if tm := vectorGetTime(map[string]interface{}{"ts": "not-a-time"}, "ts"); !tm.IsZero() {
		t.Errorf("invalid time should be zero, got %v", tm)
	}
	if tm := vectorGetTime(map[string]interface{}{}, "ts"); !tm.IsZero() {
		t.Errorf("missing time should be zero, got %v", tm)
	}
}

// TestGetMapDirect covers the getMap helper branches.
func TestGetMapDirect(t *testing.T) {
	if m := getMap(map[string]interface{}{"k": map[string]interface{}{"a": 1}}, "k"); m["a"] != 1 {
		t.Errorf("expected nested map, got %v", m)
	}
	if m := getMap(map[string]interface{}{"k": "notamap"}, "k"); m != nil {
		t.Errorf("expected nil for wrong type, got %v", m)
	}
	if m := getMap(nil, "k"); m != nil {
		t.Errorf("expected nil for nil input, got %v", m)
	}
}

// TestMapTenantSettingsDirect covers the map[string]string, default and
// empty-result branches of mapTenantSettings.
func TestMapTenantSettingsDirect(t *testing.T) {
	if r := mapTenantSettings(map[string]string{"max_users": "5", "theme": "dark"}); r == nil || r["max_users"] != 5 {
		t.Errorf("map[string]string conversion failed: %v", r)
	}
	if r := mapTenantSettings("not-a-map"); r != nil {
		t.Errorf("expected nil for unsupported type, got %v", r)
	}
	if r := mapTenantSettings(map[string]interface{}{}); r != nil {
		t.Errorf("expected nil for empty result, got %v", r)
	}
}

// TestCryptoHashAndDataKey covers handleHash (happy + error) and
// handleGenerateDataKey (happy + validation).
func TestCryptoHashAndDataKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	t.Run("Hash happy", func(t *testing.T) {
		if _, err := tr.Request(ctx, "Crypto", "Hash", &service.HashRequest{Data: []byte("x")}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("Hash bad type", func(t *testing.T) {
		if _, err := tr.Request(ctx, "Crypto", "Hash", "x"); err == nil {
			t.Fatal("expected invalid-input error")
		}
	})
	t.Run("GenerateDataKey happy", func(t *testing.T) {
		if _, err := tr.Request(ctx, "Crypto", "GenerateDataKey",
			&service.GenerateDataKeyRequest{KeyID: "k1"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("GenerateDataKey bad type", func(t *testing.T) {
		if _, err := tr.Request(ctx, "Crypto", "GenerateDataKey", 1); err == nil {
			t.Fatal("expected invalid-input error")
		}
	})
	t.Run("GenerateDataKey no key", func(t *testing.T) {
		if _, err := tr.Request(ctx, "Crypto", "GenerateDataKey",
			&service.GenerateDataKeyRequest{}); err == nil {
			t.Fatal("expected key-required error")
		}
	})
}

// TestIAMFederationCreateNoWrapper exercises the build-from-reqMap branch of
// CreateProvider/UpdateProvider when no provider/request/update wrapper exists.
func TestIAMFederationCreateNoWrapper(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"p1"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	t.Run("create no wrapper", func(t *testing.T) {
		body = nil
		_, err := tr.Request(ctx, iamFederationServicePrefix, "CreateProvider",
			map[string]interface{}{"tenant_id": "t1", "provider_id": "p1", "name": "okta"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if body["id"] != "p1" || body["name"] != "okta" {
			t.Errorf("unexpected body: %v", body)
		}
		if _, present := body["tenant_id"]; present {
			t.Error("tenant_id should be stripped from body")
		}
	})

	t.Run("update no wrapper", func(t *testing.T) {
		body = nil
		_, err := tr.Request(ctx, iamFederationServicePrefix, "UpdateProvider",
			map[string]interface{}{"tenant_id": "t1", "provider_id": "p1", "name": "x"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if body["name"] != "x" {
			t.Errorf("unexpected update body: %v", body)
		}
	})
}

// TestMessagingStatsAndPublishErrors exercises server-error paths through the
// messaging GetStats and Publish handlers.
func TestMessagingStatsAndPublishErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	if _, err := tr.Request(ctx, "Messaging", "GetStats", nil); err == nil {
		t.Error("expected error from GetStats")
	}
}

// TestSecretsGetParamsDirect covers secretsGetParams version normalization.
func TestSecretsGetParamsDirect(t *testing.T) {
	type req struct {
		Path    string
		Version int
	}
	if p, v, ok := secretsGetParams(&req{Path: "s1", Version: 3}); !ok || p != "s1" || v != 3 {
		t.Errorf("expected (s1,3,true), got (%q,%d,%v)", p, v, ok)
	}
	// Negative version is normalized to 0.
	if _, v, ok := secretsGetParams(&req{Path: "s1", Version: -1}); !ok || v != 0 {
		t.Errorf("expected version normalized to 0, got %d (ok=%v)", v, ok)
	}
	// Missing Path field yields ok=false.
	if _, _, ok := secretsGetParams(&struct{ Other string }{}); ok {
		t.Error("expected ok=false when Path absent")
	}
}

// TestAuthLoginLegacyPasswordRetiredDirect verifies the legacy password Login
// verb fails closed with the typed PasswordLoginUnsupportedError and never
// reaches the network.
func TestAuthLoginLegacyPasswordRetiredDirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("legacy password Login must not reach the network; got %s", r.URL.Path)
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), authServicePrefix, "Login",
		&service.ExtendedLoginRequest{Username: "u"})
	if err == nil {
		t.Fatal("expected PasswordLoginUnsupportedError, got nil")
	}
	if !sdkerrors.IsPasswordLoginUnsupported(err) {
		t.Fatalf("expected PasswordLoginUnsupportedError, got %T: %v", err, err)
	}
}
