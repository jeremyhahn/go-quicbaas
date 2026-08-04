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

// kms_admin_handler_test.go covers the QUIC transport wiring for the KMS
// key-administration verbs: opcode resolution, response decoding, and the
// request transforms that re-encode SDK request shapes into the canonical
// service wire types.

package quic

import (
	"encoding/json"
	"testing"
	"time"

	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// =============================================================================
// Opcode resolution
// =============================================================================

func TestMethodToOpcode_KMSAdminVerbs(t *testing.T) {
	tests := []struct {
		method string
		want   byte
	}{
		{"RevokeKey", OpKMSRevokeKey},
		{"GetPublicKey", OpKMSGetPublicKey},
		{"GetSupportedAlgorithms", OpKMSGetSupportedAlgorithms},
		{"SetKeyRotationPolicy", OpKMSSetRotationPolicy},
		{"GetKeyRotationPolicy", OpKMSGetRotationPolicy},
		{"DeleteKeyRotationPolicy", OpKMSDeleteRotationPolicy},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			opcode, ok := MethodToOpcode("quicbaas.KMS", tt.method)
			if !ok {
				t.Fatalf("no opcode for quicbaas.KMS/%s", tt.method)
			}
			if opcode != tt.want {
				t.Errorf("opcode = 0x%02X, want 0x%02X", opcode, tt.want)
			}
		})
	}
}

func TestOpcodeToMethod_KMSAdminVerbs(t *testing.T) {
	tests := []struct {
		opcode byte
		want   string
	}{
		{OpKMSRevokeKey, "quicbaas.KMS/RevokeKey"},
		{OpKMSGetPublicKey, "quicbaas.KMS/GetPublicKey"},
		{OpKMSGetSupportedAlgorithms, "quicbaas.KMS/GetSupportedAlgorithms"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			method, ok := OpcodeToMethod(tt.opcode)
			if !ok || method != tt.want {
				t.Errorf("OpcodeToMethod(0x%02X) = (%q, %v), want %q", tt.opcode, method, ok, tt.want)
			}
		})
	}
}

// =============================================================================
// Response decoding
// =============================================================================

func TestDecodeKMSResponse_RevokeKey(t *testing.T) {
	payload, err := json.Marshal(&service.RevokeKeyResponse{KeyID: "k1", Status: "compromised"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded, err := decodeKMSResponse("RevokeKey", payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp, ok := decoded.(*service.RevokeKeyResponse)
	if !ok {
		t.Fatalf("unexpected type %T", decoded)
	}
	if resp.KeyID != "k1" {
		t.Errorf("KeyID = %q, want k1", resp.KeyID)
	}
}

func TestDecodeKMSResponse_GetPublicKey(t *testing.T) {
	payload, err := json.Marshal(&service.GetPublicKeyResponse{
		KeyID:     "k1",
		PublicKey: []byte{0x30, 0x01},
		Version:   2,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded, err := decodeKMSResponse("GetPublicKey", payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp, ok := decoded.(*service.GetPublicKeyResponse)
	if !ok {
		t.Fatalf("unexpected type %T", decoded)
	}
	if resp.KeyID != "k1" || len(resp.PublicKey) != 2 || resp.Version != 2 {
		t.Errorf("unexpected response %+v", resp)
	}
}

func TestDecodeKMSResponse_GetSupportedAlgorithms(t *testing.T) {
	payload := []byte(`["AES-256-GCM","RSA-2048"]`)
	decoded, err := decodeKMSResponse("GetSupportedAlgorithms", payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	algorithms, ok := decoded.([]string)
	if !ok {
		t.Fatalf("unexpected type %T", decoded)
	}
	if len(algorithms) != 2 || algorithms[0] != "AES-256-GCM" {
		t.Errorf("unexpected algorithms %v", algorithms)
	}
}

func TestDecodeKMSResponse_GetSupportedAlgorithms_InvalidJSON(t *testing.T) {
	if _, err := decodeKMSResponse("GetSupportedAlgorithms", []byte("{not-an-array")); err == nil {
		t.Fatal("expected decode error for malformed payload")
	}
}

func TestDecodeKMSResponse_RotationPolicyVerbs(t *testing.T) {
	setPayload, err := json.Marshal(&service.SetKeyRotationPolicyResponse{
		KeyID:  "k1",
		Policy: &service.RotationPolicy{Enabled: true, Interval: time.Hour},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded, err := decodeKMSResponse("SetKeyRotationPolicy", setPayload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	setResp, ok := decoded.(*service.SetKeyRotationPolicyResponse)
	if !ok {
		t.Fatalf("unexpected type %T", decoded)
	}
	if setResp.Policy == nil || setResp.Policy.Interval != time.Hour {
		t.Errorf("unexpected policy %+v", setResp.Policy)
	}

	getPayload, _ := json.Marshal(&service.GetKeyRotationPolicyResponse{KeyID: "k1"})
	if decoded, err = decodeKMSResponse("GetKeyRotationPolicy", getPayload); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if _, ok := decoded.(*service.GetKeyRotationPolicyResponse); !ok {
		t.Fatalf("unexpected type %T", decoded)
	}

	delPayload, _ := json.Marshal(&service.DeleteKeyRotationPolicyResponse{KeyID: "k1"})
	if decoded, err = decodeKMSResponse("DeleteKeyRotationPolicy", delPayload); err != nil {
		t.Fatalf("decode delete: %v", err)
	}
	if _, ok := decoded.(*service.DeleteKeyRotationPolicyResponse); !ok {
		t.Fatalf("unexpected type %T", decoded)
	}
}

// =============================================================================
// Request transforms
// =============================================================================

func TestTransformKMSRequest_SetKeyRotationPolicy_Flat(t *testing.T) {
	// Mirror the SDK client's unexported flat setKeyRotationPolicyRequest
	// JSON shape.
	flat := &struct {
		KeyID           string `json:"key_id"`
		AutoRotate      bool   `json:"auto_rotate"`
		IntervalSeconds int64  `json:"interval_seconds"`
		RetainVersions  int    `json:"retain_versions,omitempty"`
		Enabled         bool   `json:"enabled"`
	}{KeyID: "k1", AutoRotate: true, IntervalSeconds: 86400, RetainVersions: 3, Enabled: true}

	out, err := transformKMSRequest("SetKeyRotationPolicy", flat)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	svcReq, ok := out.(*service.SetKeyRotationPolicyRequest)
	if !ok {
		t.Fatalf("unexpected type %T", out)
	}
	if svcReq.KeyID != "k1" {
		t.Errorf("KeyID = %q, want k1", svcReq.KeyID)
	}
	if svcReq.Policy == nil || svcReq.Policy.Interval != 24*time.Hour ||
		!svcReq.Policy.AutoRotate || !svcReq.Policy.Enabled || svcReq.Policy.RetainVersions != 3 {
		t.Errorf("unexpected policy %+v", svcReq.Policy)
	}
}

func TestTransformKMSRequest_SetKeyRotationPolicy_ServicePassthrough(t *testing.T) {
	in := &service.SetKeyRotationPolicyRequest{KeyID: "k1"}
	out, err := transformKMSRequest("SetKeyRotationPolicy", in)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if out != in {
		t.Error("expected canonical service request to pass through unchanged")
	}
}

func TestTransformKMSRequest_UpdateKeyMetadata(t *testing.T) {
	out, err := transformKMSRequest("UpdateKeyMetadata", &kmsclient.UpdateKeyMetadataRequest{
		KeyID:      "k1",
		Name:       "renamed",
		Tags:       map[string]string{"env": "test"},
		RemoveTags: []string{"stale"},
	})
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	svcReq, ok := out.(*service.UpdateKeyMetadataRequest)
	if !ok {
		t.Fatalf("unexpected type %T", out)
	}
	if svcReq.Name == nil || *svcReq.Name != "renamed" {
		t.Errorf("Name = %v, want renamed", svcReq.Name)
	}
	if svcReq.Description != nil {
		t.Errorf("Description = %v, want nil (empty means no change)", svcReq.Description)
	}
	if len(svcReq.RemoveTags) != 1 {
		t.Errorf("RemoveTags = %v", svcReq.RemoveTags)
	}
}

func TestTransformKMSRequest_UpdateKeyMetadata_InvalidType(t *testing.T) {
	if _, err := transformKMSRequest("UpdateKeyMetadata", "bad"); err == nil {
		t.Fatal("expected error for invalid request type")
	}
}

func TestTransformKMSRequest_UnknownMethodPassthrough(t *testing.T) {
	in := &struct{ KeyID string }{KeyID: "k1"}
	out, err := transformKMSRequest("GetKey", in)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if out != in {
		t.Error("expected untransformed method to pass through unchanged")
	}
}
