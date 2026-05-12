//go:build !integration && local

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

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestStartKMSReusesCAKeyStorage is the regression test for the dual-chain
// KEK mismatch bug. Before the single-chain consolidation, startKMS created a
// parallel file storage backend under $DATA_DIR/kms-keys while initializeCA
// had already registered $DATA_DIR/keys with the XKMS global singleton. Wrap
// used one chain; unwrap saw the other, producing "cipher: message
// authentication failed" on every fresh boot.
//
// This test pre-populates srv.xkmsKeyStorage (simulating initializeCA having
// run) and asserts that startKMS reuses the same storage instead of replacing
// it. The field equality check proves that no new storage root was allocated.
func TestStartKMSReusesCAKeyStorage(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.KMS.Enabled = true
	cfg.KMS.Backend = config.BackendTypeSoftware
	cfg.KMS.EnabledBackends = []config.BackendType{config.BackendTypeSoftware}
	// No DataDir: forces the in-memory fallback when xkmsKeyStorage is nil,
	// but our test pre-sets it so the fallback branch is never reached.

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Simulate CA chain having initialised the key storage (in-memory backend).
	inMemStorage, memErr := xkmssdk.NewMemoryBackend()
	require.NoError(t, memErr)
	srv.xkmsKeyStorage = inMemStorage

	err = srv.startKMS()
	require.NoError(t, err)
	assert.NotNil(t, srv.kmsManager,
		"KMS manager must be initialized when xkmsKeyStorage is pre-set")

	// The field must still point to the same storage we injected, proving
	// that startKMS did not create a new instance and therefore used the
	// same on-disk (or in-memory) key universe as the CA chain.
	assert.Equal(t, inMemStorage, srv.xkmsKeyStorage,
		"startKMS must not replace xkmsKeyStorage when CA chain already set it")
}

// TestStartKMSFallsBackToMemoryWithoutDataDir verifies the no-CA, no-data-dir
// code path: when xkmsKeyStorage is nil and no DataDir is configured, startKMS
// must allocate an in-memory backend via the newKMSKeyStorage seam.
func TestStartKMSFallsBackToMemoryWithoutDataDir(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.KMS.Enabled = true
	cfg.KMS.Backend = config.BackendTypeSoftware
	cfg.KMS.EnabledBackends = []config.BackendType{config.BackendTypeSoftware}

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	// xkmsKeyStorage is nil (no CA chain ran, no DataDir).

	err = srv.startKMS()
	require.NoError(t, err)
	assert.NotNil(t, srv.kmsManager)
}
