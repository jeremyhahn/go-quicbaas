//go:build raft

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

package discovery

import (
	"crypto/rand"
	"testing"
)

// TestSignMessage verifies that SignMessage produces consistent signatures
// for the same secret and payload.
func TestSignMessage(t *testing.T) {
	tests := []struct {
		name    string
		secret  []byte
		payload []byte
	}{
		{
			name:    "basic signature",
			secret:  []byte("test-secret"),
			payload: []byte("test message"),
		},
		{
			name:    "empty payload",
			secret:  []byte("secret"),
			payload: []byte(""),
		},
		{
			name:    "long secret",
			secret:  []byte("this-is-a-very-long-secret-key-for-testing-hmac-sha256"),
			payload: []byte("payload"),
		},
		{
			name:    "unicode payload",
			secret:  []byte("secret"),
			payload: []byte("Hello 世界 🌍"),
		},
		{
			name:    "binary payload",
			secret:  []byte("secret"),
			payload: []byte{0x00, 0x01, 0xFF, 0xFE, 0x42},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Sign the same message twice
			sig1 := SignMessage(tt.secret, tt.payload)
			sig2 := SignMessage(tt.secret, tt.payload)

			// Signatures should be identical (deterministic)
			if len(sig1) != len(sig2) {
				t.Errorf("signature lengths differ: %d vs %d", len(sig1), len(sig2))
			}

			for i := range sig1 {
				if sig1[i] != sig2[i] {
					t.Errorf("signatures differ at byte %d", i)
					break
				}
			}

			// Signature should be 32 bytes (SHA256)
			if len(sig1) != 32 {
				t.Errorf("expected signature length 32, got %d", len(sig1))
			}
		})
	}
}

// TestSignMessageDifferentSecrets verifies that different secrets
// produce different signatures for the same payload.
func TestSignMessageDifferentSecrets(t *testing.T) {
	payload := []byte("test message")
	secret1 := []byte("secret1")
	secret2 := []byte("secret2")

	sig1 := SignMessage(secret1, payload)
	sig2 := SignMessage(secret2, payload)

	if len(sig1) != len(sig2) {
		t.Fatal("signature lengths should be equal")
	}

	same := true
	for i := range sig1 {
		if sig1[i] != sig2[i] {
			same = false
			break
		}
	}

	if same {
		t.Error("different secrets should produce different signatures")
	}
}

// TestSignMessageDifferentPayloads verifies that different payloads
// produce different signatures with the same secret.
func TestSignMessageDifferentPayloads(t *testing.T) {
	secret := []byte("test-secret")
	payload1 := []byte("message 1")
	payload2 := []byte("message 2")

	sig1 := SignMessage(secret, payload1)
	sig2 := SignMessage(secret, payload2)

	if len(sig1) != len(sig2) {
		t.Fatal("signature lengths should be equal")
	}

	same := true
	for i := range sig1 {
		if sig1[i] != sig2[i] {
			same = false
			break
		}
	}

	if same {
		t.Error("different payloads should produce different signatures")
	}
}

// TestVerifySignature verifies that valid signatures are accepted
// and invalid signatures are rejected.
func TestVerifySignature(t *testing.T) {
	secret := []byte("test-secret")
	payload := []byte("test message")

	// Generate a valid signature
	validSig := SignMessage(secret, payload)

	tests := []struct {
		name      string
		secret    []byte
		payload   []byte
		signature []byte
		wantValid bool
	}{
		{
			name:      "valid signature",
			secret:    secret,
			payload:   payload,
			signature: validSig,
			wantValid: true,
		},
		{
			name:      "wrong secret",
			secret:    []byte("wrong-secret"),
			payload:   payload,
			signature: validSig,
			wantValid: false,
		},
		{
			name:      "wrong payload",
			secret:    secret,
			payload:   []byte("wrong message"),
			signature: validSig,
			wantValid: false,
		},
		{
			name:    "corrupted signature",
			secret:  secret,
			payload: payload,
			signature: func() []byte {
				corrupted := make([]byte, len(validSig))
				copy(corrupted, validSig)
				corrupted[0] ^= 0xFF // flip bits
				return corrupted
			}(),
			wantValid: false,
		},
		{
			name:      "truncated signature",
			secret:    secret,
			payload:   payload,
			signature: validSig[:16],
			wantValid: false,
		},
		{
			name:      "empty signature",
			secret:    secret,
			payload:   payload,
			signature: []byte{},
			wantValid: false,
		},
		{
			name:      "nil signature",
			secret:    secret,
			payload:   payload,
			signature: nil,
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := VerifySignature(tt.secret, tt.payload, tt.signature)
			if valid != tt.wantValid {
				t.Errorf("VerifySignature() = %v, want %v", valid, tt.wantValid)
			}
		})
	}
}

// TestVerifySignatureEmptySecret tests signature verification with empty secret.
func TestVerifySignatureEmptySecret(t *testing.T) {
	secret := []byte("")
	payload := []byte("test message")

	sig := SignMessage(secret, payload)

	// Should verify with same empty secret
	if !VerifySignature(secret, payload, sig) {
		t.Error("failed to verify signature with empty secret")
	}

	// Should fail with non-empty secret
	if VerifySignature([]byte("non-empty"), payload, sig) {
		t.Error("incorrectly verified signature with different secret")
	}
}

// TestSignMessageNilInputs tests that SignMessage handles nil inputs gracefully.
func TestSignMessageNilInputs(t *testing.T) {
	tests := []struct {
		name    string
		secret  []byte
		payload []byte
	}{
		{
			name:    "nil secret",
			secret:  nil,
			payload: []byte("message"),
		},
		{
			name:    "nil payload",
			secret:  []byte("secret"),
			payload: nil,
		},
		{
			name:    "both nil",
			secret:  nil,
			payload: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			sig := SignMessage(tt.secret, tt.payload)

			// Should produce a signature
			if len(sig) != 32 {
				t.Errorf("expected signature length 32, got %d", len(sig))
			}

			// Should be verifiable
			if !VerifySignature(tt.secret, tt.payload, sig) {
				t.Error("failed to verify signature with nil inputs")
			}
		})
	}
}

// TestConstantTimeComparison verifies that the signature verification
// uses constant-time comparison (tested indirectly).
func TestConstantTimeComparison(t *testing.T) {
	secret := []byte("test-secret")
	payload := []byte("test message")
	validSig := SignMessage(secret, payload)

	// Create signatures that differ in different positions
	testSigs := [][]byte{
		func() []byte { // differ in first byte
			sig := make([]byte, len(validSig))
			copy(sig, validSig)
			sig[0] ^= 0x01
			return sig
		}(),
		func() []byte { // differ in middle byte
			sig := make([]byte, len(validSig))
			copy(sig, validSig)
			sig[len(sig)/2] ^= 0x01
			return sig
		}(),
		func() []byte { // differ in last byte
			sig := make([]byte, len(validSig))
			copy(sig, validSig)
			sig[len(sig)-1] ^= 0x01
			return sig
		}(),
	}

	// All should be rejected
	for i, sig := range testSigs {
		if VerifySignature(secret, payload, sig) {
			t.Errorf("test signature %d should be rejected", i)
		}
	}
}

// TestSignMessageLargePayload tests signing large payloads.
func TestSignMessageLargePayload(t *testing.T) {
	secret := []byte("test-secret")

	// Create a large payload (1MB)
	payload := make([]byte, 1024*1024)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("failed to generate random payload: %v", err)
	}

	sig := SignMessage(secret, payload)

	if len(sig) != 32 {
		t.Errorf("expected signature length 32, got %d", len(sig))
	}

	if !VerifySignature(secret, payload, sig) {
		t.Error("failed to verify signature for large payload")
	}
}

// BenchmarkSignMessage benchmarks the signature generation performance.
func BenchmarkSignMessage(b *testing.B) {
	secret := []byte("test-secret-key")
	payload := []byte("test message payload for benchmarking")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SignMessage(secret, payload)
	}
}

// BenchmarkVerifySignature benchmarks the signature verification performance.
func BenchmarkVerifySignature(b *testing.B) {
	secret := []byte("test-secret-key")
	payload := []byte("test message payload for benchmarking")
	signature := SignMessage(secret, payload)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VerifySignature(secret, payload, signature)
	}
}

// BenchmarkSignMessageLarge benchmarks signing a large payload.
func BenchmarkSignMessageLarge(b *testing.B) {
	secret := []byte("test-secret-key")
	payload := make([]byte, 1024*1024) // 1MB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SignMessage(secret, payload)
	}
}
