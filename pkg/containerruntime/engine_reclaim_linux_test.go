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

	"github.com/stretchr/testify/require"
)

// A reclaim retried after an earlier one destroyed the engine state, or of a
// container whose create never reached the engine, finds nothing and is done.
func TestLibcontainerEngineReclaim_AContainerItDoesNotKnowIsAlreadyReclaimed(t *testing.T) {
	engine := newLibcontainerEngine(t.TempDir())
	require.NoError(t, engine.Reclaim("0123456789abcdef"))
}
