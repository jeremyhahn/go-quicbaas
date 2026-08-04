//go:build mcp

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
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// adminKMSService embeds testKMSService and overrides the key-administration
// methods so success and error paths can be exercised.
type adminKMSService struct {
	*testKMSService
	publicKeyResp *service.GetPublicKeyResponse
	publicKeyErr  error
	algorithms    []string
	algorithmsErr error
}

func (s *adminKMSService) GetPublicKey(_ context.Context, _ *service.GetPublicKeyRequest) (*service.GetPublicKeyResponse, error) {
	return s.publicKeyResp, s.publicKeyErr
}

func (s *adminKMSService) GetSupportedAlgorithms(_ context.Context) ([]string, error) {
	return s.algorithms, s.algorithmsErr
}

// =============================================================================
// quicbaas_kms_get_public_key
// =============================================================================

func TestExecuteKMSGetPublicKey_Success(t *testing.T) {
	der := []byte{0x30, 0x82, 0x01, 0x22}
	registry := newRotationRegistry(t, &adminKMSService{
		testKMSService: &testKMSService{},
		publicKeyResp: &service.GetPublicKeyResponse{
			KeyID:     "key-123",
			PublicKey: der,
			Algorithm: service.KeyAlgRSA2048,
			Version:   3,
			Timestamp: time.Now(),
		},
	})

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_get_public_key",
		map[string]interface{}{"key_id": "key-123"})
	require.NoError(t, err)

	resultMap := decodeMarshalled(t, result)
	assert.Equal(t, "key-123", resultMap["key_id"])
	assert.Equal(t, base64.StdEncoding.EncodeToString(der), resultMap["public_key"])
	assert.Equal(t, string(service.KeyAlgRSA2048), resultMap["algorithm"])
	assert.Equal(t, float64(3), resultMap["version"])
}

func TestExecuteKMSGetPublicKey_MissingKeyID(t *testing.T) {
	registry := newRotationRegistry(t, &adminKMSService{testKMSService: &testKMSService{}})

	_, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_get_public_key",
		map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id")
}

func TestExecuteKMSGetPublicKey_ServiceError(t *testing.T) {
	registry := newRotationRegistry(t, &adminKMSService{
		testKMSService: &testKMSService{},
		publicKeyErr:   errors.New(errors.ErrNotFound, "GetPublicKey", "no such key"),
	})

	_, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_get_public_key",
		map[string]interface{}{"key_id": "missing"})
	require.Error(t, err)
}

func TestExecuteKMSGetPublicKey_NoService(t *testing.T) {
	registry := newNoKMSRegistry(t)

	_, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_get_public_key",
		map[string]interface{}{"key_id": "key-123"})
	require.Error(t, err)
}

// =============================================================================
// quicbaas_kms_get_supported_algorithms
// =============================================================================

func TestExecuteKMSGetSupportedAlgorithms_Success(t *testing.T) {
	registry := newRotationRegistry(t, &adminKMSService{
		testKMSService: &testKMSService{},
		algorithms:     []string{"AES-256-GCM", "RSA-2048"},
	})

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_get_supported_algorithms",
		map[string]interface{}{})
	require.NoError(t, err)

	resultMap := decodeMarshalled(t, result)
	algorithms, ok := resultMap["algorithms"].([]interface{})
	require.True(t, ok, "algorithms should be an array")
	assert.Len(t, algorithms, 2)
	assert.Contains(t, algorithms, "AES-256-GCM")
}

func TestExecuteKMSGetSupportedAlgorithms_NilBecomesEmpty(t *testing.T) {
	registry := newRotationRegistry(t, &adminKMSService{testKMSService: &testKMSService{}})

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_get_supported_algorithms",
		map[string]interface{}{})
	require.NoError(t, err)

	resultMap := decodeMarshalled(t, result)
	algorithms, ok := resultMap["algorithms"].([]interface{})
	require.True(t, ok, "algorithms should be an array")
	assert.Empty(t, algorithms)
}

func TestExecuteKMSGetSupportedAlgorithms_ServiceError(t *testing.T) {
	registry := newRotationRegistry(t, &adminKMSService{
		testKMSService: &testKMSService{},
		algorithmsErr:  errors.New(errors.ErrInternal, "GetSupportedAlgorithms", "backend error"),
	})

	_, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_get_supported_algorithms",
		map[string]interface{}{})
	require.Error(t, err)
}

func TestExecuteKMSGetSupportedAlgorithms_NoService(t *testing.T) {
	registry := newNoKMSRegistry(t)

	_, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_get_supported_algorithms",
		map[string]interface{}{})
	require.Error(t, err)
}

// TestKMSAdminToolsRegistered asserts the new tools are present in the
// registry's tool definitions.
func TestKMSAdminToolsRegistered(t *testing.T) {
	registry := newRotationRegistry(t, &adminKMSService{testKMSService: &testKMSService{}})
	registry.registerKMSTools()

	names := make(map[string]bool, len(registry.tools))
	for _, tool := range registry.tools {
		names[tool.Name] = true
	}
	assert.True(t, names["quicbaas_kms_get_public_key"], "quicbaas_kms_get_public_key not registered")
	assert.True(t, names["quicbaas_kms_get_supported_algorithms"], "quicbaas_kms_get_supported_algorithms not registered")
}
