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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

var energyBase = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

func at(seconds int) time.Time { return energyBase.Add(time.Duration(seconds) * time.Second) }

func points(provenance Provenance, pairs ...float64) []telemetryPoint {
	out := make([]telemetryPoint, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, telemetryPoint{at: at(int(pairs[i])), value: pairs[i+1], provenance: provenance})
	}
	return out
}

func TestIntegrateDeviceEnergy_ConstantPowerOverTheWindow(t *testing.T) {
	t.Parallel()
	gpu := &deviceSeries{device: "GPU-1", power: points(ProvenanceMeasured, 0, 360, 30, 360, 60, 360)}

	energy, ok := integrateDeviceEnergy([]*deviceSeries{gpu}, DeviceGPU, at(0), at(60), nil, 0)
	require.True(t, ok)
	// 360 W for 60 s = 21600 J = 6 Wh = 6000 mWh.
	assert.InDelta(t, 6000, energy.MilliWh, 1e-6)
	assert.Equal(t, ProvenanceMeasured, energy.Provenance)
	assert.Equal(t, DeviceGPU, energy.Kind)
	assert.Equal(t, 1, energy.Devices)
}

func TestIntegrateDeviceEnergy_StepsFollowTheSamples(t *testing.T) {
	t.Parallel()
	// 100 W until t=30, then 300 W: the window [20, 40] is 10 s at each.
	gpu := &deviceSeries{device: "GPU-1", power: points(ProvenanceMeasured, 0, 100, 30, 300)}
	energy, ok := integrateDeviceEnergy([]*deviceSeries{gpu}, DeviceGPU, at(20), at(40), nil, 0)
	require.True(t, ok)
	assert.InDelta(t, (100*10+300*10)/3.6, energy.MilliWh, 1e-6)
}

func TestIntegrateDeviceEnergy_ConcurrentTurnsShareTheDevice(t *testing.T) {
	t.Parallel()
	gpu := &deviceSeries{device: "GPU-1", power: points(ProvenanceMeasured, 0, 360, 60, 360)}
	// Another turn was on the device for the second half of the window.
	sharers := []occupancySpan{{start: at(30), end: at(90)}}

	energy, ok := integrateDeviceEnergy([]*deviceSeries{gpu}, DeviceGPU, at(0), at(60), sharers, 0)
	require.True(t, ok)
	// 30 s alone at 360 W, 30 s at half of it.
	assert.InDelta(t, (360*30+180*30)/3.6, energy.MilliWh, 1e-6)
}

func TestIntegrateDeviceEnergy_NPUScalesTheDrawByItsBusyFraction(t *testing.T) {
	t.Parallel()
	npu := &deviceSeries{device: "accel0", utilization: points(ProvenanceDerived, 0, 0.5, 30, 0.5)}

	energy, ok := integrateDeviceEnergy([]*deviceSeries{npu}, DeviceNPU, at(0), at(36), nil, 10)
	require.True(t, ok)
	// 10 W × 0.5 busy × 36 s = 180 J = 50 mWh.
	assert.InDelta(t, 50, energy.MilliWh, 1e-6)
	assert.Equal(t, ProvenanceDerived, energy.Provenance)

	_, ok = integrateDeviceEnergy([]*deviceSeries{npu}, DeviceNPU, at(0), at(36), nil, 0)
	assert.False(t, ok, "a busy fraction with no configured draw is not an energy")
}

func TestIntegrateDeviceEnergy_SumsDevicesAndKeepsTheWeakestProvenance(t *testing.T) {
	t.Parallel()
	a := &deviceSeries{device: "GPU-A", power: points(ProvenanceMeasured, 0, 100)}
	b := &deviceSeries{device: "GPU-B", power: points(ProvenanceDerived, 0, 200)}
	energy, ok := integrateDeviceEnergy([]*deviceSeries{a, b}, DeviceGPU, at(0), at(36), nil, 0)
	require.True(t, ok)
	assert.InDelta(t, 300*36/3.6, energy.MilliWh, 1e-6)
	assert.Equal(t, ProvenanceDerived, energy.Provenance)
	assert.Equal(t, 2, energy.Devices)
}

func TestIntegrateDeviceEnergy_RefusesWhatItCannotCover(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		devices []*deviceSeries
		from    time.Time
		to      time.Time
	}{
		"no devices":     {nil, at(0), at(60)},
		"empty window":   {[]*deviceSeries{{device: "g", power: points(ProvenanceMeasured, 0, 1)}}, at(10), at(10)},
		"inverted":       {[]*deviceSeries{{device: "g", power: points(ProvenanceMeasured, 0, 1)}}, at(10), at(5)},
		"no signal":      {[]*deviceSeries{{device: "g"}}, at(0), at(60)},
		"gap too wide":   {[]*deviceSeries{{device: "g", power: points(ProvenanceMeasured, 0, 1, 300, 1)}}, at(0), at(300)},
		"stale sample":   {[]*deviceSeries{{device: "g", power: points(ProvenanceMeasured, 0, 1)}}, at(200), at(260)},
		"one of two bad": {[]*deviceSeries{{device: "a", power: points(ProvenanceMeasured, 0, 1)}, {device: "b"}}, at(0), at(10)},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, ok := integrateDeviceEnergy(tc.devices, DeviceGPU, tc.from, tc.to, nil, 0)
			assert.False(t, ok)
		})
	}
}

func TestPointAt(t *testing.T) {
	t.Parallel()
	series := points(ProvenanceMeasured, 0, 1, 60, 2)
	tests := map[string]struct {
		at   time.Time
		want float64
		ok   bool
	}{
		"exact":                    {at(0), 1, true},
		"holds the earlier":        {at(59), 1, true},
		"leads from the next":      {at(-30), 1, true},
		"after the last, in reach": {at(120), 2, true},
		"beyond reach":             {at(200), 0, false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := pointAt(series, tc.at)
			assert.Equal(t, tc.ok, ok)
			if ok {
				assert.Equal(t, tc.want, got.value)
			}
		})
	}
}

func TestTurnOccupancy_SharersAndPruning(t *testing.T) {
	t.Parallel()
	var o turnOccupancy
	key := occupancyKey{host: "gpu-host:9091", kind: DeviceGPU}
	other := occupancyKey{host: "other:1", kind: DeviceGPU}

	o.begin(key, "mine", at(0))
	o.begin(key, "done-before", at(-100))
	o.end(key, "done-before", at(-50))
	o.begin(key, "overlaps", at(10))
	o.end(key, "overlaps", at(20))
	o.begin(key, "running", at(40))
	o.begin(other, "elsewhere", at(0))
	o.end(key, "never-begun", at(5))

	sharers := o.sharers(key, "mine", at(0), at(60))
	require.Len(t, sharers, 2)
	var sawRunning bool
	for _, s := range sharers {
		if s.start.Equal(at(40)) {
			sawRunning = true
			assert.Equal(t, at(60), s.end, "a running turn is closed at the window's end")
		}
	}
	assert.True(t, sawRunning)

	// Beginning a turn long after prunes spans that finished past retention;
	// a turn still running is never pruned.
	o.end(key, "mine", at(60))
	o.end(other, "elsewhere", at(60))
	later := at(0).Add(occupancyRetention + time.Hour)
	o.begin(key, "later", later)
	remaining := o.sharers(key, "later", at(-200), at(100))
	require.Len(t, remaining, 1, "finished spans past retention are pruned")
	assert.Equal(t, at(40), remaining[0].start, "the running turn survives")
	assert.Empty(t, o.sharers(other, "x", at(0), at(100)))
}

func TestPriceBook_StampWithEnergy(t *testing.T) {
	t.Parallel()
	rate := 0.20
	book, err := NewPriceBook(config.PricingConfig{
		ElectricityPricePerKWh: &rate,
		EnginePowerWatts:       map[string]float64{"ollama": 600},
		EngineDevices:          map[string]string{"ollama": "gpu", "openvino": "npu"},
	})
	require.NoError(t, err)

	kind, ok := book.EngineDevice("ollama")
	assert.True(t, ok)
	assert.Equal(t, DeviceGPU, kind)
	_, ok = book.EngineDevice("claude")
	assert.False(t, ok)
	draw, ok := book.EngineDraw("t", "ollama", "m", nil)
	assert.True(t, ok)
	assert.Equal(t, 600.0, draw)

	entry := func(engine string) *ConsumptionEntry {
		return &ConsumptionEntry{TenantID: "t", Engine: engine, Model: "review-q8", Host: "gpu-host:9091", DurationMillis: 60_000, InputTokens: 10}
	}

	t.Run("measured energy outranks the configured draw", func(t *testing.T) {
		t.Parallel()
		e := entry("ollama")
		require.True(t, book.StampWithEnergy(e, nil, &MeasuredEnergy{MilliWh: 5000, Provenance: ProvenanceMeasured, Kind: DeviceGPU}))
		assert.Equal(t, int64(5000), e.EnergyMilliWh)
		assert.Equal(t, int64(1000), e.EquivalentMicros, "5 Wh at $0.20/kWh is $0.001")
		assert.Equal(t, ProvenanceMeasured, e.EnergyProvenance)
		assert.Equal(t, PriceBasisElectricity, e.PriceBasis)
		assert.Contains(t, e.PriceRef, "measured:gpu@gpu-host:9091")
		assert.Equal(t, localProvider, e.Provider)
	})
	t.Run("measured energy needs no configured draw", func(t *testing.T) {
		t.Parallel()
		e := entry("openvino")
		require.True(t, book.StampWithEnergy(e, nil, &MeasuredEnergy{MilliWh: 50, Provenance: ProvenanceDerived, Kind: DeviceNPU}))
		assert.Equal(t, ProvenanceDerived, e.EnergyProvenance)
	})
	t.Run("no measurement is estimated and says so", func(t *testing.T) {
		t.Parallel()
		e := entry("ollama")
		require.True(t, book.StampWithEnergy(e, nil, nil))
		assert.Equal(t, int64(10_000), e.EnergyMilliWh, "600 W for 60 s")
		assert.Equal(t, ProvenanceEstimated, e.EnergyProvenance)
	})
	t.Run("an energy with no telemetry provenance is not a measurement", func(t *testing.T) {
		t.Parallel()
		e := entry("ollama")
		require.True(t, book.StampWithEnergy(e, nil, &MeasuredEnergy{MilliWh: 1, Provenance: ProvenanceEstimated}))
		assert.Equal(t, ProvenanceEstimated, e.EnergyProvenance)
		assert.Equal(t, int64(10_000), e.EnergyMilliWh)
	})
	t.Run("nil entry", func(t *testing.T) {
		t.Parallel()
		assert.False(t, book.StampWithEnergy(nil, nil, nil))
	})
}

func TestConsumptionTotals_CountsMeasuredEnergy(t *testing.T) {
	t.Parallel()
	var totals ConsumptionTotals
	priced := func(p Provenance) ConsumptionEntry {
		return ConsumptionEntry{PriceBasis: PriceBasisElectricity, PriceRef: "r", EnergyMilliWh: 10, EnergyProvenance: p, Outcome: ConsumptionOutcomeSuccess}
	}
	totals.add(priced(ProvenanceMeasured))
	totals.add(priced(ProvenanceDerived))
	totals.add(priced(ProvenanceEstimated))
	assert.Equal(t, 3, totals.ElectricityEntries)
	assert.Equal(t, 2, totals.MeasuredEnergyEntries)
	assert.Equal(t, int64(30), totals.EnergyMilliWh)
}

func TestRecordConsumption_MeasuresEnergyFromFleetTelemetry(t *testing.T) {
	t.Parallel()
	impl := newTestService(t)
	book, err := NewPriceBook(config.PricingConfig{
		EnginePowerWatts: map[string]float64{"ollama": 600},
		EngineDevices:    map[string]string{"ollama": "gpu"},
	})
	require.NoError(t, err)
	impl.SetPriceBook(book)
	ledger := NewInMemoryConsumptionLedger()
	impl.SetConsumptionLedger(ledger)
	series := newMemorySeries()
	store, err := NewFleetTelemetryStore(series)
	require.NoError(t, err)
	impl.SetFleetTelemetryStore(store)
	impl.SetNode("gpu-host:9091")

	ctx := ContextWithTenant(context.Background(), "t1")
	now := time.Now().UTC()
	// The GPU drew 300 W across the whole turn.
	for _, offset := range []time.Duration{-40 * time.Second, -10 * time.Second, 5 * time.Second} {
		require.NoError(t, store.Record(ctx, "t1", "n", "gpu-host:9091", now.Add(offset), []DeviceSample{gpuSample("GPU-1", 300)}))
	}

	turn := completedTurn{turnID: "turn-1", sessionID: "s1", engine: "ollama", model: "review-q8", duration: 30 * time.Second}
	impl.recordConsumption(ctx, turn, TurnAttribution{}, turnSession{host: "gpu-host:9091"}, &TokenUsage{InputTokens: 5}, ConsumptionOutcomeSuccess)

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	require.NoError(t, err)
	require.Equal(t, 1, rollup.Entries)
	assert.Equal(t, 1, rollup.MeasuredEnergyEntries)
	assert.InDelta(t, 300*30/3.6, float64(rollup.EnergyMilliWh), 50, "300 W for about 30 s")

	// A host with no telemetry is estimated from the configured draw.
	other := completedTurn{turnID: "turn-2", sessionID: "s2", engine: "ollama", model: "review-q8", duration: 6 * time.Second}
	impl.recordConsumption(ctx, other, TurnAttribution{}, turnSession{host: "vm-host:9091"}, &TokenUsage{InputTokens: 5}, ConsumptionOutcomeSuccess)
	rollup, err = ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	require.NoError(t, err)
	assert.Equal(t, 2, rollup.ElectricityEntries)
	assert.Equal(t, 1, rollup.MeasuredEnergyEntries)
}

func TestBeginTurnOccupancy_TracksOnlyDeviceBoundEngines(t *testing.T) {
	t.Parallel()
	impl := newTestService(t)
	book, err := NewPriceBook(config.PricingConfig{EngineDevices: map[string]string{"ollama": "gpu"}})
	require.NoError(t, err)
	impl.SetPriceBook(book)
	impl.SetNode("cp:1")

	end := impl.beginTurnOccupancy("t-1", "ollama", "", at(0))
	end(at(10))
	key := occupancyKey{host: "cp:1", kind: DeviceGPU}
	assert.Len(t, impl.occupancy.sharers(key, "other", at(0), at(20)), 1)

	// An engine bound to no device, and a turn with no id, occupy nothing.
	impl.beginTurnOccupancy("t-2", "claude", "", at(0))(at(1))
	impl.beginTurnOccupancy("", "ollama", "", at(0))(at(1))
	assert.Len(t, impl.occupancy.sharers(key, "other", at(0), at(20)), 1)
}
