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

package orchestration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memorySeries is an in-memory TelemetrySeries test double: points are kept
// per tenant and filtered by metric, tags and time exactly as the time-series
// engine filters them.
type memorySeries struct {
	mu       sync.Mutex
	points   map[string][]TelemetryPoint
	writeErr error
	queryErr error
}

func newMemorySeries() *memorySeries {
	return &memorySeries{points: make(map[string][]TelemetryPoint)}
}

func (m *memorySeries) WritePoints(_ context.Context, tenantID string, points []TelemetryPoint) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.points[tenantID] = append(m.points[tenantID], points...)
	return nil
}

func (m *memorySeries) QueryPoints(_ context.Context, tenantID, metric string, match map[string]string, from, to time.Time) ([]TelemetryPoint, error) {
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []TelemetryPoint
	for _, p := range m.points[tenantID] {
		if p.Metric != metric || p.At.Before(from) || p.At.After(to) {
			continue
		}
		matched := true
		for k, v := range match {
			if p.Tags[k] != v {
				matched = false
			}
		}
		if matched {
			out = append(out, p)
		}
	}
	return out, nil
}

func gpuSample(id string, watts float64) DeviceSample {
	return DeviceSample{
		Kind:             DeviceGPU,
		ID:               id,
		Name:             "RTX",
		PowerWatts:       Measured(watts),
		Utilization:      Measured(0.9),
		MemoryUsedBytes:  Measured(1024),
		MemoryTotalBytes: Measured(4096),
	}
}

func TestDeviceSample_Validate(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		sample DeviceSample
		field  string
	}{
		"valid gpu":           {gpuSample("GPU-1", 300), ""},
		"unread signals":      {DeviceSample{Kind: DeviceNPU, ID: "accel0"}, ""},
		"unknown kind":        {DeviceSample{Kind: "fpga", ID: "x"}, "kind"},
		"missing id":          {DeviceSample{Kind: DeviceGPU, ID: " "}, "id"},
		"negative power":      {DeviceSample{Kind: DeviceGPU, ID: "g", PowerWatts: Measured(-1)}, "power_watts"},
		"negative memory":     {DeviceSample{Kind: DeviceGPU, ID: "g", MemoryUsedBytes: Measured(-1)}, "memory_used_bytes"},
		"utilization above 1": {DeviceSample{Kind: DeviceGPU, ID: "g", Utilization: Measured(1.5)}, "utilization"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := tc.sample.Validate()
			if tc.field == "" {
				require.NoError(t, err)
				return
			}
			var invalid *DeviceTelemetryInvalidError
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, tc.field, invalid.Field)
			assert.ErrorIs(t, err, ErrDeviceTelemetryInvalid)
		})
	}
}

func TestParseDeviceKind(t *testing.T) {
	t.Parallel()
	kind, ok := ParseDeviceKind("npu")
	assert.True(t, ok)
	assert.Equal(t, DeviceNPU, kind)
	_, ok = ParseDeviceKind("tpu")
	assert.False(t, ok)
}

func TestNewFleetTelemetryStore_RequiresSeries(t *testing.T) {
	t.Parallel()
	_, err := NewFleetTelemetryStore(nil)
	assert.ErrorIs(t, err, ErrDeviceTelemetryInvalid)
}

func TestFleetTelemetryStore_RecordFilesKnownReadingsOnly(t *testing.T) {
	t.Parallel()
	series := newMemorySeries()
	store, err := NewFleetTelemetryStore(series)
	require.NoError(t, err)
	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	npu := DeviceSample{Kind: DeviceNPU, ID: "accel0", Utilization: Derived(0.25)}
	require.NoError(t, store.Record(context.Background(), "t1", "node-1", "gpu-host:9091", at,
		[]DeviceSample{gpuSample("GPU-1", 300), npu}))

	points := series.points["t1"]
	// Four GPU signals, and only the NPU's one read signal: an unread
	// reading has no number to file.
	require.Len(t, points, 5)
	var npuPoint TelemetryPoint
	for _, p := range points {
		if p.Tags[telemetryTagDevice] == "accel0" {
			npuPoint = p
		}
	}
	assert.Equal(t, MetricDeviceUtilization, npuPoint.Metric)
	assert.Equal(t, "derived", npuPoint.Tags[telemetryTagProvenance])
	assert.Equal(t, "gpu-host:9091", npuPoint.Tags[telemetryTagHost])
	assert.Equal(t, "node-1", npuPoint.Tags[telemetryTagNode])
	assert.Equal(t, at, npuPoint.At)
}

func TestFleetTelemetryStore_RecordRefusesWholeReportOnOneBadSample(t *testing.T) {
	t.Parallel()
	series := newMemorySeries()
	store, err := NewFleetTelemetryStore(series)
	require.NoError(t, err)

	err = store.Record(context.Background(), "t1", "node-1", "h:1", time.Now(),
		[]DeviceSample{gpuSample("GPU-1", 300), {Kind: "fpga", ID: "x"}})
	assert.ErrorIs(t, err, ErrDeviceTelemetryInvalid)
	assert.Empty(t, series.points["t1"], "a malformed report must file nothing, not half of itself")

	err = store.Record(context.Background(), "t1", "node-1", "", time.Now(), []DeviceSample{gpuSample("GPU-1", 1)})
	assert.ErrorIs(t, err, ErrDeviceTelemetryInvalid, "telemetry is read back by host, so a host is required")
}

func TestFleetTelemetryStore_RecordWrapsStoreFailure(t *testing.T) {
	t.Parallel()
	series := newMemorySeries()
	series.writeErr = errors.New("raft unavailable")
	store, err := NewFleetTelemetryStore(series)
	require.NoError(t, err)

	err = store.Record(context.Background(), "t1", "n", "h:1", time.Now(), []DeviceSample{gpuSample("GPU-1", 1)})
	assert.ErrorIs(t, err, ErrDeviceTelemetryStore)
	assert.ErrorIs(t, err, series.writeErr)
	assert.Contains(t, err.Error(), "host=h:1")

	// Nothing read: nothing written, and no call to fail.
	require.NoError(t, store.Record(context.Background(), "t1", "n", "h:1", time.Now(),
		[]DeviceSample{{Kind: DeviceCPU, ID: "package-0"}}))
}

func TestFleetTelemetryStore_DeviceSeriesGroupsAndSorts(t *testing.T) {
	t.Parallel()
	series := newMemorySeries()
	store, err := NewFleetTelemetryStore(series)
	require.NoError(t, err)
	ctx := context.Background()
	base := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	// Written out of order, on two GPUs and one other host.
	require.NoError(t, store.Record(ctx, "t1", "n", "h:1", base.Add(30*time.Second), []DeviceSample{gpuSample("GPU-B", 200), gpuSample("GPU-A", 100)}))
	require.NoError(t, store.Record(ctx, "t1", "n", "h:1", base, []DeviceSample{gpuSample("GPU-A", 50)}))
	require.NoError(t, store.Record(ctx, "t1", "n2", "h:2", base, []DeviceSample{gpuSample("GPU-C", 999)}))
	// A point with no provenance tag cannot be vouched for.
	series.points["t1"] = append(series.points["t1"], TelemetryPoint{
		Metric: MetricDevicePowerWatts, Value: 1, At: base,
		Tags: map[string]string{telemetryTagHost: "h:1", telemetryTagKind: "gpu", telemetryTagDevice: "GPU-A"},
	})

	devices, err := store.deviceSeries(ctx, "t1", "h:1", DeviceGPU, base.Add(-time.Minute), base.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, devices, 2)
	assert.Equal(t, "GPU-A", devices[0].device)
	require.Len(t, devices[0].power, 2)
	assert.Equal(t, 50.0, devices[0].power[0].value, "points are ordered by time")
	assert.Equal(t, 100.0, devices[0].power[1].value)
	assert.Len(t, devices[0].utilization, 2)
	assert.Equal(t, "GPU-B", devices[1].device)
}

func TestFleetTelemetryStore_DeviceSeriesWrapsQueryFailure(t *testing.T) {
	t.Parallel()
	series := newMemorySeries()
	series.queryErr = errors.New("closed")
	store, err := NewFleetTelemetryStore(series)
	require.NoError(t, err)
	_, err = store.deviceSeries(context.Background(), "t1", "h:1", DeviceGPU, time.Now(), time.Now())
	assert.ErrorIs(t, err, ErrDeviceTelemetryStore)
}

func TestDeviceMetrics_ListsEverySeries(t *testing.T) {
	t.Parallel()
	assert.ElementsMatch(t, []string{
		MetricDevicePowerWatts, MetricDeviceUtilization, MetricDeviceMemoryUsedBytes, MetricDeviceMemoryTotalBytes,
	}, DeviceMetrics())
}

func TestFleetNodeHeartbeat_FilesDeviceTelemetryUnderTheNodesHost(t *testing.T) {
	t.Parallel()
	impl := newTestService(t)
	impl.SetFleetRegistry(NewInMemoryFleetRegistry())
	series := newMemorySeries()
	store, err := NewFleetTelemetryStore(series)
	require.NoError(t, err)
	impl.SetFleetTelemetryStore(store)
	ctx := context.Background()

	resp, err := impl.RegisterFleetNode(ctx, &RegisterFleetNodeRequest{
		TenantID: "tenant-1", Name: "gpu-host", Host: "gpu-host:9091", NodeType: "agent-runtime",
	})
	require.NoError(t, err)

	observed := &FleetNodeObservation{Backends: []string{"ollama"}, Devices: []DeviceSample{gpuSample("GPU-1", 420)}}
	require.NoError(t, impl.FleetNodeHeartbeat(ctx, "tenant-1", resp.Node.ID, observed))

	points := series.points["tenant-1"]
	require.NotEmpty(t, points)
	assert.Equal(t, "gpu-host:9091", points[0].Tags[telemetryTagHost])
	assert.Equal(t, resp.Node.ID, points[0].Tags[telemetryTagNode])
}

func TestFleetNodeHeartbeat_TelemetryFailureDoesNotFailTheHeartbeat(t *testing.T) {
	t.Parallel()
	impl := newTestService(t)
	impl.SetFleetRegistry(NewInMemoryFleetRegistry())
	series := newMemorySeries()
	series.writeErr = errors.New("disk full")
	store, err := NewFleetTelemetryStore(series)
	require.NoError(t, err)
	impl.SetFleetTelemetryStore(store)
	ctx := context.Background()

	resp, err := impl.RegisterFleetNode(ctx, &RegisterFleetNodeRequest{
		TenantID: "tenant-1", Name: "n", Host: "n:1", NodeType: "agent-runtime",
	})
	require.NoError(t, err)
	observed := &FleetNodeObservation{Backends: []string{"ollama"}, Devices: []DeviceSample{gpuSample("GPU-1", 1)}}
	assert.NoError(t, impl.FleetNodeHeartbeat(ctx, "tenant-1", resp.Node.ID, observed),
		"a heartbeat is a liveness signal first; telemetry storage must not report the node dead")

	// An unknown node is refused by the registry before telemetry is touched.
	assert.Error(t, impl.FleetNodeHeartbeat(ctx, "tenant-1", "no-such-node", observed))
}
