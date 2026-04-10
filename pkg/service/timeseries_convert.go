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

package service

import (
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// tsDataPoint is a local intermediate type used in the service layer to
// avoid importing the timeseries package directly. Converted to storage
// types at storage boundaries via the functions in
// timeseries_storage_bridge.go.
type tsDataPoint struct {
	Metric    string
	Tags      map[string]string
	Value     float64
	Timestamp time.Time
}

// localPointsToSDK converts a slice of local tsDataPoint pointers to
// qrdbtransport.TSDataPoint values for use with the SDK client.
func localPointsToSDK(points []*tsDataPoint) []qrdbtransport.TSDataPoint {
	out := make([]qrdbtransport.TSDataPoint, len(points))
	for i, p := range points {
		ts := p.Timestamp
		if ts.IsZero() {
			ts = time.Now()
		}
		out[i] = qrdbtransport.TSDataPoint{
			Metric:      p.Metric,
			Value:       p.Value,
			TimestampNs: ts.UnixNano(),
			Tags:        mapToSDKTags(p.Tags),
		}
	}
	return out
}
