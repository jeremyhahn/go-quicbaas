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

//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

// lifecycle_kms_backend_test.go covers the P0 silent-downgrade fix: startKMS now
// resolves the configured crypto backend through the central BackendResolver
// instead of unconditionally constructing a software backend. These tests prove:
//   - software / "" / auto-with-no-hardware still resolve to software (unchanged);
//   - a configured hardware backend name makes the resolver pick THAT backend and
//     threads the persistent key store into construction;
//   - an uncompiled backend surfaces BackendNotCompiledError (never silent
//     software) and leaves no KMS manager behind.
//
// Real TPM/HSM hardware is unavailable in unit tests, so the construction step is
// intercepted via the buildKMSServiceBackendFn seam: it captures the spec +
// store the resolver was asked to build and substitutes a software backend so
// downstream XKMS wiring succeeds. The resolver's TYPE decision (Resolve) runs
// for real and needs no hardware.

package server

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// nonexistentTPMDevice is a device path guaranteed not to exist so DetectHardware
// reports the TPM absent regardless of the test host (keeps the auto-detect tests
// deterministic and hardware-free).
const nonexistentTPMDevice = "/nonexistent/quicbaas-test-tpm"

// captureKMSBackendBuild installs a buildKMSServiceBackendFn seam that records
// the spec and key store startKMS hands to construction, then returns a real
// software ServiceBackend (built with the same store) so the rest of startKMS
// proceeds. The returned pointers are read after startKMS completes.
func captureKMSBackendBuild(t *testing.T) (*BackendSpec, *xkmssdk.StorageBackend) {
	t.Helper()
	var gotSpec BackendSpec
	var gotStore xkmssdk.StorageBackend
	buildKMSServiceBackendFn = func(r *BackendResolver, spec BackendSpec, store xkmssdk.StorageBackend) (xkmssdk.ServiceBackend, error) {
		gotSpec = spec
		gotStore = store
		// Build a software backend with the SAME store so downstream XKMS init
		// succeeds without a real TPM/HSM. The resolver's type decision for the
		// configured backend is asserted separately via Resolve(gotSpec).
		return r.ServiceBackendWithStore(BackendSpec{Backend: config.BackendTypeSoftware}, store)
	}
	return &gotSpec, &gotStore
}

// TestStartKMS_ResolvesConfiguredBackend is the table-driven proof that startKMS
// honors the crypto-backend configuration and its precedence chain instead of
// always using software.
func TestStartKMS_ResolvesConfiguredBackend(t *testing.T) {
	cases := []struct {
		name        string
		cryptoKMS   config.BackendType // crypto.subsystems.kms.backend
		cryptoTop   config.BackendType // crypto.backend
		legacyKMS   config.BackendType // kms.backend
		wantResolve config.BackendType
	}{
		{
			name:        "legacy software (default) stays software",
			legacyKMS:   config.BackendTypeSoftware,
			wantResolve: config.BackendTypeSoftware,
		},
		{
			name:        "empty everywhere auto-detects software (no hardware)",
			wantResolve: config.BackendTypeSoftware,
		},
		{
			name:        "explicit auto-detects software (no hardware)",
			legacyKMS:   config.BackendAuto,
			wantResolve: config.BackendTypeSoftware,
		},
		{
			name:        "per-subsystem override selects tpm2",
			cryptoKMS:   config.BackendTypeTPM2,
			legacyKMS:   config.BackendTypeSoftware, // legacy must NOT win
			wantResolve: config.BackendTypeTPM2,
		},
		{
			name:        "process-wide crypto default selects tpm2",
			cryptoTop:   config.BackendTypeTPM2,
			legacyKMS:   config.BackendTypeSoftware, // legacy must NOT win
			wantResolve: config.BackendTypeTPM2,
		},
		{
			name:        "legacy kms.backend honored when no crypto.* set",
			legacyKMS:   config.BackendTypeTPM2,
			wantResolve: config.BackendTypeTPM2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			saveAndRestoreSeams(t)
			srv := withTempServer(t)
			srv.config.KMS.Enabled = true
			// Force the TPM absent so the auto-detect cases resolve to software.
			srv.config.Seal.TPM2.Device = nonexistentTPMDevice
			srv.config.Crypto.Subsystems.KMS.Backend = tc.cryptoKMS
			srv.config.Crypto.Backend = tc.cryptoTop
			srv.config.KMS.Backend = tc.legacyKMS

			gotSpec, gotStore := captureKMSBackendBuild(t)

			err := srv.startKMS()
			require.NoError(t, err, "startKMS must succeed for a compiled backend")
			require.NotNil(t, srv.kmsManager, "KMS manager must be initialized")

			// The resolver was asked for the configured backend: resolving the
			// captured spec must yield the expected concrete type (no hardware
			// access — Resolve only consults the cached probe + factory map).
			r := NewBackendResolver(srv.config)
			resolved, rErr := r.Resolve(*gotSpec)
			require.NoError(t, rErr)
			assert.Equal(t, tc.wantResolve, resolved,
				"startKMS must resolve to the configured backend, not silently software")

			// The PERSISTENT key store was threaded into construction.
			assert.NotNil(t, *gotStore, "the persistent key store must be threaded into the backend")
		})
	}
}

// TestStartKMS_ThreadsPersistentStore proves the persistent CA-chain key store
// (not a fresh in-memory store) is the one handed to backend construction, so
// KMS keys live in the same on-disk universe across restarts.
func TestStartKMS_ThreadsPersistentStore(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := &config.Config{}
	cfg.KMS.Enabled = true
	cfg.KMS.Backend = config.BackendTypeSoftware
	cfg.Seal.TPM2.Device = nonexistentTPMDevice
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Simulate the CA chain having initialised the shared key storage.
	caStore, memErr := xkmssdk.NewMemoryBackend()
	require.NoError(t, memErr)
	srv.xkmsKeyStorage = caStore

	_, gotStore := captureKMSBackendBuild(t)

	require.NoError(t, srv.startKMS())
	assert.Same(t, caStore, *gotStore,
		"startKMS must thread the CA-chain persistent store into backend construction, not a new store")
}

// TestStartKMS_UncompiledBackendIsTypedError proves a configured-but-not-compiled
// backend (pkcs11 in the default binary) surfaces BackendNotCompiledError and
// does NOT silently fall back to software — the whole point of the P0 fix.
func TestStartKMS_UncompiledBackendIsTypedError(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.KMS.Enabled = true
	srv.config.Seal.TPM2.Device = nonexistentTPMDevice
	srv.config.Crypto.Subsystems.KMS.Backend = config.BackendTypePKCS11

	// Detect a silent fallback: if construction were reached, this would fire.
	constructed := false
	buildKMSServiceBackendFn = func(r *BackendResolver, spec BackendSpec, store xkmssdk.StorageBackend) (xkmssdk.ServiceBackend, error) {
		constructed = true
		return r.ServiceBackendWithStore(BackendSpec{Backend: config.BackendTypeSoftware}, store)
	}

	err := srv.startKMS()
	require.Error(t, err, "an uncompiled backend must abort startKMS")

	var notCompiled *BackendNotCompiledError
	require.True(t, errors.As(err, &notCompiled),
		"the error must surface as *BackendNotCompiledError, got %v", err)
	assert.Equal(t, config.BackendTypePKCS11, notCompiled.Backend)
	assert.False(t, constructed, "startKMS must NOT construct any backend for an uncompiled type")
	assert.Nil(t, srv.kmsManager, "no KMS manager may be left set after a resolve failure")
}

// preInitializeGlobalXKMS resets the process-global xkms singleton and
// re-initializes it with a software instance registered under the given name,
// simulating initializeCA having run first. The singleton is reset again on
// cleanup so later tests see a clean slate.
func preInitializeGlobalXKMS(t *testing.T, name string) {
	t.Helper()

	dbxkms.Reset()
	t.Cleanup(dbxkms.Reset)

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	provider, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{
		KeyStorage: xkmssdk.NewStorageMemory(),
	})
	require.NoError(t, err)
	sb, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     provider,
		CertStorage: xkmssdk.NewStorageMemory(),
	})
	require.NoError(t, err)

	require.NoError(t, dbxkms.Initialize(&dbxkms.Config{
		DefaultBackend: name,
		Backends:       map[string]xkmssdk.ServiceBackend{name: sb},
		Logger:         logger.NewAdapter(log),
	}))
}

// TestStartKMS_AddsResolvedBackendToRunningXKMS proves the initializeCA-first
// boot order: when the global xkms singleton is already initialized WITHOUT
// the KMS-resolved backend name, startKMS must register the resolved backend
// with the running service instead of silently discarding it (which would
// leave the KMS registry riding whatever the earlier registration was).
func TestStartKMS_AddsResolvedBackendToRunningXKMS(t *testing.T) {
	saveAndRestoreSeams(t)
	// Simulate an earlier registration under a DIFFERENT name so the resolved
	// "software" backend is missing from the running service.
	preInitializeGlobalXKMS(t, "tpm2")

	srv := withTempServer(t)
	srv.config.KMS.Enabled = true
	srv.config.KMS.Backend = config.BackendTypeSoftware
	srv.config.Seal.TPM2.Device = nonexistentTPMDevice

	captureKMSBackendBuild(t)

	require.NoError(t, srv.startKMS())
	require.NotNil(t, srv.kmsManager)

	assert.Contains(t, dbxkms.Backends(), string(config.BackendTypeSoftware),
		"startKMS must add the resolved backend to the already-initialized xkms service")
}

// TestStartKMS_ReusesEquivalentRegisteredBackend proves the software fast
// path: when the singleton already carries the resolved backend name (the
// unified software registration initializeCA creates), startKMS must NOT
// replace it — the registration seam stays untouched and startKMS succeeds by
// riding the existing single key universe.
func TestStartKMS_ReusesEquivalentRegisteredBackend(t *testing.T) {
	saveAndRestoreSeams(t)
	preInitializeGlobalXKMS(t, string(config.BackendTypeSoftware))

	registered := false
	registerXKMSBackendFn = func(name string, backend xkmssdk.ServiceBackend) error {
		registered = true
		return nil
	}

	srv := withTempServer(t)
	srv.config.KMS.Enabled = true
	srv.config.KMS.Backend = config.BackendTypeSoftware
	srv.config.Seal.TPM2.Device = nonexistentTPMDevice

	captureKMSBackendBuild(t)

	require.NoError(t, srv.startKMS())
	require.NotNil(t, srv.kmsManager)
	assert.False(t, registered,
		"startKMS must reuse the already-registered software backend, not replace it")
}

// TestStartKMS_XKMSRegistrationFailureIsTypedError proves a failed
// registration on the already-initialized service aborts startKMS with a
// typed error — never a silent degrade onto whichever backends happen to be
// registered.
func TestStartKMS_XKMSRegistrationFailureIsTypedError(t *testing.T) {
	saveAndRestoreSeams(t)
	preInitializeGlobalXKMS(t, "tpm2")

	sentinel := errors.New("boom: registration rejected")
	registerXKMSBackendFn = func(name string, backend xkmssdk.ServiceBackend) error {
		return sentinel
	}

	srv := withTempServer(t)
	srv.config.KMS.Enabled = true
	srv.config.KMS.Backend = config.BackendTypeSoftware
	srv.config.Seal.TPM2.Device = nonexistentTPMDevice

	captureKMSBackendBuild(t)

	err := srv.startKMS()
	require.Error(t, err, "registration failure must abort startKMS")
	assert.True(t, errors.Is(err, sentinel), "the typed error chain must carry the cause, got %v", err)
	assert.Contains(t, err.Error(), "register KMS backend with XKMS")
	assert.Nil(t, srv.kmsManager, "no KMS manager may be left set after a registration failure")
}
