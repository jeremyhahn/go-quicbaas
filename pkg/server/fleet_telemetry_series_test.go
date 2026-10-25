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

package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// recordingTSStorage is an interfaces.TimeSeriesStorage test double that
// records what it was asked and answers with canned points.
type recordingTSStorage struct {
	interfaces.TimeSeriesStorage
	batch        []interfaces.DataPoint
	query        *interfaces.QueryRequest
	answer       []*interfaces.DataPoint
	stored       []string
	policies     map[string]*interfaces.RetentionPolicy
	writeErr     error
	queryErr     error
	listErr      error
	retentionErr error
}

func (r *recordingTSStorage) ListMetrics(_ context.Context, _ string, _ int) ([]string, error) {
	return r.stored, r.listErr
}

func (r *recordingTSStorage) SetRetentionPolicy(_ context.Context, metric string, policy *interfaces.RetentionPolicy) error {
	if r.retentionErr != nil {
		return r.retentionErr
	}
	if r.policies == nil {
		r.policies = map[string]*interfaces.RetentionPolicy{}
	}
	r.policies[metric] = policy
	return nil
}

func (r *recordingTSStorage) WriteBatch(_ context.Context, points []interfaces.DataPoint) error {
	r.batch = points
	return r.writeErr
}

func (r *recordingTSStorage) QueryRich(_ context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
	r.query = req
	if r.queryErr != nil {
		return nil, r.queryErr
	}
	return &interfaces.QueryResult{Metric: req.Metric, Points: r.answer}, nil
}

// asItself is a service-identity binder for storage that authorizes nothing.
func asItself(ctx context.Context) (context.Context, error) { return ctx, nil }

func TestFleetTelemetrySeries_RequiresStorage(t *testing.T) {
	_, err := newFleetTelemetrySeries(nil, time.Hour, asItself)
	assert.ErrorIs(t, err, errFleetTelemetryNoStorage)
}

func TestFleetTelemetrySeries_RequiresAServiceIdentity(t *testing.T) {
	_, err := newFleetTelemetrySeries(&recordingTSStorage{}, time.Hour, nil)
	assert.ErrorIs(t, err, errFleetTelemetryNoServiceIdentity)
}

// A series whose service identity cannot be bound files nothing and reads
// nothing: it never falls back to the caller's identity.
func TestFleetTelemetrySeries_AnUnboundServiceIdentityStopsEveryCall(t *testing.T) {
	unbound := errors.New("no service principal")
	storage := &recordingTSStorage{}
	series, err := newFleetTelemetrySeries(storage, time.Hour, func(context.Context) (context.Context, error) {
		return nil, unbound
	})
	require.NoError(t, err)
	ctx := context.Background()

	assert.ErrorIs(t, series.WritePoints(ctx, "acme", []orchestration.TelemetryPoint{{Metric: orchestration.MetricDevicePowerWatts, Value: 1, At: time.Now()}}), unbound)
	assert.ErrorIs(t, series.ApplyRetention(ctx), unbound)
	_, err = series.QueryPoints(ctx, "acme", orchestration.MetricDevicePowerWatts, nil, time.Now().Add(-time.Hour), time.Now())
	assert.ErrorIs(t, err, unbound)
	assert.Empty(t, storage.batch)
	assert.Nil(t, storage.query)
}

func TestFleetTelemetrySeries_RequiresAPositiveRetention(t *testing.T) {
	for _, retention := range []time.Duration{0, -time.Hour} {
		_, err := newFleetTelemetrySeries(&recordingTSStorage{}, retention, asItself)
		var invalid *FleetTelemetryRetentionInvalidError
		assert.ErrorAs(t, err, &invalid, "retention %s", retention)
	}
}

func TestFleetTelemetrySeries_FirstWriteRetainsTheTenantsSeriesOnce(t *testing.T) {
	storage := &recordingTSStorage{}
	series, err := newFleetTelemetrySeries(storage, 14*24*time.Hour, asItself)
	require.NoError(t, err)
	point := []orchestration.TelemetryPoint{{Metric: orchestration.MetricDevicePowerWatts, Value: 1, At: time.Now()}}

	require.NoError(t, series.WritePoints(context.Background(), "acme", point))
	require.Len(t, storage.policies, len(orchestration.DeviceMetrics()))
	for _, metric := range orchestration.DeviceMetrics() {
		policy := storage.policies["ts-tenant/acme/"+metric]
		require.NotNil(t, policy, metric)
		assert.Equal(t, 14*24*time.Hour, policy.Duration)
		assert.Equal(t, interfaces.AggNone, policy.Aggregation, "raw points are kept, not downsampled")
	}

	// A second write does not set the policy again.
	storage.policies = nil
	require.NoError(t, series.WritePoints(context.Background(), "acme", point))
	assert.Empty(t, storage.policies)
}

func TestFleetTelemetrySeries_ARefusedRetentionIsReportedAndRetried(t *testing.T) {
	storage := &recordingTSStorage{retentionErr: errors.New("sealed")}
	series, err := newFleetTelemetrySeries(storage, time.Hour, asItself)
	require.NoError(t, err)
	point := []orchestration.TelemetryPoint{{Metric: orchestration.MetricDeviceUtilization, Value: 0.5, At: time.Now()}}

	err = series.WritePoints(context.Background(), "acme", point)
	var refused *FleetTelemetryRetentionError
	require.ErrorAs(t, err, &refused)
	assert.ErrorIs(t, err, storage.retentionErr)
	assert.Len(t, storage.batch, 1, "the points are filed even when the policy is not")

	storage.retentionErr = nil
	require.NoError(t, series.WritePoints(context.Background(), "acme", point))
	assert.Len(t, storage.policies, len(orchestration.DeviceMetrics()), "the next write retries")
}

func TestFleetTelemetrySeries_ApplyRetentionCoversEveryStoredDeviceSeries(t *testing.T) {
	storage := &recordingTSStorage{stored: []string{
		"ts-tenant/acme/" + orchestration.MetricDevicePowerWatts,
		"ts-tenant/globex/" + orchestration.MetricDeviceMemoryUsedBytes,
		orchestration.MetricDeviceUtilization,
		"ts-tenant/acme/app.requests",
		"app/" + orchestration.MetricDevicePowerWatts,
		"ts-tenant/" + orchestration.MetricDevicePowerWatts,
	}}
	series, err := newFleetTelemetrySeries(storage, 7*24*time.Hour, asItself)
	require.NoError(t, err)

	require.NoError(t, series.ApplyRetention(context.Background()))
	assert.Len(t, storage.policies, 3)
	for _, metric := range storage.stored[:3] {
		require.Contains(t, storage.policies, metric)
		assert.Equal(t, 7*24*time.Hour, storage.policies[metric].Duration)
	}
}

func TestFleetTelemetrySeries_ApplyRetentionReportsFailures(t *testing.T) {
	storage := &recordingTSStorage{listErr: errors.New("closed")}
	series, err := newFleetTelemetrySeries(storage, time.Hour, asItself)
	require.NoError(t, err)
	assert.ErrorIs(t, series.ApplyRetention(context.Background()), storage.listErr)

	storage.listErr = nil
	storage.stored = []string{orchestration.MetricDevicePowerWatts}
	storage.retentionErr = errors.New("read-only")
	var refused *FleetTelemetryRetentionError
	require.ErrorAs(t, series.ApplyRetention(context.Background()), &refused)
	assert.Equal(t, orchestration.MetricDevicePowerWatts, refused.Metric)
}

func TestFleetTelemetrySeries_WritesUnderTheTenantsScopedMetric(t *testing.T) {
	storage := &recordingTSStorage{}
	series, err := newFleetTelemetrySeries(storage, time.Hour, asItself)
	require.NoError(t, err)
	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	require.NoError(t, series.WritePoints(context.Background(), "acme", []orchestration.TelemetryPoint{
		{Metric: orchestration.MetricDevicePowerWatts, Tags: map[string]string{"host": "h:1"}, Value: 300, At: at},
	}))
	require.Len(t, storage.batch, 1)
	assert.Equal(t, "ts-tenant/acme/"+orchestration.MetricDevicePowerWatts, storage.batch[0].Metric)
	assert.Equal(t, at, storage.batch[0].Timestamp)

	storage.writeErr = errors.New("raft down")
	assert.ErrorIs(t, series.WritePoints(context.Background(), "acme", nil), storage.writeErr)
}

func TestFleetTelemetrySeries_QueriesTheScopedMetricWithItsTags(t *testing.T) {
	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	storage := &recordingTSStorage{answer: []*interfaces.DataPoint{
		{Metric: "ts-tenant/acme/fleet.device.power_watts", Value: 250, Timestamp: at, Tags: map[string]string{"device": "GPU-1"}},
		nil,
	}}
	series, err := newFleetTelemetrySeries(storage, time.Hour, asItself)
	require.NoError(t, err)

	match := map[string]string{"host": "h:1", "kind": "gpu"}
	points, err := series.QueryPoints(context.Background(), "acme", orchestration.MetricDevicePowerWatts, match, at.Add(-time.Minute), at)
	require.NoError(t, err)
	assert.Equal(t, "ts-tenant/acme/"+orchestration.MetricDevicePowerWatts, storage.query.Metric)
	assert.Equal(t, match, storage.query.Tags)
	assert.Equal(t, interfaces.AggNone, storage.query.Aggregation, "the integration needs raw points")
	require.Len(t, points, 1)
	assert.Equal(t, orchestration.MetricDevicePowerWatts, points[0].Metric, "callers see the unscoped name")
	assert.Equal(t, "GPU-1", points[0].Tags["device"])

	storage.queryErr = errors.New("closed")
	_, err = series.QueryPoints(context.Background(), "acme", "m", nil, at, at)
	assert.ErrorIs(t, err, storage.queryErr)
}
