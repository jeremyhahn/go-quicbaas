//go:build !raft

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

package devcluster

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewManager(t *testing.T) {
	t.Run("returns error when raft is not compiled", func(t *testing.T) {
		mgr, err := NewManager(nil)
		assert.Nil(t, mgr)
		assert.ErrorIs(t, err, ErrRaftNotCompiled)
	})
}

func TestManagerStart(t *testing.T) {
	t.Run("returns error when raft is not compiled", func(t *testing.T) {
		mgr := &Manager{}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		err := mgr.Start(ctx)
		assert.ErrorIs(t, err, ErrRaftNotCompiled)
	})
}

func TestManagerStop(t *testing.T) {
	t.Run("returns error when raft is not compiled", func(t *testing.T) {
		mgr := &Manager{}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		err := mgr.Stop(ctx)
		assert.ErrorIs(t, err, ErrRaftNotCompiled)
	})
}

func TestManagerNodes(t *testing.T) {
	t.Run("returns nil when raft is not compiled", func(t *testing.T) {
		mgr := &Manager{}
		nodes := mgr.Nodes()
		assert.Nil(t, nodes)
	})
}

func TestManagerNodeCount(t *testing.T) {
	t.Run("returns 0 when raft is not compiled", func(t *testing.T) {
		mgr := &Manager{}
		count := mgr.NodeCount()
		assert.Equal(t, 0, count)
	})
}

func TestManagerGetNodeConfig(t *testing.T) {
	t.Run("returns error when raft is not compiled", func(t *testing.T) {
		mgr := &Manager{}
		cfg, err := mgr.GetNodeConfig(1)
		assert.Nil(t, cfg)
		assert.ErrorIs(t, err, ErrRaftNotCompiled)
	})
}

func TestManagerGetNode(t *testing.T) {
	t.Run("returns error when raft is not compiled", func(t *testing.T) {
		mgr := &Manager{}
		node, err := mgr.GetNode(1)
		assert.Nil(t, node)
		assert.ErrorIs(t, err, ErrRaftNotCompiled)
	})
}

func TestManagerIsStarted(t *testing.T) {
	t.Run("returns false when raft is not compiled", func(t *testing.T) {
		mgr := &Manager{}
		assert.False(t, mgr.IsStarted())
	})
}

func TestManagerIsStopped(t *testing.T) {
	t.Run("returns true when raft is not compiled", func(t *testing.T) {
		mgr := &Manager{}
		assert.True(t, mgr.IsStopped())
	})
}

func TestManagerDevClusterConfig(t *testing.T) {
	t.Run("returns nil when raft is not compiled", func(t *testing.T) {
		mgr := &Manager{}
		cfg := mgr.DevClusterConfig()
		assert.Nil(t, cfg)
	})
}

func TestNodeInstanceIsRunning(t *testing.T) {
	t.Run("returns false when raft is not compiled", func(t *testing.T) {
		node := &NodeInstance{}
		assert.False(t, node.IsRunning())
	})
}

func TestNodeInstanceIsReady(t *testing.T) {
	t.Run("returns false when raft is not compiled", func(t *testing.T) {
		node := &NodeInstance{}
		assert.False(t, node.IsReady())
	})
}

func TestNodeInstanceSetRunning(t *testing.T) {
	t.Run("is a no-op when raft is not compiled", func(t *testing.T) {
		node := &NodeInstance{}
		// Should not panic
		assert.NotPanics(t, func() {
			node.SetRunning(true)
			node.SetRunning(false)
		})
		// State should remain unchanged (false)
		assert.False(t, node.IsRunning())
	})
}

func TestNodeInstanceSetReady(t *testing.T) {
	t.Run("is a no-op when raft is not compiled", func(t *testing.T) {
		node := &NodeInstance{}
		// Should not panic
		assert.NotPanics(t, func() {
			node.SetReady(true)
			node.SetReady(false)
		})
		// State should remain unchanged (false)
		assert.False(t, node.IsReady())
	})
}

func TestNodeInstanceStopCh(t *testing.T) {
	t.Run("returns nil when raft is not compiled", func(t *testing.T) {
		node := &NodeInstance{}
		ch := node.StopCh()
		assert.Nil(t, ch)
	})
}

func TestNodeInstanceErrCh(t *testing.T) {
	t.Run("returns nil when raft is not compiled", func(t *testing.T) {
		node := &NodeInstance{}
		ch := node.ErrCh()
		assert.Nil(t, ch)
	})
}

func TestNodeInstanceSendError(t *testing.T) {
	t.Run("is a no-op when raft is not compiled", func(t *testing.T) {
		node := &NodeInstance{}
		// Should not panic
		assert.NotPanics(t, func() {
			node.SendError(nil)
			node.SendError(ErrRaftNotCompiled)
		})
	})
}

func TestErrRaftNotCompiled(t *testing.T) {
	t.Run("error message is correct", func(t *testing.T) {
		assert.Contains(t, ErrRaftNotCompiled.Error(), "raft support not compiled")
	})
}
