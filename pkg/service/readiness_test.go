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

package service

import (
	"context"
	stderrors "errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func readinessTestLogger(t *testing.T) interfaces.Logger {
	cfg := logger.LogConfig{
		Level:  "error",
		Format: "console",
		Output: "stdout",
	}
	log, err := logger.New(cfg)
	require.NoError(t, err)
	return logger.NewAdapter(log)
}

// readinessTestTransport implements the transport interfaces needed for
// readiness tests. It supports configurable GetShardState responses.
type readinessTestTransport struct {
	svcBoostMockTransport
	getShardStateFn func(ctx context.Context, shardID uint64) (*transport.ShardStateResponse, error)
	mu              sync.Mutex
	checkedShards   []uint64
	callCount       int32
}

func (t *readinessTestTransport) GetShardState(ctx context.Context, shardID uint64) (*transport.ShardStateResponse, error) {
	t.mu.Lock()
	t.checkedShards = append(t.checkedShards, shardID)
	t.mu.Unlock()
	atomic.AddInt32(&t.callCount, 1)

	if t.getShardStateFn != nil {
		return t.getShardStateFn(ctx, shardID)
	}
	return &transport.ShardStateResponse{
		ShardID:   shardID,
		LeaderID:  1,
		HasLeader: true,
	}, nil
}

// readinessTestQRDBProvider provides per-shard SDK clients for readiness tests.
type readinessTestQRDBProvider struct {
	client *qrdbsdk.Client
}

func newReadinessTestQRDBProvider(t *readinessTestTransport) *readinessTestQRDBProvider {
	client, err := qrdbsdk.NewClient(t)
	if err != nil {
		panic(fmt.Sprintf("failed to create mock qrdb client: %v", err))
	}
	return &readinessTestQRDBProvider{client: client}
}

func (p *readinessTestQRDBProvider) ForShard(_ uint64) (*qrdbsdk.Client, error) {
	return p.client, nil
}

func (p *readinessTestQRDBProvider) Close() error {
	return p.client.Close()
}

func (p *readinessTestQRDBProvider) IsEmbedded() bool { return false }

// TestReadiness_NilService tests that Readiness returns not ready when service is nil.
func TestReadiness_NilService(t *testing.T) {
	healthSvc := &healthService{svc: nil}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, resp.Ready)
	assert.Equal(t, "service not initialized", resp.Reason)
}

// TestReadiness_NilConfig tests that Readiness returns not ready when config is nil.
func TestReadiness_NilConfig(t *testing.T) {
	svc := &quicBaaS{
		config: nil,
	}
	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, resp.Ready)
	assert.Equal(t, "service not initialized", resp.Reason)
}

// TestReadiness_NoQRDBClient_LocalMode tests that Readiness returns ready in local mode (no SDK client).
func TestReadiness_NoQRDBClient_LocalMode(t *testing.T) {
	svc := &quicBaaS{
		config:     &ServiceConfig{},
		qrdbClient: nil, // No qrdb client = local mode
		logger:     readinessTestLogger(t),
	}
	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Ready)
}

// TestReadiness_AllShardsHaveLeaders tests that Readiness returns ready when all shards have leaders.
func TestReadiness_AllShardsHaveLeaders(t *testing.T) {
	mockTransport := &readinessTestTransport{
		getShardStateFn: func(ctx context.Context, shardID uint64) (*transport.ShardStateResponse, error) {
			return &transport.ShardStateResponse{
				ShardID:   shardID,
				LeaderID:  1,
				HasLeader: true,
			}, nil
		},
	}
	provider := newReadinessTestQRDBProvider(mockTransport)

	svc := &quicBaaS{
		config: &ServiceConfig{
			Mode:   "cluster",
			Shards: []uint64{1, 2, 3},
		},
		qrdbClient: provider,
		logger:     readinessTestLogger(t),
	}
	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Ready)
	assert.Empty(t, resp.Reason)
}

// TestReadiness_ShardHasNoLeader tests that Readiness returns not ready when a shard has no leader.
func TestReadiness_ShardHasNoLeader(t *testing.T) {
	mockTransport := &readinessTestTransport{
		getShardStateFn: func(ctx context.Context, shardID uint64) (*transport.ShardStateResponse, error) {
			if shardID == 2 {
				return &transport.ShardStateResponse{
					ShardID:   shardID,
					HasLeader: false,
				}, nil
			}
			return &transport.ShardStateResponse{
				ShardID:   shardID,
				LeaderID:  1,
				HasLeader: true,
			}, nil
		},
	}
	provider := newReadinessTestQRDBProvider(mockTransport)

	svc := &quicBaaS{
		config: &ServiceConfig{
			Mode:   "cluster",
			Shards: []uint64{1, 2, 3},
		},
		qrdbClient: provider,
		logger:     readinessTestLogger(t),
	}
	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, resp.Ready)
	assert.Contains(t, resp.Reason, "shard")
}

// TestReadiness_DefaultShard1 tests that Readiness defaults to checking shard 1 when no shards configured.
func TestReadiness_DefaultShard1(t *testing.T) {
	mockTransport := &readinessTestTransport{}
	provider := newReadinessTestQRDBProvider(mockTransport)

	svc := &quicBaaS{
		config: &ServiceConfig{
			Mode:   "cluster",
			Shards: nil, // No shards configured
		},
		qrdbClient: provider,
		logger:     readinessTestLogger(t),
	}
	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Ready)

	mockTransport.mu.Lock()
	defer mockTransport.mu.Unlock()
	require.Len(t, mockTransport.checkedShards, 1)
	assert.Equal(t, uint64(1), mockTransport.checkedShards[0], "Should default to checking shard 1")
}

// TestReadiness_FirstShardNoLeader tests that Readiness fails fast on first shard without leader.
func TestReadiness_FirstShardNoLeader(t *testing.T) {
	mockTransport := &readinessTestTransport{
		getShardStateFn: func(ctx context.Context, shardID uint64) (*transport.ShardStateResponse, error) {
			return nil, stderrors.New("no leader")
		},
	}
	provider := newReadinessTestQRDBProvider(mockTransport)

	svc := &quicBaaS{
		config: &ServiceConfig{
			Mode:   "cluster",
			Shards: []uint64{1, 2, 3},
		},
		qrdbClient: provider,
		logger:     readinessTestLogger(t),
	}
	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.False(t, resp.Ready)
	assert.Equal(t, int32(1), atomic.LoadInt32(&mockTransport.callCount), "Should fail fast after first shard without leader")
}

// TestReadiness_EmptyShardsArray tests that Readiness defaults to shard 1 with empty shards array.
func TestReadiness_EmptyShardsArray(t *testing.T) {
	mockTransport := &readinessTestTransport{}
	provider := newReadinessTestQRDBProvider(mockTransport)

	svc := &quicBaaS{
		config: &ServiceConfig{
			Mode:   "cluster",
			Shards: []uint64{}, // Empty array
		},
		qrdbClient: provider,
		logger:     readinessTestLogger(t),
	}
	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.True(t, resp.Ready)

	mockTransport.mu.Lock()
	defer mockTransport.mu.Unlock()
	require.Len(t, mockTransport.checkedShards, 1)
	assert.Equal(t, uint64(1), mockTransport.checkedShards[0], "Should default to checking shard 1 with empty array")
}

// TestReadiness_MultipleShardsChecked tests that all configured shards are checked.
func TestReadiness_MultipleShardsChecked(t *testing.T) {
	mockTransport := &readinessTestTransport{}
	provider := newReadinessTestQRDBProvider(mockTransport)

	svc := &quicBaaS{
		config: &ServiceConfig{
			Mode:   "cluster",
			Shards: []uint64{1, 2, 3},
		},
		qrdbClient: provider,
		logger:     readinessTestLogger(t),
	}
	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.True(t, resp.Ready)

	mockTransport.mu.Lock()
	defer mockTransport.mu.Unlock()
	assert.Equal(t, []uint64{1, 2, 3}, mockTransport.checkedShards, "All shards should be checked")
}

// TestReadiness_GetShardStateError tests readiness when GetShardState returns an error.
func TestReadiness_GetShardStateError(t *testing.T) {
	mockTransport := &readinessTestTransport{
		getShardStateFn: func(ctx context.Context, shardID uint64) (*transport.ShardStateResponse, error) {
			return nil, stderrors.New("connection refused")
		},
	}
	provider := newReadinessTestQRDBProvider(mockTransport)

	svc := &quicBaaS{
		config: &ServiceConfig{
			Mode:   "cluster",
			Shards: []uint64{1},
		},
		qrdbClient: provider,
		logger:     readinessTestLogger(t),
	}
	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.False(t, resp.Ready)
	assert.Contains(t, resp.Reason, "shard")
}
