//go:build !integration

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ---------------------------------------------------------------------------
// StandaloneModeError
// ---------------------------------------------------------------------------

func TestStandaloneModeError_Format(t *testing.T) {
	t.Run("formats error with operation name", func(t *testing.T) {
		err := &StandaloneModeError{Operation: "propose"}
		assert.Equal(t, "propose unavailable in standalone mode", err.Error())
	})
}

func TestNewStandaloneModeError_WrapsDragonError(t *testing.T) {
	t.Run("creates DragonError with unavailable code", func(t *testing.T) {
		err := newStandaloneModeError("test.op", "cluster mutation")
		require.NotNil(t, err)
		assert.Equal(t, dberrors.ErrUnavailable, err.Code)
		assert.Equal(t, "test.op", err.Op)

		var sme *StandaloneModeError
		assert.True(t, errors.As(err, &sme))
		assert.Equal(t, "cluster mutation", sme.Operation)
	})
}

// ---------------------------------------------------------------------------
// standaloneRaftNode identity and health
// ---------------------------------------------------------------------------

func TestStandaloneRaftNode_Identity(t *testing.T) {
	node := newStandaloneRaftNode(42, "127.0.0.1:9001")

	t.Run("ID returns formatted identifier", func(t *testing.T) {
		assert.Equal(t, "standalone-42", node.ID())
	})

	t.Run("RaftAddress returns configured address", func(t *testing.T) {
		assert.Equal(t, "127.0.0.1:9001", node.RaftAddress())
	})

	t.Run("ShardCount returns zero", func(t *testing.T) {
		assert.Equal(t, 0, node.ShardCount())
	})

	t.Run("GetNodeHostInfo returns address", func(t *testing.T) {
		info := node.GetNodeHostInfo(interfaces.NodeHostInfoOption{})
		require.NotNil(t, info)
		assert.Equal(t, "127.0.0.1:9001", info.RaftAddress)
	})
}

func TestStandaloneRaftNode_HealthCheck(t *testing.T) {
	node := newStandaloneRaftNode(1, "127.0.0.1:9001")

	t.Run("returns healthy when open", func(t *testing.T) {
		status := node.HealthCheck()
		assert.Equal(t, "healthy", status.Status)
		assert.True(t, status.DiskOK)
		assert.Equal(t, 0, status.ShardCount)
	})

	t.Run("returns closed after Close", func(t *testing.T) {
		require.NoError(t, node.Close())
		status := node.HealthCheck()
		assert.Equal(t, "closed", status.Status)
	})
}

// ---------------------------------------------------------------------------
// standaloneRaftNode no-op and error operations
// ---------------------------------------------------------------------------

func TestStandaloneRaftNode_Drain(t *testing.T) {
	node := newStandaloneRaftNode(1, "addr")

	t.Run("Drain is no-op", func(t *testing.T) {
		err := node.Drain()
		require.NoError(t, err)
	})

	t.Run("IsDraining returns false", func(t *testing.T) {
		assert.False(t, node.IsDraining())
	})
}

func TestStandaloneRaftNode_ConsensusErrors(t *testing.T) {
	node := newStandaloneRaftNode(1, "addr")
	ctx := context.Background()

	t.Run("SyncPropose returns unavailable error", func(t *testing.T) {
		_, err := node.SyncPropose(ctx, 1, []byte("data"))
		require.Error(t, err)
		var de *dberrors.DragonError
		assert.True(t, errors.As(err, &de))
		assert.Equal(t, dberrors.ErrUnavailable, de.Code)
	})

	t.Run("ProposeWithMode returns unavailable error", func(t *testing.T) {
		_, err := node.ProposeWithMode(ctx, 1, []byte("data"), interfaces.WriteModeSync)
		require.Error(t, err)
		var de *dberrors.DragonError
		assert.True(t, errors.As(err, &de))
	})

	t.Run("SyncRead returns unavailable error", func(t *testing.T) {
		_, err := node.SyncRead(ctx, 1, nil)
		require.Error(t, err)
	})

	t.Run("StaleRead returns unavailable error", func(t *testing.T) {
		_, err := node.StaleRead(ctx, 1, nil)
		require.Error(t, err)
	})

	t.Run("ReadWithConsistency returns unavailable error", func(t *testing.T) {
		_, err := node.ReadWithConsistency(ctx, 1, nil, interfaces.ReadOptions{})
		require.Error(t, err)
	})

	t.Run("GetLeaderID returns unavailable error", func(t *testing.T) {
		_, _, err := node.GetLeaderID(1)
		require.Error(t, err)
	})

	t.Run("GetShardRaftState returns unavailable error", func(t *testing.T) {
		_, _, err := node.GetShardRaftState(1)
		require.Error(t, err)
	})
}

func TestStandaloneRaftNode_ClusterOps(t *testing.T) {
	node := newStandaloneRaftNode(1, "addr")
	ctx := context.Background()

	t.Run("RequestAddNode returns unavailable error", func(t *testing.T) {
		_, err := node.RequestAddNode(ctx, 1, 2, "addr", 0)
		require.Error(t, err)
	})

	t.Run("RequestRemoveNode returns unavailable error", func(t *testing.T) {
		_, err := node.RequestRemoveNode(ctx, 1, 2, 0)
		require.Error(t, err)
	})

	t.Run("RequestSnapshot returns unavailable error", func(t *testing.T) {
		_, err := node.RequestSnapshot(ctx, 1)
		require.Error(t, err)
	})

	t.Run("StartShard returns unavailable error", func(t *testing.T) {
		err := node.StartShard(nil, false, nil, interfaces.ShardConfig{})
		require.Error(t, err)
	})

	t.Run("StopShard returns unavailable error", func(t *testing.T) {
		err := node.StopShard(1)
		require.Error(t, err)
	})
}

func TestStandaloneRaftNode_CloseIdempotent(t *testing.T) {
	node := newStandaloneRaftNode(1, "addr")

	t.Run("close is idempotent", func(t *testing.T) {
		require.NoError(t, node.Close())
		require.NoError(t, node.Close())
	})
}
