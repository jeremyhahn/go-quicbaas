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

// Package timeseries provides the time-series client for the QuicBaaS SDK.
//
// The TimeSeries client supports writing and querying time-series data with
// support for metric names, tags, timestamps, and various aggregation functions.
//
// # Basic Usage
//
//	client := timeseries.NewClient(transport)
//
//	// Write a data point
//	err := client.Write(ctx, &timeseries.WriteRequest{
//	    Metric:    "cpu.usage",
//	    Tags:      map[string]string{"host": "server1", "region": "us-east"},
//	    Timestamp: time.Now(),
//	    Value:     75.5,
//	})
//
//	// Query data with aggregation
//	resp, err := client.Query(ctx, &timeseries.QueryRequest{
//	    Metric:    "cpu.usage",
//	    Tags:      map[string]string{"host": "server1"},
//	    Start:     time.Now().Add(-1 * time.Hour),
//	    End:       time.Now(),
//	    Aggregate: timeseries.AggregateAvg,
//	    Interval:  5 * time.Minute,
//	})
//
// # Aggregation Functions
//
// The following aggregation functions are supported:
//
//   - AggregateNone: No aggregation, return raw data points
//   - AggregateSum: Sum of values in each interval
//   - AggregateAvg: Average of values in each interval
//   - AggregateMin: Minimum value in each interval
//   - AggregateMax: Maximum value in each interval
//   - AggregateCount: Count of values in each interval
//
// # Query Intervals
//
// When using aggregation, the Interval field specifies the time bucket size.
// Data points within each interval are aggregated according to the specified
// function. If Interval is zero with a non-None aggregate, a validation error
// is returned.
package timeseries
