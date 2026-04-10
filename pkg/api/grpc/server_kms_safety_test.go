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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// =============================================================================
// Safety Status Helper Tests
// =============================================================================

func TestSafetyStatusFromPercent_AllThresholds(t *testing.T) {
	tests := []struct {
		name     string
		percent  float64
		expected string
	}{
		{
			name:     "exceeded at 100%",
			percent:  100.0,
			expected: "exceeded",
		},
		{
			name:     "exceeded above 100%",
			percent:  150.0,
			expected: "exceeded",
		},
		{
			name:     "critical at 95%",
			percent:  95.0,
			expected: "critical",
		},
		{
			name:     "critical at 99%",
			percent:  99.99,
			expected: "critical",
		},
		{
			name:     "warning at 80%",
			percent:  80.0,
			expected: "warning",
		},
		{
			name:     "warning at 94%",
			percent:  94.99,
			expected: "warning",
		},
		{
			name:     "ok at 79%",
			percent:  79.99,
			expected: "ok",
		},
		{
			name:     "ok at 0%",
			percent:  0.0,
			expected: "ok",
		},
		{
			name:     "ok at 50%",
			percent:  50.0,
			expected: "ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := safetyStatusFromPercent(tt.percent)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Wrapping Algorithm Conversion Tests
// =============================================================================

func TestProtoWrappingAlgorithmToString_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		alg      pb.WrappingAlgorithm
		expected string
	}{
		{
			name:     "AES-KW",
			alg:      pb.WrappingAlgorithm_WRAP_ALG_AES_KW,
			expected: "AES-KW",
		},
		{
			name:     "AES-KWP",
			alg:      pb.WrappingAlgorithm_WRAP_ALG_AES_KWP,
			expected: "AES-KWP",
		},
		{
			name:     "AES-GCM",
			alg:      pb.WrappingAlgorithm_WRAP_ALG_AES_GCM,
			expected: "AES-GCM",
		},
		{
			name:     "RSA-OAEP-SHA256",
			alg:      pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA256,
			expected: "RSA-OAEP-SHA256",
		},
		{
			name:     "RSA-OAEP-SHA384",
			alg:      pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA384,
			expected: "RSA-OAEP-SHA384",
		},
		{
			name:     "RSA-OAEP-SHA512",
			alg:      pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA512,
			expected: "RSA-OAEP-SHA512",
		},
		{
			name:     "ECDH-ES+A256KW",
			alg:      pb.WrappingAlgorithm_WRAP_ALG_ECDH_ES_A256KW,
			expected: "ECDH-ES+A256KW",
		},
		{
			name:     "unspecified defaults to AES-GCM",
			alg:      pb.WrappingAlgorithm_WRAP_ALG_UNSPECIFIED,
			expected: "AES-GCM",
		},
		{
			name:     "unknown value defaults to AES-GCM",
			alg:      pb.WrappingAlgorithm(999),
			expected: "AES-GCM",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoWrappingAlgorithmToString(tt.alg)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Export Format Conversion Tests
// =============================================================================

func TestProtoExportFormatToString_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		format   pb.ExportFormat
		expected string
	}{
		{
			name:     "raw format",
			format:   pb.ExportFormat_EXPORT_FORMAT_RAW,
			expected: "raw",
		},
		{
			name:     "jwk format",
			format:   pb.ExportFormat_EXPORT_FORMAT_JWK,
			expected: "jwk",
		},
		{
			name:     "pem format",
			format:   pb.ExportFormat_EXPORT_FORMAT_PEM,
			expected: "pem",
		},
		{
			name:     "unspecified defaults to raw",
			format:   pb.ExportFormat_EXPORT_FORMAT_UNSPECIFIED,
			expected: "raw",
		},
		{
			name:     "unknown value defaults to raw",
			format:   pb.ExportFormat(999),
			expected: "raw",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoExportFormatToString(tt.format)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// KMS Server Safety Operations Tests
// =============================================================================

func TestKMSServer_ExportKey_NilService(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &KMSServer{
		logger: log,
		kmsSvc: nil, // No service configured
	}

	ctx := context.Background()
	resp, err := server.ExportKey(ctx, &pb.ExportKeyRequest{
		KeyId:         "key-1",
		WrappingKeyId: "wrap-key-1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unimplemented, st.Code())
	assert.Contains(t, st.Message(), "KMS service not configured")
}

func TestKMSServer_ExportKey_EmptyKeyID(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := &mockKMSService{}
	server := &KMSServer{
		logger: log,
		kmsSvc: mockSvc,
	}

	ctx := context.Background()
	resp, err := server.ExportKey(ctx, &pb.ExportKeyRequest{
		KeyId:         "",
		WrappingKeyId: "wrap-key-1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "key_id is required")
}

func TestKMSServer_ExportKey_EmptyWrappingKeyID(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := &mockKMSService{}
	server := &KMSServer{
		logger: log,
		kmsSvc: mockSvc,
	}

	ctx := context.Background()
	resp, err := server.ExportKey(ctx, &pb.ExportKeyRequest{
		KeyId:         "key-1",
		WrappingKeyId: "",
	})

	require.Error(t, err)
	assert.Nil(t, resp)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "wrapping_key_id is required")
}

func TestKMSServer_UnwrapKey_NilService(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &KMSServer{
		logger: log,
		kmsSvc: nil,
	}

	ctx := context.Background()
	resp, err := server.UnwrapKey(ctx, &pb.UnwrapKeyRequest{
		KeyId:           "key-1",
		WrappedMaterial: []byte("wrapped-data"),
		WrappingKeyId:   "wrap-key-1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unimplemented, st.Code())
	assert.Contains(t, st.Message(), "KMS service not configured")
}

func TestKMSServer_UnwrapKey_EmptyKeyID(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := &mockKMSService{}
	server := &KMSServer{
		logger: log,
		kmsSvc: mockSvc,
	}

	ctx := context.Background()
	resp, err := server.UnwrapKey(ctx, &pb.UnwrapKeyRequest{
		KeyId:           "",
		WrappedMaterial: []byte("wrapped-data"),
		WrappingKeyId:   "wrap-key-1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "key_id is required")
}

func TestKMSServer_UnwrapKey_EmptyWrappedMaterial(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := &mockKMSService{}
	server := &KMSServer{
		logger: log,
		kmsSvc: mockSvc,
	}

	ctx := context.Background()
	resp, err := server.UnwrapKey(ctx, &pb.UnwrapKeyRequest{
		KeyId:           "key-1",
		WrappedMaterial: []byte{},
		WrappingKeyId:   "wrap-key-1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "wrapped_material is required")
}

func TestKMSServer_UnwrapKey_EmptyWrappingKeyID(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := &mockKMSService{}
	server := &KMSServer{
		logger: log,
		kmsSvc: mockSvc,
	}

	ctx := context.Background()
	resp, err := server.UnwrapKey(ctx, &pb.UnwrapKeyRequest{
		KeyId:           "key-1",
		WrappedMaterial: []byte("wrapped-data"),
		WrappingKeyId:   "",
	})

	require.Error(t, err)
	assert.Nil(t, resp)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "wrapping_key_id is required")
}

func TestKMSServer_SetKeySafety_NilService(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &KMSServer{
		logger: log,
		kmsSvc: nil,
	}

	ctx := context.Background()
	resp, err := server.SetKeySafety(ctx, &pb.SetKeySafetyRequest{
		KeyId:         "key-1",
		MaxOperations: 1000,
		MaxBytes:      1024 * 1024,
	})

	require.Error(t, err)
	assert.Nil(t, resp)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unimplemented, st.Code())
	assert.Contains(t, st.Message(), "KMS service not configured")
}

func TestKMSServer_SetKeySafety_EmptyKeyID(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := &mockKMSService{}
	server := &KMSServer{
		logger: log,
		kmsSvc: mockSvc,
	}

	ctx := context.Background()
	resp, err := server.SetKeySafety(ctx, &pb.SetKeySafetyRequest{
		KeyId:         "",
		MaxOperations: 1000,
	})

	require.Error(t, err)
	assert.Nil(t, resp)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "key_id is required")
}

func TestKMSServer_ClearKeySafety_NilService(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &KMSServer{
		logger: log,
		kmsSvc: nil,
	}

	ctx := context.Background()
	resp, err := server.ClearKeySafety(ctx, &pb.ClearKeySafetyRequest{
		KeyId: "key-1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unimplemented, st.Code())
	assert.Contains(t, st.Message(), "KMS service not configured")
}

func TestKMSServer_ClearKeySafety_EmptyKeyID(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := &mockKMSService{}
	server := &KMSServer{
		logger: log,
		kmsSvc: mockSvc,
	}

	ctx := context.Background()
	resp, err := server.ClearKeySafety(ctx, &pb.ClearKeySafetyRequest{
		KeyId: "",
	})

	require.Error(t, err)
	assert.Nil(t, resp)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "key_id is required")
}

func TestKMSServer_SafetyCheck_NilService(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &KMSServer{
		logger: log,
		kmsSvc: nil,
	}

	ctx := context.Background()
	resp, err := server.SafetyCheck(ctx, &pb.SafetyCheckRequest{})

	require.Error(t, err)
	assert.Nil(t, resp)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unimplemented, st.Code())
	assert.Contains(t, st.Message(), "KMS service not configured")
}

func TestKMSServer_SafetyReport_NilService(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &KMSServer{
		logger: log,
		kmsSvc: nil,
	}

	ctx := context.Background()
	resp, err := server.SafetyReport(ctx, &pb.SafetyReportRequest{})

	require.Error(t, err)
	assert.Nil(t, resp)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unimplemented, st.Code())
	assert.Contains(t, st.Message(), "KMS service not configured")
}

// =============================================================================
// KMS Server Service Configuration Tests
// =============================================================================

func TestNewKMSServerWithService(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := &mockKMSService{}
	mockMgr := &mockKMSManager{}

	server := NewKMSServerWithService(mockMgr, mockSvc, log)

	require.NotNil(t, server)
	assert.Equal(t, mockSvc, server.kmsSvc)
	// Logger gets wrapped with component field, just verify it's not nil
	assert.NotNil(t, server.logger)
}

func TestKMSServer_SetKMSService(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &KMSServer{
		logger: log,
	}

	mockSvc := &mockKMSService{}
	server.SetKMSService(mockSvc)

	assert.Equal(t, mockSvc, server.kmsSvc)
}

// =============================================================================
// Proto Key Algorithm Conversion Tests
// =============================================================================

func TestProtoKeyAlgorithmToString_Coverage(t *testing.T) {
	tests := []struct {
		name     string
		alg      pb.KeyAlgorithm
		expected string
	}{
		{
			name:     "AES-128-GCM",
			alg:      pb.KeyAlgorithm_KEY_ALG_AES_128_GCM,
			expected: "AES-128-GCM",
		},
		{
			name:     "AES-256-GCM",
			alg:      pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
			expected: "AES-256-GCM",
		},
		{
			name:     "RSA-2048",
			alg:      pb.KeyAlgorithm_KEY_ALG_RSA_2048,
			expected: "RSA-2048",
		},
		{
			name:     "RSA-4096",
			alg:      pb.KeyAlgorithm_KEY_ALG_RSA_4096,
			expected: "RSA-4096",
		},
		{
			name:     "ECDSA-P256",
			alg:      pb.KeyAlgorithm_KEY_ALG_ECDSA_P256,
			expected: "ECDSA-P256",
		},
		{
			name:     "ECDSA-P384",
			alg:      pb.KeyAlgorithm_KEY_ALG_ECDSA_P384,
			expected: "ECDSA-P384",
		},
		{
			name:     "ED25519",
			alg:      pb.KeyAlgorithm_KEY_ALG_ED25519,
			expected: "ED25519",
		},
		{
			name:     "CHACHA20-POLY1305",
			alg:      pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305,
			expected: "CHACHA20-POLY1305",
		},
		{
			name:     "unspecified defaults to AES-256-GCM",
			alg:      pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED,
			expected: "AES-256-GCM",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoKeyAlgorithmToString(tt.alg)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Additional Proto Key Algorithm Cases for Full Coverage
// =============================================================================

func TestProtoKeyAlgorithmToString_AllAlgorithms(t *testing.T) {
	tests := []struct {
		name     string
		alg      pb.KeyAlgorithm
		expected string
	}{
		{"AES-192-GCM", pb.KeyAlgorithm_KEY_ALG_AES_192_GCM, "AES-192-GCM"},
		{"RSA-3072", pb.KeyAlgorithm_KEY_ALG_RSA_3072, "RSA-3072"},
		{"ECDSA-P521", pb.KeyAlgorithm_KEY_ALG_ECDSA_P521, "ECDSA-P521"},
		{"XCHACHA20-POLY1305", pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305, "XCHACHA20-POLY1305"},
		{"HMAC-SHA256", pb.KeyAlgorithm_KEY_ALG_HMAC_SHA256, "HMAC-SHA256"},
		{"HMAC-SHA384", pb.KeyAlgorithm_KEY_ALG_HMAC_SHA384, "HMAC-SHA384"},
		{"HMAC-SHA512", pb.KeyAlgorithm_KEY_ALG_HMAC_SHA512, "HMAC-SHA512"},
		{"ED448", pb.KeyAlgorithm_KEY_ALG_ED448, "ED448"},
		{"X25519", pb.KeyAlgorithm_KEY_ALG_X25519, "X25519"},
		{"X448", pb.KeyAlgorithm_KEY_ALG_X448, "X448"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoKeyAlgorithmToString(tt.alg)
			assert.Equal(t, tt.expected, result)
		})
	}
}
