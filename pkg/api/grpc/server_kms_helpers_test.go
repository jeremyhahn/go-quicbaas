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

package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ========================================
// convertServiceKeyAlgorithm Tests
// ========================================

func TestConvertServiceKeyAlgorithm(t *testing.T) {
	tests := []struct {
		name     string
		input    service.KeyAlgorithm
		expected pb.KeyAlgorithm
		wantErr  bool
	}{
		{
			name:     "AES-128-GCM",
			input:    service.KeyAlgAES128GCM,
			expected: pb.KeyAlgorithm_KEY_ALG_AES_128_GCM,
			wantErr:  false,
		},
		{
			name:     "AES-192-GCM",
			input:    service.KeyAlgAES192GCM,
			expected: pb.KeyAlgorithm_KEY_ALG_AES_192_GCM,
			wantErr:  false,
		},
		{
			name:     "AES-256-GCM",
			input:    service.KeyAlgAES256GCM,
			expected: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
			wantErr:  false,
		},
		{
			name:     "ChaCha20-Poly1305",
			input:    service.KeyAlgChaCha20Poly1305,
			expected: pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305,
			wantErr:  false,
		},
		{
			name:     "XChaCha20-Poly1305",
			input:    service.KeyAlgXChaCha20Poly1305,
			expected: pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305,
			wantErr:  false,
		},
		{
			name:     "RSA-2048",
			input:    service.KeyAlgRSA2048,
			expected: pb.KeyAlgorithm_KEY_ALG_RSA_2048,
			wantErr:  false,
		},
		{
			name:     "RSA-3072",
			input:    service.KeyAlgRSA3072,
			expected: pb.KeyAlgorithm_KEY_ALG_RSA_3072,
			wantErr:  false,
		},
		{
			name:     "RSA-4096",
			input:    service.KeyAlgRSA4096,
			expected: pb.KeyAlgorithm_KEY_ALG_RSA_4096,
			wantErr:  false,
		},
		{
			name:     "ECDSA-P256",
			input:    service.KeyAlgECDSAP256,
			expected: pb.KeyAlgorithm_KEY_ALG_ECDSA_P256,
			wantErr:  false,
		},
		{
			name:     "ECDSA-P384",
			input:    service.KeyAlgECDSAP384,
			expected: pb.KeyAlgorithm_KEY_ALG_ECDSA_P384,
			wantErr:  false,
		},
		{
			name:     "ECDSA-P521",
			input:    service.KeyAlgECDSAP521,
			expected: pb.KeyAlgorithm_KEY_ALG_ECDSA_P521,
			wantErr:  false,
		},
		{
			name:     "Ed25519",
			input:    service.KeyAlgEd25519,
			expected: pb.KeyAlgorithm_KEY_ALG_ED25519,
			wantErr:  false,
		},
		{
			name:     "Ed448",
			input:    service.KeyAlgEd448,
			expected: pb.KeyAlgorithm_KEY_ALG_ED448,
			wantErr:  false,
		},
		{
			name:     "X25519",
			input:    service.KeyAlgX25519,
			expected: pb.KeyAlgorithm_KEY_ALG_X25519,
			wantErr:  false,
		},
		{
			name:     "X448",
			input:    service.KeyAlgX448,
			expected: pb.KeyAlgorithm_KEY_ALG_X448,
			wantErr:  false,
		},
		{
			name:     "HMAC-SHA256",
			input:    service.KeyAlgHMACSHA256,
			expected: pb.KeyAlgorithm_KEY_ALG_HMAC_SHA256,
			wantErr:  false,
		},
		{
			name:     "HMAC-SHA384",
			input:    service.KeyAlgHMACSHA384,
			expected: pb.KeyAlgorithm_KEY_ALG_HMAC_SHA384,
			wantErr:  false,
		},
		{
			name:     "HMAC-SHA512",
			input:    service.KeyAlgHMACSHA512,
			expected: pb.KeyAlgorithm_KEY_ALG_HMAC_SHA512,
			wantErr:  false,
		},
		{
			name:     "unknown algorithm returns error",
			input:    service.KeyAlgorithm("UNKNOWN"),
			expected: pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := convertServiceKeyAlgorithm(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, tt.expected, result)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// ========================================
// convertProtoKeyAlgorithm Tests
// ========================================

func TestConvertProtoKeyAlgorithm(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.KeyAlgorithm
		expected service.KeyAlgorithm
	}{
		{
			name:     "AES-128-GCM",
			input:    pb.KeyAlgorithm_KEY_ALG_AES_128_GCM,
			expected: service.KeyAlgAES128GCM,
		},
		{
			name:     "AES-192-GCM",
			input:    pb.KeyAlgorithm_KEY_ALG_AES_192_GCM,
			expected: service.KeyAlgAES192GCM,
		},
		{
			name:     "AES-256-GCM",
			input:    pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
			expected: service.KeyAlgAES256GCM,
		},
		{
			name:     "ChaCha20-Poly1305",
			input:    pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305,
			expected: service.KeyAlgChaCha20Poly1305,
		},
		{
			name:     "XChaCha20-Poly1305",
			input:    pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305,
			expected: service.KeyAlgXChaCha20Poly1305,
		},
		{
			name:     "RSA-2048",
			input:    pb.KeyAlgorithm_KEY_ALG_RSA_2048,
			expected: service.KeyAlgRSA2048,
		},
		{
			name:     "RSA-3072",
			input:    pb.KeyAlgorithm_KEY_ALG_RSA_3072,
			expected: service.KeyAlgRSA3072,
		},
		{
			name:     "RSA-4096",
			input:    pb.KeyAlgorithm_KEY_ALG_RSA_4096,
			expected: service.KeyAlgRSA4096,
		},
		{
			name:     "ECDSA-P256",
			input:    pb.KeyAlgorithm_KEY_ALG_ECDSA_P256,
			expected: service.KeyAlgECDSAP256,
		},
		{
			name:     "ECDSA-P384",
			input:    pb.KeyAlgorithm_KEY_ALG_ECDSA_P384,
			expected: service.KeyAlgECDSAP384,
		},
		{
			name:     "ECDSA-P521",
			input:    pb.KeyAlgorithm_KEY_ALG_ECDSA_P521,
			expected: service.KeyAlgECDSAP521,
		},
		{
			name:     "Ed25519",
			input:    pb.KeyAlgorithm_KEY_ALG_ED25519,
			expected: service.KeyAlgEd25519,
		},
		{
			name:     "Ed448",
			input:    pb.KeyAlgorithm_KEY_ALG_ED448,
			expected: service.KeyAlgEd448,
		},
		{
			name:     "X25519",
			input:    pb.KeyAlgorithm_KEY_ALG_X25519,
			expected: service.KeyAlgX25519,
		},
		{
			name:     "X448",
			input:    pb.KeyAlgorithm_KEY_ALG_X448,
			expected: service.KeyAlgX448,
		},
		{
			name:     "HMAC-SHA256",
			input:    pb.KeyAlgorithm_KEY_ALG_HMAC_SHA256,
			expected: service.KeyAlgHMACSHA256,
		},
		{
			name:     "HMAC-SHA384",
			input:    pb.KeyAlgorithm_KEY_ALG_HMAC_SHA384,
			expected: service.KeyAlgHMACSHA384,
		},
		{
			name:     "HMAC-SHA512",
			input:    pb.KeyAlgorithm_KEY_ALG_HMAC_SHA512,
			expected: service.KeyAlgHMACSHA512,
		},
		{
			name:     "unspecified defaults to AES-256-GCM",
			input:    pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED,
			expected: service.KeyAlgAES256GCM,
		},
		{
			name:     "unknown defaults to AES-256-GCM",
			input:    pb.KeyAlgorithm(999),
			expected: service.KeyAlgAES256GCM,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoKeyAlgorithm(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ========================================
// convertKeyStatus Tests
// ========================================

func TestConvertKeyStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    service.KeyStatus
		expected pb.KeyStatus
	}{
		{
			name:     "enabled",
			input:    service.KeyStatusEnabled,
			expected: pb.KeyStatus_KEY_STATUS_ENABLED,
		},
		{
			name:     "disabled",
			input:    service.KeyStatusDisabled,
			expected: pb.KeyStatus_KEY_STATUS_DISABLED,
		},
		{
			name:     "pending deletion",
			input:    service.KeyStatusPendingDeletion,
			expected: pb.KeyStatus_KEY_STATUS_PENDING_DELETION,
		},
		{
			name:     "deleted",
			input:    service.KeyStatusDeleted,
			expected: pb.KeyStatus_KEY_STATUS_DELETED,
		},
		{
			name:     "rotating",
			input:    service.KeyStatusRotating,
			expected: pb.KeyStatus_KEY_STATUS_ROTATING,
		},
		{
			name:     "compromised",
			input:    service.KeyStatusCompromised,
			expected: pb.KeyStatus_KEY_STATUS_COMPROMISED,
		},
		{
			name:     "unknown defaults to unspecified",
			input:    service.KeyStatus("unknown"),
			expected: pb.KeyStatus_KEY_STATUS_UNSPECIFIED,
		},
		{
			name:     "empty defaults to unspecified",
			input:    service.KeyStatus(""),
			expected: pb.KeyStatus_KEY_STATUS_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertKeyStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
