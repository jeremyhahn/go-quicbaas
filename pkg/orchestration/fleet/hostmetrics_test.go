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
	"io/fs"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// procStat renders /proc/stat with two CPUs and the given aggregate
// user/system/idle ticks (nice, iowait, irq, softirq and steal held at zero).
func procStat(user, system, idle string) []byte {
	return []byte("cpu  " + user + " 0 " + system + " " + idle + " 0 0 0 0 0 0\n" +
		"cpu0 1 0 1 1 0 0 0 0 0 0\n" +
		"cpu1 1 0 1 1 0 0 0 0 0 0\n" +
		"intr 12345\n")
}

func procFS(stat []byte) fstest.MapFS {
	return fstest.MapFS{
		"stat":    {Data: stat},
		"loadavg": {Data: []byte("1.50 1.25 0.75 2/812 4242\n")},
		"meminfo": {Data: []byte("MemTotal:       65536 kB\nMemFree:         1024 kB\nMemAvailable:   32768 kB\nHugePages_Total:    0\n")},
	}
}

func fixedDisk(total, available float64) diskUsageFunc {
	return func(string) (diskUsage, error) { return diskUsage{total: total, available: available}, nil }
}

func TestHostMetricsSampler_ReadsEverySignalWithProvenance(t *testing.T) {
	t.Parallel()
	logger := &telemetryLogger{}
	sampler := newHostMetricsSampler(procFS(procStat("100", "100", "800")), fixedDisk(1000, 400), "/work", logger,
		func() time.Time { return telemetryT0 })

	first := sampler.Sample(context.Background())
	require.NoError(t, first.Validate())
	assert.Equal(t, telemetryT0, first.CollectedAt)
	assert.False(t, first.CPUUtilization.Known(), "a utilisation needs two readings")

	cases := map[string]struct {
		reading orchestration.Reading
		want    float64
	}{
		"cpu count":      {first.CPUCount, 2},
		"load1":          {first.Load1, 1.5},
		"load5":          {first.Load5, 1.25},
		"load15":         {first.Load15, 0.75},
		"mem total":      {first.MemTotalBytes, 65536 * 1024},
		"mem available":  {first.MemAvailableBytes, 32768 * 1024},
		"disk total":     {first.DiskTotalBytes, 1000},
		"disk available": {first.DiskAvailableBytes, 400},
	}
	for name, tc := range cases {
		got, ok := tc.reading.Value()
		require.True(t, ok, name)
		assert.Equal(t, tc.want, got, name)
		assert.Equal(t, orchestration.ProvenanceMeasured, tc.reading.Provenance(), name)
	}

	// 300 busy of 1000 elapsed ticks.
	sampler.procfs = procFS(procStat("250", "250", "1500"))
	second := sampler.Sample(context.Background())
	util, ok := second.CPUUtilization.Value()
	require.True(t, ok)
	assert.InDelta(t, 0.3, util, 1e-9)
	assert.Equal(t, orchestration.ProvenanceDerived, second.CPUUtilization.Provenance())
	assert.Zero(t, logger.warns)
}

func TestHostMetricsSampler_AnUnreadableSourceIsAbsentNotZero(t *testing.T) {
	t.Parallel()
	logger := &telemetryLogger{}
	noDisk := func(string) (diskUsage, error) { return diskUsage{}, ErrHostDiskUnsupported }
	sampler := newHostMetricsSampler(fstest.MapFS{}, noDisk, "/work", logger, func() time.Time { return telemetryT0 })

	for i := 0; i < 3; i++ {
		sample := sampler.Sample(context.Background())
		require.NoError(t, sample.Validate())
		for name, reading := range map[string]orchestration.Reading{
			"cpu":    sample.CPUUtilization,
			"count":  sample.CPUCount,
			"load":   sample.Load1,
			"memory": sample.MemAvailableBytes,
			"disk":   sample.DiskAvailableBytes,
		} {
			assert.False(t, reading.Known(), "%s must be unread, never zero", name)
		}
	}
	assert.Equal(t, 4, logger.warns, "each unreadable source is logged once: stat, loadavg, meminfo, statfs")
}

func TestHostMetricsSampler_MalformedKernelOutputIsUnread(t *testing.T) {
	t.Parallel()
	sampler := newHostMetricsSampler(fstest.MapFS{
		"stat":    {Data: []byte("cpu 1 2 3\ncpu0 1 2 3\n")},
		"loadavg": {Data: []byte("1.0 nope 2.0\n")},
		"meminfo": {Data: []byte("MemTotal: lots kB\nMemAvailable:   2048 kB\n")},
	}, fixedDisk(10, 5), "/work", &telemetryLogger{}, func() time.Time { return telemetryT0 })

	sample := sampler.Sample(context.Background())
	assert.False(t, sample.CPUCount.Known(), "a short cpu line is not a reading")
	assert.False(t, sample.Load5.Known())
	assert.False(t, sample.MemTotalBytes.Known(), "a value that does not parse is left out, not zero")
	available, ok := sample.MemAvailableBytes.Value()
	require.True(t, ok, "one bad meminfo line does not cost the others")
	assert.Equal(t, 2048.0*1024, available)
}

func TestCPUUtilization_RefusesWhatCannotBeAnInterval(t *testing.T) {
	t.Parallel()
	start := cpuTimes{busy: 100, total: 1000}
	assert.False(t, cpuUtilization(start, start).Known(), "no elapsed ticks divides by zero")
	assert.False(t, cpuUtilization(start, cpuTimes{busy: 50, total: 2000}).Known(), "busy counter went backwards")
	assert.False(t, cpuUtilization(start, cpuTimes{busy: 200, total: 900}).Known(), "total counter went backwards")
	full, ok := cpuUtilization(start, cpuTimes{busy: 1100, total: 2000}).Value()
	require.True(t, ok)
	assert.Equal(t, 1.0, full)
}

// Placement samples the embedded agent's host on every spawn, so two samples
// can land inside one kernel tick and read the same counters.
func TestHostMetricsSampler_ReusesAFreshUtilisationWhileTheCountersStandStill(t *testing.T) {
	t.Parallel()
	now := telemetryT0
	sampler := newHostMetricsSampler(procFS(procStat("100", "100", "800")), fixedDisk(1000, 400), "/work",
		&telemetryLogger{}, func() time.Time { return now })
	sampler.Sample(context.Background())

	// 300 busy of 1000 elapsed ticks.
	now = now.Add(time.Second)
	sampler.procfs = procFS(procStat("250", "250", "1500"))
	measured, ok := sampler.Sample(context.Background()).CPUUtilization.Value()
	require.True(t, ok)
	assert.InDelta(t, 0.3, measured, 1e-9)

	now = now.Add(time.Millisecond)
	reused, ok := sampler.Sample(context.Background()).CPUUtilization.Value()
	require.True(t, ok, "counters that have not advanced keep the fresh measurement")
	assert.InDelta(t, 0.3, reused, 1e-9)

	now = now.Add(time.Hour)
	assert.False(t, sampler.Sample(context.Background()).CPUUtilization.Known(),
		"a measurement past its freshness is not reused")

	// The baseline stayed at the last reading that advanced: 500 busy of
	// 1000 elapsed ticks since it.
	sampler.procfs = procFS(procStat("500", "500", "2000"))
	next, ok := sampler.Sample(context.Background()).CPUUtilization.Value()
	require.True(t, ok)
	assert.InDelta(t, 0.5, next, 1e-9)
}

// gatedProcFS serves a fixed sequence of /proc/stat readings and holds the
// second read until release is closed, signalling entered when it starts.
type gatedProcFS struct {
	stats   [][]byte
	reads   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (g *gatedProcFS) Open(name string) (fs.File, error) {
	return procFS(nil).Open(name)
}

func (g *gatedProcFS) ReadFile(name string) ([]byte, error) {
	if name != "stat" {
		return fs.ReadFile(procFS(nil), name)
	}
	read := g.reads.Add(1)
	if read == 2 {
		close(g.entered)
		<-g.release
	}
	return g.stats[read-1], nil
}

// A collector that read the counters first records them first: a later
// reading never becomes the baseline of an earlier one.
func TestHostMetricsSampler_SerialisesCollectionWithItsState(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		procfs := &gatedProcFS{
			stats: [][]byte{
				procStat("100", "100", "800"),  // primes: 200 busy of 1000
				procStat("250", "250", "1500"), // 300 busy of 1000 more
				procStat("500", "500", "2000"), // 500 busy of 1000 more
			},
			entered: make(chan struct{}),
			release: make(chan struct{}),
		}
		sampler := newHostMetricsSampler(procfs, fixedDisk(1000, 400), "/work", &telemetryLogger{}, time.Now)
		sampler.Sample(context.Background())

		var first, second *orchestration.HostMetrics
		firstDone, secondDone := make(chan struct{}), make(chan struct{})
		go func() {
			first = sampler.Sample(context.Background())
			close(firstDone)
		}()
		<-procfs.entered
		go func() {
			second = sampler.Sample(context.Background())
			close(secondDone)
		}()
		// The second collector has either finished or is waiting its turn.
		synctest.Wait()
		close(procfs.release)
		<-firstDone
		<-secondDone

		got, ok := first.CPUUtilization.Value()
		require.True(t, ok, "the first collector's reading follows the primed one")
		assert.InDelta(t, 0.3, got, 1e-9)
		got, ok = second.CPUUtilization.Value()
		require.True(t, ok)
		assert.InDelta(t, 0.5, got, 1e-9)
	})
}

func TestParseProcStat_RequiresTheAggregateAndPerCPULines(t *testing.T) {
	t.Parallel()
	_, _, err := parseProcStat([]byte("cpu0 1 0 1 1 0 0 0 0\n"))
	assert.ErrorIs(t, err, ErrHostMetricsMalformed, "no aggregate line")
	_, _, err = parseProcStat([]byte("cpu  1 0 1 1 0 0 0 0\n"))
	assert.ErrorIs(t, err, ErrHostMetricsMalformed, "no per-CPU line")
	_, _, err = parseProcStat([]byte("cpu  1 0 x 1 0 0 0 0\ncpu0 1\n"))
	assert.Error(t, err)

	times, count, err := parseProcStat(procStat("10", "20", "70"))
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Equal(t, cpuTimes{busy: 30, total: 100}, times)
}

func TestNewHostMetricsSampler_RequiresLoggerAndWorkDir(t *testing.T) {
	t.Parallel()
	_, err := NewHostMetricsSampler(nil, "/work")
	assert.ErrorIs(t, err, ErrHostMetricsSamplerNoLogger)
	_, err = NewHostMetricsSampler(&telemetryLogger{}, "  ")
	assert.ErrorIs(t, err, ErrHostMetricsSamplerNoWorkDir)

	sampler, err := NewHostMetricsSampler(&telemetryLogger{}, t.TempDir())
	require.NoError(t, err)
	require.NoError(t, sampler.Sample(context.Background()).Validate())
}

func TestHeartbeatRequestBody_CarriesHostMetricsOnlyWhenSampled(t *testing.T) {
	t.Parallel()
	without := heartbeatRequestBody(HeartbeatStatus{Backends: []string{"ollama"}})
	assert.NotContains(t, without, `"host_metrics"`, "a daemon that samples nothing reports nothing")

	with := heartbeatRequestBody(HeartbeatStatus{
		Backends: []string{"ollama"},
		HostMetrics: &orchestration.HostMetrics{
			CollectedAt:   telemetryT0,
			MemTotalBytes: orchestration.Measured(1 << 30),
		},
	})
	var decoded struct {
		HostMetrics *orchestration.HostMetrics `json:"host_metrics"`
	}
	require.NoError(t, json.Unmarshal([]byte(with), &decoded))
	require.NotNil(t, decoded.HostMetrics)
	assert.True(t, decoded.HostMetrics.CollectedAt.Equal(telemetryT0))
	total, ok := decoded.HostMetrics.MemTotalBytes.Value()
	require.True(t, ok)
	assert.Equal(t, float64(1<<30), total)
	assert.False(t, decoded.HostMetrics.MemAvailableBytes.Known(), "an unread signal crosses the wire as unread")
}

// stubHostMetrics is a HostMetricsSampler returning a fixed sample.
type stubHostMetrics struct{ sample *orchestration.HostMetrics }

func (s stubHostMetrics) Sample(context.Context) *orchestration.HostMetrics { return s.sample }

func TestDaemon_SendHeartbeatCarriesHostMetrics(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	identity := &orchestration.AgentIdentity{AgentID: "hb-host-metrics", TenantID: cfg.TenantID}
	sampled := stubHostMetrics{&orchestration.HostMetrics{CollectedAt: telemetryT0, Load1: orchestration.Measured(3)}}

	for name, sampler := range map[string]HostMetricsSampler{"sampled": sampled, "no sampler": nil} {
		t.Run(name, func(t *testing.T) {
			hb := &capturingHeartbeat{}
			d, err := NewDaemon(cfg, DaemonDeps{
				Runtime:     newTestRuntime(t),
				Enroller:    &fakeEnroller{identity: identity},
				Heartbeat:   hb,
				SpawnServer: &fakeSpawnServer{addr: "127.0.0.1:0"},
				Logger:      &testLogger{t: t},
				HostMetrics: sampler,
			})
			require.NoError(t, err)
			d.identity.Store(identity)
			d.sendHeartbeat(context.Background())
			if sampler == nil {
				assert.Nil(t, hb.last.HostMetrics, "a daemon with no sampler reports nothing")
				return
			}
			require.NotNil(t, hb.last.HostMetrics)
			load, ok := hb.last.HostMetrics.Load1.Value()
			require.True(t, ok)
			assert.Equal(t, 3.0, load)
		})
	}
}
