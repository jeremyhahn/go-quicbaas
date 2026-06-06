//go:build local && !raft && !rest && !quic && !mcp && !rag && !iam && !integration

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

// lifecycle_stub_branches_local_test.go covers the conditional branches inside
// the transport stub functions (QUIC/REST) that the no-server happy-path tests
// skip: the "server enabled in config" warning branch of startQUICServer and
// the "server instance present" delegation branch of stopQUICServer /
// stopRESTServer. The stub seams are overridden so the delegation path returns
// deterministically without a compiled transport.

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicapi "github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
)

// TestServer_StartQUICServer_Local_EnabledLogsWarning covers the branch where
// QUIC is requested in config but not compiled: the stub logs a warning and
// returns nil rather than erroring (so a non-QUIC build still boots).
func TestServer_StartQUICServer_Local_EnabledLogsWarning(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.QUIC.Enabled = true

	require.NoError(t, srv.startQUICServer(),
		"the QUIC stub must boot cleanly even when QUIC is enabled but not compiled")
}

// TestServer_StopQUICServer_Local_DelegatesWhenServerPresent covers the
// non-nil branch of stopQUICServer: when a quicServer instance is present the
// stub delegates to the quicStopFn seam.
func TestServer_StopQUICServer_Local_DelegatesWhenServerPresent(t *testing.T) {
	srv := withTempServer(t)
	srv.quicServer = &quicapi.Server{}

	sentinel := errors.New("quic stop seam invoked")
	orig := quicStopFn
	quicStopFn = func(_ *quicapi.Server) error { return sentinel }
	t.Cleanup(func() { quicStopFn = orig })

	err := srv.stopQUICServer()
	assert.ErrorIs(t, err, sentinel,
		"stopQUICServer must delegate to the seam when a server instance is present")
}

// TestServer_StopRESTServer_Local_DelegatesWhenServerPresent covers the
// non-nil branch of stopRESTServer.
func TestServer_StopRESTServer_Local_DelegatesWhenServerPresent(t *testing.T) {
	srv := withTempServer(t)
	srv.restServer = &restapi.Server{}

	sentinel := errors.New("rest shutdown seam invoked")
	orig := restShutdownFn
	restShutdownFn = func(_ *restapi.Server, _ context.Context) error { return sentinel }
	t.Cleanup(func() { restShutdownFn = orig })

	err := srv.stopRESTServer(context.Background())
	assert.ErrorIs(t, err, sentinel,
		"stopRESTServer must delegate to the seam when a server instance is present")
}
