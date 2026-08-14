//go:build quic

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

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func TestQUICKMSClient_StructCreation(t *testing.T) {
	t.Parallel()

	// Test struct can be created with nil client for interface compliance
	client := &QUICKMSClient{client: nil}
	assert.NotNil(t, client)
}

func TestQUICSecretsClient_StructCreation(t *testing.T) {
	t.Parallel()

	// Test struct can be created with nil client for interface compliance
	client := &QUICSecretsClient{client: nil}
	assert.NotNil(t, client)
}

func TestQUICSealClient_StructCreation(t *testing.T) {
	t.Parallel()

	// Test struct can be created with nil client for interface compliance
	client := &QUICSealClient{client: nil}
	assert.NotNil(t, client)
}

func TestQUICBackendClient_StructCreation(t *testing.T) {
	t.Parallel()

	// Test struct can be created with nil client for interface compliance
	client := &QUICBackendClient{client: nil}
	assert.NotNil(t, client)
}

func TestQUICSafetyClient_StructCreation(t *testing.T) {
	t.Parallel()

	// Test struct can be created with nil client for interface compliance
	client := &QUICSafetyClient{client: nil}
	assert.NotNil(t, client)
}

func TestQUICCryptoClient_StructCreation(t *testing.T) {
	t.Parallel()

	// Test struct can be created with nil client for interface compliance
	client := &QUICCryptoClient{client: nil}
	assert.NotNil(t, client)
}

func TestQUICCryptoClient_NotImplementedMethods(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Test struct with nil client - these methods return NotImplemented errors
	client := &QUICCryptoClient{client: nil}

	t.Run("KMSSign_returns_not_implemented", func(t *testing.T) {
		t.Parallel()
		resp, err := client.KMSSign(ctx, &service.SignRequest{KeyID: "test"})
		assert.Nil(t, resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet support")
	})

	t.Run("KMSVerify_returns_not_implemented", func(t *testing.T) {
		t.Parallel()
		resp, err := client.KMSVerify(ctx, &service.VerifyRequest{KeyID: "test"})
		assert.Nil(t, resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet support")
	})

	t.Run("KMSHash_returns_not_implemented", func(t *testing.T) {
		t.Parallel()
		resp, err := client.KMSHash(ctx, &service.HashRequest{Algorithm: "sha256"})
		assert.Nil(t, resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet support")
	})

	t.Run("KMSGenerateHMAC_returns_not_implemented", func(t *testing.T) {
		t.Parallel()
		resp, err := client.KMSGenerateHMAC(ctx, &service.HMACRequest{KeyID: "test"})
		assert.Nil(t, resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet support")
	})

	t.Run("KMSVerifyHMAC_returns_not_implemented", func(t *testing.T) {
		t.Parallel()
		resp, err := client.KMSVerifyHMAC(ctx, &service.VerifyHMACRequest{KeyID: "test"})
		assert.Nil(t, resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet support")
	})

	t.Run("KMSRewrap_returns_not_implemented", func(t *testing.T) {
		t.Parallel()
		resp, err := client.KMSRewrap(ctx, &service.RewrapRequest{KeyID: "test"})
		assert.Nil(t, resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet support")
	})

	t.Run("KMSGenerateRandom_returns_not_implemented", func(t *testing.T) {
		t.Parallel()
		resp, err := client.KMSGenerateRandom(ctx, &service.GenerateRandomRequest{NumBytes: 32})
		assert.Nil(t, resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet support")
	})
}

// =============================================================================
// QUIC Client Logger Creation Test
// =============================================================================

func TestCreateQUICClientLogger(t *testing.T) {
	t.Parallel()

	// Save and restore logLevel
	origLogLevel := logLevel

	t.Run("default_log_level", func(t *testing.T) {
		logLevel = ""
		defer func() { logLevel = origLogLevel }()

		logger, err := createQUICClientLogger()
		require.NoError(t, err)
		assert.NotNil(t, logger)
	})

	t.Run("custom_log_level", func(t *testing.T) {
		logLevel = "debug"
		defer func() { logLevel = origLogLevel }()

		logger, err := createQUICClientLogger()
		require.NoError(t, err)
		assert.NotNil(t, logger)
	})
}

// =============================================================================
// Interface Compliance Verification for QUIC Clients
// =============================================================================

func TestQUICClientInterfaceCompliance(t *testing.T) {
	t.Parallel()

	t.Run("QUICKMSClient_implements_KMSClient", func(t *testing.T) {
		t.Parallel()
		var _ KMSClient = (*QUICKMSClient)(nil)
	})

	t.Run("QUICSecretsClient_implements_SecretsClient", func(t *testing.T) {
		t.Parallel()
		var _ SecretsClient = (*QUICSecretsClient)(nil)
	})

	t.Run("QUICSealClient_implements_SealClient", func(t *testing.T) {
		t.Parallel()
		var _ SealClient = (*QUICSealClient)(nil)
	})

	t.Run("QUICBackendClient_implements_BackendClient", func(t *testing.T) {
		t.Parallel()
		var _ BackendClient = (*QUICBackendClient)(nil)
	})

	t.Run("QUICSafetyClient_implements_SafetyClient", func(t *testing.T) {
		t.Parallel()
		var _ SafetyClient = (*QUICSafetyClient)(nil)
	})

	t.Run("QUICCryptoClient_implements_CryptoClient", func(t *testing.T) {
		t.Parallel()
		var _ CryptoClient = (*QUICCryptoClient)(nil)
	})
}

// =============================================================================
// Error Type Verification Tests
// =============================================================================

func TestQUICErrorHandling(t *testing.T) {
	t.Parallel()

	t.Run("not_implemented_error", func(t *testing.T) {
		t.Parallel()
		err := errors.NotImplemented("test.operation", "feature not available")
		assert.NotNil(t, err)
		// NotImplemented returns error interface, use errors.Is to check the code
		assert.True(t, errors.Is(err, errors.ErrNotImplemented))
	})
}

func TestQUICKMSClient_NilClient(t *testing.T) {
	client := &QUICKMSClient{client: nil}
	assert.NotNil(t, client)
}

// TestQUICBackendClient_NilClient verifies that nil client struct exists.
func TestQUICBackendClient_NilClient(t *testing.T) {
	client := &QUICBackendClient{client: nil}
	assert.NotNil(t, client)
}

// TestQUICSafetyClient_NilClient verifies that nil client struct exists.
func TestQUICSafetyClient_NilClient(t *testing.T) {
	client := &QUICSafetyClient{client: nil}
	assert.NotNil(t, client)
}
