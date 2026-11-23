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

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/internal/containerruntime"
	"github.com/jeremyhahn/go-quicbaas/internal/inprocessruntime"
	"github.com/jeremyhahn/go-quicbaas/pkg/fleet"
)

// The agent's main registers the in-process runtime, and the "libcontainer"
// selector then resolves to it under the agent's own root: a JobRuntime when
// this host lets it start, never "not built into this binary".
func TestRegisterInProcessContainerRuntime_BuildsTheJobRuntimeButNoSelectorTakesIt(t *testing.T) {
	registerInProcessContainerRuntime()
	t.Cleanup(func() { inprocessruntime.Register(nil) })

	rt, err := fleet.ResolveContainerRuntimeIn(fleet.ContainerRuntimeSelector("libcontainer"), t.TempDir(), newFleetServeLogger())
	require.ErrorIs(t, err, fleet.ErrConfigUnknownRuntime, "no exported resolver takes the in-process runtime's key")
	assert.Nil(t, rt)

	rt, err = newInProcessContainerRuntime(t.TempDir(), newFleetServeLogger())
	if err != nil {
		t.Skipf("the in-process runtime cannot start on this host: %v", err)
	}
	assert.IsType(t, &containerruntime.JobRuntime{}, rt)
}
