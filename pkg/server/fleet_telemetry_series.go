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
	"slices"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// fleetTelemetrySeries files fleet device telemetry in the node's time-series
// storage — the qrdb node's own engine, reached through its SDK transport
// (node_timeseries_storage.go) — under each tenant's scoped metric names.
//
// Every fleet.device.* series carries a retention policy. It is applied to
// the series already stored at startup (ApplyRetention) and to a tenant's
// series on the tenant's first report after startup, so a tenant created
// while the node runs is covered too. Setting a policy is a keyed overwrite,
// so both are idempotent.
type fleetTelemetrySeries struct {
	storage   interfaces.TimeSeriesStorage
	retention time.Duration

	// asService binds a context to the server's service identity. The
	// server files the telemetry a node reported on its own behalf: the
	// reporting daemon's grant (iam.FleetRuntimeGrant) holds no time-series
	// permission, and must not.
	asService func(context.Context) (context.Context, error)

	// retained holds the tenants whose series carry the policy in this
	// process's lifetime; a tenant is added only once its policy is set.
	retained sync.Map
}

// FleetTelemetryRetentionError reports a retention policy the node's
// time-series engine refused for a fleet device series.
type FleetTelemetryRetentionError struct {
	Metric string
	Cause  error
}

// Error implements error.
func (e *FleetTelemetryRetentionError) Error() string {
	return "fleet telemetry: setting the retention of " + e.Metric + ": " + e.Cause.Error()
}

// Unwrap returns the engine's error.
func (e *FleetTelemetryRetentionError) Unwrap() error { return e.Cause }

// FleetTelemetryRetentionInvalidError reports a retention that is not a
// positive duration.
type FleetTelemetryRetentionInvalidError struct {
	Retention time.Duration
}

// Error implements error.
func (e *FleetTelemetryRetentionInvalidError) Error() string {
	return "fleet telemetry: retention must be positive; got " + e.Retention.String()
}

// Compile-time interface check.
var _ orchestration.TelemetrySeries = (*fleetTelemetrySeries)(nil)

// errFleetTelemetryNoStorage reports a series built over no storage.
var errFleetTelemetryNoStorage = errors.New("fleet telemetry: time-series storage is not configured")

// errFleetTelemetryNoServiceIdentity reports a series built with no way to
// bind the server's service identity.
var errFleetTelemetryNoServiceIdentity = errors.New("fleet telemetry: no service identity binder is configured")

// newFleetTelemetrySeries returns a TelemetrySeries over storage whose series
// keep retention of raw points. Every storage call runs under the context
// asService returns (Server.ServiceContext).
func newFleetTelemetrySeries(storage interfaces.TimeSeriesStorage, retention time.Duration, asService func(context.Context) (context.Context, error)) (*fleetTelemetrySeries, error) {
	if storage == nil {
		return nil, errFleetTelemetryNoStorage
	}
	if retention <= 0 {
		return nil, &FleetTelemetryRetentionInvalidError{Retention: retention}
	}
	if asService == nil {
		return nil, errFleetTelemetryNoServiceIdentity
	}
	return &fleetTelemetrySeries{storage: storage, retention: retention, asService: asService}, nil
}

// bind binds ctx to the service identity in the system tenant. The
// service principal is a system-tenant principal, so it acts in the system
// tenant whichever tenant the reporting request was bound to; the series
// keeps each tenant's data apart in the metric name
// (service.TenantScopedMetric).
func (f *fleetTelemetrySeries) bind(ctx context.Context) (context.Context, error) {
	return f.asService(tenantctx.WithSystem(ctx))
}

// WritePoints implements orchestration.TelemetrySeries. The points are written
// as one batch, so a heartbeat's telemetry lands whole or not at all. The
// tenant's first write in this process also sets its series' retention; a
// failure to is reported after the points are filed, and retried next write.
func (f *fleetTelemetrySeries) WritePoints(ctx context.Context, tenantID string, points []orchestration.TelemetryPoint) error {
	ctx, err := f.bind(ctx)
	if err != nil {
		return err
	}
	batch := make([]interfaces.DataPoint, len(points))
	for i, p := range points {
		batch[i] = interfaces.DataPoint{
			Metric:    service.TenantScopedMetric(tenantID, p.Metric),
			Tags:      p.Tags,
			Value:     p.Value,
			Timestamp: p.At,
		}
	}
	if err := f.storage.WriteBatch(ctx, batch); err != nil {
		return err
	}
	return f.retainTenant(ctx, tenantID)
}

// ApplyRetention sets the retention policy on every fleet device series
// already stored, under every tenant. It is run at startup, so series whose
// tenants never report again still age out.
func (f *fleetTelemetrySeries) ApplyRetention(ctx context.Context) error {
	ctx, err := f.bind(ctx)
	if err != nil {
		return err
	}
	stored, err := f.storage.ListMetrics(ctx, "", 0)
	if err != nil {
		return err
	}
	for _, metric := range stored {
		if !isFleetDeviceSeries(metric) {
			continue
		}
		if err := f.setRetention(ctx, metric); err != nil {
			return err
		}
	}
	return nil
}

// retainTenant sets the retention policy on tenantID's fleet device series
// once per process lifetime.
func (f *fleetTelemetrySeries) retainTenant(ctx context.Context, tenantID string) error {
	if _, done := f.retained.Load(tenantID); done {
		return nil
	}
	for _, metric := range orchestration.DeviceMetrics() {
		if err := f.setRetention(ctx, service.TenantScopedMetric(tenantID, metric)); err != nil {
			return err
		}
	}
	f.retained.Store(tenantID, struct{}{})
	return nil
}

// setRetention keeps metric's raw points for the series' retention.
func (f *fleetTelemetrySeries) setRetention(ctx context.Context, metric string) error {
	policy := &interfaces.RetentionPolicy{Duration: f.retention, Aggregation: interfaces.AggNone}
	if err := f.storage.SetRetentionPolicy(ctx, metric, policy); err != nil {
		return &FleetTelemetryRetentionError{Metric: metric, Cause: err}
	}
	return nil
}

// isFleetDeviceSeries reports whether a stored metric name is a fleet device
// series under any tenant.
func isFleetDeviceSeries(stored string) bool {
	_, metric, ok := service.SplitTenantScopedMetric(stored)
	return ok && slices.Contains(orchestration.DeviceMetrics(), metric)
}

// QueryPoints implements orchestration.TelemetrySeries. The server reads the
// telemetry it filed to cost a turn, so the read runs under its service
// identity too; the tenant scope is the metric name's.
func (f *fleetTelemetrySeries) QueryPoints(ctx context.Context, tenantID, metric string, match map[string]string, from, to time.Time) ([]orchestration.TelemetryPoint, error) {
	ctx, err := f.bind(ctx)
	if err != nil {
		return nil, err
	}
	result, err := f.storage.QueryRich(ctx, &interfaces.QueryRequest{
		Metric:      service.TenantScopedMetric(tenantID, metric),
		Tags:        match,
		StartTime:   from,
		EndTime:     to,
		Aggregation: interfaces.AggNone,
	})
	if err != nil {
		return nil, err
	}
	points := make([]orchestration.TelemetryPoint, 0, len(result.Points))
	for _, p := range result.Points {
		if p == nil {
			continue
		}
		points = append(points, orchestration.TelemetryPoint{
			Metric: metric,
			Tags:   p.Tags,
			Value:  p.Value,
			At:     p.Timestamp,
		})
	}
	return points, nil
}
