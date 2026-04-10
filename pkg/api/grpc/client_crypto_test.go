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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestConvertServiceHashAlgorithmToProto validates hash algorithm conversion from service to proto.
func TestConvertServiceHashAlgorithmToProto(t *testing.T) {
	tests := []struct {
		name     string
		input    service.HashAlgorithm
		expected pb.HashAlgorithm
	}{
		{
			name:     "SHA256",
			input:    service.HashAlgSHA256,
			expected: pb.HashAlgorithm_HASH_ALG_SHA256,
		},
		{
			name:     "SHA384",
			input:    service.HashAlgSHA384,
			expected: pb.HashAlgorithm_HASH_ALG_SHA384,
		},
		{
			name:     "SHA512",
			input:    service.HashAlgSHA512,
			expected: pb.HashAlgorithm_HASH_ALG_SHA512,
		},
		{
			name:     "SHA3-256",
			input:    service.HashAlgSHA3_256,
			expected: pb.HashAlgorithm_HASH_ALG_SHA3_256,
		},
		{
			name:     "SHA3-384",
			input:    service.HashAlgSHA3_384,
			expected: pb.HashAlgorithm_HASH_ALG_SHA3_384,
		},
		{
			name:     "SHA3-512",
			input:    service.HashAlgSHA3_512,
			expected: pb.HashAlgorithm_HASH_ALG_SHA3_512,
		},
		{
			name:     "BLAKE2b-256",
			input:    service.HashAlgBLAKE2b256,
			expected: pb.HashAlgorithm_HASH_ALG_BLAKE2B_256,
		},
		{
			name:     "BLAKE2b-512",
			input:    service.HashAlgBLAKE2b512,
			expected: pb.HashAlgorithm_HASH_ALG_BLAKE2B_512,
		},
		{
			name:     "unknown defaults to SHA256",
			input:    service.HashAlgorithm("UNKNOWN"),
			expected: pb.HashAlgorithm_HASH_ALG_SHA256,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertServiceHashAlgorithmToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestConvertProtoToServiceHashAlgorithm validates hash algorithm conversion from proto to service.
func TestConvertProtoToServiceHashAlgorithm(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.HashAlgorithm
		expected service.HashAlgorithm
	}{
		{
			name:     "SHA256",
			input:    pb.HashAlgorithm_HASH_ALG_SHA256,
			expected: service.HashAlgSHA256,
		},
		{
			name:     "SHA384",
			input:    pb.HashAlgorithm_HASH_ALG_SHA384,
			expected: service.HashAlgSHA384,
		},
		{
			name:     "SHA512",
			input:    pb.HashAlgorithm_HASH_ALG_SHA512,
			expected: service.HashAlgSHA512,
		},
		{
			name:     "SHA3-256",
			input:    pb.HashAlgorithm_HASH_ALG_SHA3_256,
			expected: service.HashAlgSHA3_256,
		},
		{
			name:     "SHA3-384",
			input:    pb.HashAlgorithm_HASH_ALG_SHA3_384,
			expected: service.HashAlgSHA3_384,
		},
		{
			name:     "SHA3-512",
			input:    pb.HashAlgorithm_HASH_ALG_SHA3_512,
			expected: service.HashAlgSHA3_512,
		},
		{
			name:     "BLAKE2b-256",
			input:    pb.HashAlgorithm_HASH_ALG_BLAKE2B_256,
			expected: service.HashAlgBLAKE2b256,
		},
		{
			name:     "BLAKE2b-512",
			input:    pb.HashAlgorithm_HASH_ALG_BLAKE2B_512,
			expected: service.HashAlgBLAKE2b512,
		},
		{
			name:     "UNSPECIFIED defaults to SHA256",
			input:    pb.HashAlgorithm_HASH_ALG_UNSPECIFIED,
			expected: service.HashAlgSHA256,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToServiceHashAlgorithm(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestKMSSignNilRequest validates that KMSSign returns an error for nil request.
func TestKMSSignNilRequest(t *testing.T) {
	client := &Client{}
	_, err := client.KMSSign(nil, nil)
	assert.Error(t, err)
}

// TestKMSVerifyNilRequest validates that KMSVerify returns an error for nil request.
func TestKMSVerifyNilRequest(t *testing.T) {
	client := &Client{}
	_, err := client.KMSVerify(nil, nil)
	assert.Error(t, err)
}

// TestKMSHashNilRequest validates that KMSHash returns an error for nil request.
func TestKMSHashNilRequest(t *testing.T) {
	client := &Client{}
	_, err := client.KMSHash(nil, nil)
	assert.Error(t, err)
}

// TestKMSGenerateHMACNilRequest validates that KMSGenerateHMAC returns an error for nil request.
func TestKMSGenerateHMACNilRequest(t *testing.T) {
	client := &Client{}
	_, err := client.KMSGenerateHMAC(nil, nil)
	assert.Error(t, err)
}

// TestKMSVerifyHMACNilRequest validates that KMSVerifyHMAC returns an error for nil request.
func TestKMSVerifyHMACNilRequest(t *testing.T) {
	client := &Client{}
	_, err := client.KMSVerifyHMAC(nil, nil)
	assert.Error(t, err)
}

// TestKMSRewrapNilRequest validates that KMSRewrap returns an error for nil request.
func TestKMSRewrapNilRequest(t *testing.T) {
	client := &Client{}
	_, err := client.KMSRewrap(nil, nil)
	assert.Error(t, err)
}

// TestKMSGenerateRandomNilRequest validates that KMSGenerateRandom returns an error for nil request.
func TestKMSGenerateRandomNilRequest(t *testing.T) {
	client := &Client{}
	_, err := client.KMSGenerateRandom(nil, nil)
	assert.Error(t, err)
}
