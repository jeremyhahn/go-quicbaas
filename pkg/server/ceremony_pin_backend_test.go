//go:build !integration

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"log/slog"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// newCeremonyTestPINBackend builds the software PIN backend quicbaasd
// selects when no TPM is present.
func newCeremonyTestPINBackend(t *testing.T) qrdbsdk.PINBackend {
	t.Helper()
	pinCfg := &config.Config{}
	pinCfg.Storage.DataDir = t.TempDir()
	backend, err := buildPINBackend(pinCfg, slog.Default(), nil)
	require.NoError(t, err)
	return backend
}

// memoryFallbackCeremonyClient wires the seal-disabled, no-data-dir path the
// way startService does: the bootstrap stack's own ceremony core stays
// selected, the platform CA is bound to it, and the server's PIN backend is
// handed only to newBootstrapCeremonyClient.
func memoryFallbackCeremonyClient(t *testing.T, pinBackend qrdbsdk.PINBackend) *qrdbsdk.Client {
	t.Helper()
	bundle, err := buildBootstrapCeremonyServices(slog.Default(), "", testTrustDomain)
	require.NoError(t, err)

	caSrv := withTempServer(t)
	caSrv.config.IAM.SPIFFETrustDomain = "example.test"
	caSrv.xkmsCA = newFakeTrustedCA(t)
	ca := caSrv.ceremonyCAAdapter()
	require.NotNil(t, ca)
	bundle.Core.SetCA(ca)

	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })
	s := &Server{logger: buildTestLogger(t), qrdbNode: node}

	client, err := newBootstrapCeremonyClient(
		s.GetQRDBKVServicer(), s.GetQRDBAdminServicer(), bundle, pinBackend, ca)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// Initialize with the mandatory SO and User PINs must succeed on the memory
// fallback: the bootstrap ceremony core is the selected core, so it must
// receive the server's PIN backend.
func TestBootstrapCeremony_MemoryFallbackInitializeWithPINs(t *testing.T) {
	client := memoryFallbackCeremonyClient(t, newCeremonyTestPINBackend(t))

	require.NoError(t, runQRDBInit(t, client, []byte("bootstrap-passphrase")),
		"memory-fallback ceremony core must provision the SO/User PINs")
}

// A ceremony core with no PIN backend must refuse Initialize rather than
// provision the PINs nowhere.
func TestBootstrapCeremony_NoPINBackendRefusesInitialize(t *testing.T) {
	client := memoryFallbackCeremonyClient(t, nil)

	err := runQRDBInit(t, client, []byte("bootstrap-passphrase"))
	require.ErrorContains(t, err, "no PIN backend configured")
}
