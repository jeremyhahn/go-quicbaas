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

// Package service provides the unified service facade for QuicBaaS.
// This file bridges local service types to the timeseries storage layer,
// centralizing all timeseries package imports for the service package.
package service

import (
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// toTimeSeriesDataPoint creates an interfaces.DataPoint from individual fields.
// If the timestamp is zero, the current time is used.
func toTimeSeriesDataPoint(metric string, tags map[string]string, value float64, ts time.Time) *interfaces.DataPoint {
	if ts.IsZero() {
		ts = time.Now()
	}
	return &interfaces.DataPoint{
		Metric:    metric,
		Tags:      tags,
		Value:     value,
		Timestamp: ts,
	}
}

// localPointsToStorageBatch converts a slice of local tsDataPoint pointers to
// a slice of interfaces.DataPoint values for the WriteBatch storage interface.
func localPointsToStorageBatch(points []*tsDataPoint) []interfaces.DataPoint {
	out := make([]interfaces.DataPoint, len(points))
	for i, p := range points {
		out[i] = interfaces.DataPoint{
			Metric:    p.Metric,
			Tags:      p.Tags,
			Value:     p.Value,
			Timestamp: p.Timestamp,
		}
	}
	return out
}

// newTimeSeriesQueryRequest creates an interfaces.QueryRequest from individual
// fields, bridging the gap between the service layer and the storage interface.
func newTimeSeriesQueryRequest(
	metric string,
	tags map[string]string,
	startTime, endTime time.Time,
	agg qrdbtransport.AggFunc,
	interval time.Duration,
	groupBy []string,
	limit int,
	percentile float64,
) *interfaces.QueryRequest {
	return &interfaces.QueryRequest{
		Metric:      metric,
		Tags:        tags,
		StartTime:   startTime,
		EndTime:     endTime,
		Aggregation: interfaces.AggFunc(agg),
		Interval:    interval,
		GroupBy:     groupBy,
		Limit:       limit,
		Percentile:  percentile,
	}
}

// newTimeSeriesDeleteRequest creates an interfaces.DeleteRequest from individual
// fields, bridging the gap between the service layer and the storage interface.
func newTimeSeriesDeleteRequest(metric string, tags map[string]string, startTime, endTime *time.Time) *interfaces.DeleteRequest {
	req := &interfaces.DeleteRequest{
		Metric: metric,
		Tags:   tags,
	}
	if startTime != nil {
		req.StartTime = *startTime
	}
	if endTime != nil {
		req.EndTime = *endTime
	}
	return req
}

// newTimeSeriesRetentionPolicy creates an interfaces.RetentionPolicy from individual
// fields, bridging the gap between the service layer and the storage interface.
func newTimeSeriesRetentionPolicy(duration, resolution time.Duration, agg qrdbtransport.AggFunc) *interfaces.RetentionPolicy {
	return &interfaces.RetentionPolicy{
		Duration:    duration,
		Resolution:  resolution,
		Aggregation: interfaces.AggFunc(agg),
	}
}
