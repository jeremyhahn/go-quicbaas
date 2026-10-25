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
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestServerSealedProbe_NoBarrierReportsUnsealed pins the zero state: a server
// with neither an embedded qrdb node nor a data-plane barrier protects nothing,
// so it must report unsealed. Reporting sealed here would arm the sealed-window
// deny gates on every transport of a daemon that has no barrier at all.
func TestServerSealedProbe_NoBarrierReportsUnsealed(t *testing.T) {
	probe := (&Server{}).SealedWindowProbe()
	assert.False(t, probe.IsSealed(),
		"a server with no node and no data-plane barrier must report unsealed")
}

// TestServerSealedProbe_UninitializedDataPlaneBarrierReportsUnsealed is the
// decisive check for the seal-disabled path: a never-initialized StorageBarrier
// reports state "sealed" internally (stateSealed is the zero value) but
// protects nothing, and gating on it would arm the sealed-window deny gates on
// every first boot.
//
// This is DELIBERATE, not a virgin-node bootstrap deadlock: this path is only
// reached when the embedded qrdb node owns no barrier (seal disabled), and with
// seal disabled there is no barrier lifecycle to bootstrap. A seal-ENABLED boot
// takes the node branch, where a virgin barrier DOES report sealed - see
// TestServerSealedProbe_InitializedSealedDataPlaneBarrierReportsSealed for the
// initialized half of the same rule.
func TestServerSealedProbe_UninitializedDataPlaneBarrierReportsUnsealed(t *testing.T) {
	cfg := config.Default()
	cfg.Seal.Enabled = false
	cfg.Seal.Strategy = "passphrase"
	cfg.Storage.DataDir = t.TempDir()

	svc, barrier, _, closer, err := buildDataPlaneBarrierService(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, barrier)
	require.NotNil(t, closer)
	defer func() { assert.NoError(t, closer.Close()) }()

	// Precondition: the barrier is internally sealed but carries no
	// initialization timestamp.
	status, statusErr := svc.BarrierStatus(t.Context())
	require.NoError(t, statusErr)
	require.True(t, status.Sealed, "a fresh barrier is internally sealed")
	require.True(t, status.InitializedAt.IsZero(), "a fresh barrier is not initialized")

	probe := (&Server{dataPlaneBarrier: barrier}).SealedWindowProbe()
	assert.False(t, probe.IsSealed(),
		"an uninitialized data-plane barrier protects nothing and must report unsealed")
}

// TestServerSealedProbe_InitializedSealedDataPlaneBarrierReportsSealed is the
// other half of the rule: once the barrier carries real protected state,
// sealing it MUST arm the sealed window so the surface collapses to the
// unseal-flow allowlist.
func TestServerSealedProbe_InitializedSealedDataPlaneBarrierReportsSealed(t *testing.T) {
	cfg := config.Default()
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "passphrase"
	cfg.Storage.DataDir = t.TempDir()

	svc, barrier, _, closer, err := buildDataPlaneBarrierService(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, barrier)
	require.NotNil(t, closer)
	defer func() { assert.NoError(t, closer.Close()) }()

	ctx := t.Context()
	creds, err := qrdbsdk.CredentialsFor(ctx, []byte("sealed-probe-unit-test-passphrase"))
	require.NoError(t, err, "protect the probe passphrase")
	defer creds.Secret.Destroy()
	require.NoError(t, barrier.Initialize(ctx, creds), "initialize the data-plane barrier")

	server := &Server{dataPlaneBarrier: barrier}
	probe := server.SealedWindowProbe()

	// Initialized and unsealed: full enforcement, no sealed window. This is
	// exactly the state a cloud/hardware-strategy daemon boots into, because
	// the node auto-initializes and auto-unseals its barrier at startup.
	assert.False(t, probe.IsSealed(),
		"an initialized, unsealed barrier must report unsealed")

	require.NoError(t, barrier.Seal(), "seal the initialized barrier")

	status, statusErr := svc.BarrierStatus(ctx)
	require.NoError(t, statusErr)
	require.True(t, status.Sealed)
	require.True(t, status.Initialized,
		"a sealed-after-init barrier must still report itself initialized")

	assert.True(t, probe.IsSealed(),
		"an initialized barrier that is sealed must arm the sealed window")
}
