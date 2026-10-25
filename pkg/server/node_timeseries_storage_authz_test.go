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

//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

package server

import (
	"context"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// tsAuthzPrincipal is the service principal of the authorization-enabled
// time-series node. It is granted every time-series permission and nothing
// else, so a call succeeds only when it carries this principal's identity.
const tsAuthzPrincipal = "quicbaas-timeseries-authz-test"

// newAuthzTSNode boots a pebble-backed standalone node that enforces
// authorization: a call whose context carries no identity is refused.
func newAuthzTSNode(t *testing.T) *qrdbnode.Node {
	t.Helper()
	qrdbsdk.RegisterPermissionRoleGrant(tsAuthzPrincipal,
		"timeseries:write_batch",
		"timeseries:get_retention",
		"timeseries:set_retention",
		"timeseries:list_metrics",
	)
	cfg := &qrdbsdk.DaemonConfig{}
	cfg.Raft.DataDir = t.TempDir()
	// Only a pebble store carries the time-series engine.
	cfg.Databases = []qrdbsdk.DaemonDatabaseConfig{
		{Name: "default", Backend: "pebble"},
	}
	node, err := qrdbnode.NewStandaloneNode(context.Background(), cfg, nil,
		qrdbnode.WithServicePrincipal(tsAuthzPrincipal))
	require.NoError(t, err)
	t.Cleanup(func() { _ = node.Close() })
	return node
}

// TestNodeTimeSeriesStorage_CarriesCallerIdentity proves retention and metric
// listing reach qrdb under the caller's identity: an authorized caller
// succeeds, and a caller with no identity gets qrdb's typed unauthenticated
// error rather than an internal one.
func TestNodeTimeSeriesStorage_CarriesCallerIdentity(t *testing.T) {
	node := newAuthzTSNode(t)
	ts := newNodeTimeSeriesStorage(node.Client())

	authorized, err := node.ServiceContext(context.Background())
	require.NoError(t, err)

	require.NoError(t, ts.WriteBatch(authorized, []interfaces.DataPoint{
		{Metric: "cpu.usage", Timestamp: time.Now(), Value: 1},
	}))
	policy := &interfaces.RetentionPolicy{Duration: time.Hour, Resolution: time.Minute, Aggregation: interfaces.AggAvg}
	require.NoError(t, ts.SetRetentionPolicy(authorized, "cpu.usage", policy))
	got, err := ts.GetRetentionPolicy(authorized, "cpu.usage")
	require.NoError(t, err)
	assert.Equal(t, time.Hour, got.Duration)
	metrics, err := ts.ListMetrics(authorized, "", 10)
	require.NoError(t, err)
	assert.Contains(t, metrics, "cpu.usage")

	anonymous := context.Background()
	_, err = ts.GetRetentionPolicy(anonymous, "cpu.usage")
	assert.ErrorIs(t, err, qrdbsdk.ErrUnauthenticated)
	assert.ErrorIs(t, ts.SetRetentionPolicy(anonymous, "cpu.usage", policy), qrdbsdk.ErrUnauthenticated)
	_, err = ts.ListMetrics(anonymous, "", 10)
	assert.ErrorIs(t, err, qrdbsdk.ErrUnauthenticated)
}
