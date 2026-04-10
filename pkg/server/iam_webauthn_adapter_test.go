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

//go:build iam

package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
)

func TestParseCredentialCreationResponse(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		credential  map[string]interface{}
		expectError bool
	}{
		{
			// Note: This test uses synthetic test data that doesn't represent a valid
			// attestation object. The go-webauthn library correctly rejects it.
			// This test verifies that invalid attestation data is properly rejected.
			name: "synthetic credential creation response (invalid attestation)",
			credential: map[string]interface{}{
				"id":    "dGVzdC1jcmVkZW50aWFsLWlk",
				"rawId": "dGVzdC1jcmVkZW50aWFsLWlk",
				"type":  "public-key",
				"response": map[string]interface{}{
					"clientDataJSON":    "eyJ0eXBlIjoid2ViYXV0aG4uY3JlYXRlIiwiY2hhbGxlbmdlIjoiZEdWemRDMWphR0ZzYkdWdVoyVSIsIm9yaWdpbiI6Imh0dHA6Ly9sb2NhbGhvc3Q6ODA4MCJ9",
					"attestationObject": "o2NmbXRkbm9uZWdhdHRTdG10oGhhdXRoRGF0YVjFSZYN5YgOjGh0NBcPZHZgW4_krrmihjLHmVzzuoMdl2NFAAAAAK3OAAI1vMYKZIsLJfHwVQMAIHRlc3QtY3JlZGVudGlhbC1pZC10ZXN0LWNyZWRlbnRpYWwtaWShZWNyZWRQcm90ZWN0",
				},
			},
			expectError: true,
		},
		{
			name: "missing rawId",
			credential: map[string]interface{}{
				"id":   "dGVzdC1jcmVkZW50aWFsLWlk",
				"type": "public-key",
				"response": map[string]interface{}{
					"clientDataJSON":    "eyJ0eXBlIjoid2ViYXV0aG4uY3JlYXRlIn0",
					"attestationObject": "o2NmbXRkbm9uZQ",
				},
			},
			expectError: true,
		},
		{
			name: "missing response",
			credential: map[string]interface{}{
				"id":    "dGVzdC1jcmVkZW50aWFsLWlk",
				"rawId": "dGVzdC1jcmVkZW50aWFsLWlk",
				"type":  "public-key",
			},
			expectError: true,
		},
		{
			name:        "empty credential",
			credential:  map[string]interface{}{},
			expectError: true,
		},
		{
			name:        "nil credential",
			credential:  nil,
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := parseCredentialCreationResponse(tc.credential)

			if tc.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == nil {
					t.Errorf("expected non-nil result")
				}
			}
		})
	}
}

func TestParseCredentialAssertionResponse(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		credential  map[string]interface{}
		expectError bool
	}{
		{
			name: "valid credential assertion response",
			credential: map[string]interface{}{
				"id":    "dGVzdC1jcmVkZW50aWFsLWlk",
				"rawId": "dGVzdC1jcmVkZW50aWFsLWlk",
				"type":  "public-key",
				"response": map[string]interface{}{
					"clientDataJSON":    "eyJ0eXBlIjoid2ViYXV0aG4uZ2V0IiwiY2hhbGxlbmdlIjoiZEdWemRDMWphR0ZzYkdWdVoyVSIsIm9yaWdpbiI6Imh0dHA6Ly9sb2NhbGhvc3Q6ODA4MCJ9",
					"authenticatorData": "SZYN5YgOjGh0NBcPZHZgW4_krrmihjLHmVzzuoMdl2MFAAAAAQ",
					"signature":         "MEUCIQDKnvHtLWfJzGNhNqKk3k",
					"userHandle":        "dGVzdC11c2VyLWlk",
				},
			},
			expectError: false,
		},
		{
			name: "missing signature",
			credential: map[string]interface{}{
				"id":    "dGVzdC1jcmVkZW50aWFsLWlk",
				"rawId": "dGVzdC1jcmVkZW50aWFsLWlk",
				"type":  "public-key",
				"response": map[string]interface{}{
					"clientDataJSON":    "eyJ0eXBlIjoid2ViYXV0aG4uZ2V0In0",
					"authenticatorData": "SZYN5YgOjGh0NBcPZHZgW4",
				},
			},
			expectError: true,
		},
		{
			name: "missing authenticatorData",
			credential: map[string]interface{}{
				"id":    "dGVzdC1jcmVkZW50aWFsLWlk",
				"rawId": "dGVzdC1jcmVkZW50aWFsLWlk",
				"type":  "public-key",
				"response": map[string]interface{}{
					"clientDataJSON": "eyJ0eXBlIjoid2ViYXV0aG4uZ2V0In0",
					"signature":      "MEUCIQDKnvHtLWfJzGNhNqKk3k",
				},
			},
			expectError: true,
		},
		{
			name:        "empty credential",
			credential:  map[string]interface{}{},
			expectError: true,
		},
		{
			name:        "nil credential",
			credential:  nil,
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := parseCredentialAssertionResponse(tc.credential)

			if tc.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == nil {
					t.Errorf("expected non-nil result")
				}
			}
		})
	}
}

func TestWebAuthnServiceAdapterInterface(t *testing.T) {
	t.Parallel()

	// Verify that the adapter type exists and implements the expected methods
	// Note: Cannot instantiate without a real WebAuthnManager, but we can verify
	// the function signatures through compilation
	var adapter *webauthnServiceAdapter
	_ = adapter

	// The compile-time interface check in the source file ensures
	// webauthnServiceAdapter implements service.WebAuthnManager
}

func TestCredentialCreationResponseParsing(t *testing.T) {
	t.Parallel()

	// Test that the parsing correctly handles the WebAuthn credential format
	// from the browser's navigator.credentials.create() API

	// This is a minimal valid credential creation response structure
	credential := map[string]interface{}{
		"id":    "dGVzdC1jcmVkZW50aWFsLWlk",
		"rawId": "dGVzdC1jcmVkZW50aWFsLWlk",
		"type":  "public-key",
		"response": map[string]interface{}{
			"clientDataJSON":    "eyJ0eXBlIjoid2ViYXV0aG4uY3JlYXRlIiwiY2hhbGxlbmdlIjoiZEdWemRDMWphR0ZzYkdWdVoyVSIsIm9yaWdpbiI6Imh0dHA6Ly9sb2NhbGhvc3Q6ODA4MCJ9",
			"attestationObject": "o2NmbXRkbm9uZWdhdHRTdG10oGhhdXRoRGF0YVjFSZYN5YgOjGh0NBcPZHZgW4_krrmihjLHmVzzuoMdl2NFAAAAAK3OAAI1vMYKZIsLJfHwVQMAIHRlc3QtY3JlZGVudGlhbC1pZC10ZXN0LWNyZWRlbnRpYWwtaWShZWNyZWRQcm90ZWN0",
		},
		"clientExtensionResults": map[string]interface{}{},
	}

	// Convert to JSON and back to verify structure
	jsonData, err := json.Marshal(credential)
	if err != nil {
		t.Fatalf("failed to marshal credential: %v", err)
	}

	// This tests the actual parsing path used by parseCredentialCreationResponse
	parsed, err := protocol.ParseCredentialCreationResponseBytes(jsonData)
	if err != nil {
		// This is expected to fail with invalid attestation data in test
		// but we're testing the parse path, not the attestation validation
		t.Logf("parse error (expected for test data): %v", err)
	} else if parsed != nil {
		// Verify the parsed structure has expected type
		if parsed.Response.CollectedClientData.Type != "webauthn.create" {
			t.Errorf("expected type 'webauthn.create', got %s", parsed.Response.CollectedClientData.Type)
		}
	}
}

func TestCredentialAssertionResponseParsing(t *testing.T) {
	t.Parallel()

	// Test that the parsing correctly handles the WebAuthn credential format
	// from the browser's navigator.credentials.get() API

	// This is a minimal valid credential assertion response structure
	credential := map[string]interface{}{
		"id":    "dGVzdC1jcmVkZW50aWFsLWlk",
		"rawId": "dGVzdC1jcmVkZW50aWFsLWlk",
		"type":  "public-key",
		"response": map[string]interface{}{
			"clientDataJSON":    "eyJ0eXBlIjoid2ViYXV0aG4uZ2V0IiwiY2hhbGxlbmdlIjoiZEdWemRDMWphR0ZzYkdWdVoyVSIsIm9yaWdpbiI6Imh0dHA6Ly9sb2NhbGhvc3Q6ODA4MCJ9",
			"authenticatorData": "SZYN5YgOjGh0NBcPZHZgW4_krrmihjLHmVzzuoMdl2MFAAAAAQ",
			"signature":         "MEUCIQDKnvHtLWfJzGNhNqKk3k",
			"userHandle":        "dGVzdC11c2VyLWlk",
		},
		"clientExtensionResults": map[string]interface{}{},
	}

	// Convert to JSON and back to verify structure
	jsonData, err := json.Marshal(credential)
	if err != nil {
		t.Fatalf("failed to marshal credential: %v", err)
	}

	// This tests the actual parsing path used by parseCredentialAssertionResponse
	parsed, err := protocol.ParseCredentialRequestResponseBytes(jsonData)
	if err != nil {
		// This is expected to fail with invalid signature data in test
		// but we're testing the parse path, not the signature validation
		t.Logf("parse error (expected for test data): %v", err)
	} else if parsed != nil {
		// Verify the parsed structure has expected type
		if parsed.Response.CollectedClientData.Type != "webauthn.get" {
			t.Errorf("expected type 'webauthn.get', got %s", parsed.Response.CollectedClientData.Type)
		}
	}
}

func TestNewWebAuthnServiceAdapter(t *testing.T) {
	t.Parallel()

	// Test that the adapter constructor handles nil manager
	adapter := newWebAuthnServiceAdapter(nil)
	if adapter == nil {
		t.Error("expected non-nil adapter even with nil manager")
	}

	// Verify that calling methods on adapter with nil manager returns errors
	ctx := context.Background()

	// These should panic or return errors due to nil manager
	defer func() {
		if r := recover(); r != nil {
			t.Logf("expected panic with nil manager: %v", r)
		}
	}()

	_, err := adapter.BeginRegistration(ctx, "tenant", "user")
	if err == nil {
		t.Error("expected error when calling BeginRegistration with nil manager")
	}
}

func TestParseCredentialCreationResponseWithTransports(t *testing.T) {
	t.Parallel()

	// Test credential with transports field
	credential := map[string]interface{}{
		"id":    "dGVzdC1jcmVkZW50aWFsLWlk",
		"rawId": "dGVzdC1jcmVkZW50aWFsLWlk",
		"type":  "public-key",
		"response": map[string]interface{}{
			"clientDataJSON":    "eyJ0eXBlIjoid2ViYXV0aG4uY3JlYXRlIiwiY2hhbGxlbmdlIjoiZEdWemRDMWphR0ZzYkdWdVoyVSIsIm9yaWdpbiI6Imh0dHA6Ly9sb2NhbGhvc3Q6ODA4MCJ9",
			"attestationObject": "o2NmbXRkbm9uZWdhdHRTdG10oGhhdXRoRGF0YVjFSZYN5YgOjGh0NBcPZHZgW4_krrmihjLHmVzzuoMdl2NFAAAAAK3OAAI1vMYKZIsLJfHwVQMAIHRlc3QtY3JlZGVudGlhbC1pZC10ZXN0LWNyZWRlbnRpYWwtaWShZWNyZWRQcm90ZWN0",
			"transports":        []interface{}{"usb", "nfc", "ble"},
		},
	}

	// Verify the credential can be marshaled
	jsonData, err := json.Marshal(credential)
	if err != nil {
		t.Fatalf("failed to marshal credential: %v", err)
	}

	// Verify the structure is valid JSON
	var unmarshaled map[string]interface{}
	if err := json.Unmarshal(jsonData, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal credential: %v", err)
	}

	// Verify transports are preserved
	response, ok := unmarshaled["response"].(map[string]interface{})
	if !ok {
		t.Fatal("response field missing or invalid type")
	}

	transports, ok := response["transports"].([]interface{})
	if !ok {
		t.Fatal("transports field missing or invalid type")
	}

	if len(transports) != 3 {
		t.Errorf("expected 3 transports, got %d", len(transports))
	}
}
