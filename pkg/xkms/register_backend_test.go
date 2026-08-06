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

// register_backend_test.go covers RegisterBackend, the boot-order seam that
// lets startKMS add a resolved backend (e.g. tpm2) to a global service that
// initializeCA already initialized from the CA key universe.

package xkms

import (
	"context"
	"errors"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// newTestServiceBackend builds an in-memory unified software ServiceBackend
// (asymmetric PKCS#8 + symmetric AES) for registration tests.
func newTestServiceBackend(t *testing.T) xkmssdk.ServiceBackend {
	t.Helper()

	provider, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{
		KeyStorage: xkmssdk.NewStorageMemory(),
	})
	require.NoError(t, err)

	certStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)

	sb, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     provider,
		CertStorage: certStorage,
	})
	require.NoError(t, err)
	return sb
}

// RegisterBackend must add a new named backend to the running service so it
// becomes resolvable and fully operational (symmetric key generation).
func TestRegisterBackend_AddsBackendToRunningService(t *testing.T) {
	setupFullTestXKMS(t)
	t.Cleanup(Reset)

	added := newTestServiceBackend(t)
	require.NoError(t, RegisterBackend("memory", added))

	assert.Contains(t, Backends(), "memory")

	got, err := GetBackend("memory")
	require.NoError(t, err)
	assert.Same(t, added, got)

	// The added backend must be usable end-to-end, not just listed: generate
	// a symmetric key through the package API against the new name.
	attrs := symmetricKeyAttrs("register-backend-sym-key")
	key, err := GenerateSymmetricKey(context.Background(), "memory", attrs)
	require.NoError(t, err)
	assert.NotNil(t, key)
}

// RegisterBackend before Initialize must fail closed with the typed
// not-initialized error — never a silent no-op.
func TestRegisterBackend_NotInitialized(t *testing.T) {
	Reset()

	err := RegisterBackend("memory", newTestServiceBackend(t))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotInitialized),
		"registration on an uninitialized service must surface ErrNotInitialized, got %v", err)
}

// An invalid backend name must surface the typed registration error wrapping
// go-xkms's validation failure.
func TestRegisterBackend_InvalidNameIsTypedError(t *testing.T) {
	setupFullTestXKMS(t)
	t.Cleanup(Reset)

	err := RegisterBackend("bad name!", newTestServiceBackend(t))
	require.Error(t, err)

	var regErr *api.XKMSBackendRegistrationError
	require.True(t, errors.As(err, &regErr),
		"invalid name must surface *api.XKMSBackendRegistrationError, got %v", err)
	assert.Equal(t, "bad name!", regErr.Backend)
	assert.NotNil(t, regErr.Unwrap())
}
