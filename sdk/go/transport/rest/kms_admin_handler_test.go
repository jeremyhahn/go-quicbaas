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

// kms_admin_handler_test.go covers the typed REST routes for the KMS
// key-administration verbs (revoke, public key, metadata, supported
// algorithms, rotation policy) wired for full transport parity.

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
)

// =============================================================================
// RevokeKey
// =============================================================================

func TestKMSRESTHandler_RevokeKey_MapsReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/kms/keys/k1/revoke" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var body kmsRevokeKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Reason != "compromised" {
			t.Errorf("reason = %q, want compromised", body.Reason)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(kmsRevokeKeyWire{
			KeyID: "k1", Status: "compromised", Timestamp: 1700000000,
		})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.KMS", "RevokeKey",
		&struct{ KeyID, Reason string }{KeyID: "k1", Reason: "compromised"})
	if err != nil {
		t.Fatalf("RevokeKey failed: %v", err)
	}
	resp, ok := result.(*service.RevokeKeyResponse)
	if !ok {
		t.Fatalf("expected *service.RevokeKeyResponse, got %T", result)
	}
	if resp.KeyID != "k1" || string(resp.Status) != "compromised" {
		t.Errorf("unexpected response %+v", resp)
	}
}

func TestKMSRESTHandler_RevokeKey_MissingKeyID(t *testing.T) {
	tr := newTestTransport(t, httptest.NewServer(http.NotFoundHandler()))
	defer tr.Close()

	if _, err := tr.Request(context.Background(), "quicbaas.KMS", "RevokeKey",
		&struct{ KeyID string }{}); err == nil {
		t.Fatal("expected error for missing key ID")
	}
}

// =============================================================================
// GetPublicKey
// =============================================================================

func TestKMSRESTHandler_GetPublicKey_HappyPath(t *testing.T) {
	der := []byte{0x30, 0x82, 0x01, 0x22}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/kms/keys/k1/public" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(kmsGetPublicKeyWire{
			KeyID: "k1", PublicKey: der, Algorithm: "RSA-2048", Version: 3, Timestamp: 1700000000,
		})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.KMS", "GetPublicKey",
		&struct{ KeyID string }{KeyID: "k1"})
	if err != nil {
		t.Fatalf("GetPublicKey failed: %v", err)
	}
	resp, ok := result.(*service.GetPublicKeyResponse)
	if !ok {
		t.Fatalf("expected *service.GetPublicKeyResponse, got %T", result)
	}
	if resp.KeyID != "k1" || len(resp.PublicKey) != len(der) || resp.Version != 3 {
		t.Errorf("unexpected response %+v", resp)
	}
}

func TestKMSRESTHandler_GetPublicKey_MissingKeyID(t *testing.T) {
	tr := newTestTransport(t, httptest.NewServer(http.NotFoundHandler()))
	defer tr.Close()

	if _, err := tr.Request(context.Background(), "quicbaas.KMS", "GetPublicKey",
		&struct{ KeyID string }{}); err == nil {
		t.Fatal("expected error for missing key ID")
	}
}

// =============================================================================
// GetKeyMetadata / UpdateKeyMetadata
// =============================================================================

func TestKMSRESTHandler_GetKeyMetadata_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/kms/keys/k1/metadata" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(kmsKeyMetadataEnvelopeWire{
			Metadata:  &kmsKeyMetadataWire{KeyID: "k1", Name: "meta", Algorithm: "AES-256-GCM", Version: 2},
			Timestamp: 1700000000,
		})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.KMS", "GetKeyMetadata",
		&struct{ KeyID string }{KeyID: "k1"})
	if err != nil {
		t.Fatalf("GetKeyMetadata failed: %v", err)
	}
	resp, ok := result.(*service.GetKeyMetadataResponse)
	if !ok {
		t.Fatalf("expected *service.GetKeyMetadataResponse, got %T", result)
	}
	if resp.Metadata == nil || resp.Metadata.KeyID != "k1" || resp.Metadata.Name != "meta" {
		t.Errorf("unexpected metadata %+v", resp.Metadata)
	}
}

func TestKMSRESTHandler_GetKeyMetadata_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	if _, err := tr.Request(context.Background(), "quicbaas.KMS", "GetKeyMetadata",
		&struct{ KeyID string }{KeyID: "missing"}); err == nil {
		t.Fatal("expected error from server")
	}
}

func TestKMSRESTHandler_UpdateKeyMetadata_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/kms/keys/k1/metadata" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body kmsUpdateKeyMetadataRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Name == nil || *body.Name != "renamed" {
			t.Errorf("name = %v, want renamed", body.Name)
		}
		if body.Description != nil {
			t.Errorf("description = %v, want absent", body.Description)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(kmsKeyMetadataEnvelopeWire{
			Metadata:  &kmsKeyMetadataWire{KeyID: "k1", Name: "renamed", Algorithm: "AES-256-GCM", Version: 2},
			Timestamp: 1700000000,
		})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.KMS", "UpdateKeyMetadata",
		&kmsclient.UpdateKeyMetadataRequest{KeyID: "k1", Name: "renamed", RemoveTags: []string{"stale"}})
	if err != nil {
		t.Fatalf("UpdateKeyMetadata failed: %v", err)
	}
	resp, ok := result.(*service.UpdateKeyMetadataResponse)
	if !ok {
		t.Fatalf("expected *service.UpdateKeyMetadataResponse, got %T", result)
	}
	if resp.Metadata == nil || resp.Metadata.Name != "renamed" {
		t.Errorf("unexpected metadata %+v", resp.Metadata)
	}
}

func TestKMSRESTHandler_UpdateKeyMetadata_MissingKeyID(t *testing.T) {
	tr := newTestTransport(t, httptest.NewServer(http.NotFoundHandler()))
	defer tr.Close()

	if _, err := tr.Request(context.Background(), "quicbaas.KMS", "UpdateKeyMetadata",
		&kmsclient.UpdateKeyMetadataRequest{}); err == nil {
		t.Fatal("expected error for missing key ID")
	}
}

// =============================================================================
// GetSupportedAlgorithms
// =============================================================================

func TestKMSRESTHandler_GetSupportedAlgorithms_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/kms/algorithms" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(kmsSupportedAlgorithmsWire{
			Algorithms: []string{"AES-256-GCM", "RSA-2048"},
			Timestamp:  1700000000,
		})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.KMS", "GetSupportedAlgorithms", struct{}{})
	if err != nil {
		t.Fatalf("GetSupportedAlgorithms failed: %v", err)
	}
	algorithms, ok := result.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", result)
	}
	if len(algorithms) != 2 || algorithms[0] != "AES-256-GCM" {
		t.Errorf("unexpected algorithms %v", algorithms)
	}
}

func TestKMSRESTHandler_GetSupportedAlgorithms_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	if _, err := tr.Request(context.Background(), "quicbaas.KMS", "GetSupportedAlgorithms", struct{}{}); err == nil {
		t.Fatal("expected error from server")
	}
}

// =============================================================================
// Rotation policy
// =============================================================================

func TestKMSRESTHandler_SetKeyRotationPolicy_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/kms/keys/k1/rotation-policy" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body kmsSetRotationPolicyRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if !body.Enabled || body.IntervalSeconds != 86400 || !body.AutoRotate || body.RetainVersions != 3 {
			t.Errorf("unexpected body %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(kmsSetKeyRotationPolicyWire{
			KeyID: "k1",
			Policy: &kmsRotationPolicyWire{
				Enabled: true, IntervalSeconds: 86400, RetainVersions: 3, AutoRotate: true,
				NextRotationUnix: 1700086400,
			},
			Timestamp: 1700000000,
		})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	// Mirror the SDK client's unexported flat setKeyRotationPolicyRequest.
	result, err := tr.Request(context.Background(), "quicbaas.KMS", "SetKeyRotationPolicy",
		&struct {
			KeyID           string
			AutoRotate      bool
			IntervalSeconds int64
			RetainVersions  int
			Enabled         bool
		}{KeyID: "k1", AutoRotate: true, IntervalSeconds: 86400, RetainVersions: 3, Enabled: true})
	if err != nil {
		t.Fatalf("SetKeyRotationPolicy failed: %v", err)
	}
	resp, ok := result.(*service.SetKeyRotationPolicyResponse)
	if !ok {
		t.Fatalf("expected *service.SetKeyRotationPolicyResponse, got %T", result)
	}
	if resp.Policy == nil || resp.Policy.Interval != 24*time.Hour {
		t.Errorf("unexpected policy %+v", resp.Policy)
	}
}

func TestKMSRESTHandler_SetKeyRotationPolicy_MissingKeyID(t *testing.T) {
	tr := newTestTransport(t, httptest.NewServer(http.NotFoundHandler()))
	defer tr.Close()

	if _, err := tr.Request(context.Background(), "quicbaas.KMS", "SetKeyRotationPolicy",
		&struct{ KeyID string }{}); err == nil {
		t.Fatal("expected error for missing key ID")
	}
}

func TestKMSRESTHandler_GetKeyRotationPolicy_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/kms/keys/k1/rotation-policy" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(kmsGetKeyRotationPolicyWire{
			KeyID:     "k1",
			Policy:    &kmsRotationPolicyWire{Enabled: true, IntervalSeconds: 3600, RetainVersions: 2},
			Timestamp: 1700000000,
		})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.KMS", "GetKeyRotationPolicy",
		&struct{ KeyID string }{KeyID: "k1"})
	if err != nil {
		t.Fatalf("GetKeyRotationPolicy failed: %v", err)
	}
	resp, ok := result.(*service.GetKeyRotationPolicyResponse)
	if !ok {
		t.Fatalf("expected *service.GetKeyRotationPolicyResponse, got %T", result)
	}
	if resp.Policy == nil || resp.Policy.Interval != time.Hour {
		t.Errorf("unexpected policy %+v", resp.Policy)
	}
}

func TestKMSRESTHandler_DeleteKeyRotationPolicy_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/kms/keys/k1/rotation-policy" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(kmsDeleteKeyRotationPolicyWire{KeyID: "k1", Timestamp: 1700000000})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.KMS", "DeleteKeyRotationPolicy",
		&struct{ KeyID string }{KeyID: "k1"})
	if err != nil {
		t.Fatalf("DeleteKeyRotationPolicy failed: %v", err)
	}
	resp, ok := result.(*service.DeleteKeyRotationPolicyResponse)
	if !ok {
		t.Fatalf("expected *service.DeleteKeyRotationPolicyResponse, got %T", result)
	}
	if resp.KeyID != "k1" {
		t.Errorf("unexpected response %+v", resp)
	}
}

func TestKMSRESTHandler_RotationPolicy_MissingKeyID(t *testing.T) {
	tr := newTestTransport(t, httptest.NewServer(http.NotFoundHandler()))
	defer tr.Close()

	if _, err := tr.Request(context.Background(), "quicbaas.KMS", "GetKeyRotationPolicy",
		&struct{ KeyID string }{}); err == nil {
		t.Fatal("expected error for missing key ID on get")
	}
	if _, err := tr.Request(context.Background(), "quicbaas.KMS", "DeleteKeyRotationPolicy",
		&struct{ KeyID string }{}); err == nil {
		t.Fatal("expected error for missing key ID on delete")
	}
}

// TestKMSRESTHandler_SetKeyPolicy_SendsServiceShape asserts the SetKeyPolicy
// body is the canonical service.SetKeyPolicyRequest envelope ({"policy": ...})
// the server unmarshals, not a bare flat policy.
func TestKMSRESTHandler_SetKeyPolicy_SendsServiceShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/kms/keys/k1/policy" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body service.SetKeyPolicyRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Policy == nil {
			t.Fatal("expected nested policy in request body")
		}
		if body.Policy.KeyID != "k1" || len(body.Policy.AllowedOperations) != 1 {
			t.Errorf("unexpected policy %+v", body.Policy)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(kmsSetKeyPolicyWire{KeyID: "k1", PolicyVersion: 2, Timestamp: 1700000000})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.KMS", "SetKeyPolicy",
		&kmsclient.SetKeyPolicyRequest{
			KeyID: "k1",
			Policy: &kmsclient.KeyPolicy{
				KeyID:             "k1",
				AllowedOperations: []kmsclient.KeyUsage{kmsclient.KeyUsageEncrypt},
				UpdatedAt:         time.Now(),
			},
		})
	if err != nil {
		t.Fatalf("SetKeyPolicy failed: %v", err)
	}
	resp, ok := result.(*service.SetKeyPolicyResponse)
	if !ok {
		t.Fatalf("expected *service.SetKeyPolicyResponse, got %T", result)
	}
	if resp.PolicyVersion != 2 {
		t.Errorf("unexpected response %+v", resp)
	}
}
