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

//go:build !integration

package server

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/stretchr/testify/require"
)

// TestNodeSlot_RefusesASecondHolder pins the slot's claim/release contract:
// one holder at a time, re-claim by the holder is fine, and only the holder
// can release it.
func TestNodeSlot_RefusesASecondHolder(t *testing.T) {
	slot := &nodeSlot{}
	a, b := &Server{}, &Server{}

	require.NoError(t, slot.claim(a))
	require.NoError(t, slot.claim(a), "the holder may claim again")

	var running *EmbeddedNodeRunningError
	require.True(t, errors.As(slot.claim(b), &running), "a second Server is refused")

	slot.release(b)
	require.True(t, errors.As(slot.claim(b), &running), "release by a non-holder leaves the holder in place")

	slot.release(a)
	require.NoError(t, slot.claim(b), "the slot is free once the holder releases it")

	unbounded := &nodeSlot{unbounded: true}
	require.NoError(t, unbounded.claim(a))
	require.NoError(t, unbounded.claim(b))
}

// TestInitStandaloneQRDBNode_RefusesASecondNodeInTheProcess proves that with
// a real slot in force a second Server cannot build an embedded node while
// the first runs, and can once the first stops.
func TestInitStandaloneQRDBNode_RefusesASecondNodeInTheProcess(t *testing.T) {
	useRealNodeSlot(t)

	srvA := withTempServer(t)
	require.NoError(t, srvA.initStandaloneQRDBNode())
	t.Cleanup(func() { _ = srvA.stopQRDBNode() })

	srvB := withTempServer(t)
	err := srvB.initStandaloneQRDBNode()
	var running *EmbeddedNodeRunningError
	require.True(t, errors.As(err, &running), "second node must be refused, got %v", err)
	require.Nil(t, srvB.getQRDBNode())

	require.NoError(t, srvA.stopQRDBNode())

	require.NoError(t, srvB.initStandaloneQRDBNode())
	t.Cleanup(func() { _ = srvB.stopQRDBNode() })
}

// TestInitClusteredQRDBNode_RefusesASecondNodeInTheProcess proves that with
// a real slot in force a second clustered Server cannot build a node while
// the first runs, and can once the first stops.
func TestInitClusteredQRDBNode_RefusesASecondNodeInTheProcess(t *testing.T) {
	useRealNodeSlot(t)

	a := newSlotClusterServer(t)
	require.NoError(t, a.initClusteredQRDBNode())
	t.Cleanup(func() { _ = a.stopQRDBNode() })

	b := newSlotClusterServer(t)
	requireNodeRunning(t, b.initClusteredQRDBNode())
	require.Nil(t, b.getQRDBNode())

	require.NoError(t, a.stopQRDBNode())

	require.NoError(t, b.initClusteredQRDBNode())
	t.Cleanup(func() { _ = b.stopQRDBNode() })
}

// TestEmbeddedNode_RefusedAcrossModes proves the slot is one per process
// whatever the mode: a standalone holder refuses a clustered builder and a
// clustered holder refuses a standalone builder.
func TestEmbeddedNode_RefusedAcrossModes(t *testing.T) {
	t.Run("standalone holds, clustered refused", func(t *testing.T) {
		useRealNodeSlot(t)

		a := withTempServer(t)
		require.NoError(t, a.initStandaloneQRDBNode())
		t.Cleanup(func() { _ = a.stopQRDBNode() })

		b := newSlotClusterServer(t)
		requireNodeRunning(t, b.initClusteredQRDBNode())
		require.Nil(t, b.getQRDBNode())

		require.NoError(t, a.stopQRDBNode())

		require.NoError(t, b.initClusteredQRDBNode())
		t.Cleanup(func() { _ = b.stopQRDBNode() })
	})

	t.Run("clustered holds, standalone refused", func(t *testing.T) {
		useRealNodeSlot(t)

		a := newSlotClusterServer(t)
		require.NoError(t, a.initClusteredQRDBNode())
		t.Cleanup(func() { _ = a.stopQRDBNode() })

		b := withTempServer(t)
		requireNodeRunning(t, b.initStandaloneQRDBNode())
		require.Nil(t, b.getQRDBNode())

		require.NoError(t, a.stopQRDBNode())

		require.NoError(t, b.initStandaloneQRDBNode())
		t.Cleanup(func() { _ = b.stopQRDBNode() })
	})
}

// TestEmbeddedNode_FailedConstructionReleasesTheSlot proves that a builder
// that fails after claiming the slot releases it, so a later Server in the
// same process can build its node.
func TestEmbeddedNode_FailedConstructionReleasesTheSlot(t *testing.T) {
	t.Run("standalone", func(t *testing.T) {
		slot := useRealNodeSlot(t)

		f := withTempServer(t)
		dataFile := filepath.Join(t.TempDir(), "datadir-as-file")
		require.NoError(t, os.WriteFile(dataFile, []byte("x"), 0o600))
		f.config.Storage.DataDir = dataFile

		err := f.initStandaloneQRDBNode()
		require.Error(t, err)
		require.ErrorAs(t, err, new(*QRDBNodeError))
		require.False(t, errors.As(err, new(*EmbeddedNodeRunningError)), "the build must fail past the claim, got %v", err)
		require.Nil(t, f.getQRDBNode())
		requireSlotFree(t, slot)

		g := withTempServer(t)
		require.NoError(t, g.initStandaloneQRDBNode())
		t.Cleanup(func() { _ = g.stopQRDBNode() })
		t.Cleanup(func() { _ = f.stopQRDBNode() })
	})

	t.Run("clustered", func(t *testing.T) {
		slot := useRealNodeSlot(t)

		f := newSlotClusterServer(t)
		f.config.Raft.JoinAddresses = []string{"=127.0.0.1:7000"}

		err := f.initClusteredQRDBNode()
		var nodeErr *QRDBNodeError
		require.ErrorAs(t, err, &nodeErr)
		require.Equal(t, "build cluster members", nodeErr.Stage)
		require.Nil(t, f.getQRDBNode())
		requireSlotFree(t, slot)

		g := newSlotClusterServer(t)
		require.NoError(t, g.initClusteredQRDBNode())
		t.Cleanup(func() { _ = g.stopQRDBNode() })
		t.Cleanup(func() { _ = f.stopQRDBNode() })
	})
}

// useRealNodeSlot swaps a real, bounded slot in for the test and restores
// the previous one when the test ends.
func useRealNodeSlot(t *testing.T) *nodeSlot {
	t.Helper()
	slot := &nodeSlot{}
	prev := activeNodeSlot.Swap(slot)
	t.Cleanup(func() { activeNodeSlot.Store(prev) })
	return slot
}

// newSlotClusterServer returns a Server configured for a single-member,
// in-memory clustered node with its own data directory.
func newSlotClusterServer(t *testing.T) *Server {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	s := &Server{config: clusterTestConfig(), logger: log}
	s.config.Storage.Backend = config.StorageBackendMemory
	s.config.Storage.DataDir = t.TempDir()
	s.config.Raft.NodeID = 1
	return s
}

// requireNodeRunning asserts err is the guard's refusal at the claim stage.
func requireNodeRunning(t *testing.T, err error) {
	t.Helper()
	var running *EmbeddedNodeRunningError
	require.True(t, errors.As(err, &running), "want EmbeddedNodeRunningError, got %v", err)
	var nodeErr *QRDBNodeError
	require.ErrorAs(t, err, &nodeErr)
	require.Equal(t, "claim embedded node", nodeErr.Stage)
}

// requireSlotFree asserts no Server holds slot.
func requireSlotFree(t *testing.T, slot *nodeSlot) {
	t.Helper()
	slot.mu.Lock()
	holder := slot.holder
	slot.mu.Unlock()
	require.Nil(t, holder, "a failed build must release the slot")
}
