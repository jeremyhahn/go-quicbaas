//go:build mcp && !integration

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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Sessions run in containers only on the fleet node agent co-located with
// quicbaasd. The orchestration service hands its backends no container
// engine, so it neither sweeps nor reaps containers of its own: with docker
// on PATH, its startup asks the engine for nothing past whether it exists.
func TestStartOrchestration_RunsNoContainersOfItsOwn(t *testing.T) {
	bin := t.TempDir()
	calls := filepath.Join(t.TempDir(), "docker.calls")
	script := "#!/bin/sh\necho \"$1\" >> " + calls + "\nexit 0\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.Orchestration.Enabled = true
	require.NoError(t, srv.initStandaloneQRDBNode())
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	require.NoError(t, srv.startService())

	require.NoError(t, srv.startOrchestration())
	t.Cleanup(func() { srv.stopTaskSourcePoller(); srv.stopTaskDispatcher(); srv.stopOrchestrationSweeps() })

	recorded, err := os.ReadFile(calls)
	if os.IsNotExist(err) {
		return
	}
	require.NoError(t, err)
	for _, verb := range strings.Fields(string(recorded)) {
		require.Equal(t, "version", verb, "quicbaasd drove a container engine of its own")
	}
}
