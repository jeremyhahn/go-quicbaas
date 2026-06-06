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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaasseal "github.com/jeremyhahn/go-quicbaas/pkg/seal"
)

// The no-op seal manager and its compatibility shims must behave
// predictably: report sealed/uninitialized, never error, and produce the
// documented placeholder responses. Production replaces this manager via
// SetSealManager, but the no-op reset point still has to be correct.

func TestNoopSealManager_StateReporting(t *testing.T) {
	mgr := &noopSealManager{}
	assert.True(t, mgr.IsSealed())
	assert.False(t, mgr.IsInitialized())

	status, err := mgr.Status(context.Background())
	require.NoError(t, err)
	m, ok := status.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "noop", m["strategy"])
	assert.Equal(t, false, m["initialized"])
	assert.Equal(t, true, m["sealed"])
}

func TestNoopSealManager_LifecycleOps(t *testing.T) {
	mgr := &noopSealManager{}
	ctx := context.Background()

	init, err := mgr.Initialize(ctx, 2, 3)
	require.NoError(t, err)
	assert.Nil(t, init)

	unseal, err := mgr.Unseal(ctx, []byte("share"))
	require.NoError(t, err)
	um, ok := unseal.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, um["sealed"])

	require.NoError(t, mgr.Seal(ctx))

	rekey, err := mgr.Rekey(ctx, 2, 3)
	require.NoError(t, err)
	assert.Nil(t, rekey)
}

func TestNoopSealManager_RecoveryAndRootToken(t *testing.T) {
	mgr := &noopSealManager{}
	ctx := context.Background()

	keys, err := mgr.GenerateRecoveryKeys(ctx, 2, 3)
	require.NoError(t, err)
	assert.Nil(t, keys)

	require.NoError(t, mgr.DeleteRecoveryKeys(ctx))

	token, err := mgr.GenerateRootToken(ctx, "nonce", [][]byte{[]byte("k")})
	require.NoError(t, err)
	assert.Empty(t, token)

	require.NoError(t, mgr.RotateRootKey(ctx))
	require.NoError(t, mgr.Close())
}

func TestSealManagerCompatShims(t *testing.T) {
	require.NoError(t, InitializeDefaultSealManager(nil))

	mgr := NewDefaultSealManager(&quicbaasseal.Config{})
	require.NotNil(t, mgr)
	assert.True(t, mgr.IsSealed())

	persistent, err := NewPersistentDefaultSealManager(&quicbaasseal.Config{}, "/tmp/seal")
	require.NoError(t, err)
	require.NotNil(t, persistent)
	assert.False(t, persistent.IsInitialized())
}
