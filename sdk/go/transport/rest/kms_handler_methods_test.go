// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// kmsKeyIDReq mirrors the internal request the SDK builds for the reflective
// KeyID-based methods (GetKey, RotateKey, DeleteKey, ListKeyVersions,
// GetKeyPolicy): a struct exposing an exported KeyID field.
type kmsKeyIDReq struct {
	KeyID string
}

// TestKMSHandlerMethodsHappyPaths drives the KMS methods that previously lacked
// coverage, asserting the HTTP method and request URI each builds.
func TestKMSHandlerMethodsHappyPaths(t *testing.T) {
	var gotMethod, gotURI string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotURI = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/kms/keys/import":
			_, _ = w.Write([]byte(`{"key":{"key_id":"k1","version":1,"algorithm":"AES-256","status":"enabled"},"timestamp":1700000000}`))
		case "/api/v1/kms/keys/k1/versions":
			_, _ = w.Write([]byte(`{"key_id":"k1","versions":[{"version":1,"status":"enabled","algorithm":"AES-256","created_at":1700000000}]}`))
		case "/api/v1/kms/keys/k1/policy":
			_, _ = w.Write([]byte(`{"policy":{"key_id":"k1","version":1,"allowed_operations":["encrypt"],"allowed_principals":[],"denied_principals":[],"require_mfa":true,"max_usage_count":10,"expires_at":"2027-01-15T08:00:00Z"},"policy_version":1,"timestamp":1700000000}`))
		default:
			_, _ = w.Write([]byte(`{"key_id":"k1","status":"pending_deletion","timestamp":1700000000,` +
				`"key":{"key_id":"k1","version":1,"algorithm":"AES-256","status":"enabled"},` +
				`"backend_type":"xkms","capabilities":{"export":true}}`))
		}
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	cases := []struct {
		name       string
		method     string
		req        interface{}
		wantMethod string
		wantURI    string
	}{
		{"ImportKey", "ImportKey",
			&kmsclient.ImportKeyRequest{KeyMaterial: []byte("material"), Name: "k1",
				Algorithm: kmsclient.KeyAlgorithm(0), Usage: []kmsclient.KeyUsage{kmsclient.KeyUsageEncrypt}},
			http.MethodPost, "/api/v1/kms/keys/import"},
		{"DeleteKey", "DeleteKey",
			&kmsKeyIDReq{KeyID: "k1"},
			http.MethodDelete, "/api/v1/kms/keys/k1"},
		{"UnwrapKey", "UnwrapKey",
			&kmsclient.UnwrapKeyRequest{KeyID: "k1", WrappedMaterial: []byte("wrapped"),
				WrappingAlgorithm: kmsclient.WrapAlgAESKW, Algorithm: kmsclient.KeyAlgorithm(0)},
			http.MethodPost, "/api/v1/kms/keys/unwrap"},
		{"UpdateKeyStatus", "UpdateKeyStatus",
			&kmsclient.UpdateKeyStatusRequest{KeyID: "k1", Status: kmsclient.KeyStatusDisabled},
			http.MethodPut, "/api/v1/kms/keys/k1/status"},
		{"ListKeyVersions", "ListKeyVersions",
			&kmsKeyIDReq{KeyID: "k1"},
			http.MethodGet, "/api/v1/kms/keys/k1/versions"},
		{"SetKeyPolicy", "SetKeyPolicy",
			&kmsclient.SetKeyPolicyRequest{KeyID: "k1", Policy: &kmsclient.KeyPolicy{
				Version: 1, AllowedOperations: []kmsclient.KeyUsage{kmsclient.KeyUsageEncrypt},
				RequireMFA: true, MaxUsageCount: 10}},
			http.MethodPut, "/api/v1/kms/keys/k1/policy"},
		{"GetKeyPolicy", "GetKeyPolicy",
			&kmsKeyIDReq{KeyID: "k1"},
			http.MethodGet, "/api/v1/kms/keys/k1/policy"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotMethod, gotURI = "", ""
			resp, err := tr.Request(ctx, "KMS", tc.method, tc.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp == nil {
				t.Fatal("expected non-nil response")
			}
			if gotMethod != tc.wantMethod {
				t.Errorf("HTTP method: want %s, got %s", tc.wantMethod, gotMethod)
			}
			if gotURI != tc.wantURI {
				t.Errorf("URI: want %s, got %s", tc.wantURI, gotURI)
			}
		})
	}
}

// TestKMSHandlerExportKey covers the ExportKey wrapping-algorithm conversion.
func TestKMSHandlerExportKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/kms/keys/k1/export" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key_id":"k1","wrapped_key":"d3JhcA==","wrapping_key_id":"wk","wrapping_algorithm":"RSA-OAEP-SHA256","format":"pem","timestamp":1700000000}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	resp, err := tr.Request(context.Background(), "KMS", "ExportKey",
		&kmsclient.ExportKeyRequest{KeyID: "k1", WrappingKeyID: "wk",
			WrappingAlgorithm: kmsclient.WrapAlgRSAOAEPSHA256, Version: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

// TestKMSHandlerMethodsValidation exercises the request-required and
// key-ID/material guards.
func TestKMSHandlerMethodsValidation(t *testing.T) {
	tr := newMappingTransport(t)
	ctx := context.Background()

	cases := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"ImportBadType", "ImportKey", "x"},
		{"DeleteNoKeyID", "DeleteKey", &kmsKeyIDReq{}},
		{"ExportBadType", "ExportKey", 1},
		{"ExportNoKeyID", "ExportKey", &kmsclient.ExportKeyRequest{}},
		{"UnwrapBadType", "UnwrapKey", "x"},
		{"UnwrapNoMaterial", "UnwrapKey", &kmsclient.UnwrapKeyRequest{KeyID: "k1"}},
		{"UpdateStatusBadType", "UpdateKeyStatus", "x"},
		{"UpdateStatusNoKeyID", "UpdateKeyStatus", &kmsclient.UpdateKeyStatusRequest{}},
		{"ListVersionsNoKeyID", "ListKeyVersions", &kmsKeyIDReq{}},
		{"SetPolicyBadType", "SetKeyPolicy", 1},
		{"SetPolicyNoKeyID", "SetKeyPolicy", &kmsclient.SetKeyPolicyRequest{Policy: &kmsclient.KeyPolicy{}}},
		{"SetPolicyNoPolicy", "SetKeyPolicy", &kmsclient.SetKeyPolicyRequest{KeyID: "k1"}},
		{"GetPolicyNoKeyID", "GetKeyPolicy", &kmsKeyIDReq{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tr.Request(ctx, "KMS", tc.method, tc.req)
			if err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

// A negative policy version is refused before the request leaves: converted
// to the service's uint64 it would wrap to a huge version.
func TestKMSHandlerSetKeyPolicyRefusesNegativeVersion(t *testing.T) {
	tr := newMappingTransport(t)

	_, err := tr.Request(context.Background(), "KMS", "SetKeyPolicy",
		&kmsclient.SetKeyPolicyRequest{KeyID: "k1", Policy: &kmsclient.KeyPolicy{Version: -1}})
	if !errors.IsInvalidInput(err) || !strings.Contains(err.Error(), "policy version") {
		t.Fatalf("err = %v, want an invalid-input refusal naming the policy version", err)
	}
}

// TestKMSWrappingAlgConversions exercises the remaining wrapping-algorithm
// branches via ExportKey requests against a recording server.
func TestKMSWrappingAlgConversions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key_id":"k1","timestamp":1700000000}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	algs := []kmsclient.WrappingAlgorithm{
		kmsclient.WrapAlgAESKW, kmsclient.WrapAlgAESKWP, kmsclient.WrapAlgAESGCM,
		kmsclient.WrapAlgRSAOAEPSHA256, kmsclient.WrapAlgRSAOAEPSHA384,
		kmsclient.WrapAlgRSAOAEPSHA512, kmsclient.WrapAlgECDHESA256KW,
	}
	for i, alg := range algs {
		_, err := tr.Request(ctx, "KMS", "ExportKey",
			&kmsclient.ExportKeyRequest{KeyID: "k1", WrappingAlgorithm: alg})
		if err != nil {
			t.Fatalf("alg index %d: unexpected error: %v", i, err)
		}
	}
}

// TestKMSWrappingAlgToString pins each wrapping algorithm to the exact name
// the service accepts (isValidWrappingAlgorithm in pkg/service/kms_helpers.go).
// ECDH-ES uses the RFC 7518 §4.6 name ECDH-ES+A256KW (go-quicbaas#1620).
func TestKMSWrappingAlgToString(t *testing.T) {
	tests := []struct {
		alg  kmsclient.WrappingAlgorithm
		want string
	}{
		{kmsclient.WrapAlgAESKW, "AES-KW"},
		{kmsclient.WrapAlgAESKWP, "AES-KWP"},
		{kmsclient.WrapAlgAESGCM, "AES-GCM"},
		{kmsclient.WrapAlgRSAOAEPSHA256, "RSA-OAEP-SHA256"},
		{kmsclient.WrapAlgRSAOAEPSHA384, "RSA-OAEP-SHA384"},
		{kmsclient.WrapAlgRSAOAEPSHA512, "RSA-OAEP-SHA512"},
		{kmsclient.WrapAlgECDHESA256KW, "ECDH-ES+A256KW"},
		{kmsclient.WrappingAlgorithm(255), ""},
	}
	for _, tt := range tests {
		if got := kmsWrappingAlgToString(tt.alg); got != tt.want {
			t.Errorf("kmsWrappingAlgToString(%d) = %q, want %q", tt.alg, got, tt.want)
		}
	}
}

// newWrappingAlgRecorder returns a server that records the wrapping_algorithm
// field of each decoded JSON request body and answers with respBody.
func newWrappingAlgRecorder(t *testing.T, respBody string) (*httptest.Server, <-chan string) {
	t.Helper()
	got := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			WrappingAlgorithm string `json:"wrapping_algorithm"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		got <- body.WrappingAlgorithm
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(respBody)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server, got
}

// TestKMSHandlerECDHESWireName checks ExportKey and UnwrapKey with ECDH-ES
// send the RFC 7518 name the service's algorithm check accepts.
func TestKMSHandlerECDHESWireName(t *testing.T) {
	tests := []struct {
		method string
		req    interface{}
		resp   string
	}{
		{
			method: "ExportKey",
			req: &kmsclient.ExportKeyRequest{KeyID: "k1", WrappingKeyID: "wk",
				WrappingAlgorithm: kmsclient.WrapAlgECDHESA256KW},
			resp: `{"key_id":"k1","wrapped_key":"d3JhcA==","wrapping_key_id":"wk","wrapping_algorithm":"ECDH-ES+A256KW","format":"raw","timestamp":1700000000}`,
		},
		{
			method: "UnwrapKey",
			req: &kmsclient.UnwrapKeyRequest{KeyID: "k2", WrappingKeyID: "wk",
				WrappedMaterial: []byte("wrap"), WrappingAlgorithm: kmsclient.WrapAlgECDHESA256KW},
			resp: `{"key_id":"k2","version":1,"algorithm":"AES-256-GCM","status":"enabled","timestamp":1700000000}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			server, gotAlg := newWrappingAlgRecorder(t, tt.resp)
			tr := newTestTransport(t, server)
			if _, err := tr.Request(context.Background(), "KMS", tt.method, tt.req); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := <-gotAlg; got != "ECDH-ES+A256KW" {
				t.Errorf("wrapping_algorithm = %q, want %q", got, "ECDH-ES+A256KW")
			}
		})
	}
}

// TestKMSHandlerExportKeyUnknownWrappingAlg checks an ExportKey with an
// unknown wrapping algorithm sends an empty name, which the service refuses.
// The transport itself does not refuse it.
func TestKMSHandlerExportKeyUnknownWrappingAlg(t *testing.T) {
	server, gotAlg := newWrappingAlgRecorder(t, `{"key_id":"k1","timestamp":1700000000}`)
	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), "KMS", "ExportKey",
		&kmsclient.ExportKeyRequest{KeyID: "k1", WrappingAlgorithm: kmsclient.WrappingAlgorithm(255)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := <-gotAlg; got != "" {
		t.Errorf("wrapping_algorithm = %q, want empty", got)
	}
}

// TestKMSUsageAndStatusConversions exercises kmsUsageToString and
// kmsStatusToString across all enum values via ImportKey (usages) and
// UpdateKeyStatus (statuses).
func TestKMSUsageAndStatusConversions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key_id":"k1","status":"enabled","timestamp":1700000000,` +
			`"key":{"key_id":"k1","version":1,"algorithm":"AES-256","status":"enabled"}}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	t.Run("all key usages via ImportKey", func(t *testing.T) {
		usages := []kmsclient.KeyUsage{
			kmsclient.KeyUsageEncrypt, kmsclient.KeyUsageDecrypt, kmsclient.KeyUsageSign,
			kmsclient.KeyUsageVerify, kmsclient.KeyUsageWrap, kmsclient.KeyUsageUnwrap,
		}
		_, err := tr.Request(ctx, "KMS", "ImportKey",
			&kmsclient.ImportKeyRequest{KeyMaterial: []byte("m"), Name: "k1", Usage: usages})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("all key statuses via UpdateKeyStatus", func(t *testing.T) {
		statuses := []kmsclient.KeyStatus{
			kmsclient.KeyStatusEnabled, kmsclient.KeyStatusDisabled,
			kmsclient.KeyStatusPendingDeletion, kmsclient.KeyStatusDeleted,
		}
		for _, s := range statuses {
			_, err := tr.Request(ctx, "KMS", "UpdateKeyStatus",
				&kmsclient.UpdateKeyStatusRequest{KeyID: "k1", Status: s})
			if err != nil {
				t.Fatalf("status %v: unexpected error: %v", s, err)
			}
		}
	})
}
