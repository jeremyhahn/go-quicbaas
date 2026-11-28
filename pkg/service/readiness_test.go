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

// readinessTestTransport drives the GetTopology-based readiness check. The
// embedded svcBoostMockTransport carries the topologyResult / topologyErr the
// readiness probe reads; this wrapper counts GetTopology calls so tests can
// assert the probe issues a single node-host topology read rather than a
// per-shard fan-out.
type readinessTestTransport struct {
	svcBoostMockTransport
	topologyCallCount int32
}

func (t *readinessTestTransport) GetTopology(ctx context.Context) (*transport.TopologyResponse, error) {
	atomic.AddInt32(&t.topologyCallCount, 1)
	return t.svcBoostMockTransport.GetTopology(ctx)
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

// leaderShard builds a topology shard entry that has an elected leader.
func leaderShard(id uint64) transport.ShardInfo {
	return transport.ShardInfo{ShardID: id, LeaderID: 1, IsLeader: id == 1}
}

// topologyWith assembles a TopologyResponse from the given shard entries.
func topologyWith(shards ...transport.ShardInfo) *transport.TopologyResponse {
	return &transport.TopologyResponse{ShardCount: len(shards), Shards: shards}
}

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

// TestReadiness_StandaloneMode tests that standalone mode is always ready.
func TestReadiness_StandaloneMode(t *testing.T) {
	svc := &quicBaaS{
		config: &ServiceConfig{Mode: "standalone"},
		logger: readinessTestLogger(t),
	}
	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.True(t, resp.Ready)
}

// TestReadiness_NoQRDBClient_LocalMode tests that Readiness returns ready in local mode (no SDK client).
func TestReadiness_NoQRDBClient_LocalMode(t *testing.T) {
	svc := &quicBaaS{
		config:     &ServiceConfig{Mode: "cluster"},
		qrdbClient: nil, // No qrdb client = local/memory mode
		logger:     readinessTestLogger(t),
	}
	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Ready)
}

// TestReadiness_AllShardsHaveLeaders tests that Readiness returns ready when
// every Raft shard in the node-host topology has an elected leader.
func TestReadiness_AllShardsHaveLeaders(t *testing.T) {
	mockTransport := &readinessTestTransport{}
	mockTransport.topologyResult = topologyWith(leaderShard(1), leaderShard(2), leaderShard(3))
	provider := newReadinessTestQRDBProvider(mockTransport)

	svc := &quicBaaS{
		config:     &ServiceConfig{Mode: "cluster", Shards: []uint64{1, 2, 3}},
		qrdbClient: provider,
		logger:     readinessTestLogger(t),
	}
	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.True(t, resp.Ready)
	assert.Empty(t, resp.Reason)
	// Readiness issues a single node-host topology read, not a per-shard fan-out.
	assert.Equal(t, int32(1), atomic.LoadInt32(&mockTransport.topologyCallCount))
}

// TestReadiness_ShardHasNoLeader tests that Readiness reports not ready when a
// Raft shard in the topology has no elected leader.
func TestReadiness_ShardHasNoLeader(t *testing.T) {
	mockTransport := &readinessTestTransport{}
	mockTransport.topologyResult = topologyWith(
		leaderShard(1),
		transport.ShardInfo{ShardID: 2, LeaderID: 0}, // no leader
		leaderShard(3),
	)
	provider := newReadinessTestQRDBProvider(mockTransport)

	svc := &quicBaaS{
		config:     &ServiceConfig{Mode: "cluster", Shards: []uint64{1, 2, 3}},
		qrdbClient: provider,
		logger:     readinessTestLogger(t),
	}
	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.False(t, resp.Ready)
	assert.Contains(t, resp.Reason, "leader")
}

// TestReadiness_SingleShardWithLeader tests the common single-platform-shard
// case: one Raft shard with a leader is ready.
func TestReadiness_SingleShardWithLeader(t *testing.T) {
	mockTransport := &readinessTestTransport{}
	mockTransport.topologyResult = topologyWith(leaderShard(1))
	provider := newReadinessTestQRDBProvider(mockTransport)

	svc := &quicBaaS{
		config:     &ServiceConfig{Mode: "cluster", Shards: nil},
		qrdbClient: provider,
		logger:     readinessTestLogger(t),
	}
	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.True(t, resp.Ready)
}

// TestReadiness_NonRaftServiceShardIgnored proves a non-Raft service shard the
// config lists (e.g. the vector shard) is NOT gated: it never appears in the
// node-host topology, so a topology whose Raft shards all have leaders is ready
// even though config.Shards names an extra shard. This is the regression the
// GetTopology rewrite fixed (the old per-config-shard GetShardState probe failed
// on the leaderless vector shard).
func TestReadiness_NonRaftServiceShardIgnored(t *testing.T) {
	mockTransport := &readinessTestTransport{}
	// Topology only contains the Raft platform shard 1; the configured vector
	// shard 200 is absent from the topology.
	mockTransport.topologyResult = topologyWith(leaderShard(1))
	provider := newReadinessTestQRDBProvider(mockTransport)

	svc := &quicBaaS{
		config:     &ServiceConfig{Mode: "cluster", Shards: []uint64{1, 200}},
		qrdbClient: provider,
		logger:     readinessTestLogger(t),
	}
	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.True(t, resp.Ready, "non-Raft config shard must not gate readiness")
}

// TestReadiness_EmptyTopologyNotReady tests that an empty node-host topology
// (no Raft shards loaded) with no local raft node is not ready.
func TestReadiness_EmptyTopologyNotReady(t *testing.T) {
	mockTransport := &readinessTestTransport{}
	mockTransport.topologyResult = topologyWith() // no shards
	provider := newReadinessTestQRDBProvider(mockTransport)

	svc := &quicBaaS{
		config:     &ServiceConfig{Mode: "cluster", Shards: []uint64{1}},
		qrdbClient: provider,
		logger:     readinessTestLogger(t),
	}
	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.False(t, resp.Ready)
	assert.Contains(t, resp.Reason, "raft")
}

// TestReadiness_GetTopologyError tests readiness when the topology read fails.
func TestReadiness_GetTopologyError(t *testing.T) {
	mockTransport := &readinessTestTransport{}
	mockTransport.topologyErr = stderrors.New("connection refused")
	provider := newReadinessTestQRDBProvider(mockTransport)

	svc := &quicBaaS{
		config:     &ServiceConfig{Mode: "cluster", Shards: []uint64{1}},
		qrdbClient: provider,
		logger:     readinessTestLogger(t),
	}
	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.False(t, resp.Ready)
	assert.Contains(t, resp.Reason, "shard")
	assert.Equal(t, int32(1), atomic.LoadInt32(&mockTransport.topologyCallCount))
}
