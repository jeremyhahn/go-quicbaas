//go:build timeseries && mcp

package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
)

// retentionTSClient is an SDK time-series client that keeps the last
// retention policy set and answers get with it.
type retentionTSClient struct {
	timeseries.Client
	gotSet *timeseries.SetRetentionRequest
	policy *timeseries.RetentionPolicy
	err    error
}

func (f *retentionTSClient) SetRetention(_ context.Context, req *timeseries.SetRetentionRequest) error {
	f.gotSet = req
	if f.err != nil {
		return f.err
	}
	p := req.Policy
	f.policy = &p
	return nil
}

func (f *retentionTSClient) GetRetention(_ context.Context, req *timeseries.GetRetentionRequest) (*timeseries.GetRetentionResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &timeseries.GetRetentionResponse{Metric: req.Metric, Policy: f.policy}, nil
}

// retentionSDKClient is a mockClient whose TimeSeries() is the fake above.
type retentionSDKClient struct {
	*mockClient
	ts *retentionTSClient
}

func (c *retentionSDKClient) TimeSeries() quicbaas.TimeSeriesClient { return c.ts }

func TestMCPTimeSeriesClient_RetentionRoundTrip(t *testing.T) {
	ts := &retentionTSClient{}
	client := &MCPTimeSeriesClient{client: &retentionSDKClient{mockClient: &mockClient{}, ts: ts}}
	ctx := context.Background()

	resp, err := client.TSGetRetentionPolicy(ctx, &cli.TSGetRetentionPolicyRequest{Metric: "cpu.usage"})
	require.NoError(t, err)
	assert.Equal(t, "cpu.usage", resp.Metric)
	assert.Nil(t, resp.Policy, "a metric without a policy must report none")

	setResp, err := client.TSSetRetentionPolicy(ctx, &cli.TSSetRetentionPolicyRequest{
		Metric: "cpu.usage",
		Policy: &cli.TSRetentionPolicy{Duration: 24 * time.Hour, Resolution: 5 * time.Minute, Aggregation: "max"},
	})
	require.NoError(t, err)
	assert.True(t, setResp.Success)
	require.NotNil(t, ts.gotSet)
	assert.Equal(t, "cpu.usage", ts.gotSet.Metric)
	assert.Equal(t, timeseries.RetentionPolicy{Duration: 24 * time.Hour, Resolution: 5 * time.Minute, Aggregation: timeseries.AggregateMax}, ts.gotSet.Policy)

	resp, err = client.TSGetRetentionPolicy(ctx, &cli.TSGetRetentionPolicyRequest{Metric: "cpu.usage"})
	require.NoError(t, err)
	require.NotNil(t, resp.Policy)
	assert.Equal(t, cli.TSRetentionPolicy{Duration: 24 * time.Hour, Resolution: 5 * time.Minute, Aggregation: "max"}, *resp.Policy)
}

func TestMCPTimeSeriesClient_DurationOnlyRetentionSendsAggregateNone(t *testing.T) {
	ts := &retentionTSClient{}
	client := &MCPTimeSeriesClient{client: &retentionSDKClient{mockClient: &mockClient{}, ts: ts}}
	ctx := context.Background()

	_, err := client.TSSetRetentionPolicy(ctx, &cli.TSSetRetentionPolicyRequest{
		Metric: "cpu.usage",
		Policy: &cli.TSRetentionPolicy{Duration: 24 * time.Hour, Aggregation: "avg"},
	})
	require.NoError(t, err)
	require.NotNil(t, ts.gotSet)
	assert.Equal(t, timeseries.AggregateNone, ts.gotSet.Policy.Aggregation)

	_, err = client.TSSetRetentionPolicy(ctx, &cli.TSSetRetentionPolicyRequest{
		Metric: "cpu.usage",
		Policy: &cli.TSRetentionPolicy{Duration: 24 * time.Hour, Resolution: 300 * time.Second, Aggregation: "avg"},
	})
	require.NoError(t, err)
	assert.Equal(t, timeseries.AggregateAvg, ts.gotSet.Policy.Aggregation)
}

func TestMCPTimeSeriesClient_RetentionErrorsAreReturned(t *testing.T) {
	ts := &retentionTSClient{err: assert.AnError}
	client := &MCPTimeSeriesClient{client: &retentionSDKClient{mockClient: &mockClient{}, ts: ts}}
	ctx := context.Background()

	_, err := client.TSSetRetentionPolicy(ctx, &cli.TSSetRetentionPolicyRequest{
		Metric: "cpu.usage", Policy: &cli.TSRetentionPolicy{Duration: time.Hour},
	})
	assert.ErrorIs(t, err, assert.AnError)
	_, err = client.TSGetRetentionPolicy(ctx, &cli.TSGetRetentionPolicyRequest{Metric: "cpu.usage"})
	assert.ErrorIs(t, err, assert.AnError)
}
