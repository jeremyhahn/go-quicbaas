//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

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

// lifecycle_ca_backend_test.go covers Phase P1 of configurable-crypto-backend:
// the CA SIGNING-key store type is driven by crypto config / the resolver
// instead of being hardcoded software. The TLS server key MUST stay software,
// and default (empty/auto) deployments MUST remain byte-for-byte software on
// every host — including TPM-equipped hosts (see resolveCAStoreType). These
// tests assert the RESOLVED StoreType; they never require a live TPM.

package server

import (
	"errors"
	"path/filepath"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestResolveCASigningSpec_Precedence asserts the CA signing-key spec precedence:
// crypto.subsystems.ca -> crypto.backend -> ca.keystore_type (legacy) -> auto,
// and that KeyRef falls back from the CA subsystem ref to the process-wide ref.
func TestResolveCASigningSpec_Precedence(t *testing.T) {
	cases := []struct {
		name        string
		mutate      func(c *config.Config)
		wantBackend config.BackendType
		wantKeyRef  string
	}{
		{
			name:        "default is auto/empty",
			mutate:      func(c *config.Config) {},
			wantBackend: "",
		},
		{
			name: "legacy ca.keystore_type used when crypto config absent",
			mutate: func(c *config.Config) {
				c.CA.KeystoreType = string(config.BackendTypeTPM2)
			},
			wantBackend: config.BackendTypeTPM2,
		},
		{
			name: "crypto.backend (process default) beats legacy ca.keystore_type",
			mutate: func(c *config.Config) {
				c.CA.KeystoreType = string(config.BackendTypeSoftware)
				c.Crypto.Backend = config.BackendTypeTPM2
			},
			wantBackend: config.BackendTypeTPM2,
		},
		{
			name: "crypto.subsystems.ca beats crypto.backend and legacy",
			mutate: func(c *config.Config) {
				c.CA.KeystoreType = string(config.BackendTypeSoftware)
				c.Crypto.Backend = config.BackendTypeSoftware
				c.Crypto.Subsystems.CA.Backend = config.BackendTypeTPM2
			},
			wantBackend: config.BackendTypeTPM2,
		},
		{
			name: "keyref: subsystem ref wins over process ref",
			mutate: func(c *config.Config) {
				c.Crypto.KeyRef = "process-ref"
				c.Crypto.Subsystems.CA.KeyRef = "ca-ref"
			},
			wantKeyRef: "ca-ref",
		},
		{
			name: "keyref: process ref used when subsystem ref empty",
			mutate: func(c *config.Config) {
				c.Crypto.KeyRef = "process-ref"
			},
			wantKeyRef: "process-ref",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := createTestConfig(t)
			tc.mutate(cfg)
			srv, err := NewServer(cfg)
			require.NoError(t, err)

			spec := srv.resolveCASigningSpec()
			require.Equal(t, tc.wantBackend, spec.Backend, "resolved CA backend")
			require.Equal(t, tc.wantKeyRef, spec.KeyRef, "resolved CA key ref")
		})
	}
}

// TestResolveCAStoreType is the load-bearing assertion: the StoreType that
// initializeCA stamps on BOTH keyConfig.StoreType and the issuing-identity
// KeystoreType. No live TPM is touched — store-type resolution is pure config
// mapping, never a device open.
func TestResolveCAStoreType(t *testing.T) {
	t.Run("default (empty/auto) stays software on every host", func(t *testing.T) {
		// Even on a TPM-equipped host, the CA default must remain software so
		// existing deployments are byte-for-byte unchanged and TLS issuance is
		// never dragged onto a tpm2: key id. Hardware auto-detect is not applied
		// to the CA.
		cfg := createTestConfig(t)
		srv, err := NewServer(cfg)
		require.NoError(t, err)

		st, err := srv.resolveCAStoreType()
		require.NoError(t, err)
		require.Equal(t, xkmssdk.StoreSoftware, st)
	})

	t.Run("explicit software resolves to StoreSoftware", func(t *testing.T) {
		cfg := createTestConfig(t)
		cfg.CA.KeystoreType = string(config.BackendTypeSoftware)
		srv, err := NewServer(cfg)
		require.NoError(t, err)

		st, err := srv.resolveCAStoreType()
		require.NoError(t, err)
		require.Equal(t, xkmssdk.StoreSoftware, st)
	})

	t.Run("legacy ca.keystore_type=tpm2 resolves to StoreTPM2", func(t *testing.T) {
		cfg := createTestConfig(t)
		cfg.CA.KeystoreType = string(config.BackendTypeTPM2)
		srv, err := NewServer(cfg)
		require.NoError(t, err)

		st, err := srv.resolveCAStoreType()
		require.NoError(t, err)
		require.Equal(t, xkmssdk.StoreTPM2, st)
	})

	t.Run("crypto.subsystems.ca=tpm2 resolves to StoreTPM2", func(t *testing.T) {
		cfg := createTestConfig(t)
		cfg.Crypto.Subsystems.CA.Backend = config.BackendTypeTPM2
		srv, err := NewServer(cfg)
		require.NoError(t, err)

		st, err := srv.resolveCAStoreType()
		require.NoError(t, err)
		require.Equal(t, xkmssdk.StoreTPM2, st)
	})

	t.Run("uncompiled backend surfaces BackendNotCompiledError (no software fallback)", func(t *testing.T) {
		cfg := createTestConfig(t)
		cfg.Crypto.Subsystems.CA.Backend = config.BackendTypePKCS11
		srv, err := NewServer(cfg)
		require.NoError(t, err)

		_, err = srv.resolveCAStoreType()
		require.Error(t, err)
		var notCompiled *BackendNotCompiledError
		require.True(t, errors.As(err, &notCompiled),
			"pkcs11 without -tags pkcs11 must surface BackendNotCompiledError, got %T: %v", err, err)
		require.Equal(t, config.BackendTypePKCS11, notCompiled.Backend)
	})
}

// TestInitializeCA_DefaultIsSoftware_NoRegression runs the full CA bootstrap on a
// default (no crypto config) deployment and asserts it succeeds with a working
// software-backed CA and TLS server key — the byte-for-byte historical path.
// This is the regression guard for TPM-equipped hosts: the CA must not auto-
// upgrade to tpm2 and break the (always-software) TLS server key.
func TestInitializeCA_DefaultIsSoftware_NoRegression(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.Security.TLSEnabled = true
	cfg.Storage.DataDir = t.TempDir()
	cfg.CA.KeysDir = filepath.Join(t.TempDir(), "ca-keys")
	cfg.Security.TLSCAFile = filepath.Join(t.TempDir(), "ca-chain.crt")

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	require.NoError(t, srv.initializeCA(), "default-config CA bootstrap must succeed")
	require.NotNil(t, srv.CA(), "CA must be initialized")
	require.NotNil(t, srv.tlsConfig, "TLS server key/cert (always software) must be wired")

	st, stErr := srv.resolveCAStoreType()
	require.NoError(t, stErr)
	require.Equal(t, xkmssdk.StoreSoftware, st, "default CA store must be software")
}

// TestInitializeCA_ExplicitSoftwareHardwareDefaultStillSoftware proves the
// precedence guard without a live TPM: a process-wide crypto.backend=tpm2 is
// pinned back to software for the CA via the per-subsystem override, so the full
// software bootstrap (CA + TLS server key) still runs and succeeds. The CA
// subsystem override must beat the process default.
func TestInitializeCA_ExplicitSoftwareHardwareDefaultStillSoftware(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.Security.TLSEnabled = true
	cfg.Storage.DataDir = t.TempDir()
	cfg.CA.KeysDir = filepath.Join(t.TempDir(), "ca-keys")
	cfg.Security.TLSCAFile = filepath.Join(t.TempDir(), "ca-chain.crt")

	cfg.Crypto.Backend = config.BackendTypeTPM2
	cfg.Crypto.Subsystems.CA.Backend = config.BackendTypeSoftware

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	st, stErr := srv.resolveCAStoreType()
	require.NoError(t, stErr)
	require.Equal(t, xkmssdk.StoreSoftware, st, "subsystem override must beat process default")

	require.NoError(t, srv.initializeCA(), "software-pinned CA bootstrap must succeed")
	require.NotNil(t, srv.tlsConfig, "TLS server key (always software) must be wired")
}

// TestCABackendForStore_SoftwareReturnsProvidedBackend asserts the per-key CA
// backend closure maps the software StoreType straight back to the supplied
// software backend without consulting the resolver. This is what keeps the
// CA's leaf/TLS keys on the exportable software keystore even when the CA
// signing key is routed to hardware.
func TestCABackendForStore_SoftwareReturnsProvidedBackend(t *testing.T) {
	cfg := createTestConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	resolver := NewBackendResolver(cfg)
	swBackend, err := resolver.ServiceBackend(BackendSpec{Backend: config.BackendTypeSoftware})
	require.NoError(t, err)

	bfs := srv.caBackendForStore(swBackend, BackendSpec{Backend: config.BackendTypeSoftware}, resolver)
	got, err := bfs("QuicBaaS Root CA", xkmssdk.StoreSoftware)
	require.NoError(t, err)
	require.True(t, got == swBackend, "software StoreType must return the provided software backend unchanged")
}

// TestCABackendForStore_NonSoftwareDelegatesToResolver asserts that a
// non-software StoreType delegates to the central BackendResolver rather than
// silently downgrading to software. With pkcs11 not compiled into the test
// binary the resolver surfaces a typed BackendNotCompiledError, proving the
// closure routes through the single crypto selector. No hardware is touched.
func TestCABackendForStore_NonSoftwareDelegatesToResolver(t *testing.T) {
	cfg := createTestConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	resolver := NewBackendResolver(cfg)
	swBackend, err := resolver.ServiceBackend(BackendSpec{Backend: config.BackendTypeSoftware})
	require.NoError(t, err)

	bfs := srv.caBackendForStore(swBackend, BackendSpec{Backend: config.BackendTypePKCS11}, resolver)
	_, err = bfs("QuicBaaS Root CA", xkmssdk.StoreTPM2)
	require.Error(t, err)
	var notCompiled *BackendNotCompiledError
	require.True(t, errors.As(err, &notCompiled),
		"non-software CA store must delegate to the resolver and surface BackendNotCompiledError, got %T: %v", err, err)
	require.Equal(t, config.BackendTypePKCS11, notCompiled.Backend)
}
