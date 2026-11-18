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

//go:build linux && cgo

package containerruntime

import (
	"testing"

	"github.com/opencontainers/cgroups"
	"github.com/stretchr/testify/require"
)

// TestLibcontainerEngine_RegistersTheCgroupDeviceHandlers pins the import
// that fills the cgroup managers' device hooks. specconv gives every
// container default device rules, and both managers refuse those rules with
// ErrDevicesUnsupported while the hooks are nil, so without it every start
// fails after the runtime has initialised.
func TestLibcontainerEngine_RegistersTheCgroupDeviceHandlers(t *testing.T) {
	require.NotNil(t, cgroups.DevicesSetV1, "cgroup v1 device handler is not registered")
	require.NotNil(t, cgroups.DevicesSetV2, "cgroup v2 device handler is not registered")
}

// A reclaim retried after an earlier one destroyed the engine state, or of a
// container whose create never reached the engine, finds nothing and is done.
func TestLibcontainerEngineReclaim_AContainerItDoesNotKnowIsAlreadyReclaimed(t *testing.T) {
	engine := newLibcontainerEngine(t.TempDir())
	require.NoError(t, engine.Reclaim("0123456789abcdef"))
}
