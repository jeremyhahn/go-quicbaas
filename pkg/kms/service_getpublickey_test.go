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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// KMSService.GetPublicKey Tests
// =============================================================================

func TestKMSService_GetPublicKey_EmptyKeyID(t *testing.T) {
	backend := NewMockBackend()
	manager, err := NewKMSService(&KMSServiceOptions{
		Registry: registryWithBackend(backend),
	})
	require.NoError(t, err)
	defer manager.Close()

	_, err = manager.GetPublicKey(context.Background(), "")
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, errors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dragonErr.Code)
	assert.Contains(t, dragonErr.Error(), "keyID cannot be empty")
}

func TestKMSService_GetPublicKey_CancelledContext(t *testing.T) {
	backend := NewMockBackend()
	manager, err := NewKMSService(&KMSServiceOptions{
		Registry: registryWithBackend(backend),
	})
	require.NoError(t, err)
	defer manager.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err = manager.GetPublicKey(ctx, "test-key")
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, errors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrTimeout, dragonErr.Code)
}

func TestKMSService_GetPublicKey_AsymmetricKey(t *testing.T) {
	backend := NewMockBackend()
	manager, err := NewKMSService(&KMSServiceOptions{
		Registry: registryWithBackend(backend),
	})
	require.NoError(t, err)
	defer manager.Close()

	// Create an asymmetric key first
	ctx := context.Background()
	attrs := testKeyAttrs("rsa-key-1", AlgRSA2048)
	_, err = backend.CreateKey(ctx, attrs)
	require.NoError(t, err)

	// Now get its public key
	pubKey, err := manager.GetPublicKey(ctx, "rsa-key-1")
	require.NoError(t, err)
	assert.NotNil(t, pubKey)
	assert.Contains(t, string(pubKey), "mock-public-key-rsa-key-1")
}

func TestKMSService_GetPublicKey_KeyNotFound(t *testing.T) {
	backend := NewMockBackend()
	manager, err := NewKMSService(&KMSServiceOptions{
		Registry: registryWithBackend(backend),
	})
	require.NoError(t, err)
	defer manager.Close()

	_, err = manager.GetPublicKey(context.Background(), "nonexistent-key")
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, errors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrNotFound, dragonErr.Code)
}

func TestKMSService_GetPublicKey_SymmetricKeyError(t *testing.T) {
	backend := NewMockBackend()
	manager, err := NewKMSService(&KMSServiceOptions{
		Registry: registryWithBackend(backend),
	})
	require.NoError(t, err)
	defer manager.Close()

	// Create a symmetric key (AES)
	ctx := context.Background()
	attrs := testKeyAttrs("aes-key-1", AlgAES256GCM)
	_, err = backend.CreateKey(ctx, attrs)
	require.NoError(t, err)

	// Symmetric keys don't have public keys
	_, err = manager.GetPublicKey(ctx, "aes-key-1")
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, errors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrInternal, dragonErr.Code)
}

func TestKMSService_GetPublicKey_BackendError(t *testing.T) {
	backend := NewMockBackend()
	manager, err := NewKMSService(&KMSServiceOptions{
		Registry: registryWithBackend(backend),
	})
	require.NoError(t, err)
	defer manager.Close()

	// Create a key then inject an error
	ctx := context.Background()
	attrs := testKeyAttrs("error-key", AlgRSA2048)
	_, err = backend.CreateKey(ctx, attrs)
	require.NoError(t, err)

	backend.SetError("ExportPublicKey", errors.New("backend failure"))

	_, err = manager.GetPublicKey(ctx, "error-key")
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, errors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrInternal, dragonErr.Code)
}

func TestKMSService_GetPublicKey_BackendUnavailable(t *testing.T) {
	backend := NewMockBackend()
	registry := NewBackendRegistry(nil)
	err := registry.Register("mock", backend, 1)
	require.NoError(t, err)

	manager, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)
	defer manager.Close()

	// Disable all backends
	err = registry.Disable("mock")
	require.NoError(t, err)

	_, err = manager.GetPublicKey(context.Background(), "some-key")
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, errors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrUnavailable, dragonErr.Code)
}

func TestKMSService_GetPublicKey_ECDSAKey(t *testing.T) {
	backend := NewMockBackend()
	manager, err := NewKMSService(&KMSServiceOptions{
		Registry: registryWithBackend(backend),
	})
	require.NoError(t, err)
	defer manager.Close()

	// Create an ECDSA key
	ctx := context.Background()
	attrs := testKeyAttrs("ecdsa-key", AlgECDSAP256)
	_, err = backend.CreateKey(ctx, attrs)
	require.NoError(t, err)

	pubKey, err := manager.GetPublicKey(ctx, "ecdsa-key")
	require.NoError(t, err)
	assert.NotNil(t, pubKey)
}
