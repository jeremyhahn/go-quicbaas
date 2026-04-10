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

package qrdb

// TimeSeriesInvalidQueryError indicates that a query passed to the
// TimeSeriesStoreAdapter was not the expected type.
type TimeSeriesInvalidQueryError struct {
	QueryType string
}

// Error returns the error message.
func (e *TimeSeriesInvalidQueryError) Error() string {
	return "timeseries: invalid query type for " + e.QueryType + ": expected *timeseries.QueryRequest"
}
