//go:build mcp

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

package mcp

import (
	"context"
	"testing"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// keyTypeCaptureKMSService records the GenerateKeyRequest the MCP tool hands
// to the KMS service layer.
type keyTypeCaptureKMSService struct {
	*testKMSService
	req *service.GenerateKeyRequest
}

func (s *keyTypeCaptureKMSService) GenerateKey(_ context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
	s.req = req
	return &service.GenerateKeyResponse{}, nil
}

// generateOverMCP runs the kms_generate_key tool with the given algorithm and
// returns the key attributes that reached the KMS service.
func generateOverMCP(t *testing.T, algorithm string) *kctypes.KeyAttributes {
	t.Helper()

	svc := &keyTypeCaptureKMSService{testKMSService: &testKMSService{}}
	r := newRotationRegistry(t, svc)

	_, err := r.executeKMSGenerateKey(context.Background(), map[string]interface{}{
		"name":      "mcp-keytype",
		"algorithm": algorithm,
	})
	require.NoError(t, err)
	require.NotNil(t, svc.req)
	require.NotNil(t, svc.req.KeyAttributes)
	return svc.req.KeyAttributes
}

func TestExecuteKMSGenerateKey_SymmetricKeyReachesServiceAsSecret(t *testing.T) {
	for _, algorithm := range []string{"AES-128-GCM", "AES-256-GCM", "ChaCha20-Poly1305", "XChaCha20-Poly1305"} {
		t.Run(algorithm, func(t *testing.T) {
			attrs := generateOverMCP(t, algorithm)

			assert.NotEmpty(t, attrs.SymmetricAlgorithm)
			assert.Equal(t, kctypes.KeyTypeSecret, attrs.KeyType,
				"a symmetric key generated over MCP must reach the KMS layer as KeyTypeSecret")
		})
	}
}

func TestExecuteKMSGenerateKey_UnknownAlgorithmDefaultsToSecretAES(t *testing.T) {
	attrs := generateOverMCP(t, "no-such-algorithm")

	assert.Equal(t, kctypes.SymmetricAES256GCM, attrs.SymmetricAlgorithm)
	assert.Equal(t, kctypes.KeyTypeSecret, attrs.KeyType)
}

func TestExecuteKMSGenerateKey_AsymmetricKeyKeepsSigningType(t *testing.T) {
	for _, algorithm := range []string{"RSA-2048", "ECDSA-P256", "Ed25519"} {
		t.Run(algorithm, func(t *testing.T) {
			attrs := generateOverMCP(t, algorithm)

			assert.Empty(t, attrs.SymmetricAlgorithm)
			assert.Equal(t, kctypes.KeyTypeSigning, attrs.KeyType)
		})
	}
}
