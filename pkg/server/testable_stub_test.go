//go:build !integration

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

// testable_stub_test.go covers the BaaS-mode TestableApp stubs. These are
// no-ops because QuicBaaS operates as a BaaS client without a Raft layer,
// but the contract must remain stable for tests that import this package.

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTestableApp_StubBehavior(t *testing.T) {
	srv := withTempServer(t)
	app := NewTestableApp(srv)
	require.NotNil(t, app)

	t.Run("GetServer returns wrapped server", func(t *testing.T) {
		assert.Same(t, srv, app.GetServer())
	})

	t.Run("WaitForLeader is a no-op", func(t *testing.T) {
		assert.NoError(t, app.WaitForLeader(1))
	})

	t.Run("GetLeaderID returns zero", func(t *testing.T) {
		id, err := app.GetLeaderID(1)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), id)
	})

	t.Run("IsLeader returns false", func(t *testing.T) {
		assert.False(t, app.IsLeader(1))
	})

	t.Run("GetNodeHostInfo returns nil", func(t *testing.T) {
		assert.Nil(t, app.GetNodeHostInfo())
	})

	t.Run("WaitForShards is a no-op", func(t *testing.T) {
		assert.NoError(t, app.WaitForShards([]uint64{1, 2, 3}))
	})
}
