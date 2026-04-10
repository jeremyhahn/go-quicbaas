//go:build timeseries && mcp

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

// Package main provides tests for the MCP timeseries client when the underlying
// quicbaas.Client returns nil from TimeSeries() -- the "timeseries client not
// available" error path for TSWritePoint, TSWriteBatch, and TSQuery.
// Also covers the "not implemented" branches for TSDeleteSeries,
// TSGetRetentionPolicy, TSSetRetentionPolicy, and TSListMetrics when the
// client is non-nil.
package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
)

// --------------------------------------------------------------------------
// Tests that hit the "timeseries client not available" branch
// (c.client.TimeSeries() returns nil from mockClient)
// --------------------------------------------------------------------------

func TestMCPTimeSeriesClient_TSWritePoint_NilTSClient(t *testing.T) {
	client := &MCPTimeSeriesClient{client: &mockClient{}}
	resp, err := client.TSWritePoint(context.Background(), &cli.WritePointRequest{
		Metric: "cpu.usage",
		Value:  42.0,
	})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeseries client not available")
}

func TestMCPTimeSeriesClient_TSWriteBatch_NilTSClient(t *testing.T) {
	client := &MCPTimeSeriesClient{client: &mockClient{}}
	resp, err := client.TSWriteBatch(context.Background(), &cli.WriteBatchRequest{
		Points: []cli.WritePointRequest{
			{Metric: "cpu.usage", Value: 42.0},
		},
	})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeseries client not available")
}

func TestMCPTimeSeriesClient_TSQuery_NilTSClient(t *testing.T) {
	client := &MCPTimeSeriesClient{client: &mockClient{}}
	resp, err := client.TSQuery(context.Background(), &cli.TSQueryRequest{
		Metric: "cpu.usage",
	})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeseries client not available")
}

// --------------------------------------------------------------------------
// Tests that hit the "not implemented" branch with a valid (non-nil) client.
// These methods check c.client == nil first (passes since mockClient
// is non-nil), then return not-implemented directly.
// --------------------------------------------------------------------------

func TestMCPTimeSeriesClient_TSDeleteSeries_NotImplemented(t *testing.T) {
	client := &MCPTimeSeriesClient{client: &mockClient{}}
	resp, err := client.TSDeleteSeries(context.Background(), &cli.TSDeleteRequest{
		Metric: "cpu.usage",
	})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
}

func TestMCPTimeSeriesClient_TSGetRetentionPolicy_NotImplemented(t *testing.T) {
	client := &MCPTimeSeriesClient{client: &mockClient{}}
	resp, err := client.TSGetRetentionPolicy(context.Background(), &cli.TSGetRetentionPolicyRequest{
		Metric: "cpu.usage",
	})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
}

func TestMCPTimeSeriesClient_TSSetRetentionPolicy_NotImplemented(t *testing.T) {
	client := &MCPTimeSeriesClient{client: &mockClient{}}
	resp, err := client.TSSetRetentionPolicy(context.Background(), &cli.TSSetRetentionPolicyRequest{
		Metric: "cpu.usage",
	})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
}

func TestMCPTimeSeriesClient_TSListMetrics_NotImplemented(t *testing.T) {
	client := &MCPTimeSeriesClient{client: &mockClient{}}
	resp, err := client.TSListMetrics(context.Background(), &cli.TSListMetricsRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
}

func TestMCPTimeSeriesClient_Close_NonNilClient(t *testing.T) {
	client := &MCPTimeSeriesClient{client: &mockClient{}}
	err := client.Close()
	assert.NoError(t, err)
}
