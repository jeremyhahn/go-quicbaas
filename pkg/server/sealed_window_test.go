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

package server

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// newProbeTestBarrier constructs a real in-memory StorageBarrier for probe
// state-machine tests (deferred software strategy: init with a passphrase,
// seal/unseal at will).
func newProbeTestBarrier(t *testing.T) *qrdbsdk.StorageBarrier {
	t.Helper()
	barrier, err := qrdbsdk.NewStorageBarrier(
		slog.Default(),
		qrdbsdk.NewMemoryBackend(),
		qrdbsdk.BarrierConfig{RootKeyPath: "test/root-key"},
		qrdbsdk.NewDeferredSoftwareStrategy(),
	)
	require.NoError(t, err)
	return barrier
}

// TestSealedWindowProbe_NoBarrierUnsealed asserts a server with neither a
// qrdb node nor a data-plane barrier reports unsealed: the probe must never
// gate a boot that has nothing to protect.
func TestSealedWindowProbe_NoBarrierUnsealed(t *testing.T) {
	s := &Server{}
	assert.False(t, s.SealedWindowProbe().IsSealed())
}

// TestSealedWindowProbe_DataPlaneBarrierStates walks the probe through the
// data-plane barrier state machine (the seal-disabled boot path):
//
//   - uninitialized barrier: internally "sealed" but protecting nothing —
//     the probe must report UNSEALED so first boots keep the full surface;
//   - initialized + unsealed: unsealed;
//   - initialized + sealed (post-boot `barrier seal`): SEALED — the gates
//     re-narrow the surface;
//   - unsealed again: unsealed.
func TestSealedWindowProbe_DataPlaneBarrierStates(t *testing.T) {
	barrier := newProbeTestBarrier(t)
	s := &Server{dataPlaneBarrier: barrier}
	probe := s.SealedWindowProbe()

	// Uninitialized: never gate.
	assert.False(t, probe.IsSealed(), "uninitialized barrier must not gate")

	// Initialize (deferred software strategy unseals on init).
	ctx := context.Background()
	require.NoError(t, barrier.Initialize(ctx, qrdbsdk.Credentials{Secret: []byte("test-passphrase")}))
	assert.False(t, probe.IsSealed(), "initialized+unsealed must not gate")

	// Seal: the probe reports sealed — every transport gate engages.
	require.NoError(t, barrier.Seal())
	assert.True(t, probe.IsSealed(), "initialized+sealed must gate")

	// Unseal: pass-through again.
	require.NoError(t, barrier.Unseal(ctx, qrdbsdk.Credentials{Secret: []byte("test-passphrase")}))
	assert.False(t, probe.IsSealed(), "re-unsealed must not gate")
}

// TestRunSealedWindow_EmbeddedModeRefused asserts embedded (library) mode
// refuses a sealed-window boot with the typed EmbeddedSealedBootError
// instead of blocking the embedder or panicking at the first GetKVStore.
func TestRunSealedWindow_EmbeddedModeRefused(t *testing.T) {
	cfg := config.Default()
	cfg.Server.Embedded = true
	s := &Server{config: cfg, logger: newGRPCAuthzTestLogger(t)}

	err := s.runSealedWindow()
	var embErr *EmbeddedSealedBootError
	require.ErrorAs(t, err, &embErr)
	assert.Contains(t, err.Error(), "WithSealPassphrase")
}

// TestNewSealedWindowClient_MissingServices asserts the sealed-window client
// composer fails closed with a typed error when the node's seal-layer
// services are incomplete.
func TestNewSealedWindowClient_MissingServices(t *testing.T) {
	s := &Server{}

	for _, cs := range []*CeremonyServices{
		nil,
		{},
		{Barrier: &strategyAwareBarrierService{}},
	} {
		client, err := newSealedWindowClient(s, cs)
		assert.Nil(t, client)
		var bootErr *SealedWindowBootError
		require.ErrorAs(t, err, &bootErr)
		assert.Equal(t, "node seal services", bootErr.Stage)
	}
}

// TestSealedNodeUnavailableError_Message pins the typed proxy error wording.
func TestSealedNodeUnavailableError_Message(t *testing.T) {
	err := &SealedNodeUnavailableError{Service: "kv"}
	assert.Equal(t,
		"sealed window: qrdb node kv service unavailable until the barrier is unsealed",
		err.Error())
}

// TestSealedServicerProxies_FailClosedWhileSealed asserts the lazy KV /
// Admin proxies fail with the typed SealedNodeUnavailableError while the
// node has no data plane — except Health, which reports the documented
// "sealed" verdict so SDK clients can connect during the window.
func TestSealedServicerProxies_FailClosedWhileSealed(t *testing.T) {
	s := &Server{} // no qrdbNode: GetQRDBKVServicer / GetQRDBAdminServicer return nil
	ctx := context.Background()

	kv := newSealedKVServicerProxy(s)
	_, err := kv.Get(ctx, "k")
	var unavailable *SealedNodeUnavailableError
	require.ErrorAs(t, err, &unavailable)
	assert.Equal(t, "kv", unavailable.Service)
	require.ErrorAs(t, kv.Put(ctx, "k", []byte("v")), &unavailable)
	_, err = kv.List(ctx, "")
	require.ErrorAs(t, err, &unavailable)

	admin := newSealedAdminServicerProxy(s)
	_, err = admin.Status(ctx)
	require.ErrorAs(t, err, &unavailable)
	assert.Equal(t, "admin", unavailable.Service)

	// Health must succeed with the sealed verdict: the qrdb SDK client
	// probes it at connect time and would otherwise refuse to connect
	// during the sealed window.
	health, err := admin.Health(ctx)
	require.NoError(t, err)
	require.NotNil(t, health)
	assert.Equal(t, "sealed", health.Status)
}

// TestConstantSealedProbe asserts the sealed-phase listeners' probe pins
// sealed for the listener's whole lifetime.
func TestConstantSealedProbe(t *testing.T) {
	assert.True(t, constantSealedProbe{}.IsSealed())
}

// TestSealedWindowBootError_Unwrap asserts error wrapping semantics.
func TestSealedWindowBootError_Unwrap(t *testing.T) {
	cause := &SealedNodeUnavailableError{Service: "kv"}
	err := &SealedWindowBootError{Stage: "sealed sdk client", Cause: cause}
	assert.Contains(t, err.Error(), "sealed sdk client")
	assert.Contains(t, err.Error(), cause.Error())
	var inner *SealedNodeUnavailableError
	assert.ErrorAs(t, err, &inner)

	noCause := &SealedWindowBootError{Stage: "node seal services"}
	assert.Equal(t, "sealed window: node seal services failed", noCause.Error())
}
