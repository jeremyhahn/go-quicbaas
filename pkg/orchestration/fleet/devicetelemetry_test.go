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

package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// fakeRunner is a commandRunner that returns canned output, so nvidia-smi is
// never executed by a unit test.
type fakeRunner struct {
	out   string
	err   error
	calls int
	args  []string
}

func (f *fakeRunner) Output(_ context.Context, _ string, args ...string) ([]byte, error) {
	f.calls++
	f.args = args
	return []byte(f.out), f.err
}

// telemetryLogger records warnings so a test can assert a failure is logged
// once rather than on every heartbeat.
type telemetryLogger struct {
	testLogger
	warns int
	infos int
}

func (l *telemetryLogger) Warnf(string, ...interface{}) { l.warns++ }
func (l *telemetryLogger) Infof(string, ...interface{}) { l.infos++ }

var telemetryT0 = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

func TestParseNVIDIAQuery(t *testing.T) {
	t.Parallel()
	out := "GPU-a5fb, NVIDIA RTX PRO 6000, 496.12, 98, 44693, 97887\n" +
		"GPU-bbbb, Tesla T4, [N/A], [Not Supported], 10, 15360\n" +
		"malformed line\n" +
		", nameless, 1, 1, 1, 1\n"
	samples := parseNVIDIAQuery(out)
	require.Len(t, samples, 2)

	gpu := samples[0]
	assert.Equal(t, orchestration.DeviceGPU, gpu.Kind)
	assert.Equal(t, "GPU-a5fb", gpu.ID)
	assert.Equal(t, "NVIDIA RTX PRO 6000", gpu.Name)
	watts, ok := gpu.PowerWatts.Value()
	require.True(t, ok)
	assert.InDelta(t, 496.12, watts, 1e-9)
	assert.Equal(t, orchestration.ProvenanceMeasured, gpu.PowerWatts.Provenance())
	util, ok := gpu.Utilization.Value()
	require.True(t, ok)
	assert.InDelta(t, 0.98, util, 1e-9)
	used, _ := gpu.MemoryUsedBytes.Value()
	assert.Equal(t, float64(44693*mebibyte), used)

	t4 := samples[1]
	assert.False(t, t4.PowerWatts.Known(), "[N/A] is unread, never zero")
	assert.False(t, t4.Utilization.Known())
	require.NoError(t, t4.Validate())

	assert.Empty(t, parseNVIDIAQuery(""))
}

func TestNVIDIASource_LogsAPersistentFailureOnce(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{err: errors.New("driver not loaded")}
	logger := &telemetryLogger{}
	source := newNVIDIASource(runner, "/usr/bin/nvidia-smi", logger)

	assert.Nil(t, source.sample(context.Background(), telemetryT0))
	assert.Nil(t, source.sample(context.Background(), telemetryT0))
	assert.Equal(t, 1, logger.warns)
	assert.Contains(t, runner.args, "--format=csv,noheader,nounits")

	runner.err = nil
	runner.out = "GPU-1, G, 10, 1, 1, 2\n"
	assert.Len(t, source.sample(context.Background(), telemetryT0), 1)
	assert.Equal(t, 1, logger.infos, "recovery is logged")
}

func npuFS(busy string) fstest.MapFS {
	return fstest.MapFS{
		"class/accel/accel0/device/npu_busy_time_us":       {Data: []byte(busy + "\n")},
		"class/accel/accel0/device/npu_memory_utilization": {Data: []byte("22314278912\n")},
		"class/accel/accel1/device/vendor":                 {Data: []byte("0x1002\n")},
	}
}

func TestNPUSource_DerivesBusyFractionFromTheCounter(t *testing.T) {
	t.Parallel()
	source := newNPUSource(npuFS("1000000"))

	first := source.sample(context.Background(), telemetryT0)
	require.Len(t, first, 1, "an accel node with no busy counter is not an NPU this source reads")
	assert.Equal(t, orchestration.DeviceNPU, first[0].Kind)
	assert.Equal(t, "accel0", first[0].ID)
	assert.False(t, first[0].Utilization.Known(), "the first reading has nothing to difference against")
	mem, ok := first[0].MemoryUsedBytes.Value()
	require.True(t, ok)
	assert.Equal(t, float64(22314278912), mem)

	// 15 s busy in a 30 s interval.
	source.sysfs = npuFS("16000000")
	second := source.sample(context.Background(), telemetryT0.Add(30*time.Second))
	util, ok := second[0].Utilization.Value()
	require.True(t, ok)
	assert.InDelta(t, 0.5, util, 1e-9)
	assert.Equal(t, orchestration.ProvenanceDerived, second[0].Utilization.Provenance())
}

func TestNPUSource_BusyFractionEdgeCases(t *testing.T) {
	t.Parallel()
	source := newNPUSource(fstest.MapFS{})
	assert.Nil(t, source.sample(context.Background(), telemetryT0), "no accel class is no NPU")

	source.busyFraction("a", counterReading{value: 100, at: telemetryT0})
	backwards := source.busyFraction("a", counterReading{value: 50, at: telemetryT0.Add(time.Second)})
	assert.False(t, backwards.Known(), "a counter that went backwards is a driver reload, not a reading")

	stalled := source.busyFraction("a", counterReading{value: 60, at: telemetryT0.Add(time.Second)})
	assert.False(t, stalled.Known(), "a clock that did not advance divides by zero")

	over := source.busyFraction("a", counterReading{value: 60 + 5_000_000, at: telemetryT0.Add(2 * time.Second)})
	v, ok := over.Value()
	require.True(t, ok)
	assert.Equal(t, 1.0, v, "busy time cannot exceed the interval")
}

func raplFS(energy string, readable bool) fstest.MapFS {
	fsys := fstest.MapFS{
		"class/powercap/intel-rapl:0/name":                {Data: []byte("package-0\n")},
		"class/powercap/intel-rapl:0/max_energy_range_uj": {Data: []byte("262143328850\n")},
		"class/powercap/intel-rapl:0:0/name":              {Data: []byte("core\n")},
		"class/powercap/intel-rapl-mmio:0/name":           {Data: []byte("package-0\n")},
	}
	if readable {
		fsys["class/powercap/intel-rapl:0/energy_uj"] = &fstest.MapFile{Data: []byte(energy + "\n")}
	}
	return fsys
}

func TestRAPLSource_DerivesPackagePower(t *testing.T) {
	t.Parallel()
	logger := &telemetryLogger{}
	source := newRAPLSource(raplFS("1000000", true), logger)

	first := source.sample(context.Background(), telemetryT0)
	require.Len(t, first, 1, "subzones and the MMIO mirror would count the package twice")
	assert.Equal(t, orchestration.DeviceCPU, first[0].Kind)
	assert.Equal(t, "package-0", first[0].ID)
	assert.False(t, first[0].PowerWatts.Known())

	// 900 J over 30 s = 30 W.
	source.sysfs = raplFS("901000000", true)
	second := source.sample(context.Background(), telemetryT0.Add(30*time.Second))
	watts, ok := second[0].PowerWatts.Value()
	require.True(t, ok)
	assert.InDelta(t, 30, watts, 1e-9)
	assert.Equal(t, orchestration.ProvenanceDerived, second[0].PowerWatts.Provenance())
	assert.Zero(t, logger.warns)
}

func TestRAPLSource_UnwrapsAWrappedCounter(t *testing.T) {
	t.Parallel()
	source := newRAPLSource(fstest.MapFS{}, &telemetryLogger{})
	source.power("q", counterReading{value: 90_000_000, at: telemetryT0}, 100_000_000)
	w := source.power("q", counterReading{value: 10_000_000, at: telemetryT0.Add(time.Second)}, 100_000_000)
	watts, ok := w.Value()
	require.True(t, ok)
	assert.InDelta(t, 20, watts, 1e-9, "10 J to the wrap plus 10 J after it, in one second")

	source.power("r", counterReading{value: 90, at: telemetryT0}, 0)
	noRange := source.power("r", counterReading{value: 10, at: telemetryT0.Add(time.Second)}, 0)
	assert.False(t, noRange.Known(), "a wrap with no known range cannot be unwrapped")
}

func TestRAPLSource_UnreadableCounterIsLoggedOnceAndReportedUnread(t *testing.T) {
	t.Parallel()
	logger := &telemetryLogger{}
	source := newRAPLSource(raplFS("", false), logger)

	for i := 0; i < 3; i++ {
		samples := source.sample(context.Background(), telemetryT0.Add(time.Duration(i)*time.Second))
		require.Len(t, samples, 1, "the package exists even when its counter is root-only")
		assert.False(t, samples[0].PowerWatts.Known())
	}
	assert.Equal(t, 1, logger.warns)
	assert.Nil(t, newRAPLSource(fstest.MapFS{}, logger).sample(context.Background(), telemetryT0))
}

// stubSource is a deviceSource returning fixed samples.
type stubSource struct{ samples []orchestration.DeviceSample }

func (s stubSource) sample(context.Context, time.Time) []orchestration.DeviceSample { return s.samples }

func TestHostDeviceSampler_CombinesSources(t *testing.T) {
	t.Parallel()
	sampler := &hostDeviceSampler{
		sources: []deviceSource{
			stubSource{[]orchestration.DeviceSample{{Kind: orchestration.DeviceGPU, ID: "g"}}},
			stubSource{},
			stubSource{[]orchestration.DeviceSample{{Kind: orchestration.DeviceNPU, ID: "n"}}},
		},
		now: func() time.Time { return telemetryT0 },
	}
	got := sampler.Sample(context.Background())
	require.Len(t, got, 2)
	assert.Equal(t, "g", got[0].ID)
	assert.Equal(t, "n", got[1].ID)
}

func TestNewHostDeviceSampler_RequiresLogger(t *testing.T) {
	t.Parallel()
	_, err := NewHostDeviceSampler(nil)
	assert.ErrorIs(t, err, ErrDeviceSamplerNoLogger)
}

func TestHeartbeatRequestBody_CarriesDevicesOnlyWhenSampled(t *testing.T) {
	t.Parallel()
	without := heartbeatRequestBody(HeartbeatStatus{Backends: []string{"ollama"}})
	assert.NotContains(t, without, `"devices"`, "a daemon that samples nothing reports nothing")

	with := heartbeatRequestBody(HeartbeatStatus{
		Backends: []string{"ollama"},
		Devices: []orchestration.DeviceSample{{
			Kind: orchestration.DeviceGPU, ID: "GPU-1", PowerWatts: orchestration.Measured(300),
		}},
	})
	var decoded struct {
		Devices []orchestration.DeviceSample `json:"devices"`
	}
	require.NoError(t, json.Unmarshal([]byte(with), &decoded))
	require.Len(t, decoded.Devices, 1)
	watts, ok := decoded.Devices[0].PowerWatts.Value()
	require.True(t, ok)
	assert.Equal(t, 300.0, watts)
	assert.False(t, decoded.Devices[0].Utilization.Known(), "an unread signal crosses the wire as unread")
	assert.True(t, strings.HasSuffix(with, "}"))
}

// capturingHeartbeat records the last status it was sent.
type capturingHeartbeat struct{ last HeartbeatStatus }

func (c *capturingHeartbeat) SendHeartbeat(_ context.Context, _ *orchestration.AgentIdentity, status HeartbeatStatus) error {
	c.last = status
	return nil
}

func TestDaemon_SendHeartbeatCarriesDeviceTelemetry(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	identity := &orchestration.AgentIdentity{AgentID: "hb-devices", TenantID: cfg.TenantID}
	sampler := &hostDeviceSampler{
		sources: []deviceSource{stubSource{[]orchestration.DeviceSample{{Kind: orchestration.DeviceGPU, ID: "GPU-1"}}}},
		now:     func() time.Time { return telemetryT0 },
	}

	for name, devices := range map[string]DeviceSampler{"sampled": sampler, "no sampler": nil} {
		t.Run(name, func(t *testing.T) {
			hb := &capturingHeartbeat{}
			d, err := NewDaemon(cfg, DaemonDeps{
				Runtime:     newTestRuntime(t),
				Enroller:    &fakeEnroller{identity: identity},
				Heartbeat:   hb,
				SpawnServer: &fakeSpawnServer{addr: "127.0.0.1:0"},
				Logger:      &testLogger{t: t},
				Devices:     devices,
			})
			require.NoError(t, err)
			d.identity.Store(identity)
			d.sendHeartbeat(context.Background())
			if devices == nil {
				assert.Nil(t, hb.last.Devices, "a daemon with no sampler reports nothing")
				return
			}
			require.Len(t, hb.last.Devices, 1)
			assert.Equal(t, "GPU-1", hb.last.Devices[0].ID)
		})
	}
}
