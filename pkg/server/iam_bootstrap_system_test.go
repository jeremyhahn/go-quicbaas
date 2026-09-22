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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestRunBootstrapSystem_ProtectionFailureIsFatal is the verification #960
// asked for by name.
//
// go-qrdb#429 made BootstrapSystemConfig.Passphrase a *secure.Buffer, and the
// buffer needs mlocked pages the kernel can refuse (it also always fails on a
// GOEXPERIMENT=boringcrypto build). The tempting handling is to carry on with
// no passphrase — and that is the trap: BootstrapSystem would then bring up a
// StrategyNone barrier that reports itself UNSEALED, so an operator who asked
// for a passphrase barrier gets plaintext at rest, permanently, with nothing
// said about it. A silent downgrade of an at-rest guarantee is worse than a
// refused boot.
//
// So: a typed error, and the operator's passphrase wiped either way.
func TestRunBootstrapSystem_ProtectionFailureIsFatal(t *testing.T) {
	cfg := config.Default()
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "passphrase"
	cfg.Storage.DataDir = t.TempDir()

	svc, barrier, _, closer, err := buildDataPlaneBarrierService(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, barrier)
	require.NotNil(t, svc)
	defer func() { assert.NoError(t, closer.Close()) }()

	sentinel := errors.New("mlock refused: cannot allocate locked pages")
	restore := newProtectedPassphrase
	newProtectedPassphrase = func([]byte) (*secure.Buffer, error) { return nil, sentinel }
	defer func() { newProtectedPassphrase = restore }()

	s := &Server{
		config:                  cfg,
		dataPlaneBarrier:        barrier,
		bootstrapSealPassphrase: []byte("operator-seal-passphrase"),
	}

	// The failure happens before deps is read, so an empty value suffices and
	// keeps the test focused on the branch under test.
	result, err := s.runBootstrapSystem(t.Context(), &qrdbsdk.FirstBootDependencies{})

	require.Error(t, err, "an unusable passphrase buffer must refuse the boot")
	assert.Nil(t, result, "no first-boot result may be produced on this path")

	var typed *FirstBootBootstrapError
	require.ErrorAs(t, err, &typed, "the caller must be able to tell this case apart")
	assert.Equal(t, "protect seal passphrase", typed.Op)
	assert.ErrorIs(t, err, ErrFirstBootBootstrap, "the classification sentinel must be reachable")
	// FirstBootBootstrapError.Unwrap deliberately returns the sentinel rather
	// than the cause, so the cause is reached through the field, not errors.Is.
	assert.Same(t, sentinel, typed.Cause, "the underlying cause must be preserved")

	assert.Nil(t, s.bootstrapSealPassphrase,
		"the operator's passphrase copy must be released even on the failure path")
}
