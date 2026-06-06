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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
)

func TestKMSRESTHandler_GenerateKey_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/kms/keys" {
			t.Errorf("expected /api/v1/kms/keys, got %s", r.URL.Path)
		}
		var body kmsGenerateKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.KeyAttributes == nil || body.KeyAttributes.Algorithm != "AES-256-GCM" {
			t.Errorf("unexpected algorithm: %+v", body.KeyAttributes)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(kmsGenerateKeyWire{
			Key:       &kmsKeyHandleWire{KeyID: "key-1", Version: 1, Algorithm: "AES-256-GCM", Status: "enabled"},
			Metadata:  &kmsKeyMetadataWire{KeyID: "key-1", Algorithm: "AES-256-GCM", Version: 1},
			Timestamp: 1700000000,
		})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.KMS", "GenerateKey", &kmsclient.GenerateKeyRequest{
		Name:      "key-1",
		Algorithm: kmsclient.AlgorithmAES256GCM,
		Usage:     []kmsclient.KeyUsage{kmsclient.KeyUsageEncrypt},
	})
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	resp, ok := result.(*service.GenerateKeyResponse)
	if !ok {
		t.Fatalf("expected *service.GenerateKeyResponse, got %T", result)
	}
	if resp.Metadata == nil || resp.Metadata.KeyID != "key-1" {
		t.Errorf("unexpected metadata: %+v", resp.Metadata)
	}
}

func TestKMSRESTHandler_GetKey_SubstitutesName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/kms/keys/my-key" {
			t.Errorf("expected /api/v1/kms/keys/my-key, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(kmsGenerateKeyWire{
			Metadata:  &kmsKeyMetadataWire{KeyID: "my-key", Algorithm: "RSA-2048", Version: 3, Usage: []string{"sign"}},
			Timestamp: 1700000000,
		})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.KMS", "GetKey", &struct{ KeyID string }{KeyID: "my-key"})
	if err != nil {
		t.Fatalf("GetKey failed: %v", err)
	}
	resp, ok := result.(*service.GetKeyResponse)
	if !ok {
		t.Fatalf("expected *service.GetKeyResponse, got %T", result)
	}
	if resp.Metadata == nil || resp.Metadata.KeyID != "my-key" || resp.Metadata.Version != 3 {
		t.Errorf("unexpected metadata: %+v", resp.Metadata)
	}
}

func TestKMSRESTHandler_GetKey_MissingKeyID(t *testing.T) {
	tr := newTestTransport(t, httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	defer tr.Close()
	_, err := tr.Request(context.Background(), "quicbaas.KMS", "GetKey", &struct{ KeyID string }{})
	if err == nil {
		t.Fatal("expected error for empty key ID")
	}
}

func TestKMSRESTHandler_RotateKey_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/kms/keys/k9/rotate" {
			t.Errorf("expected rotate path, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(kmsRotateKeyWire{KeyID: "k9", OldVersion: 1, NewVersion: 2, Timestamp: 1700000000})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.KMS", "RotateKey", &struct{ KeyID string }{KeyID: "k9"})
	if err != nil {
		t.Fatalf("RotateKey failed: %v", err)
	}
	resp := result.(*service.RotateKeyResponse)
	if resp.NewVersion != 2 {
		t.Errorf("expected new version 2, got %d", resp.NewVersion)
	}
}

func TestKMSRESTHandler_ExportKey_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/kms/keys/src/export" {
			t.Errorf("expected export path, got %s", r.URL.Path)
		}
		var body service.ExportKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.WrappingKeyID != "wrap" {
			t.Errorf("expected wrapping key 'wrap', got %q", body.WrappingKeyID)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(kmsExportKeyWire{
			KeyID: "src", WrappedKey: []byte("wrapped"), WrappingKeyID: "wrap", Timestamp: 1700000000,
		})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.KMS", "ExportKey", &kmsclient.ExportKeyRequest{
		KeyID:         "src",
		WrappingKeyID: "wrap",
	})
	if err != nil {
		t.Fatalf("ExportKey failed: %v", err)
	}
	resp := result.(*service.ExportKeyResponse)
	if string(resp.WrappedKey) != "wrapped" {
		t.Errorf("unexpected wrapped key: %q", resp.WrappedKey)
	}
}

func TestKMSRESTHandler_ListKeys_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom","code":"internal"}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.KMS", "ListKeys", &struct{}{})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

func TestKMSRESTHandler_ListKeys_MalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.KMS", "ListKeys", &struct{}{})
	if err == nil {
		t.Fatal("expected decode error for malformed response")
	}
}

func TestKMSRESTHandler_BackendStatus_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/kms/backend/status" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(kmsBackendStatusWire{
			BackendType: "software", Status: "healthy",
			Capabilities: map[string]bool{"encrypt": true},
		})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.KMS", "BackendStatus", &struct{}{})
	if err != nil {
		t.Fatalf("BackendStatus failed: %v", err)
	}
	resp := result.(*service.BackendStatusResponse)
	if resp.BackendType != "software" || !resp.Capabilities["encrypt"] {
		t.Errorf("unexpected backend status: %+v", resp)
	}
}
