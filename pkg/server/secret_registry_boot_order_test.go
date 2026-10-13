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

//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

// secret_registry_boot_order_test.go proves that initializeCAFn is invoked
// AFTER the qrdb node has been constructed. This regression prevents any future
// refactor from accidentally placing CA initialization before the credential store
// is available.

package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Boot order regression: CA runs after qrdb standalone node init
// =============================================================================

// TestBootOrder_CARunsAfterStandaloneNodeInit verifies that when
// Server.initialize() completes the standalone-node switch-case, s.qrdbNode is
// already set before initializeCAFn is invoked.
//
// Mechanism: initializeCAFn is stubbed to capture s.qrdbNode at the moment of
// invocation and then return errBootOrderSentinel to abort the rest of init.
// After initialize() returns we assert the captured value was non-nil. A nil
// value would mean the stub ran BEFORE node construction — the old broken order.
func TestBootOrder_CARunsAfterStandaloneNodeInit(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.Embedded = true
	cfg.Server.Mode = "" // empty → resolved to "standalone" by getEffectiveMode
	// Boot order needs no network listener. With any protocol enabled the
	// embedded node binds the fixture's REST/gRPC ports during initialize(),
	// and a port taken by another job on a shared CI host failed this test
	// before the CA seam ran (go-quicbaas#408). Standalone mode has no Raft
	// host, so with these off initialize() binds no socket at all.
	cfg.Server.REST.Enabled = false
	cfg.Server.GRPC.Enabled = false
	cfg.Server.MCP.Enabled = false
	require.False(t, cfg.NetworkServing())

	// Stub seams that would require external infrastructure.
	seamMu.Lock()
	initIAMComponentsFn = func(_ *Server) (service.IAMComponents, error) { return nil, nil }
	initSealManagerFn = func(_ *logger.Logger) error { return nil }
	validateRoutingFn = func(_ []uint64, _ map[string]uint64) error { return nil }
	seamMu.Unlock()

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Record whether the CA seam ran at all, and whether qrdbNode was set
	// when it did. The first separates an infrastructure failure from a
	// boot-order violation; only the second is what this test asserts.
	var caReached, nodeWasSetAtCAInit bool

	seamMu.Lock()
	initializeCAFn = func(s *Server) error {
		caReached = true
		nodeWasSetAtCAInit = s.qrdbNode != nil
		// Return sentinel so initialize() aborts immediately after the CA step.
		return errBootOrderSentinel
	}
	seamMu.Unlock()

	err = srv.initialize()
	if !caReached {
		t.Fatalf("initialize() failed before reaching the CA seam, so boot order "+
			"was not tested (an infrastructure failure, not a CA-before-node "+
			"violation): %v", err)
	}
	require.ErrorIs(t, err, errBootOrderSentinel,
		"initialize must return the CA seam's sentinel once the seam has run")
	assert.True(t, nodeWasSetAtCAInit,
		"s.qrdbNode must be non-nil when initializeCAFn is called — CA must run AFTER node init")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// errBootOrderSentinel is the typed sentinel returned by the CA-init stub to
// abort Server.initialize() immediately after the CA phase.
var errBootOrderSentinel = &bootOrderSentinelError{}

type bootOrderSentinelError struct{}

func (e *bootOrderSentinelError) Error() string {
	return "boot-order test: CA-init stub sentinel"
}
