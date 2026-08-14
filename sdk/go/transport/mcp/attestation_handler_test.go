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

package mcp

import (
	"encoding/base64"
	"encoding/pem"
	"strings"
	"testing"

	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
)

// testCertDER is arbitrary DER-shaped bytes; joinPEMBundle only wraps them.
var testCertDER = []byte{0x30, 0x82, 0x01, 0x02}

func TestIsMCPAttestationService(t *testing.T) {
	for _, svc := range []string{"quicbaas.Attestation", "Attestation"} {
		if !isMCPAttestationService(svc) {
			t.Errorf("%q must route through the attestation MCP handler", svc)
		}
	}
	for _, svc := range []string{"quicbaas.Trust", "quicbaas.KV", ""} {
		if isMCPAttestationService(svc) {
			t.Errorf("%q must not route through the attestation MCP handler", svc)
		}
	}
}

// TestMCPAttestationToolNames pins the derived tool names against the server
// registrations in pkg/api/mcp/attestation_tools.go. The MCP transport derives
// them generically, so a rename on either side breaks the round trip with an
// opaque "tool not registered".
func TestMCPAttestationToolNames(t *testing.T) {
	tr := &Transport{}
	want := map[string]string{
		"Verify":          "quicbaas_attestation_verify",
		"VerifyChain":     "quicbaas_attestation_verify_chain",
		"AttestContainer": "quicbaas_attestation_attest_container",
	}
	for method, expected := range want {
		if got := tr.mapToToolName("quicbaas.Attestation", method); got != expected {
			t.Errorf("mapToToolName(%s) = %q, want %q", method, got, expected)
		}
	}
}

func TestAttestationMCPHandler_TransformVerifyRequest(t *testing.T) {
	h := newAttestationHandler()

	t.Run("BinaryFieldsAreBase64", func(t *testing.T) {
		args, err := h.transformRequest("Verify", &attestationclient.VerifyRequest{
			Format:          "tpm2",
			Backend:         "tpm2",
			ChainPEM:        [][]byte{testCertDER},
			AttestedKeyDER:  []byte("key"),
			AttestationData: []byte("data"),
			Signature:       []byte("sig"),
			Nonce:           []byte("nonce"),
			Options: &attestationclient.VerifyOptions{
				CheckFreshness:  true,
				AllowSelfSigned: true,
			},
		})
		if err != nil {
			t.Fatalf("transformRequest: %v", err)
		}

		if args["format"] != "tpm2" {
			t.Errorf("format = %v", args["format"])
		}
		for field, want := range map[string]string{
			"attested_key_der": "key",
			"attestation_data": "data",
			"signature":        "sig",
			"nonce":            "nonce",
		} {
			encoded, ok := args[field].(string)
			if !ok {
				t.Fatalf("%s is not a string: %T", field, args[field])
			}
			decoded, decErr := base64.StdEncoding.DecodeString(encoded)
			if decErr != nil {
				t.Fatalf("%s is not base64: %v", field, decErr)
			}
			if string(decoded) != want {
				t.Errorf("%s = %q, want %q", field, decoded, want)
			}
		}

		bundle, ok := args["chain_pem"].(string)
		if !ok {
			t.Fatalf("chain_pem is not a string: %T", args["chain_pem"])
		}
		block, _ := pem.Decode([]byte(bundle))
		if block == nil || block.Type != "CERTIFICATE" {
			t.Fatalf("DER entries must be PEM-wrapped for the tool schema: %q", bundle)
		}

		if args["check_freshness"] != true || args["allow_self_signed"] != true {
			t.Errorf("options did not reach the arguments: %+v", args)
		}
	})

	t.Run("NonceOmittedWhenAbsent", func(t *testing.T) {
		args, err := h.transformRequest("Verify", &attestationclient.VerifyRequest{
			Format:    "tpm2",
			ChainPEM:  [][]byte{testCertDER},
			Signature: []byte("sig"),
		})
		if err != nil {
			t.Fatalf("transformRequest: %v", err)
		}
		if _, present := args["nonce"]; present {
			t.Error("an absent nonce must not be sent as an empty string")
		}
	})

	t.Run("Errors", func(t *testing.T) {
		cases := map[string]interface{}{
			"WrongType":     &attestationclient.VerifyChainRequest{},
			"NilRequest":    (*attestationclient.VerifyRequest)(nil),
			"MissingFormat": &attestationclient.VerifyRequest{ChainPEM: [][]byte{testCertDER}},
			"MissingChain":  &attestationclient.VerifyRequest{Format: "tpm2"},
		}
		for name, req := range cases {
			t.Run(name, func(t *testing.T) {
				if _, err := h.transformRequest("Verify", req); err == nil {
					t.Fatal("expected an error")
				}
			})
		}
	})
}

func TestAttestationMCPHandler_TransformVerifyChainRequest(t *testing.T) {
	h := newAttestationHandler()

	args, err := h.transformRequest("VerifyChain", &attestationclient.VerifyChainRequest{
		Format:   "tpm2",
		ChainPEM: [][]byte{testCertDER, testCertDER},
		Options:  &attestationclient.VerifyOptions{AllowSelfSigned: true},
	})
	if err != nil {
		t.Fatalf("transformRequest: %v", err)
	}

	bundle, ok := args["chain_pem"].(string)
	if !ok {
		t.Fatalf("chain_pem is not a string: %T", args["chain_pem"])
	}
	if strings.Count(bundle, "BEGIN CERTIFICATE") != 2 {
		t.Fatalf("both chain entries must appear in the bundle: %q", bundle)
	}
	if args["allow_self_signed"] != true {
		t.Error("allow_self_signed did not reach the arguments")
	}

	if _, err := h.transformRequest("VerifyChain", &attestationclient.VerifyChainRequest{}); err == nil {
		t.Fatal("expected an error for an empty chain")
	}
	if _, err := h.transformRequest("VerifyChain", &attestationclient.VerifyRequest{}); err == nil {
		t.Fatal("expected an error for the wrong request type")
	}
}

func TestAttestationMCPHandler_TransformAttestContainerRequest(t *testing.T) {
	h := newAttestationHandler()

	args, err := h.transformRequest("AttestContainer", &attestationclient.AttestContainerRequest{
		DeviceID:        "device-1",
		ContainerID:     "container-1",
		Image:           "alpine@sha256:abc",
		ImageDigest:     "sha256:abc",
		TrustLabel:      "prod",
		TrustLevel:      "measured",
		MeasurementPCR:  16,
		MeasurementBank: "sha256",
	})
	if err != nil {
		t.Fatalf("transformRequest: %v", err)
	}
	if args["image"] != "alpine@sha256:abc" || args["trust_level"] != "measured" {
		t.Fatalf("unexpected arguments: %+v", args)
	}
	if args["measurement_pcr"] != 16 {
		t.Fatalf("measurement_pcr = %v", args["measurement_pcr"])
	}

	if _, err := h.transformRequest("AttestContainer", &attestationclient.AttestContainerRequest{}); err == nil {
		t.Fatal("expected an error for a missing image")
	}
	if _, err := h.transformRequest("AttestContainer", &attestationclient.VerifyRequest{}); err == nil {
		t.Fatal("expected an error for the wrong request type")
	}
}

func TestAttestationMCPHandler_TransformResponses(t *testing.T) {
	h := newAttestationHandler()

	t.Run("Verify", func(t *testing.T) {
		resp, err := h.transformResponse("Verify", map[string]interface{}{
			"valid":           true,
			"format":          "tpm2",
			"chain_valid":     true,
			"signature_valid": true,
			"reason":          "",
		})
		if err != nil {
			t.Fatalf("transformResponse: %v", err)
		}
		typed, ok := resp.(*attestationclient.VerifyResponse)
		if !ok {
			t.Fatalf("expected *attestation.VerifyResponse, got %T", resp)
		}
		if !typed.Valid || !typed.ChainValid || !typed.SignatureValid {
			t.Fatalf("fields did not round-trip: %+v", typed)
		}
	})

	t.Run("VerifyChain", func(t *testing.T) {
		resp, err := h.transformResponse("VerifyChain", map[string]interface{}{
			"valid":  false,
			"reason": "chain not anchored",
		})
		if err != nil {
			t.Fatalf("transformResponse: %v", err)
		}
		typed, ok := resp.(*attestationclient.VerifyChainResponse)
		if !ok {
			t.Fatalf("expected *attestation.VerifyChainResponse, got %T", resp)
		}
		if typed.Valid || typed.Reason != "chain not anchored" {
			t.Fatalf("fields did not round-trip: %+v", typed)
		}
	})

	t.Run("AttestContainer", func(t *testing.T) {
		resp, err := h.transformResponse("AttestContainer", map[string]interface{}{
			"attested":         true,
			"container_id":     "container-1",
			"config_digest":    "sha256:cfg",
			"measured":         true,
			"measurement_pcr":  float64(16),
			"measurement_bank": "sha256",
			"attested_at":      "2026-08-09T12:00:00Z",
			"quoted":           base64.StdEncoding.EncodeToString([]byte("tpms-attest")),
			"signature":        base64.StdEncoding.EncodeToString([]byte("sig")),
			"nonce":            base64.StdEncoding.EncodeToString([]byte("nonce")),
		})
		if err != nil {
			t.Fatalf("transformResponse: %v", err)
		}
		typed, ok := resp.(*attestationclient.AttestContainerResponse)
		if !ok {
			t.Fatalf("expected *attestation.AttestContainerResponse, got %T", resp)
		}
		if !typed.Attested || !typed.Measured || typed.MeasurementPCR != 16 {
			t.Fatalf("fields did not round-trip: %+v", typed)
		}
		if string(typed.Quoted) != "tpms-attest" || string(typed.Signature) != "sig" {
			t.Fatalf("binary evidence did not decode: %+v", typed)
		}
		if typed.AttestedAt != "2026-08-09T12:00:00Z" {
			t.Fatalf("attested_at = %q", typed.AttestedAt)
		}
	})

	// A malformed evidence blob must not fail the whole call: the
	// authoritative verdict lives in separate scalar fields.
	t.Run("MalformedBinaryFieldIsTolerated", func(t *testing.T) {
		resp, err := h.transformResponse("AttestContainer", map[string]interface{}{
			"attested": true,
			"quoted":   "!!!not base64!!!",
		})
		if err != nil {
			t.Fatalf("transformResponse: %v", err)
		}
		typed := resp.(*attestationclient.AttestContainerResponse)
		if !typed.Attested {
			t.Fatal("the verdict must survive an unreadable evidence blob")
		}
		if typed.Quoted != nil {
			t.Fatal("an undecodable blob must be dropped, not garbled")
		}
	})

	t.Run("UnknownMethod", func(t *testing.T) {
		if _, err := h.transformResponse("Quarantine", map[string]interface{}{}); err == nil {
			t.Fatal("expected an unimplemented error")
		}
		if _, err := h.transformRequest("Quarantine", nil); err == nil {
			t.Fatal("expected an unimplemented error")
		}
	})
}

func TestJoinPEMBundle(t *testing.T) {
	t.Run("WrapsDER", func(t *testing.T) {
		bundle := joinPEMBundle([][]byte{testCertDER})
		if !strings.Contains(bundle, "BEGIN CERTIFICATE") {
			t.Fatalf("DER must be PEM-wrapped: %q", bundle)
		}
	})

	t.Run("PreservesPEM", func(t *testing.T) {
		encoded := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: testCertDER})
		bundle := joinPEMBundle([][]byte{encoded})
		if strings.Count(bundle, "BEGIN CERTIFICATE") != 1 {
			t.Fatalf("an already-PEM entry must not be double-wrapped: %q", bundle)
		}
	})

	t.Run("SkipsEmptyEntries", func(t *testing.T) {
		bundle := joinPEMBundle([][]byte{nil, testCertDER, {}})
		if strings.Count(bundle, "BEGIN CERTIFICATE") != 1 {
			t.Fatalf("empty entries must be skipped: %q", bundle)
		}
	})

	t.Run("EmptyInput", func(t *testing.T) {
		if got := joinPEMBundle(nil); got != "" {
			t.Fatalf("expected an empty bundle, got %q", got)
		}
	})
}
