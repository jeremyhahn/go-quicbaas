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
	"strings"
	"testing"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// The permissions of the go-qrdb verbs the read RPCs call. Each RPC declares
// the permission of its verb (api/proto/quicbaas.proto, ruling 161).
const (
	permKVRange         = "kv:range"
	permAdminTopology   = "admin:topology"
	permAdminShardState = "admin:shard_state"
)

func (t *authorizingTransport) Scan(_ context.Context, prefix string, _ ...transport.ReadOption) (map[string][]byte, error) {
	if err := t.authorize(permKVRange); err != nil {
		return nil, err
	}
	matched := make(map[string][]byte)
	for key, value := range t.data {
		if strings.HasPrefix(key, prefix) {
			matched[key] = value
		}
	}
	return matched, nil
}

func (t *authorizingTransport) GetTopology(_ context.Context) (*transport.TopologyResponse, error) {
	if err := t.authorize(permAdminTopology); err != nil {
		return nil, err
	}
	return &transport.TopologyResponse{}, nil
}

func (t *authorizingTransport) GetShardState(_ context.Context, shardID uint64) (*transport.ShardStateResponse, error) {
	if err := t.authorize(permAdminShardState); err != nil {
		return nil, err
	}
	return &transport.ShardStateResponse{ShardID: shardID, LeaderID: 1}, nil
}

// readRPCs drives each read RPC whose declaration names its go-qrdb verb.
var readRPCs = map[string]struct {
	grant string
	call  func(ctx context.Context, svc QuicBaaS) error
}{
	"List": {permKVRange, func(ctx context.Context, svc QuicBaaS) error {
		stream := make(chan *ListResponse, 1)
		return svc.List(ctx, &ListRequest{EntityType: "user"}, stream)
	}},
	"ListEntityTypes": {permKVRange, func(ctx context.Context, svc QuicBaaS) error {
		_, err := svc.ListEntityTypes(ctx, &ListEntityTypesRequest{})
		return err
	}},
	"ClusterStatus": {permAdminTopology, func(ctx context.Context, svc QuicBaaS) error {
		_, err := svc.ClusterStatus(ctx, &ClusterStatusRequest{})
		return err
	}},
	"GetLeader": {permAdminShardState, func(ctx context.Context, svc QuicBaaS) error {
		_, err := svc.GetLeader(ctx, &GetLeaderRequest{ShardID: 1})
		return err
	}},
	"Aggregate": {permKVRange, func(ctx context.Context, svc QuicBaaS) error {
		_, err := svc.Aggregate(ctx, &AggregateRequest{
			EntityType: "metric", Function: AggregateFunctionCount, StartTime: 1, EndTime: 100,
		})
		return err
	}},
	"TimeSeriesQuery": {permKVRange, func(ctx context.Context, svc QuicBaaS) error {
		stream := make(chan *TimeSeriesQueryResponse, 16)
		return svc.TimeSeriesQuery(ctx, &TimeSeriesQueryRequest{
			EntityType: "metric", StartTime: 1, EndTime: 100,
		}, stream)
	}},
}

func TestReadRPCs_DeclaredGrantAloneSucceeds(t *testing.T) {
	for name, rpc := range readRPCs {
		t.Run(name, func(t *testing.T) {
			svc := newAuthorizingService(t, newAuthorizingTransport(rpc.grant))
			require.NoError(t, rpc.call(tenantctx.WithSystem(context.Background()), svc))
		})
	}
}

func TestReadRPCs_WithoutDeclaredGrantIsForbidden(t *testing.T) {
	for name, rpc := range readRPCs {
		t.Run(name, func(t *testing.T) {
			// The caller holds every other read grant, but not this RPC's.
			var others []string
			for _, other := range []string{permKVRange, permAdminTopology, permAdminShardState} {
				if other != rpc.grant {
					others = append(others, other)
				}
			}
			svc := newAuthorizingService(t, newAuthorizingTransport(others...))
			requirePermissionRefusal(t, rpc.call(tenantctx.WithSystem(context.Background()), svc))
		})
	}
}

// metricPoints seeds one in-range point for tenant A and one for the system
// keyspace, both under entity type "metric".
func metricPoints(tr *authorizingTransport) {
	tr.data["__t:tenant-a:metric:1"] = []byte(`{"id":"a1","timestamp":10,"value":7}`)
	tr.data["metric:1"] = []byte(`{"id":"s1","timestamp":10,"value":9}`)
}

func TestAggregate_SeesOnlyTheCallersTenant(t *testing.T) {
	tr := newAuthorizingTransport(permKVRange)
	metricPoints(tr)
	svc := newAuthorizingService(t, tr)
	req := &AggregateRequest{EntityType: "metric", Function: AggregateFunctionSum, StartTime: 1, EndTime: 100}

	resp, err := svc.Aggregate(tenantctx.WithTenant(context.Background(), "tenant-b"), req)
	require.NoError(t, err)
	assert.Zero(t, resp.Count, "tenant B must aggregate none of tenant A's or the system's points")

	resp, err = svc.Aggregate(tenantctx.WithTenant(context.Background(), "tenant-a"), req)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), resp.Count)
	assert.Equal(t, float64(7), resp.Result, "tenant A aggregates its own point only")
}

func TestTimeSeriesQuery_SeesOnlyTheCallersTenant(t *testing.T) {
	tr := newAuthorizingTransport(permKVRange)
	metricPoints(tr)
	svc := newAuthorizingService(t, tr)
	query := func(tenant string) []*TimeSeriesPoint {
		stream := make(chan *TimeSeriesQueryResponse, 16)
		err := svc.TimeSeriesQuery(tenantctx.WithTenant(context.Background(), tenant),
			&TimeSeriesQueryRequest{EntityType: "metric", StartTime: 1, EndTime: 100}, stream)
		require.NoError(t, err)
		var points []*TimeSeriesPoint
		for resp := range stream {
			points = append(points, resp.DataPoints...)
		}
		return points
	}

	assert.Empty(t, query("tenant-b"), "tenant B must read none of tenant A's or the system's points")
	points := query("tenant-a")
	require.Len(t, points, 1)
	assert.Equal(t, float64(7), points[0].Value, "tenant A reads its own point only")
}
