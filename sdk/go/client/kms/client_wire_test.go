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

// client_wire_test.go pins the JSON wire shape of the internal (unexported)
// KMS request types. The QUIC and MCP transports marshal these structs
// directly, so their JSON tags must match the canonical service.*Request
// snake_case fields the servers decode.

package kms

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// TestRevokeKeyRequest_WireShape asserts the revoke wire request carries
// key_id and an omitted-when-empty reason.
func TestRevokeKeyRequest_WireShape(t *testing.T) {
	raw, err := json.Marshal(&revokeKeyRequest{KeyID: "k1"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != `{"key_id":"k1"}` {
		t.Errorf("wire = %s, want {\"key_id\":\"k1\"}", raw)
	}

	raw, err = json.Marshal(&revokeKeyRequest{KeyID: "k1", Reason: "compromised"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var svcReq service.RevokeKeyRequest
	if err := json.Unmarshal(raw, &svcReq); err != nil {
		t.Fatalf("unmarshal into service request: %v", err)
	}
	if svcReq.KeyID != "k1" || svcReq.Reason != "compromised" {
		t.Errorf("service request = %+v, want key_id=k1 reason=compromised", svcReq)
	}
}

// TestRevokeKey_SendsEmptyReason asserts the public RevokeKey(ctx, keyID)
// signature passes an empty reason on the wire request.
func TestRevokeKey_SendsEmptyReason(t *testing.T) {
	var captured interface{}
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			if method != "RevokeKey" {
				t.Errorf("method = %q, want RevokeKey", method)
			}
			captured = req
			return &service.RevokeKeyResponse{KeyID: "k1"}, nil
		},
	}
	client := NewClient(mockT)

	if err := client.RevokeKey(context.Background(), "k1", ""); err != nil {
		t.Fatalf("RevokeKey: %v", err)
	}
	req, ok := captured.(*revokeKeyRequest)
	if !ok {
		t.Fatalf("unexpected request type %T", captured)
	}
	if req.KeyID != "k1" {
		t.Errorf("KeyID = %q, want k1", req.KeyID)
	}
	if req.Reason != "" {
		t.Errorf("Reason = %q, want empty (public signature does not expose it)", req.Reason)
	}
}

// TestInternalKeyIDRequests_WireShape asserts every single-key internal
// request marshals its identifier as key_id, matching the canonical service
// request shape decoded by the QUIC server handlers.
func TestInternalKeyIDRequests_WireShape(t *testing.T) {
	tests := []struct {
		name string
		req  interface{}
	}{
		{"getKeyRequest", &getKeyRequest{KeyID: "k1"}},
		{"getPublicKeyRequest", &getPublicKeyRequest{KeyID: "k1"}},
		{"rotateKeyRequest", &rotateKeyRequest{KeyID: "k1"}},
		{"deleteKeyRequest", &deleteKeyRequest{KeyID: "k1"}},
		{"listKeyVersionsRequest", &listKeyVersionsRequest{KeyID: "k1"}},
		{"getKeyMetadataRequest", &getKeyMetadataRequest{KeyID: "k1"}},
		{"getKeyPolicyRequest", &getKeyPolicyRequest{KeyID: "k1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.req)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded map[string]interface{}
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if decoded["key_id"] != "k1" {
				t.Errorf("wire = %s, want key_id=k1", raw)
			}
		})
	}
}

// TestInternalKeyIDRequests_InvalidWireShape asserts a PascalCase KeyID field
// (the pre-tag wire shape) no longer appears on the wire.
func TestInternalKeyIDRequests_InvalidWireShape(t *testing.T) {
	raw, err := json.Marshal(&getKeyRequest{KeyID: "k1"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, exists := decoded["KeyID"]; exists {
		t.Errorf("wire still carries PascalCase KeyID: %s", raw)
	}
}
