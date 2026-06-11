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

// initialize_node_failure_tt_test.go covers the standalone-node error return of
// Server.initialize: when the qrdb engine node fails to construct, initialize
// aborts after startStorage and before any later subsystem. A not-compiled
// hardware seal strategy is the hermetic way to make node construction fail
// (buildHardwareSealStrategies rejects it before any TPM/HSM is touched).

package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestServer_Initialize_TT_StandaloneNodeFailurePropagates drives the
// standalone-node init error return of initialize via a not-compiled hardware
// seal strategy.
func TestServer_Initialize_TT_StandaloneNodeFailurePropagates(t *testing.T) {
	if _, registered := hardwareStrategyFactories["awskms"]; registered {
		t.Fatal("awskms must not be compiled into the default build for this test")
	}

	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.Seal.Enabled = true
	srv.config.Seal.Strategy = "awskms" // build-tagged, absent → node init fails

	err := srv.initialize()
	require.Error(t, err, "initialize must abort when the standalone node fails to construct")
	var notCompiled *StrategyNotCompiledError
	require.ErrorAs(t, err, &notCompiled,
		"the node-init failure must surface the not-compiled hardware strategy error")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	_ = errors.Unwrap(err)
}
