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

package quic

import (
	"encoding/json"
	"testing"

	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
)

func TestIsAttestationService(t *testing.T) {
	for _, svc := range []string{"quicbaas.Attestation", "Attestation"} {
		if !isAttestationService(svc) {
			t.Errorf("%q must be recognised as the attestation service", svc)
		}
	}
	for _, svc := range []string{"quicbaas.Trust", "quicbaas.Orchestrator", "quicbaas.Certificate", ""} {
		if isAttestationService(svc) {
			t.Errorf("%q must not be recognised as the attestation service", svc)
		}
	}
}

// TestAttestationMethodToSubOp pins the wire contract against the server's
// table in pkg/api/quic/protocol.go. A drift here silently mis-routes a
// request onto another surface's handler AND its authorization mapping.
func TestAttestationMethodToSubOp(t *testing.T) {
	want := map[string]byte{
		"Verify":          0x70,
		"VerifyChain":     0x71,
		"AttestContainer": 0x72,
	}

	for method, expected := range want {
		subOp, ok := AttestationMethodToSubOp(method)
		if !ok {
			t.Fatalf("method %s is not wired over QUIC", method)
		}
		if subOp != expected {
			t.Fatalf("%s sub-op = 0x%02X, want 0x%02X", method, subOp, expected)
		}
	}

	if len(attestationMethodToSubOp) != len(want) {
		t.Fatalf("sub-op table has %d entries, expectation table has %d",
			len(attestationMethodToSubOp), len(want))
	}

	if _, ok := AttestationMethodToSubOp("Quarantine"); ok {
		t.Fatal("Tier 3 verbs must not resolve to a sub-op")
	}
}

// TestAttestationSubOpRangeIsDisjoint proves the attestation range does not
// overlap the other vocabularies sharing the OpOrchestrator dispatch byte.
func TestAttestationSubOpRangeIsDisjoint(t *testing.T) {
	taken := map[byte]string{
		SubOpCertExtCrossSign:           "certext",
		SubOpCertExtChain:               "certext",
		SubOpCertExtIssueDeviceIdentity: "certext",
	}
	for _, subOp := range attestationMethodToSubOp {
		if owner, clash := taken[subOp]; clash {
			t.Fatalf("attestation sub-op 0x%02X collides with %s", subOp, owner)
		}
	}
}

// TestAttestationOpcodeRoutesToOrchestrator pins that both the short and
// package-qualified service names resolve to the shared dispatch opcode.
func TestAttestationOpcodeRoutesToOrchestrator(t *testing.T) {
	for _, svc := range []string{"Attestation", "quicbaas.Attestation"} {
		for _, method := range []string{"Verify", "VerifyChain", "AttestContainer"} {
			opcode, ok := MethodToOpcode(svc, method)
			if !ok {
				t.Fatalf("%s/%s has no opcode", svc, method)
			}
			if opcode != OpOrchestrator {
				t.Fatalf("%s/%s opcode = 0x%02X, want OpOrchestrator 0x%02X",
					svc, method, opcode, OpOrchestrator)
			}
		}
	}
}

func TestDecodeAttestationResponse(t *testing.T) {
	t.Run("Verify", func(t *testing.T) {
		payload, err := json.Marshal(map[string]interface{}{
			"valid":           true,
			"format":          "tpm2",
			"chain_valid":     true,
			"signature_valid": true,
			"pcrs_valid":      true,
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		decoded, err := decodeAttestationResponse("Verify", payload)
		if err != nil {
			t.Fatalf("decodeAttestationResponse: %v", err)
		}
		typed, ok := decoded.(*attestationclient.VerifyResponse)
		if !ok {
			t.Fatalf("expected *attestation.VerifyResponse, got %T", decoded)
		}
		if !typed.Valid || !typed.ChainValid || !typed.SignatureValid || !typed.PCRsValid {
			t.Fatalf("fields did not round-trip: %+v", typed)
		}
	})

	t.Run("VerifyChain", func(t *testing.T) {
		payload := []byte(`{"valid":false,"reason":"chain not anchored"}`)
		decoded, err := decodeAttestationResponse("VerifyChain", payload)
		if err != nil {
			t.Fatalf("decodeAttestationResponse: %v", err)
		}
		typed, ok := decoded.(*attestationclient.VerifyChainResponse)
		if !ok {
			t.Fatalf("expected *attestation.VerifyChainResponse, got %T", decoded)
		}
		if typed.Valid || typed.Reason != "chain not anchored" {
			t.Fatalf("fields did not round-trip: %+v", typed)
		}
	})

	// The server encodes the service-layer struct, whose AttestedAt is a
	// time.Time. It must land in the leaf type's string field: that
	// equivalence is what lets this transport skip a converter.
	t.Run("AttestContainerTimestampCrossesAsString", func(t *testing.T) {
		payload := []byte(`{
			"attested": true,
			"container_id": "container-1",
			"config_digest": "sha256:cfg",
			"measured": true,
			"measurement_pcr": 16,
			"measurement_bank": "sha256",
			"attested_at": "2026-08-09T12:00:00Z"
		}`)

		decoded, err := decodeAttestationResponse("AttestContainer", payload)
		if err != nil {
			t.Fatalf("decodeAttestationResponse: %v", err)
		}
		typed, ok := decoded.(*attestationclient.AttestContainerResponse)
		if !ok {
			t.Fatalf("expected *attestation.AttestContainerResponse, got %T", decoded)
		}
		if !typed.Attested || !typed.Measured {
			t.Fatalf("fields did not round-trip: %+v", typed)
		}
		if typed.MeasurementPCR != 16 || typed.MeasurementBank != "sha256" {
			t.Fatalf("measurement fields did not round-trip: %+v", typed)
		}
		if typed.AttestedAt != "2026-08-09T12:00:00Z" {
			t.Fatalf("attested_at = %q", typed.AttestedAt)
		}
	})

	t.Run("EmptyPayloadIsNil", func(t *testing.T) {
		decoded, err := decodeAttestationResponse("Verify", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if decoded != nil {
			t.Fatalf("expected nil for an empty payload, got %T", decoded)
		}
	})

	t.Run("UnknownMethodRejected", func(t *testing.T) {
		if _, err := decodeAttestationResponse("Quarantine", []byte(`{}`)); err == nil {
			t.Fatal("an unknown method must be rejected rather than silently mis-routed")
		}
	})

	t.Run("MalformedPayloadRejected", func(t *testing.T) {
		for _, method := range []string{"Verify", "VerifyChain", "AttestContainer"} {
			if _, err := decodeAttestationResponse(method, []byte("{not json")); err == nil {
				t.Fatalf("%s: expected a decode error", method)
			}
		}
	})
}
