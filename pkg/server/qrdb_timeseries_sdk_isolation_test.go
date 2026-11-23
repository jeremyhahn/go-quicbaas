//go:build !integration && qrdb_ts

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

// qrdb_timeseries_sdk_isolation_test.go drives the SDK's embedded
// Search().QRDBTimeSeries() against a real node through the broker's
// time-series proxy. It lives in the external test package because the SDK
// imports pkg/server.
package server_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	quicbaassdk "github.com/jeremyhahn/go-quicbaas/sdk/go"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// TestEmbeddedSDKTimeSeries_TenantIsolation pins that a tenant named with
// transport.ContextWithTenant scopes every embedded time-series verb from
// the SDK down to the node: globex and the system scope neither see nor
// overwrite acme's metric and policy.
func TestEmbeddedSDKTimeSeries_TenantIsolation(t *testing.T) {
	srv := server.NewScopedTimeSeriesTestServer(t)
	c, err := quicbaassdk.NewClient("embedded",
		quicbaassdk.WithEmbedded(&testutil.MockQuicBaaS{}),
		quicbaassdk.WithEmbeddedQRDB(quicbaassdk.QRDBEmbeddedServicers{
			KV:         srv.GetQRDBKVServicer(),
			Admin:      srv.GetQRDBAdminServicer(),
			TimeSeries: srv.QRDBTimeSeriesServicer(),
		}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ts, err := c.Search().QRDBTimeSeries()
	require.NoError(t, err)

	acme := sdktransport.ContextWithTenant(context.Background(), "acme")
	globex := sdktransport.ContextWithTenant(context.Background(), "globex")
	system := context.Background()
	hour := int64(time.Hour)
	now := time.Now().UnixNano()
	window := &qrdbtransport.TSQueryRequest{Metric: "m", StartNs: now - int64(time.Minute), EndNs: now + int64(time.Minute)}

	require.NoError(t, ts.TSWrite(acme, &qrdbtransport.TSDataPoint{Metric: "m", Value: 1, TimestampNs: now}))
	require.NoError(t, ts.TSSetRetention(acme, &qrdbtransport.TSSetRetentionRequest{
		Metric: "m", Policy: &qrdbtransport.TSRetentionPolicy{DurationNs: hour},
	}))

	acmePolicy := func() int64 {
		t.Helper()
		got, err := ts.TSGetRetention(acme, &qrdbtransport.TSGetRetentionRequest{Metric: "m"})
		require.NoError(t, err)
		require.NotNil(t, got.Policy, "acme reads its own policy")
		return got.Policy.DurationNs
	}
	require.Equal(t, hour, acmePolicy())
	metrics, err := ts.TSListMetrics(acme, &qrdbtransport.TSListMetricsRequest{})
	require.NoError(t, err)
	require.Contains(t, metrics.Metrics, "m")

	for name, ctx := range map[string]context.Context{"globex": globex, "system": system} {
		got, err := ts.TSGetRetention(ctx, &qrdbtransport.TSGetRetentionRequest{Metric: "m"})
		require.NoError(t, err, name)
		if got.Policy != nil {
			require.NotEqual(t, hour, got.Policy.DurationNs, "%s must not see acme's policy", name)
		}
		metrics, err := ts.TSListMetrics(ctx, &qrdbtransport.TSListMetricsRequest{})
		require.NoError(t, err, name)
		require.NotContains(t, metrics.Metrics, "m", "%s must not list acme's metric", name)
		points, err := ts.TSQuery(ctx, window)
		require.NoError(t, err, name)
		require.Empty(t, points.Points, "%s must not read acme's point", name)
	}

	require.NoError(t, ts.TSSetRetention(globex, &qrdbtransport.TSSetRetentionRequest{
		Metric: "m", Policy: &qrdbtransport.TSRetentionPolicy{DurationNs: 2 * hour},
	}))
	require.Equal(t, hour, acmePolicy(), "globex must not overwrite acme's policy")

	_, err = ts.TSDeleteSeries(globex, &qrdbtransport.TSDeleteRequest{Metric: "m"})
	require.NoError(t, err)
	points, err := ts.TSQuery(acme, window)
	require.NoError(t, err)
	require.Len(t, points.Points, 1, "globex must not delete acme's point")
	require.Equal(t, hour, acmePolicy())
}
