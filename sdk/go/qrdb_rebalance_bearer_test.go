// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

package quicbaas

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

// fakeBearerRebalanceClient records the authorization metadata each call
// carries and answers with fixed, distinguishable responses.
type fakeBearerRebalanceClient struct {
	seen   map[string][]string
	stats  *QRDBShardStatsResponse
	plan   *QRDBRebalancePlan
	result *QRDBRebalanceResult
	report *QRDBImbalanceReport
}

func (f *fakeBearerRebalanceClient) record(ctx context.Context, verb string) {
	md, _ := metadata.FromOutgoingContext(ctx)
	f.seen[verb] = md.Get("authorization")
}

func (f *fakeBearerRebalanceClient) GetShardStats(ctx context.Context, _ uint64) (*QRDBShardStatsResponse, error) {
	f.record(ctx, "GetShardStats")
	return f.stats, nil
}

func (f *fakeBearerRebalanceClient) CreateRebalancePlan(ctx context.Context, _ *QRDBRebalancePlanRequest) (*QRDBRebalancePlan, error) {
	f.record(ctx, "CreateRebalancePlan")
	return f.plan, nil
}

func (f *fakeBearerRebalanceClient) ExecuteRebalance(ctx context.Context, _ *QRDBRebalancePlan) (*QRDBRebalanceResult, error) {
	f.record(ctx, "ExecuteRebalance")
	return f.result, nil
}

func (f *fakeBearerRebalanceClient) GetImbalanceReport(ctx context.Context) (*QRDBImbalanceReport, error) {
	f.record(ctx, "GetImbalanceReport")
	return f.report, nil
}

func TestQRDBRebalance_BearerClientForwardsEveryVerb(t *testing.T) {
	fake := &fakeBearerRebalanceClient{
		seen:   map[string][]string{},
		stats:  &QRDBShardStatsResponse{ShardID: 7},
		plan:   &QRDBRebalancePlan{},
		result: &QRDBRebalanceResult{},
		report: &QRDBImbalanceReport{},
	}
	b := &qrdbBearerRebalanceClient{inner: fake, bearer: "Bearer x"}
	ctx := context.Background()

	stats, err := b.GetShardStats(ctx, 7)
	require.NoError(t, err)
	assert.Same(t, fake.stats, stats)
	assert.Equal(t, uint64(7), stats.ShardID)

	plan, err := b.CreateRebalancePlan(ctx, &QRDBRebalancePlanRequest{})
	require.NoError(t, err)
	assert.Same(t, fake.plan, plan)

	result, err := b.ExecuteRebalance(ctx, plan)
	require.NoError(t, err)
	assert.Same(t, fake.result, result)

	report, err := b.GetImbalanceReport(ctx)
	require.NoError(t, err)
	assert.Same(t, fake.report, report)

	for _, verb := range []string{"GetShardStats", "CreateRebalancePlan", "ExecuteRebalance", "GetImbalanceReport"} {
		assert.Equal(t, []string{"Bearer x"}, fake.seen[verb], verb)
	}
}
