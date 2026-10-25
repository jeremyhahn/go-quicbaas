//go:build timeseries && !integration

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//   See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//   Contact licensing@automatethethings.com for commercial licensing options.

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// float64Ptr is a helper to create a *float64 for test data.

func TestLC_StartTimeSeries_NilStorage(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.timeSeriesStorage = nil
	srv.config.TimeSeries.StoragePath = t.TempDir()

	// startTimeSeries is a no-op when qrdbNode is nil and timeSeriesStorage
	// is nil (time series disabled). The storage is NOT initialized here;
	// initTimeSeriesStorage must be called explicitly on the legacy path.
	err := srv.startTimeSeries()
	assert.NoError(t, err)
	// Storage remains nil since neither qrdbNode nor pre-existing storage
	// is available in this test path.
	assert.Nil(t, srv.timeSeriesStorage)

	// stopTimeSeries with nil storage is a no-op.
	require.NoError(t, srv.stopTimeSeries())
}

func TestLC_StopTimeSeries_NilStorage(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.timeSeriesStorage = nil
	assert.NoError(t, srv.stopTimeSeries())
}
