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
	"crypto/elliptic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestConvertServiceToProtoKeyAlgorithm_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected pb.KeyAlgorithm
	}{
		// AES algorithms
		{"AES-128-GCM", "AES-128-GCM", pb.KeyAlgorithm_KEY_ALG_AES_128_GCM},
		{"AES-192-GCM", "AES-192-GCM", pb.KeyAlgorithm_KEY_ALG_AES_192_GCM},
		{"AES-256-GCM", "AES-256-GCM", pb.KeyAlgorithm_KEY_ALG_AES_256_GCM},
		// ChaCha algorithms
		{"CHACHA20-POLY1305", "CHACHA20-POLY1305", pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305},
		{"XCHACHA20-POLY1305", "XCHACHA20-POLY1305", pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305},
		// RSA algorithms
		{"RSA-2048", "RSA-2048", pb.KeyAlgorithm_KEY_ALG_RSA_2048},
		{"RSA-3072", "RSA-3072", pb.KeyAlgorithm_KEY_ALG_RSA_3072},
		{"RSA-4096", "RSA-4096", pb.KeyAlgorithm_KEY_ALG_RSA_4096},
		// ECDSA algorithms
		{"ECDSA-P256", "ECDSA-P256", pb.KeyAlgorithm_KEY_ALG_ECDSA_P256},
		{"ECDSA-P384", "ECDSA-P384", pb.KeyAlgorithm_KEY_ALG_ECDSA_P384},
		{"ECDSA-P521", "ECDSA-P521", pb.KeyAlgorithm_KEY_ALG_ECDSA_P521},
		// Edwards curve algorithms
		{"ED25519", "ED25519", pb.KeyAlgorithm_KEY_ALG_ED25519},
		{"ED448", "ED448", pb.KeyAlgorithm_KEY_ALG_ED448},
		{"X25519", "X25519", pb.KeyAlgorithm_KEY_ALG_X25519},
		{"X448", "X448", pb.KeyAlgorithm_KEY_ALG_X448},
		// HMAC algorithms
		{"HMAC-SHA256", "HMAC-SHA256", pb.KeyAlgorithm_KEY_ALG_HMAC_SHA256},
		{"HMAC-SHA384", "HMAC-SHA384", pb.KeyAlgorithm_KEY_ALG_HMAC_SHA384},
		{"HMAC-SHA512", "HMAC-SHA512", pb.KeyAlgorithm_KEY_ALG_HMAC_SHA512},
		// Unknown/default
		{"unknown algorithm", "UNKNOWN-ALG", pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED},
		{"empty string", "", pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertServiceToProtoKeyAlgorithm(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertServiceKeyAlgorithmToProto_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    service.KeyAlgorithm
		expected pb.KeyAlgorithm
	}{
		{"AES128GCM", service.KeyAlgAES128GCM, pb.KeyAlgorithm_KEY_ALG_AES_128_GCM},
		{"AES192GCM", service.KeyAlgAES192GCM, pb.KeyAlgorithm_KEY_ALG_AES_192_GCM},
		{"AES256GCM", service.KeyAlgAES256GCM, pb.KeyAlgorithm_KEY_ALG_AES_256_GCM},
		{"ChaCha20Poly1305", service.KeyAlgChaCha20Poly1305, pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305},
		{"XChaCha20Poly1305", service.KeyAlgXChaCha20Poly1305, pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305},
		{"RSA2048", service.KeyAlgRSA2048, pb.KeyAlgorithm_KEY_ALG_RSA_2048},
		{"RSA3072", service.KeyAlgRSA3072, pb.KeyAlgorithm_KEY_ALG_RSA_3072},
		{"RSA4096", service.KeyAlgRSA4096, pb.KeyAlgorithm_KEY_ALG_RSA_4096},
		{"ECDSA-P256", service.KeyAlgECDSAP256, pb.KeyAlgorithm_KEY_ALG_ECDSA_P256},
		{"ECDSA-P384", service.KeyAlgECDSAP384, pb.KeyAlgorithm_KEY_ALG_ECDSA_P384},
		{"ECDSA-P521", service.KeyAlgECDSAP521, pb.KeyAlgorithm_KEY_ALG_ECDSA_P521},
		{"ED25519", service.KeyAlgEd25519, pb.KeyAlgorithm_KEY_ALG_ED25519},
		{"ED448", service.KeyAlgEd448, pb.KeyAlgorithm_KEY_ALG_ED448},
		{"X25519", service.KeyAlgX25519, pb.KeyAlgorithm_KEY_ALG_X25519},
		{"X448", service.KeyAlgX448, pb.KeyAlgorithm_KEY_ALG_X448},
		{"HMAC-SHA256", service.KeyAlgHMACSHA256, pb.KeyAlgorithm_KEY_ALG_HMAC_SHA256},
		{"HMAC-SHA384", service.KeyAlgHMACSHA384, pb.KeyAlgorithm_KEY_ALG_HMAC_SHA384},
		{"HMAC-SHA512", service.KeyAlgHMACSHA512, pb.KeyAlgorithm_KEY_ALG_HMAC_SHA512},
		{"unknown", service.KeyAlgorithm("unknown"), pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertServiceKeyAlgorithmToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertProtoToServiceKeyAlgorithm_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.KeyAlgorithm
		expected service.KeyAlgorithm
	}{
		{"AES128GCM", pb.KeyAlgorithm_KEY_ALG_AES_128_GCM, service.KeyAlgAES128GCM},
		{"AES192GCM", pb.KeyAlgorithm_KEY_ALG_AES_192_GCM, service.KeyAlgAES192GCM},
		{"AES256GCM", pb.KeyAlgorithm_KEY_ALG_AES_256_GCM, service.KeyAlgAES256GCM},
		{"ChaCha20Poly1305", pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305, service.KeyAlgChaCha20Poly1305},
		{"XChaCha20Poly1305", pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305, service.KeyAlgXChaCha20Poly1305},
		{"RSA2048", pb.KeyAlgorithm_KEY_ALG_RSA_2048, service.KeyAlgRSA2048},
		{"RSA3072", pb.KeyAlgorithm_KEY_ALG_RSA_3072, service.KeyAlgRSA3072},
		{"RSA4096", pb.KeyAlgorithm_KEY_ALG_RSA_4096, service.KeyAlgRSA4096},
		{"ECDSA-P256", pb.KeyAlgorithm_KEY_ALG_ECDSA_P256, service.KeyAlgECDSAP256},
		{"ECDSA-P384", pb.KeyAlgorithm_KEY_ALG_ECDSA_P384, service.KeyAlgECDSAP384},
		{"ECDSA-P521", pb.KeyAlgorithm_KEY_ALG_ECDSA_P521, service.KeyAlgECDSAP521},
		{"ED25519", pb.KeyAlgorithm_KEY_ALG_ED25519, service.KeyAlgEd25519},
		{"ED448", pb.KeyAlgorithm_KEY_ALG_ED448, service.KeyAlgEd448},
		{"X25519", pb.KeyAlgorithm_KEY_ALG_X25519, service.KeyAlgX25519},
		{"X448", pb.KeyAlgorithm_KEY_ALG_X448, service.KeyAlgX448},
		{"HMAC-SHA256", pb.KeyAlgorithm_KEY_ALG_HMAC_SHA256, service.KeyAlgHMACSHA256},
		{"HMAC-SHA384", pb.KeyAlgorithm_KEY_ALG_HMAC_SHA384, service.KeyAlgHMACSHA384},
		{"HMAC-SHA512", pb.KeyAlgorithm_KEY_ALG_HMAC_SHA512, service.KeyAlgHMACSHA512},
		{"unspecified", pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED, ""},
		{"unknown value", pb.KeyAlgorithm(999), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToServiceKeyAlgorithm(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertProtoToServiceKeyStatus_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.KeyStatus
		expected service.KeyStatus
	}{
		{"enabled", pb.KeyStatus_KEY_STATUS_ENABLED, service.KeyStatusEnabled},
		{"disabled", pb.KeyStatus_KEY_STATUS_DISABLED, service.KeyStatusDisabled},
		{"pending_deletion", pb.KeyStatus_KEY_STATUS_PENDING_DELETION, service.KeyStatusPendingDeletion},
		{"deleted", pb.KeyStatus_KEY_STATUS_DELETED, service.KeyStatusDeleted},
		{"rotating", pb.KeyStatus_KEY_STATUS_ROTATING, service.KeyStatusRotating},
		{"compromised", pb.KeyStatus_KEY_STATUS_COMPROMISED, service.KeyStatusCompromised},
		{"unspecified", pb.KeyStatus_KEY_STATUS_UNSPECIFIED, ""},
		{"unknown value", pb.KeyStatus(999), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToServiceKeyStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertServiceToProtoKeyStatus_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    service.KeyStatus
		expected pb.KeyStatus
	}{
		{"enabled", service.KeyStatusEnabled, pb.KeyStatus_KEY_STATUS_ENABLED},
		{"disabled", service.KeyStatusDisabled, pb.KeyStatus_KEY_STATUS_DISABLED},
		{"pending_deletion", service.KeyStatusPendingDeletion, pb.KeyStatus_KEY_STATUS_PENDING_DELETION},
		{"deleted", service.KeyStatusDeleted, pb.KeyStatus_KEY_STATUS_DELETED},
		{"rotating", service.KeyStatusRotating, pb.KeyStatus_KEY_STATUS_ROTATING},
		{"compromised", service.KeyStatusCompromised, pb.KeyStatus_KEY_STATUS_COMPROMISED},
		{"unknown", service.KeyStatus("unknown"), pb.KeyStatus_KEY_STATUS_UNSPECIFIED},
		{"empty", service.KeyStatus(""), pb.KeyStatus_KEY_STATUS_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertServiceToProtoKeyStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertKeyAttributesToProto_NilInput(t *testing.T) {
	alg, keySize, usage, exportable, err := convertKeyAttributesToProto(nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "key attributes cannot be nil")
	assert.Equal(t, pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED, alg)
	assert.Equal(t, int32(0), keySize)
	assert.Nil(t, usage)
	assert.False(t, exportable)
}

func TestConvertKeyAttributesToProto_SymmetricAlgorithms(t *testing.T) {
	tests := []struct {
		name            string
		symmetricAlg    xkmssdk.SymmetricAlgorithm
		expectedAlg     pb.KeyAlgorithm
		expectedKeySize int32
	}{
		{"AES128GCM", xkmssdk.SymmetricAES128GCM, pb.KeyAlgorithm_KEY_ALG_AES_128_GCM, 16},
		{"AES192GCM", xkmssdk.SymmetricAES192GCM, pb.KeyAlgorithm_KEY_ALG_AES_192_GCM, 24},
		{"AES256GCM", xkmssdk.SymmetricAES256GCM, pb.KeyAlgorithm_KEY_ALG_AES_256_GCM, 32},
		{"ChaCha20Poly1305", xkmssdk.SymmetricChaCha20Poly1305, pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305, 32},
		{"XChaCha20Poly1305", xkmssdk.SymmetricXChaCha20Poly1305, pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305, 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: tt.symmetricAlg,
			}

			alg, keySize, usage, _, err := convertKeyAttributesToProto(attrs)

			require.NoError(t, err)
			assert.Equal(t, tt.expectedAlg, alg)
			assert.Equal(t, tt.expectedKeySize, keySize)
			assert.Contains(t, usage, pb.KeyUsage_KEY_USAGE_ENCRYPT)
			assert.Contains(t, usage, pb.KeyUsage_KEY_USAGE_DECRYPT)
		})
	}
}

func TestConvertKeyAttributesToProto_UnsupportedSymmetricAlgorithm(t *testing.T) {
	attrs := &xkmssdk.KeyAttributes{
		SymmetricAlgorithm: xkmssdk.SymmetricAlgorithm("UNSUPPORTED-ALG"),
	}

	alg, keySize, usage, exportable, err := convertKeyAttributesToProto(attrs)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported symmetric algorithm")
	assert.Equal(t, pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED, alg)
	assert.Equal(t, int32(0), keySize)
	assert.Nil(t, usage)
	assert.False(t, exportable)
}

func TestConvertKeyAttributesToProto_RSAAlgorithms(t *testing.T) {
	tests := []struct {
		name            string
		rsaKeySize      int
		expectedAlg     pb.KeyAlgorithm
		expectedKeySize int32
	}{
		{"RSA2048", 2048, pb.KeyAlgorithm_KEY_ALG_RSA_2048, 2048},
		{"RSA3072", 3072, pb.KeyAlgorithm_KEY_ALG_RSA_3072, 3072},
		{"RSA4096", 4096, pb.KeyAlgorithm_KEY_ALG_RSA_4096, 4096},
		{"RSA default", 0, pb.KeyAlgorithm_KEY_ALG_RSA_4096, 4096}, // Default to 4096
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := &xkmssdk.KeyAttributes{
				KeyAlgorithm: 1, // x509.RSA
			}
			if tt.rsaKeySize > 0 {
				attrs.RSAAttributes = &xkmssdk.RSAAttributes{
					KeySize: tt.rsaKeySize,
				}
			}

			alg, keySize, usage, _, err := convertKeyAttributesToProto(attrs)

			require.NoError(t, err)
			assert.Equal(t, tt.expectedAlg, alg)
			assert.Equal(t, tt.expectedKeySize, keySize)
			assert.Contains(t, usage, pb.KeyUsage_KEY_USAGE_SIGN)
			assert.Contains(t, usage, pb.KeyUsage_KEY_USAGE_VERIFY)
		})
	}
}

func TestConvertKeyAttributesToProto_ECDSAAlgorithms(t *testing.T) {
	tests := []struct {
		name            string
		curve           elliptic.Curve
		expectedAlg     pb.KeyAlgorithm
		expectedKeySize int32
	}{
		{"P256", elliptic.P256(), pb.KeyAlgorithm_KEY_ALG_ECDSA_P256, 256},
		{"P384", elliptic.P384(), pb.KeyAlgorithm_KEY_ALG_ECDSA_P384, 384},
		{"P521", elliptic.P521(), pb.KeyAlgorithm_KEY_ALG_ECDSA_P521, 521},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := &xkmssdk.KeyAttributes{
				KeyAlgorithm: 3, // x509.ECDSA
				ECCAttributes: &xkmssdk.ECCAttributes{
					Curve: tt.curve,
				},
			}

			alg, keySize, usage, _, err := convertKeyAttributesToProto(attrs)

			require.NoError(t, err)
			assert.Equal(t, tt.expectedAlg, alg)
			assert.Equal(t, tt.expectedKeySize, keySize)
			assert.Contains(t, usage, pb.KeyUsage_KEY_USAGE_SIGN)
			assert.Contains(t, usage, pb.KeyUsage_KEY_USAGE_VERIFY)
		})
	}
}

func TestConvertKeyAttributesToProto_ECDSAWithoutCurve(t *testing.T) {
	attrs := &xkmssdk.KeyAttributes{
		KeyAlgorithm: 3, // x509.ECDSA
		// No ECCAttributes - should default to P256
	}

	alg, keySize, _, _, err := convertKeyAttributesToProto(attrs)

	require.NoError(t, err)
	assert.Equal(t, pb.KeyAlgorithm_KEY_ALG_ECDSA_P256, alg)
	assert.Equal(t, int32(256), keySize)
}

func TestConvertKeyAttributesToProto_Ed25519(t *testing.T) {
	attrs := &xkmssdk.KeyAttributes{
		KeyAlgorithm: 4, // x509.Ed25519
	}

	alg, keySize, usage, _, err := convertKeyAttributesToProto(attrs)

	require.NoError(t, err)
	assert.Equal(t, pb.KeyAlgorithm_KEY_ALG_ED25519, alg)
	assert.Equal(t, int32(256), keySize)
	assert.Contains(t, usage, pb.KeyUsage_KEY_USAGE_SIGN)
	assert.Contains(t, usage, pb.KeyUsage_KEY_USAGE_VERIFY)
}

func TestConvertKeyAttributesToProto_UnsupportedAsymmetricAlgorithm(t *testing.T) {
	attrs := &xkmssdk.KeyAttributes{
		KeyAlgorithm: 99, // Unknown algorithm
	}

	alg, keySize, usage, exportable, err := convertKeyAttributesToProto(attrs)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported key algorithm")
	assert.Equal(t, pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED, alg)
	assert.Equal(t, int32(0), keySize)
	assert.Nil(t, usage)
	assert.False(t, exportable)
}

func TestConvertKeyAttributesToProto_NoAlgorithmSet(t *testing.T) {
	attrs := &xkmssdk.KeyAttributes{
		// Neither SymmetricAlgorithm nor KeyAlgorithm set
	}

	alg, keySize, usage, exportable, err := convertKeyAttributesToProto(attrs)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "either SymmetricAlgorithm or KeyAlgorithm must be set")
	assert.Equal(t, pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED, alg)
	assert.Equal(t, int32(0), keySize)
	assert.Nil(t, usage)
	assert.False(t, exportable)
}

func TestConvertProtoKeyVersionInfo_NilInput(t *testing.T) {
	result := convertProtoKeyVersionInfo("key-123", nil)
	assert.Nil(t, result)
}

func TestConvertProtoKeyVersionInfo_ValidInput(t *testing.T) {
	now := int64(1609459200) // 2021-01-01
	info := &pb.KeyVersionInfo{
		Version:   3,
		Status:    pb.KeyStatus_KEY_STATUS_ENABLED,
		Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
		CreatedAt: now,
	}

	result := convertProtoKeyVersionInfo("key-123", info)

	require.NotNil(t, result)
	assert.Equal(t, "key-123", result.KeyID)
	assert.Equal(t, uint64(3), result.Version)
	assert.Equal(t, service.KeyStatusEnabled, result.Status)
	assert.Equal(t, service.KeyAlgAES256GCM, result.Algorithm)
	assert.Nil(t, result.DisabledAt)
}

func TestConvertProtoKeyVersionInfo_WithDisabledAt(t *testing.T) {
	now := int64(1609459200)
	disabledTime := int64(1609545600) // 2021-01-02

	info := &pb.KeyVersionInfo{
		Version:    2,
		Status:     pb.KeyStatus_KEY_STATUS_DISABLED,
		Algorithm:  pb.KeyAlgorithm_KEY_ALG_RSA_4096,
		CreatedAt:  now,
		DisabledAt: disabledTime,
	}

	result := convertProtoKeyVersionInfo("key-456", info)

	require.NotNil(t, result)
	assert.Equal(t, "key-456", result.KeyID)
	require.NotNil(t, result.DisabledAt)
	assert.Equal(t, disabledTime, result.DisabledAt.Unix())
}

func TestConvertProtoToServiceKeyUsage_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.KeyUsage
		expected service.KeyUsage
	}{
		{"encrypt", pb.KeyUsage_KEY_USAGE_ENCRYPT, service.KeyUsageEncrypt},
		{"decrypt", pb.KeyUsage_KEY_USAGE_DECRYPT, service.KeyUsageDecrypt},
		{"sign", pb.KeyUsage_KEY_USAGE_SIGN, service.KeyUsageSign},
		{"verify", pb.KeyUsage_KEY_USAGE_VERIFY, service.KeyUsageVerify},
		{"wrap_key", pb.KeyUsage_KEY_USAGE_WRAP_KEY, service.KeyUsageWrapKey},
		{"derive_key", pb.KeyUsage_KEY_USAGE_DERIVE_KEY, service.KeyUsageDeriveKey},
		{"generate_mac", pb.KeyUsage_KEY_USAGE_GENERATE_MAC, service.KeyUsageGenerateMAC},
		{"verify_mac", pb.KeyUsage_KEY_USAGE_VERIFY_MAC, service.KeyUsageVerifyMAC},
		{"unspecified", pb.KeyUsage_KEY_USAGE_UNSPECIFIED, ""},
		{"unknown value", pb.KeyUsage(999), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToServiceKeyUsage(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertProtoToServiceKeyUsageList_EmptySlice(t *testing.T) {
	result := convertProtoToServiceKeyUsageList([]pb.KeyUsage{})
	assert.Empty(t, result)
	assert.NotNil(t, result)
}

func TestConvertProtoToServiceKeyUsageList_MultipleUsages(t *testing.T) {
	input := []pb.KeyUsage{
		pb.KeyUsage_KEY_USAGE_ENCRYPT,
		pb.KeyUsage_KEY_USAGE_DECRYPT,
		pb.KeyUsage_KEY_USAGE_SIGN,
	}

	result := convertProtoToServiceKeyUsageList(input)

	require.Len(t, result, 3)
	assert.Equal(t, service.KeyUsageEncrypt, result[0])
	assert.Equal(t, service.KeyUsageDecrypt, result[1])
	assert.Equal(t, service.KeyUsageSign, result[2])
}
