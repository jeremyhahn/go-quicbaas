//go:build timeseries

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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
)

// =============================================================================
// TimeSeries – nil-conn error paths (getClient returns error)
// =============================================================================

func TestClient_TSWritePoint_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.TSWritePoint(context.Background(), &cli.WritePointRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "not connected")
}

func TestClient_TSWriteBatch_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.TSWriteBatch(context.Background(), &cli.WriteBatchRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "not connected")
}

func TestClient_TSQuery_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.TSQuery(context.Background(), &cli.TSQueryRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "not connected")
}

func TestClient_TSDeleteSeries_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.TSDeleteSeries(context.Background(), &cli.TSDeleteRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "not connected")
}

func TestClient_TSGetRetentionPolicy_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.TSGetRetentionPolicy(context.Background(), &cli.TSGetRetentionPolicyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "not connected")
}

func TestClient_TSSetRetentionPolicy_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.TSSetRetentionPolicy(context.Background(), &cli.TSSetRetentionPolicyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "not connected")
}

func TestClient_TSListMetrics_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.TSListMetrics(context.Background(), &cli.TSListMetricsRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "not connected")
}
