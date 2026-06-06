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

//go:build timeseries

package server

import (
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// TestNewNodeTimeSeriesStorage_NilClient verifies construction with a nil
// client is safe. Operations will fail, but construction must not panic.
func TestNewNodeTimeSeriesStorage_NilClient(t *testing.T) {
	storage := newNodeTimeSeriesStorage(nil)
	require.NotNil(t, storage)
}

// TestNodeTimeSeriesStorage_Type verifies the concrete type implements the interface.
func TestNodeTimeSeriesStorage_Type(t *testing.T) {
	storage := newNodeTimeSeriesStorage(nil)
	_, ok := storage.(*nodeTimeSeriesStorage)
	assert.True(t, ok, "newNodeTimeSeriesStorage must return a *nodeTimeSeriesStorage")
}

// TestToTSDataPoint_NilInput verifies that a nil DataPoint returns nil.
func TestToTSDataPoint_NilInput(t *testing.T) {
	result := toTSDataPoint(nil)
	assert.Nil(t, result)
}

// TestToTSDataPoint_RoundTrip verifies that an interfaces.DataPoint converts
// to a qrdb TSDataPoint and back with the same values.
func TestToTSDataPoint_RoundTrip(t *testing.T) {
	ts := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	point := &interfaces.DataPoint{
		Metric:    "cpu.usage",
		Value:     0.75,
		Timestamp: ts,
		Tags:      map[string]string{"host": "node01"},
	}

	tsPoint := toTSDataPoint(point)
	require.NotNil(t, tsPoint)
	assert.Equal(t, "cpu.usage", tsPoint.Metric)
	assert.InDelta(t, 0.75, tsPoint.Value, 1e-9)
	assert.Equal(t, ts.UnixNano(), tsPoint.TimestampNs)
	require.Len(t, tsPoint.Tags, 1)
	assert.Equal(t, "host", tsPoint.Tags[0].Key)
	assert.Equal(t, "node01", tsPoint.Tags[0].Value)

	// Reverse conversion
	back := fromTSDataPoint(tsPoint)
	require.NotNil(t, back)
	assert.Equal(t, point.Metric, back.Metric)
	assert.InDelta(t, point.Value, back.Value, 1e-9)
	assert.Equal(t, point.Timestamp.UnixNano(), back.Timestamp.UnixNano(), "timestamp nanoseconds should be equal")
	assert.Equal(t, "node01", back.Tags["host"])
}

// TestIfaceTagsToTS_Empty verifies that an empty or nil map produces a nil
// tag slice (avoids allocating an empty slice).
func TestIfaceTagsToTS_Empty(t *testing.T) {
	assert.Nil(t, ifaceTagsToTS(nil))
	assert.Nil(t, ifaceTagsToTS(map[string]string{}))
}

// TestTSTagsToIface_Empty verifies that an empty tag slice produces a nil map.
func TestTSTagsToIface_Empty(t *testing.T) {
	assert.Nil(t, tsTagsToIface(nil))
	assert.Nil(t, tsTagsToIface([]qrdbtransport.TSTag{}))
}

// TestFromTSDataPoint_NilInput verifies that a nil transport point returns nil
// rather than dereferencing it.
func TestFromTSDataPoint_NilInput(t *testing.T) {
	assert.Nil(t, fromTSDataPoint(nil))
}
