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

package kms

import (
	"context"
	"crypto/elliptic"
	"crypto/x509"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// TestGetAlgorithmInfo_AllAlgorithms verifies all 17 registered algorithms return correct information.
func TestGetAlgorithmInfo_AllAlgorithms(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		expected  AlgorithmInfo
	}{
		{
			name:      "AES-128-GCM",
			algorithm: AlgAES128GCM,
			expected: AlgorithmInfo{
				Name:       AlgAES128GCM,
				Type:       AlgorithmTypeSymmetric,
				KeySize:    128,
				BlockSize:  16,
				IVSize:     12,
				TagSize:    16,
				CanEncrypt: true,
				CanWrap:    true,
				CanDerive:  false,
				CanSign:    false,
			},
		},
		{
			name:      "AES-192-GCM",
			algorithm: AlgAES192GCM,
			expected: AlgorithmInfo{
				Name:       AlgAES192GCM,
				Type:       AlgorithmTypeSymmetric,
				KeySize:    192,
				BlockSize:  16,
				IVSize:     12,
				TagSize:    16,
				CanEncrypt: true,
				CanWrap:    true,
				CanDerive:  false,
				CanSign:    false,
			},
		},
		{
			name:      "AES-256-GCM",
			algorithm: AlgAES256GCM,
			expected: AlgorithmInfo{
				Name:       AlgAES256GCM,
				Type:       AlgorithmTypeSymmetric,
				KeySize:    256,
				BlockSize:  16,
				IVSize:     12,
				TagSize:    16,
				CanEncrypt: true,
				CanWrap:    true,
				CanDerive:  false,
				CanSign:    false,
			},
		},
		{
			name:      "CHACHA20-POLY1305",
			algorithm: AlgChaCha20Poly1305,
			expected: AlgorithmInfo{
				Name:       AlgChaCha20Poly1305,
				Type:       AlgorithmTypeSymmetric,
				KeySize:    256,
				BlockSize:  0,
				IVSize:     12,
				TagSize:    16,
				CanEncrypt: true,
				CanWrap:    true,
				CanDerive:  false,
				CanSign:    false,
			},
		},
		{
			name:      "XCHACHA20-POLY1305",
			algorithm: AlgXChaCha20Poly1305,
			expected: AlgorithmInfo{
				Name:       AlgXChaCha20Poly1305,
				Type:       AlgorithmTypeSymmetric,
				KeySize:    256,
				BlockSize:  0,
				IVSize:     24, // Extended 192-bit nonce
				TagSize:    16,
				CanEncrypt: true,
				CanWrap:    true,
				CanDerive:  false,
				CanSign:    false,
			},
		},
		{
			name:      "RSA-2048",
			algorithm: AlgRSA2048,
			expected: AlgorithmInfo{
				Name:          AlgRSA2048,
				Type:          AlgorithmTypeAsymmetric,
				KeySize:       2048,
				BlockSize:     0,
				IVSize:        0,
				TagSize:       0,
				SignatureSize: 256,
				CanEncrypt:    true,
				CanSign:       true,
				CanWrap:       true,
				CanDerive:     false,
			},
		},
		{
			name:      "RSA-3072",
			algorithm: AlgRSA3072,
			expected: AlgorithmInfo{
				Name:          AlgRSA3072,
				Type:          AlgorithmTypeAsymmetric,
				KeySize:       3072,
				BlockSize:     0,
				IVSize:        0,
				TagSize:       0,
				SignatureSize: 384,
				CanEncrypt:    true,
				CanSign:       true,
				CanWrap:       true,
				CanDerive:     false,
			},
		},
		{
			name:      "RSA-4096",
			algorithm: AlgRSA4096,
			expected: AlgorithmInfo{
				Name:          AlgRSA4096,
				Type:          AlgorithmTypeAsymmetric,
				KeySize:       4096,
				BlockSize:     0,
				IVSize:        0,
				TagSize:       0,
				SignatureSize: 512,
				CanEncrypt:    true,
				CanSign:       true,
				CanWrap:       true,
				CanDerive:     false,
			},
		},
		{
			name:      "ECDSA-P256",
			algorithm: AlgECDSAP256,
			expected: AlgorithmInfo{
				Name:          AlgECDSAP256,
				Type:          AlgorithmTypeAsymmetric,
				KeySize:       256,
				BlockSize:     0,
				IVSize:        0,
				TagSize:       0,
				SignatureSize: 64,
				CanEncrypt:    false,
				CanSign:       true,
				CanWrap:       false,
				CanDerive:     false,
			},
		},
		{
			name:      "ECDSA-P384",
			algorithm: AlgECDSAP384,
			expected: AlgorithmInfo{
				Name:          AlgECDSAP384,
				Type:          AlgorithmTypeAsymmetric,
				KeySize:       384,
				BlockSize:     0,
				IVSize:        0,
				TagSize:       0,
				SignatureSize: 96,
				CanEncrypt:    false,
				CanSign:       true,
				CanWrap:       false,
				CanDerive:     false,
			},
		},
		{
			name:      "ECDSA-P521",
			algorithm: AlgECDSAP521,
			expected: AlgorithmInfo{
				Name:          AlgECDSAP521,
				Type:          AlgorithmTypeAsymmetric,
				KeySize:       521,
				BlockSize:     0,
				IVSize:        0,
				TagSize:       0,
				SignatureSize: 132,
				CanEncrypt:    false,
				CanSign:       true,
				CanWrap:       false,
				CanDerive:     false,
			},
		},
		{
			name:      "ED25519",
			algorithm: AlgEd25519,
			expected: AlgorithmInfo{
				Name:          AlgEd25519,
				Type:          AlgorithmTypeAsymmetric,
				KeySize:       256,
				BlockSize:     0,
				IVSize:        0,
				TagSize:       0,
				SignatureSize: 64,
				CanEncrypt:    false,
				CanSign:       true,
				CanWrap:       false,
				CanDerive:     false,
			},
		},
		{
			name:      "ED448",
			algorithm: AlgEd448,
			expected: AlgorithmInfo{
				Name:          AlgEd448,
				Type:          AlgorithmTypeAsymmetric,
				KeySize:       448,
				BlockSize:     0,
				IVSize:        0,
				TagSize:       0,
				SignatureSize: 114,
				CanEncrypt:    false,
				CanSign:       true,
				CanWrap:       false,
				CanDerive:     false,
			},
		},
		{
			name:      "X25519",
			algorithm: AlgX25519,
			expected: AlgorithmInfo{
				Name:       AlgX25519,
				Type:       AlgorithmTypeKeyExchange,
				KeySize:    256,
				BlockSize:  0,
				IVSize:     0,
				TagSize:    0,
				CanEncrypt: false,
				CanSign:    false,
				CanWrap:    false,
				CanDerive:  true,
			},
		},
		{
			name:      "X448",
			algorithm: AlgX448,
			expected: AlgorithmInfo{
				Name:       AlgX448,
				Type:       AlgorithmTypeKeyExchange,
				KeySize:    448,
				BlockSize:  0,
				IVSize:     0,
				TagSize:    0,
				CanEncrypt: false,
				CanSign:    false,
				CanWrap:    false,
				CanDerive:  true,
			},
		},
		{
			name:      "HMAC-SHA256",
			algorithm: AlgHMACSHA256,
			expected: AlgorithmInfo{
				Name:          AlgHMACSHA256,
				Type:          AlgorithmTypeHMAC,
				KeySize:       256,
				BlockSize:     0,
				IVSize:        0,
				TagSize:       32,
				SignatureSize: 32,
				CanEncrypt:    false,
				CanSign:       true,
				CanWrap:       false,
				CanDerive:     true,
			},
		},
		{
			name:      "HMAC-SHA384",
			algorithm: AlgHMACSHA384,
			expected: AlgorithmInfo{
				Name:          AlgHMACSHA384,
				Type:          AlgorithmTypeHMAC,
				KeySize:       384,
				BlockSize:     0,
				IVSize:        0,
				TagSize:       48,
				SignatureSize: 48,
				CanEncrypt:    false,
				CanSign:       true,
				CanWrap:       false,
				CanDerive:     true,
			},
		},
		{
			name:      "HMAC-SHA512",
			algorithm: AlgHMACSHA512,
			expected: AlgorithmInfo{
				Name:          AlgHMACSHA512,
				Type:          AlgorithmTypeHMAC,
				KeySize:       512,
				BlockSize:     0,
				IVSize:        0,
				TagSize:       64,
				SignatureSize: 64,
				CanEncrypt:    false,
				CanSign:       true,
				CanWrap:       false,
				CanDerive:     true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := GetAlgorithmInfo(tt.algorithm)
			if err != nil {
				t.Fatalf("GetAlgorithmInfo(%s) returned error: %v", tt.algorithm, err)
			}

			if info.Name != tt.expected.Name {
				t.Errorf("Name: got %s, want %s", info.Name, tt.expected.Name)
			}
			if info.Type != tt.expected.Type {
				t.Errorf("Type: got %v, want %v", info.Type, tt.expected.Type)
			}
			if info.KeySize != tt.expected.KeySize {
				t.Errorf("KeySize: got %d, want %d", info.KeySize, tt.expected.KeySize)
			}
			if info.BlockSize != tt.expected.BlockSize {
				t.Errorf("BlockSize: got %d, want %d", info.BlockSize, tt.expected.BlockSize)
			}
			if info.IVSize != tt.expected.IVSize {
				t.Errorf("IVSize: got %d, want %d", info.IVSize, tt.expected.IVSize)
			}
			if info.TagSize != tt.expected.TagSize {
				t.Errorf("TagSize: got %d, want %d", info.TagSize, tt.expected.TagSize)
			}
			if info.SignatureSize != tt.expected.SignatureSize {
				t.Errorf("SignatureSize: got %d, want %d", info.SignatureSize, tt.expected.SignatureSize)
			}
			if info.CanEncrypt != tt.expected.CanEncrypt {
				t.Errorf("CanEncrypt: got %v, want %v", info.CanEncrypt, tt.expected.CanEncrypt)
			}
			if info.CanSign != tt.expected.CanSign {
				t.Errorf("CanSign: got %v, want %v", info.CanSign, tt.expected.CanSign)
			}
			if info.CanWrap != tt.expected.CanWrap {
				t.Errorf("CanWrap: got %v, want %v", info.CanWrap, tt.expected.CanWrap)
			}
			if info.CanDerive != tt.expected.CanDerive {
				t.Errorf("CanDerive: got %v, want %v", info.CanDerive, tt.expected.CanDerive)
			}
		})
	}
}

// TestGetAlgorithmInfo_InvalidAlgorithm tests error handling for unknown algorithms.
func TestGetAlgorithmInfo_InvalidAlgorithm(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
	}{
		{"empty string", ""},
		{"invalid name", "INVALID-ALGORITHM"},
		{"partial match", "AES-256"},
		{"wrong separator", "AES_256_GCM"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := GetAlgorithmInfo(tt.algorithm)
			if err == nil {
				t.Errorf("GetAlgorithmInfo(%s) expected error, got nil", tt.algorithm)
			}
			if info != nil {
				t.Errorf("GetAlgorithmInfo(%s) expected nil info, got %+v", tt.algorithm, info)
			}
		})
	}
}

// TestGetAlgorithmInfo_CaseInsensitive tests that algorithm lookup is case-insensitive.
func TestGetAlgorithmInfo_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"lowercase aes", "aes-256-gcm", AlgAES256GCM},
		{"mixed case aes", "Aes-256-Gcm", AlgAES256GCM},
		{"lowercase chacha20", "chacha20-poly1305", AlgChaCha20Poly1305},
		{"mixed case chacha20", "ChaCha20-Poly1305", AlgChaCha20Poly1305},
		{"lowercase xchacha20", "xchacha20-poly1305", AlgXChaCha20Poly1305},
		{"uppercase canonical", "CHACHA20-POLY1305", AlgChaCha20Poly1305},
		{"lowercase rsa", "rsa-2048", AlgRSA2048},
		{"lowercase hmac", "hmac-sha256", AlgHMACSHA256},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := GetAlgorithmInfo(tt.input)
			if err != nil {
				t.Errorf("GetAlgorithmInfo(%s) unexpected error: %v", tt.input, err)
				return
			}
			if info.Name != tt.expected {
				t.Errorf("GetAlgorithmInfo(%s).Name = %s, want %s", tt.input, info.Name, tt.expected)
			}
		})
	}
}

// TestIsValidAlgorithm_CaseInsensitive tests that algorithm validation is case-insensitive.
func TestIsValidAlgorithm_CaseInsensitive(t *testing.T) {
	caseVariants := []string{
		"aes-256-gcm",
		"Aes-256-Gcm",
		"chacha20-poly1305",
		"ChaCha20-Poly1305",
		"xchacha20-poly1305",
		"rsa-2048",
		"hmac-sha256",
	}

	for _, alg := range caseVariants {
		t.Run(alg, func(t *testing.T) {
			if !IsValidAlgorithm(alg) {
				t.Errorf("IsValidAlgorithm(%s) = false, want true", alg)
			}
		})
	}
}

// TestIsValidAlgorithm_ValidAlgorithms tests validation of all valid algorithms.
func TestIsValidAlgorithm_ValidAlgorithms(t *testing.T) {
	validAlgorithms := []string{
		AlgAES128GCM, AlgAES192GCM, AlgAES256GCM, AlgChaCha20Poly1305, AlgXChaCha20Poly1305,
		AlgRSA2048, AlgRSA3072, AlgRSA4096,
		AlgECDSAP256, AlgECDSAP384, AlgECDSAP521,
		AlgEd25519, AlgEd448,
		AlgX25519, AlgX448,
		AlgHMACSHA256, AlgHMACSHA384, AlgHMACSHA512,
	}

	for _, alg := range validAlgorithms {
		t.Run(alg, func(t *testing.T) {
			if !IsValidAlgorithm(alg) {
				t.Errorf("IsValidAlgorithm(%s) = false, want true", alg)
			}
		})
	}
}

// TestIsValidAlgorithm_InvalidAlgorithms tests validation of invalid algorithms.
func TestIsValidAlgorithm_InvalidAlgorithms(t *testing.T) {
	invalidAlgorithms := []string{
		"",
		"INVALID",
		"AES-128",
		"RSA-1024",
		"ECDSA-P192",
	}

	for _, alg := range invalidAlgorithms {
		t.Run(alg, func(t *testing.T) {
			if IsValidAlgorithm(alg) {
				t.Errorf("IsValidAlgorithm(%s) = true, want false", alg)
			}
		})
	}
}

// TestListAlgorithms verifies all 18 algorithms are returned and sorted.
func TestListAlgorithms(t *testing.T) {
	algos := ListAlgorithms()

	// Check count
	if len(algos) != 18 {
		t.Errorf("ListAlgorithms() returned %d algorithms, want 18", len(algos))
	}

	// Check sorted
	if !sort.StringsAreSorted(algos) {
		t.Errorf("ListAlgorithms() result is not sorted: %v", algos)
	}

	// Verify all expected algorithms are present
	expected := map[string]bool{
		AlgAES128GCM: true, AlgAES192GCM: true, AlgAES256GCM: true,
		AlgChaCha20Poly1305: true, AlgXChaCha20Poly1305: true,
		AlgRSA2048: true, AlgRSA3072: true, AlgRSA4096: true,
		AlgECDSAP256: true, AlgECDSAP384: true, AlgECDSAP521: true,
		AlgEd25519: true, AlgEd448: true,
		AlgX25519: true, AlgX448: true,
		AlgHMACSHA256: true, AlgHMACSHA384: true, AlgHMACSHA512: true,
	}

	for _, alg := range algos {
		if !expected[alg] {
			t.Errorf("Unexpected algorithm in list: %s", alg)
		}
		delete(expected, alg)
	}

	if len(expected) > 0 {
		t.Errorf("Missing algorithms from list: %v", expected)
	}
}

// TestListAlgorithmsByType_Symmetric tests filtering symmetric algorithms.
func TestListAlgorithmsByType_Symmetric(t *testing.T) {
	algos := ListAlgorithmsByType(AlgorithmTypeSymmetric)

	expectedCount := 5
	if len(algos) != expectedCount {
		t.Errorf("ListAlgorithmsByType(Symmetric) returned %d algorithms, want %d", len(algos), expectedCount)
	}

	if !sort.StringsAreSorted(algos) {
		t.Errorf("ListAlgorithmsByType(Symmetric) result is not sorted: %v", algos)
	}

	expected := map[string]bool{
		AlgAES128GCM: true, AlgAES192GCM: true, AlgAES256GCM: true,
		AlgChaCha20Poly1305: true, AlgXChaCha20Poly1305: true,
	}

	for _, alg := range algos {
		if !expected[alg] {
			t.Errorf("Unexpected symmetric algorithm: %s", alg)
		}
		delete(expected, alg)
	}

	if len(expected) > 0 {
		t.Errorf("Missing symmetric algorithms: %v", expected)
	}
}

// TestListAlgorithmsByType_Asymmetric tests filtering asymmetric algorithms.
func TestListAlgorithmsByType_Asymmetric(t *testing.T) {
	algos := ListAlgorithmsByType(AlgorithmTypeAsymmetric)

	expectedCount := 8
	if len(algos) != expectedCount {
		t.Errorf("ListAlgorithmsByType(Asymmetric) returned %d algorithms, want %d", len(algos), expectedCount)
	}

	if !sort.StringsAreSorted(algos) {
		t.Errorf("ListAlgorithmsByType(Asymmetric) result is not sorted: %v", algos)
	}

	expected := map[string]bool{
		AlgRSA2048: true, AlgRSA3072: true, AlgRSA4096: true,
		AlgECDSAP256: true, AlgECDSAP384: true, AlgECDSAP521: true,
		AlgEd25519: true, AlgEd448: true,
	}

	for _, alg := range algos {
		if !expected[alg] {
			t.Errorf("Unexpected asymmetric algorithm: %s", alg)
		}
		delete(expected, alg)
	}

	if len(expected) > 0 {
		t.Errorf("Missing asymmetric algorithms: %v", expected)
	}
}

// TestListAlgorithmsByType_HMAC tests filtering HMAC algorithms.
func TestListAlgorithmsByType_HMAC(t *testing.T) {
	algos := ListAlgorithmsByType(AlgorithmTypeHMAC)

	expectedCount := 3
	if len(algos) != expectedCount {
		t.Errorf("ListAlgorithmsByType(HMAC) returned %d algorithms, want %d", len(algos), expectedCount)
	}

	if !sort.StringsAreSorted(algos) {
		t.Errorf("ListAlgorithmsByType(HMAC) result is not sorted: %v", algos)
	}

	expected := map[string]bool{
		AlgHMACSHA256: true, AlgHMACSHA384: true, AlgHMACSHA512: true,
	}

	for _, alg := range algos {
		if !expected[alg] {
			t.Errorf("Unexpected HMAC algorithm: %s", alg)
		}
		delete(expected, alg)
	}

	if len(expected) > 0 {
		t.Errorf("Missing HMAC algorithms: %v", expected)
	}
}

// TestListAlgorithmsByType_KeyExchange tests filtering key exchange algorithms.
func TestListAlgorithmsByType_KeyExchange(t *testing.T) {
	algos := ListAlgorithmsByType(AlgorithmTypeKeyExchange)

	expectedCount := 2
	if len(algos) != expectedCount {
		t.Errorf("ListAlgorithmsByType(KeyExchange) returned %d algorithms, want %d", len(algos), expectedCount)
	}

	if !sort.StringsAreSorted(algos) {
		t.Errorf("ListAlgorithmsByType(KeyExchange) result is not sorted: %v", algos)
	}

	expected := map[string]bool{
		AlgX25519: true, AlgX448: true,
	}

	for _, alg := range algos {
		if !expected[alg] {
			t.Errorf("Unexpected key exchange algorithm: %s", alg)
		}
		delete(expected, alg)
	}

	if len(expected) > 0 {
		t.Errorf("Missing key exchange algorithms: %v", expected)
	}
}

// TestValidateKeySize_CorrectSizes tests validation with correct key sizes.
func TestValidateKeySize_CorrectSizes(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		keyBytes  int
	}{
		{"AES-128-GCM", AlgAES128GCM, 16},
		{"AES-192-GCM", AlgAES192GCM, 24},
		{"AES-256-GCM", AlgAES256GCM, 32},
		{"CHACHA20-POLY1305", AlgChaCha20Poly1305, 32},
		{"RSA-2048", AlgRSA2048, 256},
		{"RSA-3072", AlgRSA3072, 384},
		{"RSA-4096", AlgRSA4096, 512},
		{"ECDSA-P256", AlgECDSAP256, 32},
		{"ECDSA-P384", AlgECDSAP384, 48},
		{"ECDSA-P521", AlgECDSAP521, 65},
		{"ED25519", AlgEd25519, 32},
		{"ED448", AlgEd448, 56},
		{"X25519", AlgX25519, 32},
		{"X448", AlgX448, 56},
		{"HMAC-SHA256", AlgHMACSHA256, 32},
		{"HMAC-SHA384", AlgHMACSHA384, 48},
		{"HMAC-SHA512", AlgHMACSHA512, 64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateKeySize(tt.algorithm, tt.keyBytes)
			if err != nil {
				t.Errorf("ValidateKeySize(%s, %d) returned error: %v", tt.algorithm, tt.keyBytes, err)
			}
		})
	}
}

// TestValidateKeySize_IncorrectSizes tests validation with incorrect key sizes.
func TestValidateKeySize_IncorrectSizes(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		keyBytes  int
	}{
		{"AES-128-GCM too small", AlgAES128GCM, 15},
		{"AES-128-GCM too large", AlgAES128GCM, 32},
		{"AES-256-GCM wrong", AlgAES256GCM, 16},
		{"RSA-2048 wrong", AlgRSA2048, 128},
		{"ECDSA-P256 wrong", AlgECDSAP256, 64},
		{"ED25519 wrong", AlgEd25519, 64},
		{"HMAC-SHA256 wrong", AlgHMACSHA256, 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateKeySize(tt.algorithm, tt.keyBytes)
			if err == nil {
				t.Errorf("ValidateKeySize(%s, %d) expected error, got nil", tt.algorithm, tt.keyBytes)
			}
		})
	}
}

// TestValidateKeySize_InvalidAlgorithm tests validation with invalid algorithm.
func TestValidateKeySize_InvalidAlgorithm(t *testing.T) {
	err := ValidateKeySize("INVALID-ALGORITHM", 32)
	if err == nil {
		t.Error("ValidateKeySize(INVALID-ALGORITHM, 32) expected error, got nil")
	}
}

// TestCanAlgorithmPerform_Encrypt tests encryption capability checks.
func TestCanAlgorithmPerform_Encrypt(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		canDo     bool
	}{
		{"AES-128-GCM can encrypt", AlgAES128GCM, true},
		{"AES-256-GCM can encrypt", AlgAES256GCM, true},
		{"CHACHA20-POLY1305 can encrypt", AlgChaCha20Poly1305, true},
		{"RSA-2048 can encrypt", AlgRSA2048, true},
		{"ECDSA-P256 cannot encrypt", AlgECDSAP256, false},
		{"ED25519 cannot encrypt", AlgEd25519, false},
		{"X25519 cannot encrypt", AlgX25519, false},
		{"HMAC-SHA256 cannot encrypt", AlgHMACSHA256, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CanAlgorithmPerform(tt.algorithm, OpEncrypt)
			if result != tt.canDo {
				t.Errorf("CanAlgorithmPerform(%s, encrypt) = %v, want %v", tt.algorithm, result, tt.canDo)
			}
		})
	}
}

// TestCanAlgorithmPerform_Decrypt tests decryption capability checks.
func TestCanAlgorithmPerform_Decrypt(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		canDo     bool
	}{
		{"AES-256-GCM can decrypt", AlgAES256GCM, true},
		{"RSA-4096 can decrypt", AlgRSA4096, true},
		{"ECDSA-P384 cannot decrypt", AlgECDSAP384, false},
		{"HMAC-SHA512 cannot decrypt", AlgHMACSHA512, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CanAlgorithmPerform(tt.algorithm, OpDecrypt)
			if result != tt.canDo {
				t.Errorf("CanAlgorithmPerform(%s, decrypt) = %v, want %v", tt.algorithm, result, tt.canDo)
			}
		})
	}
}

// TestCanAlgorithmPerform_Sign tests signing capability checks.
func TestCanAlgorithmPerform_Sign(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		canDo     bool
	}{
		{"RSA-2048 can sign", AlgRSA2048, true},
		{"ECDSA-P256 can sign", AlgECDSAP256, true},
		{"ECDSA-P384 can sign", AlgECDSAP384, true},
		{"ED25519 can sign", AlgEd25519, true},
		{"ED448 can sign", AlgEd448, true},
		{"HMAC-SHA256 can sign", AlgHMACSHA256, true},
		{"HMAC-SHA384 can sign", AlgHMACSHA384, true},
		{"AES-256-GCM cannot sign", AlgAES256GCM, false},
		{"X25519 cannot sign", AlgX25519, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CanAlgorithmPerform(tt.algorithm, OpSign)
			if result != tt.canDo {
				t.Errorf("CanAlgorithmPerform(%s, sign) = %v, want %v", tt.algorithm, result, tt.canDo)
			}
		})
	}
}

// TestCanAlgorithmPerform_Verify tests verification capability checks.
func TestCanAlgorithmPerform_Verify(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		canDo     bool
	}{
		{"RSA-3072 can verify", AlgRSA3072, true},
		{"ECDSA-P521 can verify", AlgECDSAP521, true},
		{"HMAC-SHA512 can verify", AlgHMACSHA512, true},
		{"AES-128-GCM cannot verify", AlgAES128GCM, false},
		{"X448 cannot verify", AlgX448, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CanAlgorithmPerform(tt.algorithm, OpVerify)
			if result != tt.canDo {
				t.Errorf("CanAlgorithmPerform(%s, verify) = %v, want %v", tt.algorithm, result, tt.canDo)
			}
		})
	}
}

// TestCanAlgorithmPerform_Wrap tests key wrapping capability checks.
func TestCanAlgorithmPerform_Wrap(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		canDo     bool
	}{
		{"AES-128-GCM can wrap", AlgAES128GCM, true},
		{"AES-256-GCM can wrap", AlgAES256GCM, true},
		{"RSA-2048 can wrap", AlgRSA2048, true},
		{"RSA-4096 can wrap", AlgRSA4096, true},
		{"ECDSA-P256 cannot wrap", AlgECDSAP256, false},
		{"HMAC-SHA256 cannot wrap", AlgHMACSHA256, false},
		{"X25519 cannot wrap", AlgX25519, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CanAlgorithmPerform(tt.algorithm, OpWrap)
			if result != tt.canDo {
				t.Errorf("CanAlgorithmPerform(%s, wrap) = %v, want %v", tt.algorithm, result, tt.canDo)
			}
		})
	}
}

// TestCanAlgorithmPerform_Unwrap tests key unwrapping capability checks.
func TestCanAlgorithmPerform_Unwrap(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		canDo     bool
	}{
		{"AES-192-GCM can unwrap", AlgAES192GCM, true},
		{"RSA-3072 can unwrap", AlgRSA3072, true},
		{"ED25519 cannot unwrap", AlgEd25519, false},
		{"HMAC-SHA384 cannot unwrap", AlgHMACSHA384, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CanAlgorithmPerform(tt.algorithm, OpUnwrap)
			if result != tt.canDo {
				t.Errorf("CanAlgorithmPerform(%s, unwrap) = %v, want %v", tt.algorithm, result, tt.canDo)
			}
		})
	}
}

// TestCanAlgorithmPerform_Derive tests key derivation capability checks.
func TestCanAlgorithmPerform_Derive(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		canDo     bool
	}{
		{"X25519 can derive", AlgX25519, true},
		{"X448 can derive", AlgX448, true},
		{"HMAC-SHA256 can derive", AlgHMACSHA256, true},
		{"HMAC-SHA384 can derive", AlgHMACSHA384, true},
		{"HMAC-SHA512 can derive", AlgHMACSHA512, true},
		{"AES-256-GCM cannot derive", AlgAES256GCM, false},
		{"RSA-2048 cannot derive", AlgRSA2048, false},
		{"ECDSA-P256 cannot derive", AlgECDSAP256, false},
		{"ED25519 cannot derive", AlgEd25519, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CanAlgorithmPerform(tt.algorithm, OpDerive)
			if result != tt.canDo {
				t.Errorf("CanAlgorithmPerform(%s, derive) = %v, want %v", tt.algorithm, result, tt.canDo)
			}
		})
	}
}

// TestCanAlgorithmPerform_InvalidOperation tests unknown operation handling.
func TestCanAlgorithmPerform_InvalidOperation(t *testing.T) {
	result := CanAlgorithmPerform(AlgAES256GCM, "invalid-op")
	if result != false {
		t.Error("CanAlgorithmPerform with invalid operation should return false")
	}
}

// TestCanAlgorithmPerform_InvalidAlgorithm tests unknown algorithm handling.
func TestCanAlgorithmPerform_InvalidAlgorithm(t *testing.T) {
	result := CanAlgorithmPerform("INVALID-ALGORITHM", OpEncrypt)
	if result != false {
		t.Error("CanAlgorithmPerform with invalid algorithm should return false")
	}
}

// TestGetKeySizeBytes tests key size retrieval for all algorithms.
func TestGetKeySizeBytes(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		expected  int
	}{
		{"AES-128-GCM", AlgAES128GCM, 16},
		{"AES-192-GCM", AlgAES192GCM, 24},
		{"AES-256-GCM", AlgAES256GCM, 32},
		{"CHACHA20-POLY1305", AlgChaCha20Poly1305, 32},
		{"RSA-2048", AlgRSA2048, 256},
		{"RSA-3072", AlgRSA3072, 384},
		{"RSA-4096", AlgRSA4096, 512},
		{"ECDSA-P256", AlgECDSAP256, 32},
		{"ECDSA-P384", AlgECDSAP384, 48},
		{"ECDSA-P521", AlgECDSAP521, 65},
		{"ED25519", AlgEd25519, 32},
		{"ED448", AlgEd448, 56},
		{"X25519", AlgX25519, 32},
		{"X448", AlgX448, 56},
		{"HMAC-SHA256", AlgHMACSHA256, 32},
		{"HMAC-SHA384", AlgHMACSHA384, 48},
		{"HMAC-SHA512", AlgHMACSHA512, 64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size, err := GetKeySizeBytes(tt.algorithm)
			if err != nil {
				t.Fatalf("GetKeySizeBytes(%s) returned error: %v", tt.algorithm, err)
			}
			if size != tt.expected {
				t.Errorf("GetKeySizeBytes(%s) = %d, want %d", tt.algorithm, size, tt.expected)
			}
		})
	}
}

// TestGetKeySizeBytes_InvalidAlgorithm tests error handling for invalid algorithm.
func TestGetKeySizeBytes_InvalidAlgorithm(t *testing.T) {
	size, err := GetKeySizeBytes("INVALID-ALGORITHM")
	if err == nil {
		t.Error("GetKeySizeBytes(INVALID-ALGORITHM) expected error, got nil")
	}
	if size != 0 {
		t.Errorf("GetKeySizeBytes(INVALID-ALGORITHM) = %d, want 0", size)
	}
}

// TestGetIVSize tests IV/nonce size retrieval for AEAD algorithms.
func TestGetIVSize(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		expected  int
	}{
		{"AES-128-GCM", AlgAES128GCM, 12},
		{"AES-192-GCM", AlgAES192GCM, 12},
		{"AES-256-GCM", AlgAES256GCM, 12},
		{"CHACHA20-POLY1305", AlgChaCha20Poly1305, 12},
		{"RSA-2048 no IV", AlgRSA2048, 0},
		{"ECDSA-P256 no IV", AlgECDSAP256, 0},
		{"ED25519 no IV", AlgEd25519, 0},
		{"X25519 no IV", AlgX25519, 0},
		{"HMAC-SHA256 no IV", AlgHMACSHA256, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size, err := GetIVSize(tt.algorithm)
			if err != nil {
				t.Fatalf("GetIVSize(%s) returned error: %v", tt.algorithm, err)
			}
			if size != tt.expected {
				t.Errorf("GetIVSize(%s) = %d, want %d", tt.algorithm, size, tt.expected)
			}
		})
	}
}

// TestGetIVSize_InvalidAlgorithm tests error handling for invalid algorithm.
func TestGetIVSize_InvalidAlgorithm(t *testing.T) {
	size, err := GetIVSize("INVALID-ALGORITHM")
	if err == nil {
		t.Error("GetIVSize(INVALID-ALGORITHM) expected error, got nil")
	}
	if size != 0 {
		t.Errorf("GetIVSize(INVALID-ALGORITHM) = %d, want 0", size)
	}
}

// TestGetTagSize tests authentication tag size retrieval.
func TestGetTagSize(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		expected  int
	}{
		{"AES-128-GCM", AlgAES128GCM, 16},
		{"AES-192-GCM", AlgAES192GCM, 16},
		{"AES-256-GCM", AlgAES256GCM, 16},
		{"CHACHA20-POLY1305", AlgChaCha20Poly1305, 16},
		{"HMAC-SHA256", AlgHMACSHA256, 32},
		{"HMAC-SHA384", AlgHMACSHA384, 48},
		{"HMAC-SHA512", AlgHMACSHA512, 64},
		{"RSA-2048 no tag", AlgRSA2048, 0},
		{"ECDSA-P256 no tag", AlgECDSAP256, 0},
		{"ED25519 no tag", AlgEd25519, 0},
		{"X25519 no tag", AlgX25519, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size, err := GetTagSize(tt.algorithm)
			if err != nil {
				t.Fatalf("GetTagSize(%s) returned error: %v", tt.algorithm, err)
			}
			if size != tt.expected {
				t.Errorf("GetTagSize(%s) = %d, want %d", tt.algorithm, size, tt.expected)
			}
		})
	}
}

// TestGetTagSize_InvalidAlgorithm tests error handling for invalid algorithm.
func TestGetTagSize_InvalidAlgorithm(t *testing.T) {
	size, err := GetTagSize("INVALID-ALGORITHM")
	if err == nil {
		t.Error("GetTagSize(INVALID-ALGORITHM) expected error, got nil")
	}
	if size != 0 {
		t.Errorf("GetTagSize(INVALID-ALGORITHM) = %d, want 0", size)
	}
}

// TestGetSignatureSize tests signature size retrieval for signing algorithms.
func TestGetSignatureSize(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		expected  int
	}{
		{"RSA-2048", AlgRSA2048, 256},
		{"RSA-3072", AlgRSA3072, 384},
		{"RSA-4096", AlgRSA4096, 512},
		{"ECDSA-P256", AlgECDSAP256, 64},
		{"ECDSA-P384", AlgECDSAP384, 96},
		{"ECDSA-P521", AlgECDSAP521, 132},
		{"ED25519", AlgEd25519, 64},
		{"ED448", AlgEd448, 114},
		{"HMAC-SHA256", AlgHMACSHA256, 32},
		{"HMAC-SHA384", AlgHMACSHA384, 48},
		{"HMAC-SHA512", AlgHMACSHA512, 64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size, err := GetSignatureSize(tt.algorithm)
			if err != nil {
				t.Fatalf("GetSignatureSize(%s) returned error: %v", tt.algorithm, err)
			}
			if size != tt.expected {
				t.Errorf("GetSignatureSize(%s) = %d, want %d", tt.algorithm, size, tt.expected)
			}
		})
	}
}

// TestGetSignatureSize_NonSigningAlgorithm tests error for non-signing algorithms.
func TestGetSignatureSize_NonSigningAlgorithm(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
	}{
		{"AES-256-GCM", AlgAES256GCM},
		{"X25519", AlgX25519},
		{"X448", AlgX448},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size, err := GetSignatureSize(tt.algorithm)
			if err == nil {
				t.Errorf("GetSignatureSize(%s) expected error, got nil", tt.algorithm)
			}
			if size != 0 {
				t.Errorf("GetSignatureSize(%s) = %d, want 0", tt.algorithm, size)
			}
		})
	}
}

// TestGetSignatureSize_InvalidAlgorithm tests error handling for invalid algorithm.
func TestGetSignatureSize_InvalidAlgorithm(t *testing.T) {
	size, err := GetSignatureSize("INVALID-ALGORITHM")
	if err == nil {
		t.Error("GetSignatureSize(INVALID-ALGORITHM) expected error, got nil")
	}
	if size != 0 {
		t.Errorf("GetSignatureSize(INVALID-ALGORITHM) = %d, want 0", size)
	}
}

// TestAlgorithmType_String tests the String method for AlgorithmType.
func TestAlgorithmType_String(t *testing.T) {
	tests := []struct {
		name     string
		algType  AlgorithmType
		expected string
	}{
		{"Symmetric", AlgorithmTypeSymmetric, "Symmetric"},
		{"Asymmetric", AlgorithmTypeAsymmetric, "Asymmetric"},
		{"HMAC", AlgorithmTypeHMAC, "HMAC"},
		{"KeyExchange", AlgorithmTypeKeyExchange, "KeyExchange"},
		{"Unknown", AlgorithmType(999), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.algType.String()
			if result != tt.expected {
				t.Errorf("AlgorithmType.String() = %s, want %s", result, tt.expected)
			}
		})
	}
}

// =============================================================================
// Key Wrapping Algorithm Tests
// =============================================================================

// TestKeyWrappingAlgorithms_AllAESSizes tests AES key wrapping for all key sizes (128, 192, 256 bit).
func TestKeyWrappingAlgorithms_AllAESSizes(t *testing.T) {
	tests := []struct {
		name         string
		algorithm    string
		keyBits      int
		keyBytes     int
		canWrap      bool
		canUnwrap    bool
		ivSize       int
		tagSize      int
		algorithmFor string // What type of wrapping this algorithm is suited for
	}{
		{
			name:         "AES-128-GCM for envelope encryption",
			algorithm:    AlgAES128GCM,
			keyBits:      128,
			keyBytes:     16,
			canWrap:      true,
			canUnwrap:    true,
			ivSize:       12,
			tagSize:      16,
			algorithmFor: "AES-GCM envelope encryption",
		},
		{
			name:         "AES-192-GCM for envelope encryption",
			algorithm:    AlgAES192GCM,
			keyBits:      192,
			keyBytes:     24,
			canWrap:      true,
			canUnwrap:    true,
			ivSize:       12,
			tagSize:      16,
			algorithmFor: "AES-GCM envelope encryption",
		},
		{
			name:         "AES-256-GCM for envelope encryption",
			algorithm:    AlgAES256GCM,
			keyBits:      256,
			keyBytes:     32,
			canWrap:      true,
			canUnwrap:    true,
			ivSize:       12,
			tagSize:      16,
			algorithmFor: "AES-GCM envelope encryption",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify algorithm info
			info, err := GetAlgorithmInfo(tt.algorithm)
			if err != nil {
				t.Fatalf("GetAlgorithmInfo(%s) failed: %v", tt.algorithm, err)
			}

			// Verify key size
			if info.KeySize != tt.keyBits {
				t.Errorf("KeySize: got %d bits, want %d bits", info.KeySize, tt.keyBits)
			}

			// Verify wrapping capabilities
			if info.CanWrap != tt.canWrap {
				t.Errorf("CanWrap: got %v, want %v", info.CanWrap, tt.canWrap)
			}

			// Verify through CanAlgorithmPerform
			if CanAlgorithmPerform(tt.algorithm, OpWrap) != tt.canWrap {
				t.Errorf("CanAlgorithmPerform(%s, wrap): got %v, want %v",
					tt.algorithm, CanAlgorithmPerform(tt.algorithm, OpWrap), tt.canWrap)
			}
			if CanAlgorithmPerform(tt.algorithm, OpUnwrap) != tt.canUnwrap {
				t.Errorf("CanAlgorithmPerform(%s, unwrap): got %v, want %v",
					tt.algorithm, CanAlgorithmPerform(tt.algorithm, OpUnwrap), tt.canUnwrap)
			}

			// Verify IV size (important for AES-GCM wrapping)
			if info.IVSize != tt.ivSize {
				t.Errorf("IVSize: got %d, want %d", info.IVSize, tt.ivSize)
			}

			// Verify tag size (authentication tag for GCM mode)
			if info.TagSize != tt.tagSize {
				t.Errorf("TagSize: got %d, want %d", info.TagSize, tt.tagSize)
			}

			// Verify key size validation
			if err := ValidateKeySize(tt.algorithm, tt.keyBytes); err != nil {
				t.Errorf("ValidateKeySize(%s, %d) failed: %v", tt.algorithm, tt.keyBytes, err)
			}

			// Verify invalid key size is rejected
			invalidKeyBytes := tt.keyBytes + 1
			if err := ValidateKeySize(tt.algorithm, invalidKeyBytes); err == nil {
				t.Errorf("ValidateKeySize(%s, %d) should have failed for invalid size",
					tt.algorithm, invalidKeyBytes)
			}
		})
	}
}

// TestRSAKeyWrapping_AllSizes tests RSA-OAEP key wrapping for all key sizes.
func TestRSAKeyWrapping_AllSizes(t *testing.T) {
	tests := []struct {
		name        string
		algorithm   string
		keyBits     int
		keyBytes    int
		canWrap     bool
		canEncrypt  bool
		maxWrapSize int // Maximum data that can be wrapped (rough estimate)
	}{
		{
			name:        "RSA-2048 OAEP wrapping",
			algorithm:   AlgRSA2048,
			keyBits:     2048,
			keyBytes:    256,
			canWrap:     true,
			canEncrypt:  true,
			maxWrapSize: 190, // 2048/8 - 2*32 - 2 for OAEP-SHA256
		},
		{
			name:        "RSA-3072 OAEP wrapping",
			algorithm:   AlgRSA3072,
			keyBits:     3072,
			keyBytes:    384,
			canWrap:     true,
			canEncrypt:  true,
			maxWrapSize: 318, // 3072/8 - 2*32 - 2 for OAEP-SHA256
		},
		{
			name:        "RSA-4096 OAEP wrapping",
			algorithm:   AlgRSA4096,
			keyBits:     4096,
			keyBytes:    512,
			canWrap:     true,
			canEncrypt:  true,
			maxWrapSize: 446, // 4096/8 - 2*32 - 2 for OAEP-SHA256
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := GetAlgorithmInfo(tt.algorithm)
			if err != nil {
				t.Fatalf("GetAlgorithmInfo(%s) failed: %v", tt.algorithm, err)
			}

			// Verify RSA key sizes
			if info.KeySize != tt.keyBits {
				t.Errorf("KeySize: got %d bits, want %d bits", info.KeySize, tt.keyBits)
			}

			// Verify RSA can wrap
			if info.CanWrap != tt.canWrap {
				t.Errorf("CanWrap: got %v, want %v", info.CanWrap, tt.canWrap)
			}

			// Verify RSA can encrypt (required for OAEP wrapping)
			if info.CanEncrypt != tt.canEncrypt {
				t.Errorf("CanEncrypt: got %v, want %v", info.CanEncrypt, tt.canEncrypt)
			}

			// Verify this is an asymmetric algorithm
			if info.Type != AlgorithmTypeAsymmetric {
				t.Errorf("Type: got %v, want %v", info.Type, AlgorithmTypeAsymmetric)
			}

			// RSA should have no IV (uses OAEP padding with internal randomness)
			if info.IVSize != 0 {
				t.Errorf("IVSize: got %d, want 0 (RSA uses OAEP padding)", info.IVSize)
			}

			// RSA should have no block size
			if info.BlockSize != 0 {
				t.Errorf("BlockSize: got %d, want 0", info.BlockSize)
			}
		})
	}
}

// TestChaCha20Poly1305_KeyWrapping tests ChaCha20-Poly1305 envelope encryption.
func TestChaCha20Poly1305_KeyWrapping(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		keyBytes  int
		ivSize    int
		tagSize   int
	}{
		{
			name:      "ChaCha20-Poly1305",
			algorithm: AlgChaCha20Poly1305,
			keyBytes:  32, // 256-bit key
			ivSize:    12, // 96-bit nonce
			tagSize:   16, // 128-bit tag
		},
		{
			name:      "XChaCha20-Poly1305",
			algorithm: AlgXChaCha20Poly1305,
			keyBytes:  32, // 256-bit key
			ivSize:    24, // 192-bit extended nonce
			tagSize:   16, // 128-bit tag
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := GetAlgorithmInfo(tt.algorithm)
			if err != nil {
				t.Fatalf("GetAlgorithmInfo(%s) failed: %v", tt.algorithm, err)
			}

			// Verify symmetric type
			if info.Type != AlgorithmTypeSymmetric {
				t.Errorf("Type: got %v, want %v", info.Type, AlgorithmTypeSymmetric)
			}

			// Verify wrapping capability
			if !info.CanWrap {
				t.Errorf("CanWrap: got false, want true")
			}

			// ChaCha20 is a stream cipher, block size should be 0
			if info.BlockSize != 0 {
				t.Errorf("BlockSize: got %d, want 0 (stream cipher)", info.BlockSize)
			}

			// Verify nonce size
			if info.IVSize != tt.ivSize {
				t.Errorf("IVSize: got %d, want %d", info.IVSize, tt.ivSize)
			}

			// Verify authentication tag size
			if info.TagSize != tt.tagSize {
				t.Errorf("TagSize: got %d, want %d", info.TagSize, tt.tagSize)
			}

			// Verify key size
			keySize, err := GetKeySizeBytes(tt.algorithm)
			if err != nil {
				t.Fatalf("GetKeySizeBytes(%s) failed: %v", tt.algorithm, err)
			}
			if keySize != tt.keyBytes {
				t.Errorf("GetKeySizeBytes: got %d, want %d", keySize, tt.keyBytes)
			}
		})
	}
}

// TestAlgorithmsCannotWrap tests algorithms that should NOT support key wrapping.
func TestAlgorithmsCannotWrap(t *testing.T) {
	// Algorithms that cannot perform key wrapping
	nonWrappingAlgorithms := []struct {
		name      string
		algorithm string
		reason    string
	}{
		{
			name:      "ECDSA-P256",
			algorithm: AlgECDSAP256,
			reason:    "ECDSA is for signing only, not encryption",
		},
		{
			name:      "ECDSA-P384",
			algorithm: AlgECDSAP384,
			reason:    "ECDSA is for signing only, not encryption",
		},
		{
			name:      "ECDSA-P521",
			algorithm: AlgECDSAP521,
			reason:    "ECDSA is for signing only, not encryption",
		},
		{
			name:      "ED25519",
			algorithm: AlgEd25519,
			reason:    "EdDSA is for signing only, not encryption",
		},
		{
			name:      "ED448",
			algorithm: AlgEd448,
			reason:    "EdDSA is for signing only, not encryption",
		},
		{
			name:      "X25519",
			algorithm: AlgX25519,
			reason:    "X25519 is for key exchange (ECDH), not direct wrapping",
		},
		{
			name:      "X448",
			algorithm: AlgX448,
			reason:    "X448 is for key exchange (ECDH), not direct wrapping",
		},
		{
			name:      "HMAC-SHA256",
			algorithm: AlgHMACSHA256,
			reason:    "HMAC is for authentication, not encryption",
		},
		{
			name:      "HMAC-SHA384",
			algorithm: AlgHMACSHA384,
			reason:    "HMAC is for authentication, not encryption",
		},
		{
			name:      "HMAC-SHA512",
			algorithm: AlgHMACSHA512,
			reason:    "HMAC is for authentication, not encryption",
		},
	}

	for _, tt := range nonWrappingAlgorithms {
		t.Run(tt.name, func(t *testing.T) {
			info, err := GetAlgorithmInfo(tt.algorithm)
			if err != nil {
				t.Fatalf("GetAlgorithmInfo(%s) failed: %v", tt.algorithm, err)
			}

			if info.CanWrap {
				t.Errorf("CanWrap: got true, want false (%s)", tt.reason)
			}

			if CanAlgorithmPerform(tt.algorithm, OpWrap) {
				t.Errorf("CanAlgorithmPerform(%s, wrap) = true, want false (%s)",
					tt.algorithm, tt.reason)
			}

			if CanAlgorithmPerform(tt.algorithm, OpUnwrap) {
				t.Errorf("CanAlgorithmPerform(%s, unwrap) = true, want false (%s)",
					tt.algorithm, tt.reason)
			}
		})
	}
}

// TestKeyWrappingValidation_KeySizes tests key size validation for wrapping algorithms.
func TestKeyWrappingValidation_KeySizes(t *testing.T) {
	tests := []struct {
		name         string
		algorithm    string
		validBytes   int
		invalidBytes []int
	}{
		{
			name:         "AES-128-GCM key sizes",
			algorithm:    AlgAES128GCM,
			validBytes:   16,
			invalidBytes: []int{0, 8, 15, 17, 24, 32},
		},
		{
			name:         "AES-192-GCM key sizes",
			algorithm:    AlgAES192GCM,
			validBytes:   24,
			invalidBytes: []int{0, 16, 23, 25, 32},
		},
		{
			name:         "AES-256-GCM key sizes",
			algorithm:    AlgAES256GCM,
			validBytes:   32,
			invalidBytes: []int{0, 16, 24, 31, 33, 64},
		},
		{
			name:         "ChaCha20-Poly1305 key sizes",
			algorithm:    AlgChaCha20Poly1305,
			validBytes:   32,
			invalidBytes: []int{0, 16, 24, 31, 33},
		},
		{
			name:         "XChaCha20-Poly1305 key sizes",
			algorithm:    AlgXChaCha20Poly1305,
			validBytes:   32,
			invalidBytes: []int{0, 16, 24, 31, 33},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test valid key size
			if err := ValidateKeySize(tt.algorithm, tt.validBytes); err != nil {
				t.Errorf("ValidateKeySize(%s, %d) should succeed: %v",
					tt.algorithm, tt.validBytes, err)
			}

			// Test invalid key sizes
			for _, invalidSize := range tt.invalidBytes {
				if err := ValidateKeySize(tt.algorithm, invalidSize); err == nil {
					t.Errorf("ValidateKeySize(%s, %d) should fail for invalid size",
						tt.algorithm, invalidSize)
				}
			}
		})
	}
}

// TestKeyWrappingOperationSymmetry tests that wrap and unwrap capabilities are symmetric.
func TestKeyWrappingOperationSymmetry(t *testing.T) {
	// For all algorithms, if they can wrap, they should also be able to unwrap
	allAlgorithms := ListAlgorithms()

	for _, alg := range allAlgorithms {
		t.Run(alg, func(t *testing.T) {
			info, err := GetAlgorithmInfo(alg)
			if err != nil {
				t.Fatalf("GetAlgorithmInfo(%s) failed: %v", alg, err)
			}

			canWrap := CanAlgorithmPerform(alg, OpWrap)
			canUnwrap := CanAlgorithmPerform(alg, OpUnwrap)

			// Wrapping and unwrapping should be symmetric
			if canWrap != canUnwrap {
				t.Errorf("Wrap/Unwrap asymmetry: CanWrap=%v, CanUnwrap=%v", canWrap, canUnwrap)
			}

			// Both should match CanWrap field
			if canWrap != info.CanWrap {
				t.Errorf("CanAlgorithmPerform(wrap) != info.CanWrap: %v != %v", canWrap, info.CanWrap)
			}
		})
	}
}

// TestKeyWrappingEncryptionCorrelation tests that wrapping algorithms also support encryption.
func TestKeyWrappingEncryptionCorrelation(t *testing.T) {
	// Algorithms that can wrap should generally also be able to encrypt
	// (since wrapping is a form of encryption)
	allAlgorithms := ListAlgorithms()

	for _, alg := range allAlgorithms {
		t.Run(alg, func(t *testing.T) {
			info, err := GetAlgorithmInfo(alg)
			if err != nil {
				t.Fatalf("GetAlgorithmInfo(%s) failed: %v", alg, err)
			}

			// If an algorithm can wrap, it should be able to encrypt
			if info.CanWrap && !info.CanEncrypt {
				t.Errorf("Algorithm %s can wrap but cannot encrypt - this is unusual", alg)
			}
		})
	}
}

// TestAESGCMWrappingParameters tests AES-GCM specific parameters for key wrapping.
func TestAESGCMWrappingParameters(t *testing.T) {
	aesAlgorithms := []string{AlgAES128GCM, AlgAES192GCM, AlgAES256GCM}

	for _, alg := range aesAlgorithms {
		t.Run(alg, func(t *testing.T) {
			info, err := GetAlgorithmInfo(alg)
			if err != nil {
				t.Fatalf("GetAlgorithmInfo(%s) failed: %v", alg, err)
			}

			// AES-GCM requires 12-byte IV (96 bits) for optimal security
			if info.IVSize != 12 {
				t.Errorf("IVSize: got %d, want 12 (96 bits for GCM)", info.IVSize)
			}

			// AES-GCM uses 16-byte auth tag (128 bits)
			if info.TagSize != 16 {
				t.Errorf("TagSize: got %d, want 16 (128 bits)", info.TagSize)
			}

			// AES block size is always 16 bytes (128 bits)
			if info.BlockSize != 16 {
				t.Errorf("BlockSize: got %d, want 16 (128 bits)", info.BlockSize)
			}

			// AES-GCM is symmetric
			if info.Type != AlgorithmTypeSymmetric {
				t.Errorf("Type: got %v, want %v", info.Type, AlgorithmTypeSymmetric)
			}
		})
	}
}

// TestCanAlgorithmPerform_Rotate tests key rotation operation support.
func TestCanAlgorithmPerform_Rotate(t *testing.T) {
	// Key rotation should be supported for all algorithms
	allAlgorithms := ListAlgorithms()

	for _, alg := range allAlgorithms {
		t.Run(alg, func(t *testing.T) {
			// Rotation is a key management operation, always supported
			if !CanAlgorithmPerform(alg, OpRotate) {
				t.Errorf("CanAlgorithmPerform(%s, rotate) = false, want true (rotation always supported)", alg)
			}
		})
	}
}

// TestAlgorithmOperationCombinations tests various operation combinations for all algorithms.
func TestAlgorithmOperationCombinations(t *testing.T) {
	operations := []string{OpEncrypt, OpDecrypt, OpSign, OpVerify, OpWrap, OpUnwrap, OpDerive, OpRotate}

	allAlgorithms := ListAlgorithms()

	for _, alg := range allAlgorithms {
		t.Run(alg, func(t *testing.T) {
			info, err := GetAlgorithmInfo(alg)
			if err != nil {
				t.Fatalf("GetAlgorithmInfo(%s) failed: %v", alg, err)
			}

			for _, op := range operations {
				result := CanAlgorithmPerform(alg, op)

				// Verify consistency with AlgorithmInfo fields
				switch op {
				case OpEncrypt, OpDecrypt:
					if result != info.CanEncrypt {
						t.Errorf("CanAlgorithmPerform(%s, %s) = %v, but CanEncrypt = %v",
							alg, op, result, info.CanEncrypt)
					}
				case OpSign, OpVerify:
					if result != info.CanSign {
						t.Errorf("CanAlgorithmPerform(%s, %s) = %v, but CanSign = %v",
							alg, op, result, info.CanSign)
					}
				case OpWrap, OpUnwrap:
					if result != info.CanWrap {
						t.Errorf("CanAlgorithmPerform(%s, %s) = %v, but CanWrap = %v",
							alg, op, result, info.CanWrap)
					}
				case OpDerive:
					if result != info.CanDerive {
						t.Errorf("CanAlgorithmPerform(%s, %s) = %v, but CanDerive = %v",
							alg, op, result, info.CanDerive)
					}
				case OpRotate:
					if !result {
						t.Errorf("CanAlgorithmPerform(%s, rotate) should always be true", alg)
					}
				}
			}
		})
	}
}

// TestListAlgorithmsByType_UnknownType tests filtering with unknown algorithm type.
func TestListAlgorithmsByType_UnknownType(t *testing.T) {
	// Unknown type should return empty list
	algos := ListAlgorithmsByType(AlgorithmType(999))
	if len(algos) != 0 {
		t.Errorf("ListAlgorithmsByType(unknown) returned %d algorithms, want 0", len(algos))
	}
}

// TestWrappingAlgorithmsHaveRequiredMetadata tests that wrapping algorithms have all required fields.
func TestWrappingAlgorithmsHaveRequiredMetadata(t *testing.T) {
	// All wrapping-capable algorithms should have:
	// - Valid key size
	// - Proper IV size (for AEAD) or 0 (for asymmetric)
	// - Proper tag size (for AEAD) or 0 (for asymmetric)

	for _, alg := range ListAlgorithms() {
		info, err := GetAlgorithmInfo(alg)
		if err != nil {
			t.Fatalf("GetAlgorithmInfo(%s) failed: %v", alg, err)
		}

		if !info.CanWrap {
			continue // Skip non-wrapping algorithms
		}

		t.Run(alg, func(t *testing.T) {
			// Key size must be positive
			if info.KeySize <= 0 {
				t.Errorf("KeySize must be positive for wrapping algorithm, got %d", info.KeySize)
			}

			// For symmetric AEAD, IV and tag sizes must be positive
			if info.Type == AlgorithmTypeSymmetric {
				if info.IVSize <= 0 {
					t.Errorf("Symmetric wrapping algorithm should have IVSize > 0, got %d", info.IVSize)
				}
				if info.TagSize <= 0 {
					t.Errorf("Symmetric wrapping algorithm should have TagSize > 0, got %d", info.TagSize)
				}
			}

			// Key size in bytes should match validation
			keySizeBytes, err := GetKeySizeBytes(alg)
			if err != nil {
				t.Fatalf("GetKeySizeBytes(%s) failed: %v", alg, err)
			}
			if keySizeBytes != info.KeySize/8 {
				t.Errorf("Key size mismatch: GetKeySizeBytes=%d, KeySize/8=%d",
					keySizeBytes, info.KeySize/8)
			}
		})
	}
}

// TestGetAlgorithmInfo_ErrorMessage tests error message format for unknown algorithms.
func TestGetAlgorithmInfo_ErrorMessage(t *testing.T) {
	unknownAlg := "UNKNOWN-ALGORITHM-12345"
	_, err := GetAlgorithmInfo(unknownAlg)
	if err == nil {
		t.Fatal("expected error for unknown algorithm")
	}

	// Error message should contain the algorithm name
	if !strings.Contains(err.Error(), unknownAlg) {
		t.Errorf("error message should contain algorithm name %q: %v", unknownAlg, err)
	}
}

// TestValidateKeySize_ErrorMessage tests error message format for invalid key sizes.
func TestValidateKeySize_ErrorMessage(t *testing.T) {
	invalidBytes := 17
	err := ValidateKeySize(AlgAES128GCM, invalidBytes)
	if err == nil {
		t.Fatal("expected error for invalid key size")
	}

	// Error should contain algorithm name and both expected and actual sizes
	errMsg := err.Error()
	if !strings.Contains(errMsg, AlgAES128GCM) {
		t.Errorf("error should contain algorithm name: %v", err)
	}
	if !strings.Contains(errMsg, "16") { // Expected size
		t.Errorf("error should contain expected size: %v", err)
	}
	if !strings.Contains(errMsg, "17") { // Actual size
		t.Errorf("error should contain actual size: %v", err)
	}
}

// TestGetSignatureSize_ErrorMessage tests error message for non-signing algorithms.
func TestGetSignatureSize_ErrorMessage(t *testing.T) {
	_, err := GetSignatureSize(AlgAES256GCM)
	if err == nil {
		t.Fatal("expected error for non-signing algorithm")
	}

	if !strings.Contains(err.Error(), AlgAES256GCM) {
		t.Errorf("error should contain algorithm name: %v", err)
	}
	if !strings.Contains(err.Error(), "signing") {
		t.Errorf("error should mention signing: %v", err)
	}
}

// TestKeyWrappingSecurityLevels tests that security levels are appropriate for key wrapping.
func TestKeyWrappingSecurityLevels(t *testing.T) {
	// Security level mapping (approximate):
	// - AES-128: ~128-bit security
	// - AES-192: ~192-bit security
	// - AES-256: ~256-bit security
	// - RSA-2048: ~112-bit security
	// - RSA-3072: ~128-bit security
	// - RSA-4096: ~140-bit security

	tests := []struct {
		algorithm     string
		minKeyBits    int
		securityLevel string
	}{
		{AlgAES128GCM, 128, "128-bit symmetric"},
		{AlgAES192GCM, 192, "192-bit symmetric"},
		{AlgAES256GCM, 256, "256-bit symmetric"},
		{AlgChaCha20Poly1305, 256, "256-bit symmetric"},
		{AlgXChaCha20Poly1305, 256, "256-bit symmetric"},
		{AlgRSA2048, 2048, "~112-bit (RSA)"},
		{AlgRSA3072, 3072, "~128-bit (RSA)"},
		{AlgRSA4096, 4096, "~140-bit (RSA)"},
	}

	for _, tt := range tests {
		t.Run(tt.algorithm, func(t *testing.T) {
			info, err := GetAlgorithmInfo(tt.algorithm)
			if err != nil {
				t.Fatalf("GetAlgorithmInfo failed: %v", err)
			}

			if info.KeySize < tt.minKeyBits {
				t.Errorf("KeySize %d is below minimum %d for %s security",
					info.KeySize, tt.minKeyBits, tt.securityLevel)
			}

			// All these should support wrapping
			if !info.CanWrap {
				t.Errorf("Algorithm %s should support key wrapping", tt.algorithm)
			}
		})
	}
}

// TestAlgorithmConsistency tests that algorithm constants match registry entries.
func TestAlgorithmConsistency(t *testing.T) {
	// Test that all defined constants are in the registry
	constants := []string{
		AlgAES128GCM, AlgAES192GCM, AlgAES256GCM,
		AlgChaCha20Poly1305, AlgXChaCha20Poly1305,
		AlgRSA2048, AlgRSA3072, AlgRSA4096,
		AlgECDSAP256, AlgECDSAP384, AlgECDSAP521,
		AlgEd25519, AlgEd448,
		AlgX25519, AlgX448,
		AlgHMACSHA256, AlgHMACSHA384, AlgHMACSHA512,
	}

	registered := ListAlgorithms()
	registeredMap := make(map[string]bool)
	for _, alg := range registered {
		registeredMap[alg] = true
	}

	for _, constant := range constants {
		t.Run(constant, func(t *testing.T) {
			if !registeredMap[constant] {
				t.Errorf("Algorithm constant %s is not in registry", constant)
			}

			info, err := GetAlgorithmInfo(constant)
			if err != nil {
				t.Fatalf("GetAlgorithmInfo(%s) failed: %v", constant, err)
			}

			// Name in info should match the constant
			if info.Name != constant {
				t.Errorf("AlgorithmInfo.Name = %s, want %s", info.Name, constant)
			}
		})
	}

	// Test that registry has exactly the expected number of algorithms
	if len(registered) != len(constants) {
		t.Errorf("Registry has %d algorithms, expected %d", len(registered), len(constants))
	}
}

// TestOperationConstants tests that operation constants are properly defined.
func TestOperationConstants(t *testing.T) {
	tests := []struct {
		operation string
		expected  string
	}{
		{OpEncrypt, "encrypt"},
		{OpDecrypt, "decrypt"},
		{OpSign, "sign"},
		{OpVerify, "verify"},
		{OpWrap, "wrap"},
		{OpUnwrap, "unwrap"},
		{OpDerive, "derive"},
		{OpRotate, "rotate"},
	}

	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			if tt.operation != tt.expected {
				t.Errorf("Operation constant value = %q, want %q", tt.operation, tt.expected)
			}
		})
	}
}

func TestCoveragePush_UpdateStoredMetadata_InvalidType(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Pass wrong type
	err = service.UpdateStoredMetadata("not-a-metadata")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be *KeyMetadata")
}

func TestCoveragePush_UpdateStoredMetadata_EmptyKeyID(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.UpdateStoredMetadata(&KeyMetadata{
		KeyID: "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestCoveragePush_UpdateStoredMetadata_Success(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.UpdateStoredMetadata(&KeyMetadata{
		KeyID:       "test-key",
		Name:        "Test Key",
		Description: "A test key",
	})
	require.NoError(t, err)
}

func TestCoveragePush_UpdateStoredMetadata_WithLogger(t *testing.T) {
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	err = service.UpdateStoredMetadata(&KeyMetadata{
		KeyID:       "test-key",
		Name:        "Test Key",
		Description: "A test key",
	})
	require.NoError(t, err)
}

// =============================================================================
// ImportKey Coverage Tests
// =============================================================================

func TestCoveragePush_ImportKey_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.ImportKey(ctx, "", []byte("material"), AlgAES256GCM)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoveragePush_ImportKey_EmptyMaterial(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.ImportKey(ctx, "test-key", []byte{}, AlgAES256GCM)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key material cannot be empty")
}

func TestCoveragePush_ImportKey_InvalidAlgorithm(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.ImportKey(ctx, "test-key", []byte("material"), "INVALID-ALG")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported algorithm")
}

func TestCoveragePush_ImportKey_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.ImportKey(ctx, "test-key", []byte("material"), AlgAES256GCM)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoveragePush_ImportKey_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	backend.SetError("ImportKey", errors.New("import failed"))

	_, err = service.ImportKey(ctx, "test-key", []byte("material"), AlgAES256GCM)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "import failed")
}

func TestCoveragePush_ImportKey_DefaultAlgorithm(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	result, err := service.ImportKey(ctx, "test-key", []byte("material"), "")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestCoveragePush_ImportKey_WithLogger(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	result, err := service.ImportKey(ctx, "test-key", []byte("material"), AlgAES256GCM)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// =============================================================================
// GetKey Coverage Tests
// =============================================================================

func TestCoveragePush_GetKey_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.GetKey(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoveragePush_GetKey_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.GetKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoveragePush_GetKey_FromCache(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create key first - this stores in cache
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Get from cache
	result, err := service.GetKey(ctx, "test-key")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestCoveragePush_GetKey_NotFoundError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.GetKey(ctx, "non-existent-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

func TestCoveragePush_GetKey_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	backend.SetError("GetKeyMetadata", errors.New("backend failed"))

	_, err = service.GetKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backend failed")
}

func TestCoveragePush_GetKey_WithLogger(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Create key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	result, err := service.GetKey(ctx, "test-key")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// =============================================================================
// RotateKey Coverage Tests
// =============================================================================

func TestCoveragePush_RotateKey_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.RotateKey(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoveragePush_RotateKey_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.RotateKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoveragePush_RotateKey_NotFoundError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.RotateKey(ctx, "non-existent-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

func TestCoveragePush_RotateKey_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	backend.SetError("RotateKey", errors.New("rotation failed"))

	_, err = service.RotateKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rotation failed")
}

func TestCoveragePush_RotateKey_Success(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	result, err := service.RotateKey(ctx, "test-key")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestCoveragePush_RotateKey_WithCache(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry:         registry,
		CacheKeyMaterial: true,
	})
	require.NoError(t, err)

	// Create key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	result, err := service.RotateKey(ctx, "test-key")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestCoveragePush_RotateKey_WithLogger(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Create key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	result, err := service.RotateKey(ctx, "test-key")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// =============================================================================
// RevokeKey Coverage Tests
// =============================================================================

func TestCoveragePush_RevokeKey_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.RevokeKey(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoveragePush_RevokeKey_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.RevokeKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoveragePush_RevokeKey_NotFoundError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.RevokeKey(ctx, "non-existent-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

func TestCoveragePush_RevokeKey_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	backend.SetError("DisableKey", errors.New("disable failed"))

	err = service.RevokeKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disable failed")
}

func TestCoveragePush_RevokeKey_Success(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	err = service.RevokeKey(ctx, "test-key")
	require.NoError(t, err)
}

func TestCoveragePush_RevokeKey_WithCache(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry:         registry,
		CacheKeyMaterial: true,
	})
	require.NoError(t, err)

	// Create key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	err = service.RevokeKey(ctx, "test-key")
	require.NoError(t, err)
}

func TestCoveragePush_RevokeKey_WithLogger(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Create key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	err = service.RevokeKey(ctx, "test-key")
	require.NoError(t, err)
}

// =============================================================================
// ListKeys Coverage Tests
// =============================================================================

func TestCoveragePush_ListKeys_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.ListKeys(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoveragePush_ListKeys_Success(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create some keys
	for i := 0; i < 3; i++ {
		_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
			CN:                 "key-" + string(rune('a'+i)),
			KeyType:            xkmssdk.KeyTypeEncryption,
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		})
		require.NoError(t, err)
	}

	keys, err := service.ListKeys(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, 3, len(keys))
}

func TestCoveragePush_ListKeys_WithPrefix(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create keys with different prefixes
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "prefix1-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "prefix2-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	keys, err := service.ListKeys(ctx, "prefix1")
	require.NoError(t, err)
	assert.Equal(t, 1, len(keys))
}

func TestCoveragePush_ListKeys_BackendError(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	backend.SetError("ListKeys", errors.New("list failed"))

	// Should return empty list, not error (backend errors are logged but not propagated)
	keys, err := service.ListKeys(ctx, "")
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestCoveragePush_ListKeys_DisabledBackend(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Disable the backend
	registry.mu.Lock()
	registry.backends["software"].Enabled = false
	registry.mu.Unlock()

	keys, err := service.ListKeys(ctx, "")
	require.NoError(t, err)
	assert.Empty(t, keys)
}

// =============================================================================
// Close Coverage Tests
// =============================================================================

func TestCoveragePush_Close_BackendCloseError(t *testing.T) {
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	backend.SetError("Close", errors.New("close failed"))

	err = service.Close()
	// Close should not fail even if backend close fails
	require.NoError(t, err)
}

func TestCoveragePush_Close_CacheCloseError(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	cache := NewKeyCache(&KeyCacheOptions{
		DefaultTTL:      time.Minute,
		MaxTTL:          time.Hour,
		MaxEntries:      100,
		CleanupInterval: time.Minute,
		MaxKeySize:      1024,
	})

	service, err := NewKMSService(&KMSServiceOptions{
		Registry:         registry,
		CacheKeyMaterial: true,
		KeyCache:         cache,
	})
	require.NoError(t, err)

	// Close the cache first to simulate error
	cache.Close()

	err = service.Close()
	// Should return error due to closed cache
	require.Error(t, err)
}

// =============================================================================
// GetSupportedAlgorithms Coverage Tests
// =============================================================================

func TestCoveragePush_GetSupportedAlgorithms_MultipleBackends(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)

	backend1 := NewMockBackend()
	backend2 := NewMockBackend()
	require.NoError(t, registry.Register("software1", backend1, 100))
	require.NoError(t, registry.Register("software2", backend2, 90))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	algs, err := service.GetSupportedAlgorithms(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, algs)
}

func TestCoveragePush_GetSupportedAlgorithms_DisabledBackend(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Disable the backend
	registry.mu.Lock()
	registry.backends["software"].Enabled = false
	registry.mu.Unlock()

	algs, err := service.GetSupportedAlgorithms(ctx)
	require.NoError(t, err)
	assert.Empty(t, algs)
}

// =============================================================================
// IsAvailable Coverage Tests
// =============================================================================

func TestCoveragePush_IsAvailable_NoBackends(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Clear backends
	registry.mu.Lock()
	registry.backends = make(map[string]*BackendInfo)
	registry.mu.Unlock()

	available := service.IsAvailable(ctx)
	assert.False(t, available)
}

func TestCoveragePush_IsAvailable_DisabledBackend(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Disable the backend
	registry.mu.Lock()
	registry.backends["software"].Enabled = false
	registry.mu.Unlock()

	available := service.IsAvailable(ctx)
	assert.False(t, available)
}

func TestCoveragePush_IsAvailable_UnavailableBackend(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	backend.SetAvailable(false)
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	available := service.IsAvailable(ctx)
	assert.False(t, available)
}

// =============================================================================
// BackendCount and ListBackends Coverage Tests
// =============================================================================

func TestCoveragePush_BackendCount(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend1 := NewMockBackend()
	backend2 := NewMockBackend()
	require.NoError(t, registry.Register("software1", backend1, 100))
	require.NoError(t, registry.Register("software2", backend2, 90))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	count := service.BackendCount()
	assert.Equal(t, 2, count)
}

func TestCoveragePush_ListBackends(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	backends := service.ListBackends()
	assert.Equal(t, 1, len(backends))
	assert.Equal(t, "software", backends[0].Name)
}

// =============================================================================
// GetRouter and GetRegistry Coverage Tests
// =============================================================================

func TestCoveragePush_GetRouter(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	router := service.GetRouter()
	assert.NotNil(t, router)
}

func TestCoveragePush_GetRegistry(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	reg := service.GetRegistry()
	assert.Equal(t, registry, reg)
}

// =============================================================================
// KeyStore GetUsage Coverage Tests
// =============================================================================

func TestCoveragePush_KeyStore_GetUsage_NoCounter(t *testing.T) {
	store := NewKeyStore()

	// Put a key without incrementing usage
	err := store.Put(&KeyMetadata{
		KeyID:     "test-key",
		Algorithm: AlgAES256GCM,
		State:     KeyStateEnabled,
	})
	require.NoError(t, err)

	// Manually remove the usage counter to simulate edge case
	store.mu.Lock()
	delete(store.usageCounter, "test-key")
	store.mu.Unlock()

	usage, err := store.GetUsage("test-key")
	require.NoError(t, err)
	assert.Equal(t, uint64(0), usage)
}

// =============================================================================
// Router RouteWithFailover Coverage Tests
// =============================================================================

func TestCoveragePush_RouteWithFailover_NoBackends(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	router := NewBackendRouter(registry, &RoutingConfig{
		FailoverEnabled:     true,
		MaxFailoverAttempts: 3,
	})

	// Clear backends
	registry.mu.Lock()
	registry.backends = make(map[string]*BackendInfo)
	registry.mu.Unlock()

	err := router.RouteWithFailover(ctx, "key", AlgAES256GCM, func(b Backend) error {
		return nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no backends available")
}

func TestCoveragePush_RouteWithFailover_FailoverDisabled(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	router := NewBackendRouter(registry, &RoutingConfig{
		FailoverEnabled: false,
	})

	executed := false
	err := router.RouteWithFailover(ctx, "", AlgAES256GCM, func(b Backend) error {
		executed = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, executed)
}

func TestCoveragePush_RouteWithFailover_FailoverDisabled_Error(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	router := NewBackendRouter(registry, &RoutingConfig{
		FailoverEnabled: false,
	})

	// Clear backends to trigger routing error
	registry.mu.Lock()
	registry.backends = make(map[string]*BackendInfo)
	registry.mu.Unlock()

	err := router.RouteWithFailover(ctx, "", AlgAES256GCM, func(b Backend) error {
		return nil
	})
	require.Error(t, err)
}

func TestCoveragePush_RouteWithFailover_AllFail(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	router := NewBackendRouter(registry, &RoutingConfig{
		FailoverEnabled:     true,
		MaxFailoverAttempts: 3,
	}, WithLogger(logger))

	failCount := 0
	err := router.RouteWithFailover(ctx, "", AlgAES256GCM, func(b Backend) error {
		failCount++
		return errors.New("operation failed")
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all failover attempts failed")
	assert.Equal(t, 1, failCount) // Only one backend
}

func TestCoveragePush_RouteWithFailover_Success(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	router := NewBackendRouter(registry, &RoutingConfig{
		FailoverEnabled:     true,
		MaxFailoverAttempts: 3,
	})

	executed := false
	err := router.RouteWithFailover(ctx, "", AlgAES256GCM, func(b Backend) error {
		executed = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, executed)
}

func TestCoveragePush_RouteWithFailover_WithDefaultBackend(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))
	defaultBackend := NewMockBackend()

	router := NewBackendRouter(registry, &RoutingConfig{
		FailoverEnabled:     true,
		MaxFailoverAttempts: 3,
	}, WithDefaultBackend(defaultBackend))

	// Clear registry backends to force use of default
	registry.mu.Lock()
	registry.backends = make(map[string]*BackendInfo)
	registry.mu.Unlock()

	executed := false
	err := router.RouteWithFailover(ctx, "", AlgAES256GCM, func(b Backend) error {
		executed = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, executed)
}

// =============================================================================
// Router GetRegistry and GetConfig Coverage Tests
// =============================================================================

func TestCoveragePush_Router_GetRegistry(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	router := NewBackendRouter(registry, nil)

	reg := router.GetRegistry()
	assert.Equal(t, registry, reg)
}

func TestCoveragePush_Router_GetConfig(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	config := &RoutingConfig{
		FailoverEnabled:     true,
		MaxFailoverAttempts: 5,
	}
	router := NewBackendRouter(registry, config)

	cfg := router.GetConfig()
	assert.Equal(t, config, cfg)
}

// =============================================================================
// Service attrsToAlgorithmString Coverage Tests
// =============================================================================

func TestCoveragePush_AttrsToAlgorithmString_NilAttrs(t *testing.T) {
	result := attrsToAlgorithmString(nil)
	assert.Equal(t, AlgAES256GCM, result)
}

func TestCoveragePush_AttrsToAlgorithmString_AES128GCM(t *testing.T) {
	result := attrsToAlgorithmString(&xkmssdk.KeyAttributes{
		SymmetricAlgorithm: xkmssdk.SymmetricAES128GCM,
	})
	assert.Equal(t, AlgAES128GCM, result)
}

func TestCoveragePush_AttrsToAlgorithmString_ChaCha20(t *testing.T) {
	result := attrsToAlgorithmString(&xkmssdk.KeyAttributes{
		SymmetricAlgorithm: xkmssdk.SymmetricChaCha20Poly1305,
	})
	assert.Equal(t, AlgChaCha20Poly1305, result)
}

// =============================================================================
// Service validateKeyForOperation Coverage Tests
// =============================================================================

func TestCoveragePush_ValidateKeyForOperation_DisabledKey(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create and disable a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	err = service.DisableKey(ctx, "test-key")
	require.NoError(t, err)

	// Try to encrypt with disabled key
	_, err = service.Encrypt(ctx, "test-key", []byte("plaintext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestCoveragePush_ValidateKeyForOperation_PendingDeletion(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Manually set key state to pending deletion
	service.store.Put(&KeyMetadata{
		KeyID:     "test-key",
		Algorithm: AlgAES256GCM,
		State:     KeyStatePendingDeletion,
	})

	ctx := context.Background()
	_, err = service.Encrypt(ctx, "test-key", []byte("plaintext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pending deletion")
}

func TestCoveragePush_ValidateKeyForOperation_DeletedKey(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Manually set key state to deleted
	service.store.Put(&KeyMetadata{
		KeyID:     "test-key",
		Algorithm: AlgAES256GCM,
		State:     KeyStateDeleted,
	})

	ctx := context.Background()
	_, err = service.Encrypt(ctx, "test-key", []byte("plaintext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deleted")
}

func TestCoveragePush_ValidateKeyForOperation_UnsupportedOperation(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a signing key and try to encrypt with it
	service.store.Put(&KeyMetadata{
		KeyID:     "signing-key",
		Algorithm: AlgECDSAP256, // Signing algorithm
		State:     KeyStateEnabled,
	})

	ctx := context.Background()
	_, err = service.Encrypt(ctx, "signing-key", []byte("plaintext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support")
}

// =============================================================================
// Service copyMetadata Coverage Tests
// =============================================================================

func TestCoveragePush_CopyMetadata_AllFields(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	now := time.Now()
	lastUsed := now.Add(-time.Hour)

	original := &KeyMetadata{
		KeyID:              "test-key",
		Name:               "Test Key",
		Description:        "A test key",
		Algorithm:          AlgAES256GCM,
		State:              KeyStateEnabled,
		Version:            2,
		Exportable:         true,
		DeletionProtection: true,
		Created:            now,
		Updated:            now,
		UsageCount:         10,
		Owner:              "test-owner",
		Tags:               map[string]string{"env": "test"},
		Usage:              []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
		LastUsedAt:         &lastUsed,
		RotationPolicy: &RotationPolicy{
			Enabled:        true,
			Interval:       30 * 24 * time.Hour,
			NextRotation:   now.Add(30 * 24 * time.Hour),
			RetainVersions: 3,
			AutoRotate:     true,
		},
	}

	copied := service.copyMetadata(original)
	require.NotNil(t, copied)

	// Verify all fields are copied
	assert.Equal(t, original.KeyID, copied.KeyID)
	assert.Equal(t, original.Name, copied.Name)
	assert.Equal(t, original.Description, copied.Description)
	assert.Equal(t, original.Algorithm, copied.Algorithm)
	assert.Equal(t, original.State, copied.State)
	assert.Equal(t, original.Version, copied.Version)
	assert.Equal(t, original.Exportable, copied.Exportable)
	assert.Equal(t, original.DeletionProtection, copied.DeletionProtection)
	assert.Equal(t, original.UsageCount, copied.UsageCount)
	assert.Equal(t, original.Owner, copied.Owner)
	assert.Equal(t, original.Tags, copied.Tags)
	assert.Equal(t, original.Usage, copied.Usage)
	assert.Equal(t, *original.LastUsedAt, *copied.LastUsedAt)
	assert.Equal(t, original.RotationPolicy.Enabled, copied.RotationPolicy.Enabled)
	assert.Equal(t, original.RotationPolicy.Interval, copied.RotationPolicy.Interval)
	assert.Equal(t, original.RotationPolicy.AutoRotate, copied.RotationPolicy.AutoRotate)

	// Verify deep copy - modifications don't affect original
	copied.Tags["new"] = "value"
	assert.NotContains(t, original.Tags, "new")
}

func TestCoveragePush_CopyMetadata_NilInput(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	copied := service.copyMetadata(nil)
	assert.Nil(t, copied)
}

// =============================================================================
// NewKMSService Coverage Tests
// =============================================================================

func TestCoveragePush_NewKMSService_NilOptions(t *testing.T) {
	_, err := NewKMSService(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "options cannot be nil")
}

func TestCoveragePush_NewKMSService_NilRegistry(t *testing.T) {
	_, err := NewKMSService(&KMSServiceOptions{
		Registry: nil,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Registry is required")
}

func TestCoveragePush_NewKMSService_EmptyRegistry(t *testing.T) {
	registry := NewBackendRegistry(nil)

	_, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must contain at least one backend")
}

func TestCoveragePush_NewKMSService_InvalidDefaultAlgorithm(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	_, err := NewKMSService(&KMSServiceOptions{
		Registry:         registry,
		DefaultAlgorithm: "INVALID-ALG",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported algorithm")
}

func TestCoveragePush_NewKMSService_WithDefaultBackend(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))
	defaultBackend := NewMockBackend()

	service, err := NewKMSService(&KMSServiceOptions{
		Registry:       registry,
		DefaultBackend: defaultBackend,
	})
	require.NoError(t, err)
	assert.NotNil(t, service)
}

func TestCoveragePush_NewKMSService_WithCacheEnabled(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry:         registry,
		CacheKeyMaterial: true,
	})
	require.NoError(t, err)
	assert.NotNil(t, service)
	assert.NotNil(t, service.cache)
}

func TestCoveragePush_NewKMSService_WithKeyStore(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))
	store := NewKeyStore()

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		KeyStore: store,
	})
	require.NoError(t, err)
	assert.NotNil(t, service)
	assert.Equal(t, store, service.store)
}

func TestCoveragePush_NewKMSService_WithRoutingConfig(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	routingConfig := &RoutingConfig{
		FailoverEnabled:     true,
		MaxFailoverAttempts: 5,
		AlgorithmRouting:    map[string]string{AlgAES256GCM: "software"},
		BackendTypeRouting:  map[config.BackendType]string{config.BackendTypeSoftware: "software"},
	}

	service, err := NewKMSService(&KMSServiceOptions{
		Registry:      registry,
		RoutingConfig: routingConfig,
	})
	require.NoError(t, err)
	assert.NotNil(t, service)
}

// =============================================================================
// GenerateKey Additional Coverage Tests
// =============================================================================

func TestCoveragePush_GenerateKey_BackendRoutingError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Clear backends to trigger routing error
	registry.mu.Lock()
	registry.backends = make(map[string]*BackendInfo)
	registry.mu.Unlock()

	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to route to backend")
}

func TestCoveragePush_GenerateKey_WithLogger(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	result, err := service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// =============================================================================
// Sign and Verify Additional Coverage Tests
// =============================================================================

func TestCoveragePush_Sign_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a signing key with proper KeyAlgorithm
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:            "signing-key",
		KeyType:       xkmssdk.KeyTypeSigning,
		KeyAlgorithm:  x509.ECDSA,
		ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P256()},
	})
	require.NoError(t, err)

	backend.SetError("Sign", errors.New("sign failed"))

	_, err = service.Sign(ctx, "signing-key", []byte("digest"), "SHA256")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sign failed")
}

func TestCoveragePush_Verify_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a signing key with proper KeyAlgorithm
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:            "signing-key",
		KeyType:       xkmssdk.KeyTypeSigning,
		KeyAlgorithm:  x509.ECDSA,
		ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P256()},
	})
	require.NoError(t, err)

	backend.SetError("Verify", errors.New("verify failed"))

	_, err = service.Verify(ctx, "signing-key", []byte("digest"), []byte("SIG1"+string(make([]byte, 60))), "SHA256")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verify failed")
}

func TestCoveragePush_Sign_Success(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a signing key with proper KeyAlgorithm
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:            "signing-key",
		KeyType:       xkmssdk.KeyTypeSigning,
		KeyAlgorithm:  x509.ECDSA,
		ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P256()},
	})
	require.NoError(t, err)

	sig, err := service.Sign(ctx, "signing-key", []byte("digest"), "SHA256")
	require.NoError(t, err)
	assert.NotEmpty(t, sig)
}

func TestCoveragePush_Verify_Success(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a signing key with proper KeyAlgorithm
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:            "signing-key",
		KeyType:       xkmssdk.KeyTypeSigning,
		KeyAlgorithm:  x509.ECDSA,
		ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P256()},
	})
	require.NoError(t, err)

	// Sign and verify
	digest := []byte("digest")
	sig, err := service.Sign(ctx, "signing-key", digest, "SHA256")
	require.NoError(t, err)

	valid, err := service.Verify(ctx, "signing-key", digest, sig, "SHA256")
	require.NoError(t, err)
	assert.True(t, valid)
}

// =============================================================================
// Router Edge Cases Coverage Tests
// =============================================================================

func TestCoveragePush_Router_RouteByBackendName_NilRegistry(t *testing.T) {
	router := &BackendRouter{
		registry: nil,
		config:   DefaultRoutingConfig(),
	}

	ctx := context.Background()
	_, err := router.Route(ctx, "hsm:signing:ecdsa-p256:my-key", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no available backend")
}

func TestCoveragePush_Router_Route_AlgorithmRouting(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	router := NewBackendRouter(registry, &RoutingConfig{
		AlgorithmRouting: map[string]string{AlgAES256GCM: "software"},
	})

	b, err := router.Route(ctx, "", AlgAES256GCM)
	require.NoError(t, err)
	assert.NotNil(t, b)
}

func TestCoveragePush_Router_Route_DefaultBackendConfig(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	router := NewBackendRouter(registry, &RoutingConfig{
		DefaultBackend: "software",
	})

	b, err := router.Route(ctx, "", "")
	require.NoError(t, err)
	assert.NotNil(t, b)
}

func TestCoveragePush_Router_Route_StructuredKeyID(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("hsm", backend, 100))

	router := NewBackendRouter(registry, DefaultRoutingConfig(), WithLogger(logger))

	b, err := router.Route(ctx, "hsm:signing:ecdsa-p256:my-key", "")
	require.NoError(t, err)
	assert.NotNil(t, b)
}

func TestCoveragePush_Router_Route_StructuredKeyID_BackendNotFound(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	router := NewBackendRouter(registry, DefaultRoutingConfig(), WithLogger(logger))

	// hsm backend doesn't exist, should fall back
	b, err := router.Route(ctx, "hsm:signing:ecdsa-p256:my-key", "")
	require.NoError(t, err)
	assert.NotNil(t, b)
}

// =============================================================================
// ParseKeyID Coverage Tests
// =============================================================================

func TestCoveragePush_ParseKeyID_Unstructured(t *testing.T) {
	parsed := ParseKeyID("simple-key")
	assert.Equal(t, "simple-key", parsed.Raw)
	assert.Equal(t, "simple-key", parsed.KeyName)
	assert.Empty(t, parsed.Backend)
	assert.Empty(t, parsed.KeyType)
	assert.Empty(t, parsed.Algorithm)
	assert.False(t, parsed.IsStructured)
}

func TestCoveragePush_ParseKeyID_Structured(t *testing.T) {
	parsed := ParseKeyID("hsm:signing:ecdsa-p256:my-key")
	assert.Equal(t, "hsm:signing:ecdsa-p256:my-key", parsed.Raw)
	assert.Equal(t, "my-key", parsed.KeyName)
	assert.Equal(t, "hsm", parsed.Backend)
	assert.Equal(t, "signing", parsed.KeyType)
	assert.Equal(t, "ecdsa-p256", parsed.Algorithm)
	assert.True(t, parsed.IsStructured)
}

func TestCoveragePush_ParseKeyID_StructuredWithColonsInName(t *testing.T) {
	parsed := ParseKeyID("software:encryption:aes-256-gcm:my:special:key")
	assert.Equal(t, "my:special:key", parsed.KeyName)
	assert.Equal(t, "software", parsed.Backend)
	assert.True(t, parsed.IsStructured)
}

// =============================================================================
// DefaultRoutingConfig Coverage Tests
// =============================================================================

func TestCoveragePush_DefaultRoutingConfig(t *testing.T) {
	config := DefaultRoutingConfig()
	assert.True(t, config.FailoverEnabled)
	assert.Equal(t, 3, config.MaxFailoverAttempts)
	assert.NotNil(t, config.AlgorithmRouting)
	assert.NotNil(t, config.BackendTypeRouting)
}

// =============================================================================
// GetBackendType Additional Coverage Tests
// =============================================================================

func TestCoveragePush_GetBackendType_WithBackends(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	backendType := service.GetBackendType()
	assert.Equal(t, config.BackendTypeSoftware, backendType)
}

// =============================================================================
// Additional Coverage Tests for Service Methods
// =============================================================================

func TestCoveragePush_EnableKey_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.EnableKey(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoveragePush_EnableKey_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.EnableKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoveragePush_EnableKey_NotFound(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.EnableKey(ctx, "non-existent-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

func TestCoveragePush_EnableKey_BackendRoutingError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Clear backends to trigger routing error
	registry.mu.Lock()
	registry.backends = make(map[string]*BackendInfo)
	registry.mu.Unlock()

	err = service.EnableKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to route to backend")
}

func TestCoveragePush_EnableKey_Success(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create and disable a key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	err = service.DisableKey(ctx, "test-key")
	require.NoError(t, err)

	// Now enable
	err = service.EnableKey(ctx, "test-key")
	require.NoError(t, err)
}

func TestCoveragePush_DisableKey_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.DisableKey(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoveragePush_DisableKey_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.DisableKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoveragePush_DisableKey_NotFound(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.DisableKey(ctx, "non-existent-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

func TestCoveragePush_DisableKey_BackendRoutingError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Clear backends to trigger routing error
	registry.mu.Lock()
	registry.backends = make(map[string]*BackendInfo)
	registry.mu.Unlock()

	err = service.DisableKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to route to backend")
}

func TestCoveragePush_DeleteKey_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.DeleteKey(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoveragePush_DeleteKey_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.DeleteKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoveragePush_DeleteKey_NotFound(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.DeleteKey(ctx, "non-existent-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

func TestCoveragePush_DeleteKey_BackendRoutingError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Clear backends to trigger routing error
	registry.mu.Lock()
	registry.backends = make(map[string]*BackendInfo)
	registry.mu.Unlock()

	err = service.DeleteKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to route to backend")
}

func TestCoveragePush_DeleteKey_Success(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Disable before delete (some backends require this)
	err = service.DisableKey(ctx, "test-key")
	require.NoError(t, err)

	// Now delete
	err = service.DeleteKey(ctx, "test-key")
	require.NoError(t, err)
}

func TestCoveragePush_DeleteKey_WithCache(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry:         registry,
		CacheKeyMaterial: true,
	})
	require.NoError(t, err)

	// Create a key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Delete - this should also clear the cache
	err = service.DeleteKey(ctx, "test-key")
	require.NoError(t, err)
}

func TestCoveragePush_Encrypt_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Encrypt(ctx, "", []byte("plaintext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoveragePush_Encrypt_EmptyPlaintext(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Encrypt(ctx, "test-key", []byte{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plaintext cannot be empty")
}

func TestCoveragePush_Encrypt_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Encrypt(ctx, "test-key", []byte("plaintext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoveragePush_Encrypt_BackendRoutingError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Clear backends to trigger routing error
	registry.mu.Lock()
	registry.backends = make(map[string]*BackendInfo)
	registry.mu.Unlock()

	_, err = service.Encrypt(ctx, "test-key", []byte("plaintext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to route to backend")
}

func TestCoveragePush_Encrypt_Success(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	ciphertext, err := service.Encrypt(ctx, "test-key", []byte("plaintext"), nil)
	require.NoError(t, err)
	assert.NotEmpty(t, ciphertext)
}

func TestCoveragePush_Encrypt_WithAAD(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	ciphertext, err := service.Encrypt(ctx, "test-key", []byte("plaintext"), []byte("aad-data"))
	require.NoError(t, err)
	assert.NotEmpty(t, ciphertext)
}

func TestCoveragePush_Decrypt_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Decrypt(ctx, "", []byte("ciphertext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoveragePush_Decrypt_EmptyCiphertext(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Decrypt(ctx, "test-key", []byte{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ciphertext cannot be empty")
}

func TestCoveragePush_Decrypt_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Decrypt(ctx, "test-key", []byte("ciphertext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}
func TestCoveragePush_GenerateDataKey_NilAttrs(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, _, err = service.GenerateDataKey(ctx, nil, 32)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key attributes cannot be nil")
}

func TestCoveragePush_GenerateDataKey_EmptyCN(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, _, err = service.GenerateDataKey(ctx, &xkmssdk.KeyAttributes{CN: ""}, 32)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CN (common name) cannot be empty")
}

func TestCoveragePush_GenerateDataKey_InvalidKeyBytes(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, _, err = service.GenerateDataKey(ctx, &xkmssdk.KeyAttributes{CN: "test-key"}, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyBytes must be positive")
}

func TestCoveragePush_GenerateDataKey_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, _, err = service.GenerateDataKey(ctx, &xkmssdk.KeyAttributes{CN: "test-key"}, 32)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoveragePush_GenerateDataKey_Success(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	plaintext, ciphertext, err := service.GenerateDataKey(ctx, &xkmssdk.KeyAttributes{CN: "test-key"}, 32)
	require.NoError(t, err)
	assert.NotEmpty(t, plaintext)
	assert.NotEmpty(t, ciphertext)
}

// =============================================================================
// Backend Factory Coverage Tests
// =============================================================================

func TestCoveragePush_BackendFactory_New_NilConfig(t *testing.T) {
	factory, err := NewBackendFactory(nil)
	require.NoError(t, err)
	assert.NotNil(t, factory)
}

func TestCoveragePush_BackendFactory_Registry(t *testing.T) {
	factory, err := NewBackendFactory(&BackendFactoryConfig{
		HealthCheckInterval: 60,
	})
	require.NoError(t, err)

	registry := factory.Registry()
	assert.NotNil(t, registry)
}

func TestCoveragePush_BackendFactory_RegisterBackend(t *testing.T) {
	logger := newCoverageTestLogger()
	factory, err := NewBackendFactory(&BackendFactoryConfig{
		Logger:              logger,
		HealthCheckInterval: 60,
	})
	require.NoError(t, err)

	backend := NewMockBackend()
	err = factory.RegisterBackend("test-backend", backend, 100)
	require.NoError(t, err)

	// Verify registration
	registry := factory.Registry()
	backends := registry.List()
	assert.Equal(t, 1, len(backends))
}

func TestCoveragePush_BackendFactory_RegisterBackend_Error(t *testing.T) {
	factory, err := NewBackendFactory(&BackendFactoryConfig{
		HealthCheckInterval: 60,
	})
	require.NoError(t, err)

	backend := NewMockBackend()

	// Register once
	err = factory.RegisterBackend("test-backend", backend, 100)
	require.NoError(t, err)

	// Try to register same backend again - should fail
	err = factory.RegisterBackend("test-backend", backend, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to register backend")
}

func TestCoveragePush_BackendFactory_Close(t *testing.T) {
	factory, err := NewBackendFactory(&BackendFactoryConfig{
		HealthCheckInterval: 60,
	})
	require.NoError(t, err)

	backend := NewMockBackend()
	err = factory.RegisterBackend("test-backend", backend, 100)
	require.NoError(t, err)

	err = factory.Close()
	require.NoError(t, err)
}

// =============================================================================
// Sign and Verify Edge Cases
// =============================================================================

func TestCoveragePush_Sign_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Sign(ctx, "", []byte("digest"), "SHA256")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoveragePush_Sign_EmptyDigest(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Sign(ctx, "test-key", []byte{}, "SHA256")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "digest cannot be empty")
}

func TestCoveragePush_Sign_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Sign(ctx, "test-key", []byte("digest"), "SHA256")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoveragePush_Verify_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Verify(ctx, "", []byte("digest"), []byte("signature"), "SHA256")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoveragePush_Verify_EmptyDigest(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Verify(ctx, "test-key", []byte{}, []byte("signature"), "SHA256")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "digest cannot be empty")
}

func TestCoveragePush_Verify_EmptySignature(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Verify(ctx, "test-key", []byte("digest"), []byte{}, "SHA256")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature cannot be empty")
}

func TestCoveragePush_Verify_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Verify(ctx, "test-key", []byte("digest"), []byte("signature"), "SHA256")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

// =============================================================================
// GetKey Additional Coverage Tests
// =============================================================================

func TestCoveragePush_GetKey_BackendRoutingError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Clear backends to trigger routing error
	registry.mu.Lock()
	registry.backends = make(map[string]*BackendInfo)
	registry.mu.Unlock()

	_, err = service.GetKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to route to backend")
}

func TestCoveragePush_GetKey_StoreMetadataSuccess(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// The store should cache the metadata
	result, err := service.GetKey(ctx, "test-key")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// =============================================================================
// ImportKey Additional Coverage Tests
// =============================================================================

func TestCoveragePush_ImportKey_BackendRoutingError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Clear backends to trigger routing error
	registry.mu.Lock()
	registry.backends = make(map[string]*BackendInfo)
	registry.mu.Unlock()

	_, err = service.ImportKey(ctx, "test-key", []byte("material"), AlgAES256GCM)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to route to backend")
}

// =============================================================================
// RotateKey Additional Coverage Tests
// =============================================================================

func TestCoveragePush_RotateKey_BackendRoutingError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Clear backends to trigger routing error
	registry.mu.Lock()
	registry.backends = make(map[string]*BackendInfo)
	registry.mu.Unlock()

	_, err = service.RotateKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to route to backend")
}

// =============================================================================
// RevokeKey Additional Coverage Tests
// =============================================================================

func TestCoveragePush_RevokeKey_BackendRoutingError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Clear backends to trigger routing error
	registry.mu.Lock()
	registry.backends = make(map[string]*BackendInfo)
	registry.mu.Unlock()

	err = service.RevokeKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to route to backend")
}

// =============================================================================
// Additional Algorithm Coverage Tests
// =============================================================================

func TestCoveragePush_AttrsToAlgorithmString_RSA(t *testing.T) {
	result := attrsToAlgorithmString(&xkmssdk.KeyAttributes{
		KeyAlgorithm: x509.RSA,
	})
	assert.Equal(t, AlgRSA2048, result)
}

func TestCoveragePush_AttrsToAlgorithmString_ECDSA(t *testing.T) {
	result := attrsToAlgorithmString(&xkmssdk.KeyAttributes{
		KeyAlgorithm: x509.ECDSA,
		ECCAttributes: &xkmssdk.ECCAttributes{
			Curve: elliptic.P256(),
		},
	})
	assert.Equal(t, AlgECDSAP256, result)
}

// =============================================================================
// EnableKey and DisableKey Backend Error Tests
// =============================================================================

func TestCoveragePush_EnableKey_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Set backend error
	backend.SetError("EnableKey", errors.New("enable failed"))

	err = service.EnableKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "enable failed")
}

func TestCoveragePush_DisableKey_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Set backend error
	backend.SetError("DisableKey", errors.New("disable failed"))

	err = service.DisableKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disable failed")
}

func TestCoveragePush_DeleteKey_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Set backend error
	backend.SetError("DeleteKey", errors.New("delete failed"))

	err = service.DeleteKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete failed")
}

// =============================================================================
// Encrypt and Decrypt Backend Error Tests
// =============================================================================

func TestCoveragePush_Encrypt_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Set backend error
	backend.SetError("Encrypt", errors.New("encrypt failed"))

	_, err = service.Encrypt(ctx, "test-key", []byte("plaintext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encrypt failed")
}

func TestCoveragePush_Decrypt_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Set backend error
	backend.SetError("Decrypt", errors.New("decrypt failed"))

	_, err = service.Decrypt(ctx, "test-key", []byte("ciphertext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decrypt failed")
}

func TestCoveragePush_Decrypt_BackendRoutingError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Clear backends to trigger routing error
	registry.mu.Lock()
	registry.backends = make(map[string]*BackendInfo)
	registry.mu.Unlock()

	_, err = service.Decrypt(ctx, "test-key", []byte("ciphertext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to route to backend")
}

// =============================================================================
// GenerateDataKey Additional Tests
// =============================================================================

func TestCoveragePush_GenerateDataKey_BackendRoutingError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Clear backends to trigger routing error
	registry.mu.Lock()
	registry.backends = make(map[string]*BackendInfo)
	registry.mu.Unlock()

	_, _, err = service.GenerateDataKey(ctx, &xkmssdk.KeyAttributes{CN: "test-key"}, 32)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to route to backend")
}

func TestCoveragePush_GenerateDataKey_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Set backend error
	backend.SetError("GenerateDataKey", errors.New("generate data key failed"))

	_, _, err = service.GenerateDataKey(ctx, &xkmssdk.KeyAttributes{CN: "test-key"}, 32)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "generate data key failed")
}

// =============================================================================
// Sign and Verify Backend Routing Error Tests
// =============================================================================

func TestCoveragePush_Sign_BackendRoutingError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Clear backends to trigger routing error
	registry.mu.Lock()
	registry.backends = make(map[string]*BackendInfo)
	registry.mu.Unlock()

	_, err = service.Sign(ctx, "test-key", []byte("digest"), "SHA256")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to route to backend")
}

func TestCoveragePush_Verify_BackendRoutingError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Clear backends to trigger routing error
	registry.mu.Lock()
	registry.backends = make(map[string]*BackendInfo)
	registry.mu.Unlock()

	_, err = service.Verify(ctx, "test-key", []byte("digest"), []byte("signature"), "SHA256")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to route to backend")
}

// =============================================================================
// Close Additional Tests
// =============================================================================

func TestCoveragePush_Close_Success(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.Close()
	require.NoError(t, err)
}

func TestCoveragePush_Close_WithCacheEnabled(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry:         registry,
		CacheKeyMaterial: true,
	})
	require.NoError(t, err)

	err = service.Close()
	require.NoError(t, err)
}

// =============================================================================
// ListKeys with Backend Keys Tests
// =============================================================================

func TestCoveragePush_ListKeys_WithBackendKeys(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create some keys
	for i := 0; i < 3; i++ {
		_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
			CN:                 "key-" + string(rune('a'+i)),
			KeyType:            xkmssdk.KeyTypeEncryption,
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		})
		require.NoError(t, err)
	}

	keys, err := service.ListKeys(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, 3, len(keys))
}

// =============================================================================
// GetDecryptedKey Tests
// =============================================================================

// =============================================================================
// Registry Additional Tests
// =============================================================================

func TestCoveragePush_Registry_Unregister(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	// List should have 1 backend
	assert.Equal(t, 1, len(registry.List()))

	// Unregister
	err := registry.Unregister("software")
	require.NoError(t, err)

	// List should be empty
	assert.Equal(t, 0, len(registry.List()))
}

func TestCoveragePush_Registry_Unregister_NotFound(t *testing.T) {
	registry := NewBackendRegistry(nil)

	err := registry.Unregister("non-existent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCoveragePush_Registry_Get(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	b, err := registry.Get("software")
	require.NoError(t, err)
	assert.NotNil(t, b)
}

func TestCoveragePush_Registry_Get_NotFound(t *testing.T) {
	registry := NewBackendRegistry(nil)

	_, err := registry.Get("non-existent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCoveragePush_Registry_Enable_Disable(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	// Disable
	err := registry.Disable("software")
	require.NoError(t, err)

	// Enable
	err = registry.Enable("software")
	require.NoError(t, err)
}

func TestCoveragePush_Registry_Enable_NotFound(t *testing.T) {
	registry := NewBackendRegistry(nil)

	err := registry.Enable("non-existent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCoveragePush_Registry_Disable_NotFound(t *testing.T) {
	registry := NewBackendRegistry(nil)

	err := registry.Disable("non-existent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCoveragePush_Registry_SetPriority(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	err := registry.SetPriority("software", 50)
	require.NoError(t, err)
}

func TestCoveragePush_Registry_SetPriority_NotFound(t *testing.T) {
	registry := NewBackendRegistry(nil)

	err := registry.SetPriority("non-existent", 50)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCoveragePush_Registry_GetHealthy(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	b, err := registry.GetHealthy(ctx)
	require.NoError(t, err)
	assert.NotNil(t, b)
}

func TestCoveragePush_Registry_GetHealthy_NoBackends(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)

	_, err := registry.GetHealthy(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no healthy backends")
}

func TestCoveragePush_Registry_GetByAlgorithm(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	b, err := registry.GetByAlgorithm(ctx, AlgAES256GCM)
	require.NoError(t, err)
	assert.NotNil(t, b)
}

func TestCoveragePush_Registry_GetByAlgorithm_NoBackends(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)

	_, err := registry.GetByAlgorithm(ctx, AlgAES256GCM)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no healthy backend supports algorithm")
}

func TestCoveragePush_Registry_GetHealth(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	health, err := registry.GetHealth("software")
	require.NoError(t, err)
	assert.NotNil(t, health)
}

func TestCoveragePush_Registry_GetHealth_NotFound(t *testing.T) {
	registry := NewBackendRegistry(nil)

	_, err := registry.GetHealth("non-existent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCoveragePush_Registry_GetAllHealth(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	health := registry.GetAllHealth()
	assert.NotNil(t, health)
	assert.Contains(t, health, "software")
}

func TestCoveragePush_Registry_ListWithBackends(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	backends := registry.ListWithBackends()
	assert.Equal(t, 1, len(backends))
	assert.NotNil(t, backends[0].Backend)
}

func TestCoveragePush_Registry_CheckHealth(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	health, err := registry.CheckHealth(ctx, "software")
	require.NoError(t, err)
	assert.NotNil(t, health)
}

func TestCoveragePush_Registry_CheckHealth_NotFound(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)

	_, err := registry.CheckHealth(ctx, "non-existent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCoveragePush_Registry_Stats(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	stats := registry.Stats()
	assert.NotNil(t, stats)
	assert.Equal(t, 1, stats.TotalBackends)
}

func TestCoveragePush_Registry_Close(t *testing.T) {
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(&RegistryConfig{
		Logger:              logger,
		HealthCheckInterval: time.Second,
	})
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	err := registry.Close()
	require.NoError(t, err)
}

// =============================================================================
// KeyCache Additional Tests
// =============================================================================

func TestCoveragePush_KeyCache_GetNotFound(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		DefaultTTL:      time.Minute,
		MaxTTL:          time.Hour,
		MaxEntries:      100,
		CleanupInterval: time.Minute,
		MaxKeySize:      1024,
	})
	defer cache.Close()

	_, found := cache.Get("non-existent")
	assert.False(t, found)
}

func TestCoveragePush_KeyCache_PutGet(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		DefaultTTL:      time.Minute,
		MaxTTL:          time.Hour,
		MaxEntries:      100,
		CleanupInterval: time.Minute,
		MaxKeySize:      1024,
	})
	defer cache.Close()

	err := cache.Put("test-key", []byte("key-material"), time.Minute)
	require.NoError(t, err)

	material, found := cache.Get("test-key")
	assert.True(t, found)
	assert.Equal(t, []byte("key-material"), material)
}

func TestCoveragePush_KeyCache_Delete(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		DefaultTTL:      time.Minute,
		MaxTTL:          time.Hour,
		MaxEntries:      100,
		CleanupInterval: time.Minute,
		MaxKeySize:      1024,
	})
	defer cache.Close()

	err := cache.Put("test-key", []byte("key-material"), time.Minute)
	require.NoError(t, err)

	cache.Delete("test-key")

	_, found := cache.Get("test-key")
	assert.False(t, found)
}

func TestCoveragePush_KeyCache_Clear(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		DefaultTTL:      time.Minute,
		MaxTTL:          time.Hour,
		MaxEntries:      100,
		CleanupInterval: time.Minute,
		MaxKeySize:      1024,
	})
	defer cache.Close()

	err := cache.Put("key1", []byte("material1"), time.Minute)
	require.NoError(t, err)

	err = cache.Put("key2", []byte("material2"), time.Minute)
	require.NoError(t, err)

	cache.Clear()

	assert.Equal(t, 0, cache.Size())
}

func TestCoveragePush_KeyCache_Size(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		DefaultTTL:      time.Minute,
		MaxTTL:          time.Hour,
		MaxEntries:      100,
		CleanupInterval: time.Minute,
		MaxKeySize:      1024,
	})
	defer cache.Close()

	assert.Equal(t, 0, cache.Size())

	err := cache.Put("key1", []byte("material"), time.Minute)
	require.NoError(t, err)

	assert.Equal(t, 1, cache.Size())
}

// =============================================================================
// KeyStore Additional Tests
// =============================================================================

func TestCoveragePush_KeyStore_List(t *testing.T) {
	store := NewKeyStore()

	// Add some keys
	for i := 0; i < 5; i++ {
		err := store.Put(&KeyMetadata{
			KeyID:     "key-" + string(rune('a'+i)),
			Algorithm: AlgAES256GCM,
			State:     KeyStateEnabled,
		})
		require.NoError(t, err)
	}

	// List all
	keys := store.List("", 0)
	assert.Equal(t, 5, len(keys))

	// List with limit
	keys = store.List("", 3)
	assert.Equal(t, 3, len(keys))

	// List with prefix
	keys = store.List("key-a", 0)
	assert.Equal(t, 1, len(keys))
}

func TestCoveragePush_KeyStore_GetByState(t *testing.T) {
	store := NewKeyStore()

	err := store.Put(&KeyMetadata{
		KeyID:     "enabled-key",
		Algorithm: AlgAES256GCM,
		State:     KeyStateEnabled,
	})
	require.NoError(t, err)

	err = store.Put(&KeyMetadata{
		KeyID:     "disabled-key",
		Algorithm: AlgAES256GCM,
		State:     KeyStateDisabled,
	})
	require.NoError(t, err)

	enabled := store.GetByState(KeyStateEnabled)
	assert.Equal(t, 1, len(enabled))
	assert.Equal(t, "enabled-key", enabled[0].KeyID)
}

func TestCoveragePush_KeyStore_IncrementVersion(t *testing.T) {
	store := NewKeyStore()

	err := store.Put(&KeyMetadata{
		KeyID:     "test-key",
		Algorithm: AlgAES256GCM,
		State:     KeyStateEnabled,
		Version:   1,
	})
	require.NoError(t, err)

	newVersion, err := store.IncrementVersion("test-key")
	require.NoError(t, err)
	assert.Equal(t, 2, newVersion)
}

func TestCoveragePush_KeyStore_IncrementVersion_NotFound(t *testing.T) {
	store := NewKeyStore()

	_, err := store.IncrementVersion("non-existent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

func TestCoveragePush_KeyStore_GetVersion(t *testing.T) {
	store := NewKeyStore()

	err := store.Put(&KeyMetadata{
		KeyID:     "test-key",
		Algorithm: AlgAES256GCM,
		State:     KeyStateEnabled,
		Version:   5,
	})
	require.NoError(t, err)

	version, err := store.GetVersion("test-key")
	require.NoError(t, err)
	assert.Equal(t, 5, version)
}

func TestCoveragePush_KeyStore_GetVersion_NotFound(t *testing.T) {
	store := NewKeyStore()

	_, err := store.GetVersion("non-existent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

func TestCoveragePush_KeyStore_IncrementUsage(t *testing.T) {
	store := NewKeyStore()

	err := store.Put(&KeyMetadata{
		KeyID:     "test-key",
		Algorithm: AlgAES256GCM,
		State:     KeyStateEnabled,
	})
	require.NoError(t, err)

	err = store.IncrementUsage("test-key")
	require.NoError(t, err)

	usage, _ := store.GetUsage("test-key")
	assert.Equal(t, uint64(1), usage)
}

func TestCoveragePush_KeyStore_IncrementUsage_NotFound(t *testing.T) {
	store := NewKeyStore()

	err := store.IncrementUsage("non-existent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

func TestCoveragePush_KeyStore_GetState(t *testing.T) {
	store := NewKeyStore()

	err := store.Put(&KeyMetadata{
		KeyID:     "test-key",
		Algorithm: AlgAES256GCM,
		State:     KeyStateEnabled,
	})
	require.NoError(t, err)

	state, err := store.GetState("test-key")
	require.NoError(t, err)
	assert.Equal(t, KeyStateEnabled, state)
}

func TestCoveragePush_KeyStore_GetState_NotFound(t *testing.T) {
	store := NewKeyStore()

	_, err := store.GetState("non-existent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

func TestCoveragePush_KeyStore_SetState_NotFound(t *testing.T) {
	store := NewKeyStore()

	err := store.SetState("non-existent", KeyStateEnabled)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

// =============================================================================
// Algorithms Tests
// =============================================================================

func TestCoveragePush_IsValidAlgorithm(t *testing.T) {
	// Valid algorithms
	assert.True(t, IsValidAlgorithm(AlgAES128GCM))
	assert.True(t, IsValidAlgorithm(AlgAES256GCM))
	assert.True(t, IsValidAlgorithm(AlgChaCha20Poly1305))
	assert.True(t, IsValidAlgorithm(AlgRSA2048))
	assert.True(t, IsValidAlgorithm(AlgRSA4096))
	assert.True(t, IsValidAlgorithm(AlgECDSAP256))
	assert.True(t, IsValidAlgorithm(AlgECDSAP384))
	assert.True(t, IsValidAlgorithm(AlgECDSAP521))
	assert.True(t, IsValidAlgorithm(AlgEd25519))

	// Invalid algorithm
	assert.False(t, IsValidAlgorithm("INVALID"))
}

func TestCoveragePush_GetAlgorithmInfo(t *testing.T) {
	info, err := GetAlgorithmInfo(AlgAES256GCM)
	require.NoError(t, err)
	assert.NotNil(t, info)
	assert.Equal(t, AlgAES256GCM, info.Name)
}

func TestCoveragePush_GetAlgorithmInfo_Invalid(t *testing.T) {
	_, err := GetAlgorithmInfo("INVALID")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown algorithm")
}

func TestCoveragePush_GetKeySizeBytes(t *testing.T) {
	size, err := GetKeySizeBytes(AlgAES256GCM)
	require.NoError(t, err)
	assert.Equal(t, 32, size)

	size, err = GetKeySizeBytes(AlgAES128GCM)
	require.NoError(t, err)
	assert.Equal(t, 16, size)
}

func TestCoveragePush_GetKeySizeBytes_Invalid(t *testing.T) {
	_, err := GetKeySizeBytes("INVALID")
	require.Error(t, err)
}

func TestCoveragePush_GetIVSize(t *testing.T) {
	size, err := GetIVSize(AlgAES256GCM)
	require.NoError(t, err)
	assert.Greater(t, size, 0)
}

func TestCoveragePush_GetIVSize_Invalid(t *testing.T) {
	_, err := GetIVSize("INVALID")
	require.Error(t, err)
}

func TestCoveragePush_GetTagSize(t *testing.T) {
	size, err := GetTagSize(AlgAES256GCM)
	require.NoError(t, err)
	assert.Greater(t, size, 0)
}

func TestCoveragePush_GetTagSize_Invalid(t *testing.T) {
	_, err := GetTagSize("INVALID")
	require.Error(t, err)
}

func TestCoveragePush_GetSignatureSize(t *testing.T) {
	size, err := GetSignatureSize(AlgECDSAP256)
	require.NoError(t, err)
	assert.Greater(t, size, 0)
}

func TestCoveragePush_GetSignatureSize_Invalid(t *testing.T) {
	_, err := GetSignatureSize("INVALID")
	require.Error(t, err)
}

func initXKMSForTest(t *testing.T) func() {
	t.Helper()

	// Reset any previous state
	dbxkms.Reset()

	// Create in-memory storage for key material
	memStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err, "failed to create memory storage")

	// Create software backend
	certStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err, "failed to create cert memory storage")

	swBackend, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{
		KeyStorage: memStorage,
	})
	require.NoError(t, err, "failed to create software backend")

	// Create keystore wrapping the software backend
	ks, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     swBackend,
		CertStorage: certStorage,
	})
	require.NoError(t, err, "failed to create keystore")

	// Initialize QuicBaaS xkms integration
	err = dbxkms.Initialize(&dbxkms.Config{
		DefaultBackend: "software",
		Backends: map[string]xkmssdk.ServiceBackend{
			"software": ks,
		},
		Logger: &mockLogger{},
	})
	require.NoError(t, err, "failed to initialize QuicBaaS xkms")

	return func() {
		dbxkms.Reset()
	}
}

// =============================================================================
// NewXKMSBackend with initialized xkms
// =============================================================================

func TestNewXKMSBackend_Initialized_Success(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)
	require.NotNil(t, backend)
	assert.Equal(t, config.BackendTypeSoftware, backend.Type())
}

func TestNewXKMSBackend_Initialized_DefaultBackendType(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: "", // should default to software
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)
	require.NotNil(t, backend)
	assert.Equal(t, config.BackendTypeSoftware, backend.Type())
}

// =============================================================================
// SupportedAlgorithms with initialized xkms
// =============================================================================

func TestXKMSBackend_SupportedAlgorithms_InitializedXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	algs := backend.SupportedAlgorithms(ctx)
	assert.NotEmpty(t, algs)
}

// =============================================================================
// CreateKey - with real xkms
// =============================================================================

func TestXKMSBackend_CreateKey_SymmetricAES256_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	attrs := &xkmssdk.KeyAttributes{
		CN:                 "test-sym-key-real",
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}

	meta, err := backend.CreateKey(ctx, attrs)
	require.NoError(t, err)
	require.NotNil(t, meta)
	assert.Equal(t, "test-sym-key-real", meta.KeyID)
	assert.Equal(t, AlgAES256GCM, meta.Algorithm)
	assert.Equal(t, KeyStateEnabled, meta.State)
	assert.Equal(t, 1, meta.Version)
}

func TestXKMSBackend_CreateKey_NilAttrs_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	meta, err := backend.CreateKey(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, meta)
	assert.Contains(t, err.Error(), "key attributes cannot be nil")
}

func TestXKMSBackend_CreateKey_EmptyCN_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	attrs := &xkmssdk.KeyAttributes{
		CN:                 "",
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}
	meta, err := backend.CreateKey(ctx, attrs)
	assert.Error(t, err)
	assert.Nil(t, meta)
	assert.Contains(t, err.Error(), "key CN (common name) cannot be empty")
}

func TestXKMSBackend_CreateKey_DefaultStoreType_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	attrs := &xkmssdk.KeyAttributes{
		CN:                 "test-default-store-real",
		StoreType:          "", // should default to backend type
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}

	meta, err := backend.CreateKey(ctx, attrs)
	require.NoError(t, err)
	require.NotNil(t, meta)
	assert.Equal(t, "test-default-store-real", meta.KeyID)
}

// =============================================================================
// ImportKey - with real xkms
// =============================================================================

func TestXKMSBackend_ImportKey_Symmetric_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	material := make([]byte, 32)
	for i := range material {
		material[i] = byte(i)
	}

	meta, err := backend.ImportKey(ctx, "imported-key-real", material, AlgAES256GCM, map[string]interface{}{
		"description": "imported test key",
		"exportable":  true,
	})
	require.NoError(t, err)
	require.NotNil(t, meta)
	assert.Equal(t, "imported-key-real", meta.KeyID)
	assert.Equal(t, AlgAES256GCM, meta.Algorithm)
	assert.Equal(t, "imported test key", meta.Description)
	assert.True(t, meta.Exportable)
}

func TestXKMSBackend_ImportKey_EmptyKeyID_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	meta, err := backend.ImportKey(ctx, "", []byte("material"), AlgAES256GCM, nil)
	assert.Error(t, err)
	assert.Nil(t, meta)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestXKMSBackend_ImportKey_EmptyMaterial_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	meta, err := backend.ImportKey(ctx, "some-key-real", nil, AlgAES256GCM, nil)
	assert.Error(t, err)
	assert.Nil(t, meta)
	assert.Contains(t, err.Error(), "key material cannot be empty")
}

func TestXKMSBackend_ImportKey_AsymmetricNotSupported_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	meta, err := backend.ImportKey(ctx, "rsa-import-real", []byte("material"), AlgRSA2048, nil)
	assert.Error(t, err)
	assert.Nil(t, meta)
	assert.Contains(t, err.Error(), "asymmetric key import not yet supported")
}

func TestXKMSBackend_ImportKey_NilOptions_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	material := make([]byte, 32)
	for i := range material {
		material[i] = byte(i + 10)
	}

	meta, err := backend.ImportKey(ctx, "imported-no-opts-real", material, AlgAES256GCM, nil)
	require.NoError(t, err)
	require.NotNil(t, meta)
	assert.Equal(t, "", meta.Description)
	assert.False(t, meta.Exportable)
}

// =============================================================================
// GetKeyMetadata - with real xkms
// =============================================================================

func TestXKMSBackend_GetKeyMetadata_CacheHit_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	attrs := &xkmssdk.KeyAttributes{
		CN:                 "cached-key-real",
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}
	_, err = backend.CreateKey(ctx, attrs)
	require.NoError(t, err)

	meta, err := backend.GetKeyMetadata(ctx, "cached-key-real")
	require.NoError(t, err)
	require.NotNil(t, meta)
	assert.Equal(t, "cached-key-real", meta.KeyID)
}

func TestXKMSBackend_GetKeyMetadata_NotFound_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	meta, err := backend.GetKeyMetadata(ctx, "nonexistent-key-real")
	assert.Error(t, err)
	assert.Nil(t, meta)
	assert.ErrorIs(t, err, ErrKeyNotFound)
}

// =============================================================================
// ListKeys - with real xkms
// =============================================================================

func TestXKMSBackend_ListKeys_WithPrefix_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	for _, name := range []string{"pfx-a", "pfx-b", "xyz-c"} {
		attrs := &xkmssdk.KeyAttributes{
			CN:                 name,
			StoreType:          xkmssdk.StoreType("software"),
			KeyType:            xkmssdk.KeyTypeEncryption,
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		}
		_, err = backend.CreateKey(ctx, attrs)
		require.NoError(t, err)
	}

	keys, _, err := backend.ListKeys(ctx, "pfx-", 0)
	require.NoError(t, err)

	prefixCount := 0
	for _, k := range keys {
		if len(k.KeyID) >= 4 && k.KeyID[:4] == "pfx-" {
			prefixCount++
		}
	}
	assert.GreaterOrEqual(t, prefixCount, 2)
}

func TestXKMSBackend_ListKeys_WithLimit_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		name := "lmt-key-" + string(rune('a'+i))
		attrs := &xkmssdk.KeyAttributes{
			CN:                 name,
			StoreType:          xkmssdk.StoreType("software"),
			KeyType:            xkmssdk.KeyTypeEncryption,
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		}
		_, err = backend.CreateKey(ctx, attrs)
		require.NoError(t, err)
	}

	keys, _, err := backend.ListKeys(ctx, "", 2)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(keys), 2)
}

// =============================================================================
// DisableKey / EnableKey - with real xkms
// =============================================================================

func TestXKMSBackend_DisableKey_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	attrs := &xkmssdk.KeyAttributes{
		CN:                 "disable-real",
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}
	_, err = backend.CreateKey(ctx, attrs)
	require.NoError(t, err)

	err = backend.DisableKey(ctx, "disable-real")
	require.NoError(t, err)

	meta, err := backend.GetKeyMetadata(ctx, "disable-real")
	require.NoError(t, err)
	assert.Equal(t, KeyStateDisabled, meta.State)
}

func TestXKMSBackend_DisableKey_NotFound_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	err = backend.DisableKey(context.Background(), "nonexistent-real")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyNotFound)
}

func TestXKMSBackend_EnableKey_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	attrs := &xkmssdk.KeyAttributes{
		CN:                 "enable-real",
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}
	_, err = backend.CreateKey(ctx, attrs)
	require.NoError(t, err)

	err = backend.DisableKey(ctx, "enable-real")
	require.NoError(t, err)

	err = backend.EnableKey(ctx, "enable-real")
	require.NoError(t, err)

	meta, err := backend.GetKeyMetadata(ctx, "enable-real")
	require.NoError(t, err)
	assert.Equal(t, KeyStateEnabled, meta.State)
}

func TestXKMSBackend_EnableKey_NotFound_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	err = backend.EnableKey(context.Background(), "nonexistent-en-real")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyNotFound)
}

// =============================================================================
// DeleteKey - with real xkms
// =============================================================================

func TestXKMSBackend_DeleteKey_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	attrs := &xkmssdk.KeyAttributes{
		CN:                 "delete-real",
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}
	_, err = backend.CreateKey(ctx, attrs)
	require.NoError(t, err)

	err = backend.DeleteKey(ctx, "delete-real")
	require.NoError(t, err)

	backend.mu.RLock()
	_, exists := backend.metadata["delete-real"]
	backend.mu.RUnlock()
	assert.False(t, exists)
}

func TestXKMSBackend_DeleteKey_NotFound_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	err = backend.DeleteKey(context.Background(), "nonexistent-del-real")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyNotFound)
}

// =============================================================================
// RotateKey - with real xkms
// =============================================================================

func TestXKMSBackend_RotateKey_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	attrs := &xkmssdk.KeyAttributes{
		CN:                 "rotate-real",
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}
	_, err = backend.CreateKey(ctx, attrs)
	require.NoError(t, err)

	meta, err := backend.RotateKey(ctx, "rotate-real")
	require.NoError(t, err)
	require.NotNil(t, meta)
	assert.Equal(t, 2, meta.Version)
}

func TestXKMSBackend_RotateKey_NotFound_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	meta, err := backend.RotateKey(context.Background(), "nonexistent-rot-real")
	assert.Error(t, err)
	assert.Nil(t, meta)
	assert.ErrorIs(t, err, ErrKeyNotFound)
}

// =============================================================================
// Encrypt / Decrypt - with real xkms
// =============================================================================

func TestXKMSBackend_EncryptDecrypt_RoundTrip_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	attrs := &xkmssdk.KeyAttributes{
		CN:                 "enc-real",
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}
	_, err = backend.CreateKey(ctx, attrs)
	require.NoError(t, err)

	plaintext := []byte("hello world - real xkms test")
	ciphertext, err := backend.Encrypt(ctx, "enc-real", plaintext, nil)
	require.NoError(t, err)
	require.NotEmpty(t, ciphertext)
	assert.NotEqual(t, plaintext, ciphertext)

	decrypted, err := backend.Decrypt(ctx, "enc-real", ciphertext, nil)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestXKMSBackend_EncryptDecrypt_WithAAD_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	attrs := &xkmssdk.KeyAttributes{
		CN:                 "aad-real",
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}
	_, err = backend.CreateKey(ctx, attrs)
	require.NoError(t, err)

	plaintext := []byte("authenticated encryption real test")
	aad := []byte("additional-authenticated-data-real")

	ciphertext, err := backend.Encrypt(ctx, "aad-real", plaintext, aad)
	require.NoError(t, err)

	decrypted, err := backend.Decrypt(ctx, "aad-real", ciphertext, aad)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestXKMSBackend_Decrypt_InvalidCiphertext_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	attrs := &xkmssdk.KeyAttributes{
		CN:                 "decrypt-err-real",
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}
	_, err = backend.CreateKey(ctx, attrs)
	require.NoError(t, err)

	_, err = backend.Decrypt(ctx, "decrypt-err-real", []byte("not-valid-json"), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to deserialize ciphertext")
}

func TestXKMSBackend_Encrypt_KeyNotFound_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	_, err = backend.Encrypt(context.Background(), "nonexistent-enc-real", []byte("data"), nil)
	assert.Error(t, err)
}

// =============================================================================
// GenerateDataKey - with real xkms
// =============================================================================

func TestXKMSBackend_GenerateDataKey_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	attrs := &xkmssdk.KeyAttributes{
		CN:                 "kek-real",
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}
	_, err = backend.CreateKey(ctx, attrs)
	require.NoError(t, err)

	plainDEK, wrappedDEK, err := backend.GenerateDataKey(ctx, "kek-real", 32)
	require.NoError(t, err)
	assert.Len(t, plainDEK, 32)
	assert.NotEmpty(t, wrappedDEK)
}

// =============================================================================
// EncryptDecrypt after rotation - with real xkms
// =============================================================================

func TestXKMSBackend_EncryptDecrypt_AfterRotation_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	attrs := &xkmssdk.KeyAttributes{
		CN:                 "rot-enc-real",
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}
	_, err = backend.CreateKey(ctx, attrs)
	require.NoError(t, err)

	plaintext1 := []byte("version 1 data real")
	ciphertext1, err := backend.Encrypt(ctx, "rot-enc-real", plaintext1, nil)
	require.NoError(t, err)

	_, err = backend.RotateKey(ctx, "rot-enc-real")
	require.NoError(t, err)

	decrypted1, err := backend.Decrypt(ctx, "rot-enc-real", ciphertext1, nil)
	require.NoError(t, err)
	assert.Equal(t, plaintext1, decrypted1)
}

// =============================================================================
// IsAvailable / Close - with real xkms
// =============================================================================

func TestXKMSBackend_IsAvailable_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)
	assert.True(t, backend.IsAvailable(context.Background()))
}

func TestXKMSBackend_Close_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	err = backend.Close()
	assert.NoError(t, err)
}

// =============================================================================
// BackendFactory.InitializeFromXKMS
// =============================================================================

func TestBackendFactory_InitializeFromXKMS_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	factory, err := NewBackendFactory(&BackendFactoryConfig{
		Logger:              &mockLogger{},
		HealthCheckInterval: 60,
	})
	require.NoError(t, err)

	ctx := context.Background()
	err = factory.InitializeFromXKMS(ctx, nil)
	require.NoError(t, err)

	backends := factory.Registry().List()
	assert.NotEmpty(t, backends)
}

func TestBackendFactory_InitializeFromXKMS_WithFilter_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	factory, err := NewBackendFactory(&BackendFactoryConfig{
		Logger:              &mockLogger{},
		HealthCheckInterval: 60,
	})
	require.NoError(t, err)

	ctx := context.Background()
	err = factory.InitializeFromXKMS(ctx, []config.BackendType{config.BackendTypeSoftware})
	require.NoError(t, err)

	backends := factory.Registry().List()
	assert.NotEmpty(t, backends)
}

func TestBackendFactory_InitializeFromXKMS_FilterNonExistent_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	factory, err := NewBackendFactory(&BackendFactoryConfig{
		Logger:              &mockLogger{},
		HealthCheckInterval: 60,
	})
	require.NoError(t, err)

	ctx := context.Background()
	err = factory.InitializeFromXKMS(ctx, []config.BackendType{"nonexistent-backend"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no backends were successfully registered")
}

func TestBackendFactory_InitializeFromXKMS_NotInitialized_RealXKMS(t *testing.T) {
	dbxkms.Reset()

	factory, err := NewBackendFactory(&BackendFactoryConfig{
		Logger:              &mockLogger{},
		HealthCheckInterval: 60,
	})
	require.NoError(t, err)

	ctx := context.Background()
	err = factory.InitializeFromXKMS(ctx, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

// =============================================================================
// Service coverage gaps
// =============================================================================

func TestKMSService_UpdateStoredMetadata_InvalidType_Coverage(t *testing.T) {
	backend := NewMockBackend()
	svc, err := NewKMSService(&KMSServiceOptions{
		Registry: registryWithBackend(backend),
	})
	require.NoError(t, err)

	err = svc.UpdateStoredMetadata("not-a-key-metadata")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "metadata must be *KeyMetadata")
}

func TestKMSService_UpdateStoredMetadata_EmptyKeyID_Coverage(t *testing.T) {
	backend := NewMockBackend()
	svc, err := NewKMSService(&KMSServiceOptions{
		Registry: registryWithBackend(backend),
	})
	require.NoError(t, err)

	err = svc.UpdateStoredMetadata(&KeyMetadata{KeyID: ""})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestKMSService_UpdateStoredMetadata_Success_Coverage(t *testing.T) {
	backend := NewMockBackend()
	svc, err := NewKMSService(&KMSServiceOptions{
		Registry: registryWithBackend(backend),
		Logger:   &mockLogger{},
	})
	require.NoError(t, err)

	err = svc.UpdateStoredMetadata(&KeyMetadata{
		KeyID:       "test-update-cov",
		Name:        "Updated",
		Description: "Desc",
		Algorithm:   AlgAES256GCM,
		State:       KeyStateEnabled,
		Version:     1,
		Created:     time.Now(),
		Updated:     time.Now(),
	})
	assert.NoError(t, err)
}

func TestKMSService_Encrypt_DisabledKeyRejected_Coverage(t *testing.T) {
	backend := NewMockBackend()
	svc, err := NewKMSService(&KMSServiceOptions{
		Registry: registryWithBackend(backend),
	})
	require.NoError(t, err)

	ctx := context.Background()
	attrs := &xkmssdk.KeyAttributes{
		CN:                 "enc-dis-cov",
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
	}
	_, err = svc.GenerateKey(ctx, attrs)
	require.NoError(t, err)

	err = svc.DisableKey(ctx, "enc-dis-cov")
	require.NoError(t, err)

	_, err = svc.Encrypt(ctx, "enc-dis-cov", []byte("data"), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestKMSService_GenerateKey_CanceledContext_Coverage(t *testing.T) {
	backend := NewMockBackend()
	svc, err := NewKMSService(&KMSServiceOptions{
		Registry: registryWithBackend(backend),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	attrs := &xkmssdk.KeyAttributes{
		CN:                 "ctx-gen-cov",
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
	}
	_, err = svc.GenerateKey(ctx, attrs)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestKMSService_ImportKey_CanceledContext_Coverage(t *testing.T) {
	backend := NewMockBackend()
	svc, err := NewKMSService(&KMSServiceOptions{
		Registry: registryWithBackend(backend),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = svc.ImportKey(ctx, "import-cov", []byte("material"), AlgAES256GCM)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestKMSService_GetKey_CanceledContext_Coverage(t *testing.T) {
	backend := NewMockBackend()
	svc, err := NewKMSService(&KMSServiceOptions{
		Registry: registryWithBackend(backend),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = svc.GetKey(ctx, "get-cov")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestKMSService_GetKey_BackendInternalError_Coverage(t *testing.T) {
	backend := NewMockBackend()
	backend.SetError("GetKeyMetadata", assert.AnError)

	svc, err := NewKMSService(&KMSServiceOptions{
		Registry: registryWithBackend(backend),
	})
	require.NoError(t, err)

	_, err = svc.GetKey(context.Background(), "err-cov")
	assert.Error(t, err)
}

func TestKMSService_RotateKey_CanceledContext_Coverage(t *testing.T) {
	backend := NewMockBackend()
	svc, err := NewKMSService(&KMSServiceOptions{
		Registry: registryWithBackend(backend),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = svc.RotateKey(ctx, "rot-cov")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestXKMSBackend_ExportPublicKey_NotAvailable(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	pubKey, err := backend.ExportPublicKey(ctx, "test-key")
	assert.Error(t, err)
	assert.Nil(t, pubKey)
	assert.ErrorIs(t, err, ErrBackendNotReady)
}

// =============================================================================
// Additional algorithmToKeyIDFormat Tests
// =============================================================================

func TestXKMSBackend_algorithmToKeyIDFormat_AllVariants(t *testing.T) {
	backend := &XKMSBackend{}

	testCases := []struct {
		algorithm string
		expected  string
	}{
		// AES variants
		{"AES-128-GCM", "aes128-gcm"},
		{"AES128-GCM", "aes128-gcm"},
		{"AES128GCM", "aes128-gcm"},
		{"AES-192-GCM", "aes192-gcm"},
		{"AES192-GCM", "aes192-gcm"},
		{"AES192GCM", "aes192-gcm"},
		{"AES-256-GCM", "aes256-gcm"},
		{"AES256-GCM", "aes256-gcm"},
		{"AES256GCM", "aes256-gcm"},
		// ChaCha20 variants
		{"CHACHA20-POLY1305", "chacha20-poly1305"},
		{"CHACHA20POLY1305", "chacha20-poly1305"},
		// XChaCha20 variants
		{"XCHACHA20-POLY1305", "xchacha20-poly1305"},
		{"XCHACHA20POLY1305", "xchacha20-poly1305"},
		// Unknown defaults to AES-256-GCM
		{"UNKNOWN-ALGO", "aes256-gcm"},
		{"", "aes256-gcm"},
	}

	for _, tc := range testCases {
		t.Run(tc.algorithm, func(t *testing.T) {
			result := backend.algorithmToKeyIDFormat(tc.algorithm)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// Additional defaultAlgorithms Tests
// =============================================================================

func TestXKMSBackend_defaultAlgorithms_Software(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	algs := backend.defaultAlgorithms()
	assert.NotEmpty(t, algs)
	// Software backend should support all algorithms including ChaCha20 variants
	assert.Contains(t, algs, AlgAES128GCM)
	assert.Contains(t, algs, AlgAES192GCM)
	assert.Contains(t, algs, AlgAES256GCM)
	assert.Contains(t, algs, AlgChaCha20Poly1305)
	assert.Contains(t, algs, AlgXChaCha20Poly1305)
	assert.Contains(t, algs, AlgRSA2048)
	assert.Contains(t, algs, AlgRSA3072)
	assert.Contains(t, algs, AlgRSA4096)
	assert.Contains(t, algs, AlgECDSAP256)
	assert.Contains(t, algs, AlgECDSAP384)
	assert.Contains(t, algs, AlgECDSAP521)
	assert.Contains(t, algs, AlgEd25519)
	assert.Contains(t, algs, AlgEd448)
	assert.Contains(t, algs, AlgX25519)
	assert.Contains(t, algs, AlgX448)
	assert.Contains(t, algs, AlgHMACSHA256)
	assert.Contains(t, algs, AlgHMACSHA384)
	assert.Contains(t, algs, AlgHMACSHA512)
}

func TestXKMSBackend_defaultAlgorithms_TPM2(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeTPM2,
	}

	algs := backend.defaultAlgorithms()
	assert.NotEmpty(t, algs)
	// TPM2 backend supports subset of algorithms
	assert.Contains(t, algs, AlgAES128GCM)
	assert.Contains(t, algs, AlgAES256GCM)
	assert.Contains(t, algs, AlgRSA2048)
	assert.Contains(t, algs, AlgRSA4096)
	assert.Contains(t, algs, AlgECDSAP256)
	assert.Contains(t, algs, AlgEd25519)
	// ChaCha20 variants NOT included for hardware backends
	assert.NotContains(t, algs, AlgChaCha20Poly1305)
	assert.NotContains(t, algs, AlgXChaCha20Poly1305)
	// Ed448, X25519, X448 NOT included for hardware backends
	assert.NotContains(t, algs, AlgEd448)
	assert.NotContains(t, algs, AlgX25519)
	assert.NotContains(t, algs, AlgX448)
}

func TestXKMSBackend_defaultAlgorithms_PKCS11(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypePKCS11,
	}

	algs := backend.defaultAlgorithms()
	assert.NotEmpty(t, algs)
	// PKCS11 backend (non-software) also supports subset
	assert.Contains(t, algs, AlgAES256GCM)
	assert.Contains(t, algs, AlgRSA4096)
	// ChaCha20 NOT included for hardware backends
	assert.NotContains(t, algs, AlgChaCha20Poly1305)
}

// =============================================================================
// Additional SupportedAlgorithms Tests for different backend types
// =============================================================================

func TestXKMSBackend_SupportedAlgorithms_TPM2NotInitialized(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeTPM2,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	algs := backend.SupportedAlgorithms(ctx)
	assert.NotEmpty(t, algs)
	// Should return TPM2 default algorithms
	assert.Contains(t, algs, AlgAES256GCM)
	assert.NotContains(t, algs, AlgChaCha20Poly1305)
}

func TestXKMSBackend_SupportedAlgorithms_PKCS11NotInitialized(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypePKCS11,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	algs := backend.SupportedAlgorithms(ctx)
	assert.NotEmpty(t, algs)
	// Should return PKCS11 default algorithms
	assert.Contains(t, algs, AlgAES256GCM)
	assert.NotContains(t, algs, AlgChaCha20Poly1305)
}

// =============================================================================
// Additional serializeEncryptedData Edge Cases
// =============================================================================

func TestXKMSBackend_serializeEncryptedData_WithMetadata(t *testing.T) {
	backend := &XKMSBackend{}

	original := &xkmssdk.EncryptedData{
		Ciphertext: []byte("encrypted-data"),
		Nonce:      []byte("nonce-value-123"),
		Tag:        []byte("auth-tag-abc"),
		Metadata:   map[string][]byte{"kv": []byte("1"), "extra": []byte("value")},
	}

	serialized, err := backend.serializeEncryptedData(original)
	require.NoError(t, err)
	require.NotEmpty(t, serialized)

	deserialized, err := backend.deserializeEncryptedData(serialized)
	require.NoError(t, err)
	require.NotNil(t, deserialized)

	assert.Equal(t, original.Ciphertext, deserialized.Ciphertext)
	assert.Equal(t, original.Nonce, deserialized.Nonce)
	assert.Equal(t, original.Tag, deserialized.Tag)
	assert.Equal(t, original.Metadata["kv"], deserialized.Metadata["kv"])
	assert.Equal(t, original.Metadata["extra"], deserialized.Metadata["extra"])
}

func TestXKMSBackend_serializeEncryptedData_LargeData(t *testing.T) {
	backend := &XKMSBackend{}

	// Create large ciphertext
	largeCiphertext := make([]byte, 1<<16) // 64KB
	for i := range largeCiphertext {
		largeCiphertext[i] = byte(i % 256)
	}

	original := &xkmssdk.EncryptedData{
		Ciphertext: largeCiphertext,
		Nonce:      []byte("nonce-value-12345678901234567890"),
		Tag:        []byte("auth-tag-16-bytes"),
	}

	serialized, err := backend.serializeEncryptedData(original)
	require.NoError(t, err)
	require.NotEmpty(t, serialized)

	deserialized, err := backend.deserializeEncryptedData(serialized)
	require.NoError(t, err)
	require.NotNil(t, deserialized)

	assert.Equal(t, len(original.Ciphertext), len(deserialized.Ciphertext))
	assert.Equal(t, original.Ciphertext, deserialized.Ciphertext)
}

// =============================================================================
// Additional isSymmetricAlgorithm Edge Cases
// =============================================================================

func TestXKMSBackend_isSymmetricAlgorithm_MoreVariants(t *testing.T) {
	backend := &XKMSBackend{}

	testCases := []struct {
		algorithm string
		expected  bool
	}{
		// More AES variants
		{"AES", true},     // Just AES prefix
		{"aes", true},     // Lowercase
		{"AES-CBC", true}, // AES-CBC (not supported but should recognize as symmetric)
		{"AES-CTR", true}, // AES-CTR
		{"AES-GCM", true}, // AES-GCM without key size
		{"AES128", true},  // Without mode
		{"AES-192", true}, // AES-192 without mode
		{"AES_256", true}, // Underscore variant
		{"aes-256-gcm", true},
		// More ChaCha variants
		{"chacha", true},
		{"CHACHA", true},
		{"ChaCha20", true},
		// XChaCha variants
		{"xchacha", true},
		{"XCHACHA", true},
		{"XChaCha20", true},
		// Non-symmetric algorithms
		{"RSA", false},
		{"ECDSA", false},
		{"ED25519", false},
		{"X25519", false},
		{"HMAC", false},
		{"DSA", false},
		{"", false},
	}

	for _, tc := range testCases {
		t.Run(tc.algorithm, func(t *testing.T) {
			result := backend.isSymmetricAlgorithm(tc.algorithm)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// Additional algorithmToHash Edge Cases
// =============================================================================

func TestXKMSBackend_algorithmToHash_MoreVariants(t *testing.T) {
	backend := &XKMSBackend{}

	testCases := []struct {
		algorithm    string
		expectedName string
	}{
		// Mixed case
		{"sha-256", "SHA-256"},
		{"Sha-256", "SHA-256"},
		{"SHA-256-RSA", "SHA-256"},
		{"RSA-SHA256", "SHA-256"},
		// With spaces or extra chars
		{"SHA256WithRSA", "SHA-256"},
		{"RSASSA-PKCS1-v1_5-SHA384", "SHA-384"},
		// SHA-512 variants
		{"SHA-512/256", "SHA-512"},
		{"SHA512RSA", "SHA-512"},
		// Default for non-matching
		{"MD5", "SHA-256"},       // MD5 is not supported, defaults to SHA-256
		{"BLAKE2", "SHA-256"},    // BLAKE2 not supported, defaults to SHA-256
		{"RIPEMD160", "SHA-256"}, // Not supported
	}

	for _, tc := range testCases {
		t.Run(tc.algorithm, func(t *testing.T) {
			result := backend.algorithmToHash(tc.algorithm)
			assert.Equal(t, tc.expectedName, result.String())
		})
	}
}

// =============================================================================
// Additional normalizeAlgorithmForXKMS Edge Cases
// =============================================================================

func TestXKMSBackend_normalizeAlgorithmForXKMS_EdgeCases(t *testing.T) {
	backend := &XKMSBackend{}

	testCases := []struct {
		input    string
		expected string
	}{
		// Single hyphen cases
		{"-", "-"},
		{"a-", "a-"},
		{"-a", "-a"},
		// Multiple hyphens
		{"A-B-C", "a-b-c"},
		{"AES-256-GCM-TAG", "aes256-gcm-tag"},
		// Numbers only
		{"256-128", "256-128"},
		{"128", "128"},
		// Mixed with underscores
		{"AES_256_GCM", "aes_256_gcm"},
		// Long algorithm names
		{"ECDSA-P521-SHA512", "ecdsa-p521-sha512"},
		// Special chars preserved (except case change)
		{"RSA-OAEP", "rsa-oaep"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := backend.normalizeAlgorithmForXKMS(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// Additional attrsToAlgorithmString Edge Cases
// =============================================================================

func TestXKMSBackend_attrsToAlgorithmString_RSANoAttributes(t *testing.T) {
	backend := &XKMSBackend{}

	// RSA without RSAAttributes should default to RSA-4096
	attrs := &xkmssdk.KeyAttributes{
		KeyAlgorithm: 1, // x509.RSA
		// RSAAttributes is nil
	}

	result := backend.attrsToAlgorithmString(attrs)
	assert.Equal(t, AlgRSA4096, result)
}

func TestXKMSBackend_attrsToAlgorithmString_ECDSANoCurve(t *testing.T) {
	backend := &XKMSBackend{}

	// ECDSA without ECCAttributes.Curve should default to P-256
	attrs := &xkmssdk.KeyAttributes{
		KeyAlgorithm:  3, // x509.ECDSA
		ECCAttributes: &xkmssdk.ECCAttributes{
			// Curve is nil
		},
	}

	result := backend.attrsToAlgorithmString(attrs)
	assert.Equal(t, AlgECDSAP256, result)
}

func TestXKMSBackend_attrsToAlgorithmString_UnknownRSASize(t *testing.T) {
	backend := &XKMSBackend{}

	// RSA with unknown key size should default to RSA-4096
	attrs := &xkmssdk.KeyAttributes{
		KeyAlgorithm:  1,                                     // x509.RSA
		RSAAttributes: &xkmssdk.RSAAttributes{KeySize: 1024}, // Uncommon size
	}

	result := backend.attrsToAlgorithmString(attrs)
	// Key size 1024 doesn't match any case, so falls through to default
	assert.Equal(t, AlgRSA4096, result)
}

// =============================================================================
// Additional signingAlgorithmToKeyIDFormat Edge Cases
// =============================================================================

func TestXKMSBackend_signingAlgorithmToKeyIDFormat_MoreVariants(t *testing.T) {
	backend := &XKMSBackend{}

	testCases := []struct {
		input    string
		expected string
	}{
		// Generic EC defaults to ECDSA-P256
		{"EC-P256", "ecdsa-p256"},
		{"EC", "ecdsa-p256"},
		// Various RSA formats
		{"RSA-PSS-2048", "rsa"},
		{"RSA-PSS", "rsa"},
		{"RSA-OAEP", "rsa"},
		{"RSA-PKCS1", "rsa"},
		// P-256 with supported formats
		{"P256", "ecdsa-p256"},
		{"P-256", "ecdsa-p256"},
		{"ECDSA-P256", "ecdsa-p256"},
		// P-384 with supported formats
		{"P384", "ecdsa-p384"},
		{"P-384", "ecdsa-p384"},
		{"ECDSA-P384", "ecdsa-p384"},
		// P-521 with supported formats
		{"P521", "ecdsa-p521"},
		{"P-521", "ecdsa-p521"},
		{"ECDSA-P521", "ecdsa-p521"},
		// Ed25519
		{"ED25519", "ed25519"},
		{"ed25519", "ed25519"},
		{"Ed25519", "ed25519"},
		// Empty and unknown default to RSA
		{"", "rsa"},
		{"DSA", "rsa"},
		{"UNKNOWN", "rsa"},
		// Unsupported curve names default to RSA (not recognized)
		{"secp256r1", "ecdsa-p256"}, // Contains P256 when uppercased
		{"prime256v1", "rsa"},       // Does NOT contain P256 - truly unknown
		{"nistp256", "ecdsa-p256"},  // Contains P256 when uppercased
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := backend.signingAlgorithmToKeyIDFormat(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// Additional buildFullKeyID Tests
// =============================================================================

func TestXKMSBackend_buildFullKeyID_AllAlgorithms(t *testing.T) {
	testCases := []struct {
		backendType config.BackendType
		keyID       string
		algorithm   string
		expected    string
	}{
		{config.BackendTypeSoftware, "my-key", "AES-128-GCM", "software:encryption:aes128-gcm:my-key"},
		{config.BackendTypeSoftware, "my-key", "AES-192-GCM", "software:encryption:aes192-gcm:my-key"},
		{config.BackendTypeSoftware, "my-key", "AES-256-GCM", "software:encryption:aes256-gcm:my-key"},
		{config.BackendTypeSoftware, "my-key", "CHACHA20-POLY1305", "software:encryption:chacha20-poly1305:my-key"},
		{config.BackendTypeSoftware, "my-key", "XCHACHA20-POLY1305", "software:encryption:xchacha20-poly1305:my-key"},
		{config.BackendTypeTPM2, "tpm-key", "AES-256-GCM", "tpm2:encryption:aes256-gcm:tpm-key"},
		{config.BackendTypePKCS11, "hsm-key", "AES-256-GCM", "pkcs11:encryption:aes256-gcm:hsm-key"},
		// Unknown algorithm defaults to AES-256-GCM
		{config.BackendTypeSoftware, "my-key", "UNKNOWN", "software:encryption:aes256-gcm:my-key"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			backend := &XKMSBackend{
				backendType: tc.backendType,
			}
			result := backend.buildFullKeyID(tc.keyID, tc.algorithm)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// Additional buildSigningKeyID Tests
// =============================================================================

func TestXKMSBackend_buildSigningKeyID_AllVariants(t *testing.T) {
	testCases := []struct {
		backendType config.BackendType
		keyID       string
		algorithm   string
		expected    string
	}{
		{config.BackendTypeSoftware, "sign-key", "RSA-2048", "software:signing:rsa:sign-key"},
		{config.BackendTypeSoftware, "sign-key", "RSA-3072", "software:signing:rsa:sign-key"},
		{config.BackendTypeSoftware, "sign-key", "RSA-4096", "software:signing:rsa:sign-key"},
		{config.BackendTypeSoftware, "sign-key", "ECDSA-P256", "software:signing:ecdsa-p256:sign-key"},
		{config.BackendTypeSoftware, "sign-key", "ECDSA-P384", "software:signing:ecdsa-p384:sign-key"},
		{config.BackendTypeSoftware, "sign-key", "ECDSA-P521", "software:signing:ecdsa-p521:sign-key"},
		{config.BackendTypeSoftware, "sign-key", "ED25519", "software:signing:ed25519:sign-key"},
		{config.BackendTypeTPM2, "tpm-sign", "RSA-2048", "tpm2:signing:rsa:tpm-sign"},
		{config.BackendTypePKCS11, "hsm-sign", "ECDSA-P256", "pkcs11:signing:ecdsa-p256:hsm-sign"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			backend := &XKMSBackend{
				backendType: tc.backendType,
			}
			result := backend.buildSigningKeyID(tc.keyID, tc.algorithm)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// Metadata Cache Access Tests
// =============================================================================

func TestXKMSBackend_MetadataCacheOperations(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	// Add key to cache
	now := time.Now()
	meta := &KeyMetadata{
		KeyID:     "test-key",
		Name:      "Test Key",
		Algorithm: AlgAES256GCM,
		State:     KeyStateEnabled,
		Version:   1,
		Created:   now,
		Updated:   now,
	}

	backend.mu.Lock()
	backend.metadata["test-key"] = meta
	backend.mu.Unlock()

	// Verify key is in cache
	backend.mu.RLock()
	cachedMeta, exists := backend.metadata["test-key"]
	backend.mu.RUnlock()

	require.True(t, exists)
	assert.Equal(t, "test-key", cachedMeta.KeyID)
	assert.Equal(t, AlgAES256GCM, cachedMeta.Algorithm)
	assert.Equal(t, KeyStateEnabled, cachedMeta.State)

	// Delete key from cache
	backend.mu.Lock()
	delete(backend.metadata, "test-key")
	backend.mu.Unlock()

	// Verify key is removed
	backend.mu.RLock()
	_, exists = backend.metadata["test-key"]
	backend.mu.RUnlock()

	assert.False(t, exists)
}

// =============================================================================
// Concurrent Metadata Access with Writes
// =============================================================================

func TestXKMSBackend_ConcurrentMetadataReadWrite(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			now := time.Now()
			keyID := "concurrent-key"
			backend.mu.Lock()
			backend.metadata[keyID] = &KeyMetadata{
				KeyID:   keyID,
				State:   KeyStateEnabled,
				Version: i + 1,
				Created: now,
				Updated: now,
			}
			backend.mu.Unlock()
		}
		done <- true
	}()

	// Reader goroutines
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				backend.mu.RLock()
				_ = backend.metadata["concurrent-key"]
				backend.mu.RUnlock()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 6; i++ {
		<-done
	}

	// Verify final state
	backend.mu.RLock()
	meta, exists := backend.metadata["concurrent-key"]
	backend.mu.RUnlock()

	require.True(t, exists)
	assert.Equal(t, 100, meta.Version)
}

// =============================================================================
// algorithmToKeyAttributes Edge Cases for EC Curves
// =============================================================================

func TestXKMSBackend_algorithmToKeyAttributes_ECVariants(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	testCases := []struct {
		algorithm   string
		expectedBit int
	}{
		{"ECDSA-P256", 256},
		{"ECDSA-P-256", 256},
		{"P256", 256},
		{"P-256", 256},
		// Note: prime256v1, secp256r1 are not recognized - they don't contain P256
		{"ECDSA-P384", 384},
		{"ECDSA-P-384", 384},
		{"P384", 384},
		{"P-384", 384},
		{"ECDSA-P521", 521},
		{"ECDSA-P-521", 521},
		{"P521", 521},
		{"P-521", 521},
	}

	for _, tc := range testCases {
		t.Run(tc.algorithm, func(t *testing.T) {
			attrs := backend.algorithmToKeyAttributes("test", tc.algorithm)
			require.NotNil(t, attrs.ECCAttributes)
			require.NotNil(t, attrs.ECCAttributes.Curve)
			assert.Equal(t, tc.expectedBit, attrs.ECCAttributes.Curve.Params().BitSize)
		})
	}
}

// =============================================================================
// Test KeyState Constants
// =============================================================================

func TestKeyState_Constants(t *testing.T) {
	assert.Equal(t, KeyState("enabled"), KeyStateEnabled)
	assert.Equal(t, KeyState("disabled"), KeyStateDisabled)
	assert.Equal(t, KeyState("pending_deletion"), KeyStatePendingDeletion)
	assert.Equal(t, KeyState("deleted"), KeyStateDeleted)
}

// =============================================================================
// Test KeyUsage Constants
// =============================================================================

func TestKeyUsage_Constants(t *testing.T) {
	assert.Equal(t, KeyUsage("encrypt"), KeyUsageEncrypt)
	assert.Equal(t, KeyUsage("decrypt"), KeyUsageDecrypt)
	assert.Equal(t, KeyUsage("sign"), KeyUsageSign)
	assert.Equal(t, KeyUsage("verify"), KeyUsageVerify)
	assert.Equal(t, KeyUsage("wrap"), KeyUsageWrap)
	assert.Equal(t, KeyUsage("unwrap"), KeyUsageUnwrap)
	assert.Equal(t, KeyUsage("derive_key"), KeyUsageDeriveKey)
}

// =============================================================================
// Test Error Constants
// =============================================================================

func TestError_Constants(t *testing.T) {
	assert.Equal(t, "key not found", ErrKeyNotFound.Error())
	assert.Equal(t, "storage cannot be nil", ErrNilStorage.Error())
	assert.Equal(t, "backend not ready", ErrBackendNotReady.Error())
}

// =============================================================================
// Additional deserializeEncryptedData Tests
// =============================================================================

func TestXKMSBackend_deserializeEncryptedData_MalformedJSON(t *testing.T) {
	backend := &XKMSBackend{}

	testCases := []struct {
		name  string
		input []byte
	}{
		{"truncated JSON", []byte(`{"ciphertext": "abc`)},
		{"invalid base64 in field", []byte(`{"ciphertext": "!!invalid!!"`)},
		{"wrong type for field", []byte(`{"ciphertext": 123}`)},
		{"array instead of object", []byte(`[1, 2, 3]`)},
		{"nested invalid", []byte(`{"metadata": {"key": }}`)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := backend.deserializeEncryptedData(tc.input)
			// All malformed JSON should produce an error
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}
}

// =============================================================================
// Test formatKeyID Passthrough
// =============================================================================

func TestXKMSBackend_formatKeyID_Passthrough(t *testing.T) {
	backend := &XKMSBackend{}

	testCases := []string{
		"simple-key",
		"key-with-dashes",
		"key_with_underscores",
		"key.with.dots",
		"key:with:colons",
		"software:encryption:aes256-gcm:my-key",
		"",
		"a",
		"very-long-key-id-with-many-parts-and-segments-and-more",
	}

	for _, tc := range testCases {
		t.Run(tc, func(t *testing.T) {
			result := backend.formatKeyID(tc)
			// formatKeyID should return the input unchanged
			assert.Equal(t, tc, result)
		})
	}
}

// =============================================================================
// Test RotationPolicy struct
// =============================================================================

func TestRotationPolicy_Fields(t *testing.T) {
	now := time.Now()
	policy := &RotationPolicy{
		Enabled:        true,
		Interval:       24 * time.Hour,
		NextRotation:   now.Add(24 * time.Hour),
		RetainVersions: 5,
		AutoRotate:     true,
	}

	assert.True(t, policy.Enabled)
	assert.Equal(t, 24*time.Hour, policy.Interval)
	assert.Equal(t, 5, policy.RetainVersions)
	assert.True(t, policy.AutoRotate)
}

// =============================================================================
// Test KeyMetadata struct
// =============================================================================

func TestKeyMetadata_Fields(t *testing.T) {
	now := time.Now()
	lastUsed := now.Add(-1 * time.Hour)
	meta := &KeyMetadata{
		KeyID:              "test-key-id",
		Name:               "Test Key Name",
		Description:        "A test key",
		Algorithm:          AlgAES256GCM,
		State:              KeyStateEnabled,
		Version:            2,
		Exportable:         true,
		DeletionProtection: false,
		Created:            now,
		Updated:            now,
		LastUsedAt:         &lastUsed,
		UsageCount:         100,
		Usage:              []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
		RotationPolicy: &RotationPolicy{
			Enabled:  true,
			Interval: 30 * 24 * time.Hour,
		},
		Tags:  map[string]string{"env": "test", "app": "quicbaas"},
		Owner: "test-user",
	}

	assert.Equal(t, "test-key-id", meta.KeyID)
	assert.Equal(t, "Test Key Name", meta.Name)
	assert.Equal(t, "A test key", meta.Description)
	assert.Equal(t, AlgAES256GCM, meta.Algorithm)
	assert.Equal(t, KeyStateEnabled, meta.State)
	assert.Equal(t, 2, meta.Version)
	assert.True(t, meta.Exportable)
	assert.False(t, meta.DeletionProtection)
	require.NotNil(t, meta.LastUsedAt)
	assert.Equal(t, uint64(100), meta.UsageCount)
	assert.Len(t, meta.Usage, 2)
	require.NotNil(t, meta.RotationPolicy)
	assert.True(t, meta.RotationPolicy.Enabled)
	assert.Equal(t, "test", meta.Tags["env"])
	assert.Equal(t, "test-user", meta.Owner)
}
