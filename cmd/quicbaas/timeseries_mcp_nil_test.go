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

// Package main provides tests for MCP timeseries client nil-client and
// not-implemented code paths.
package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
)

// TestMCPTimeSeriesClient_TSWritePoint_NilClient verifies nil client returns error.
func TestMCPTimeSeriesClient_TSWritePoint_NilClient(t *testing.T) {
	client := &MCPTimeSeriesClient{client: nil}
	resp, err := client.TSWritePoint(context.Background(), &cli.WritePointRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MCP client not initialized")
}

// TestMCPTimeSeriesClient_TSWriteBatch_NilClient verifies nil client returns error.
func TestMCPTimeSeriesClient_TSWriteBatch_NilClient(t *testing.T) {
	client := &MCPTimeSeriesClient{client: nil}
	resp, err := client.TSWriteBatch(context.Background(), &cli.WriteBatchRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MCP client not initialized")
}

// TestMCPTimeSeriesClient_TSQuery_NilClient verifies nil client returns error.
func TestMCPTimeSeriesClient_TSQuery_NilClient(t *testing.T) {
	client := &MCPTimeSeriesClient{client: nil}
	resp, err := client.TSQuery(context.Background(), &cli.TSQueryRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MCP client not initialized")
}

// TestMCPTimeSeriesClient_TSDeleteSeries_NilClient verifies nil client returns error.
func TestMCPTimeSeriesClient_TSDeleteSeries_NilClient(t *testing.T) {
	client := &MCPTimeSeriesClient{client: nil}
	resp, err := client.TSDeleteSeries(context.Background(), &cli.TSDeleteRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MCP client not initialized")
}

// TestMCPTimeSeriesClient_TSGetRetentionPolicy_NilClient verifies nil client returns error.
func TestMCPTimeSeriesClient_TSGetRetentionPolicy_NilClient(t *testing.T) {
	client := &MCPTimeSeriesClient{client: nil}
	resp, err := client.TSGetRetentionPolicy(context.Background(), &cli.TSGetRetentionPolicyRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MCP client not initialized")
}

// TestMCPTimeSeriesClient_TSSetRetentionPolicy_NilClient verifies nil client returns error.
func TestMCPTimeSeriesClient_TSSetRetentionPolicy_NilClient(t *testing.T) {
	client := &MCPTimeSeriesClient{client: nil}
	resp, err := client.TSSetRetentionPolicy(context.Background(), &cli.TSSetRetentionPolicyRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MCP client not initialized")
}

// TestMCPTimeSeriesClient_TSListMetrics_NilClient verifies nil client returns error.
func TestMCPTimeSeriesClient_TSListMetrics_NilClient(t *testing.T) {
	client := &MCPTimeSeriesClient{client: nil}
	resp, err := client.TSListMetrics(context.Background(), &cli.TSListMetricsRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MCP client not initialized")
}

// TestMCPTimeSeriesClient_Close_NilClient verifies nil client close returns nil.
func TestMCPTimeSeriesClient_Close_NilClient(t *testing.T) {
	client := &MCPTimeSeriesClient{client: nil}
	err := client.Close()
	assert.NoError(t, err)
}
