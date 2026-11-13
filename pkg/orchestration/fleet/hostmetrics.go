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
	"bufio"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// HostMetricsSampler reads the host's CPU, load, memory and disk once per
// heartbeat. The control plane judges the node's health from the sample
// (orchestration.FleetHealthJudge).
type HostMetricsSampler interface {
	// Sample returns one sample stamped with its collection time. A signal
	// that could not be read is an unread Reading in the sample, never a
	// zero.
	Sample(ctx context.Context) *orchestration.HostMetrics
}

// diskUsage is the size of a filesystem and the space an unprivileged writer
// can still use on it, in bytes.
type diskUsage struct {
	total     float64
	available float64
}

// diskUsageFunc reads the filesystem holding path. statDisk is the host's.
type diskUsageFunc func(path string) (diskUsage, error)

// cpuTimes is one reading of the kernel's cumulative host-wide CPU counters,
// in clock ticks.
type cpuTimes struct {
	busy  uint64
	total uint64
}

// hostMetricsSampler reads Linux's /proc and the work directory's filesystem.
// On a host without them every signal is unread, which the control plane
// judges unmeasured: absent is reported as absent.
type hostMetricsSampler struct {
	procfs  fs.FS
	disk    diskUsageFunc
	workDir string
	logger  interfaces.Logger
	now     func() time.Time

	// cpuTurn serialises CPU collection: a collector reads the counters and
	// updates the state below in one turn, so a reading never becomes the
	// baseline of one taken before it. A one-slot channel, not a mutex, so a
	// collector waiting its turn is visibly blocked to testing/synctest.
	cpuTurn chan struct{}
	// lastCPU is the counter reading the next utilisation is derived against.
	lastCPU *cpuTimes
	// lastUtilization is the latest utilisation derived, at lastUtilizationAt;
	// it answers a sample whose counters have not advanced since lastCPU.
	lastUtilization   orchestration.Reading
	lastUtilizationAt time.Time

	// warned holds the sources already logged as unreadable, so a host
	// without one logs it once rather than every heartbeat.
	warned sync.Map
}

// NewHostMetricsSampler returns the host's sampler. workDir names the
// filesystem whose space is reported: the one the daemon's work lands on.
func NewHostMetricsSampler(logger interfaces.Logger, workDir string) (HostMetricsSampler, error) {
	if logger == nil {
		return nil, ErrHostMetricsSamplerNoLogger
	}
	if strings.TrimSpace(workDir) == "" {
		return nil, ErrHostMetricsSamplerNoWorkDir
	}
	return newHostMetricsSampler(os.DirFS("/proc"), statDisk, workDir, logger, time.Now), nil
}

func newHostMetricsSampler(procfs fs.FS, disk diskUsageFunc, workDir string, logger interfaces.Logger, now func() time.Time) *hostMetricsSampler {
	return &hostMetricsSampler{
		procfs:  procfs,
		disk:    disk,
		workDir: workDir,
		logger:  logger,
		now:     now,
		cpuTurn: make(chan struct{}, 1),
	}
}

// Sample implements HostMetricsSampler.
func (s *hostMetricsSampler) Sample(_ context.Context) *orchestration.HostMetrics {
	sample := &orchestration.HostMetrics{CollectedAt: s.now()}
	s.sampleCPU(sample)
	s.sampleLoad(sample)
	s.sampleMemory(sample)
	s.sampleDisk(sample)
	return sample
}

// sampleCPU reads /proc/stat: the online CPU count, and the host-wide
// utilisation since the previous sample. The first sample has no previous
// reading, so its utilisation is unread rather than a guess.
func (s *hostMetricsSampler) sampleCPU(sample *orchestration.HostMetrics) {
	s.cpuTurn <- struct{}{}
	defer func() { <-s.cpuTurn }()

	raw, err := fs.ReadFile(s.procfs, "stat")
	if err != nil {
		s.warnOnce("stat", err)
		return
	}
	current, count, err := parseProcStat(raw)
	if err != nil {
		s.warnOnce("stat", err)
		return
	}
	sample.CPUCount = orchestration.Measured(float64(count))
	sample.CPUUtilization = s.advanceCPU(current, sample.CollectedAt)
}

// cpuUtilizationFreshFor bounds how long a derived utilisation answers samples
// whose counters have not advanced. The counters advance every kernel tick, so
// standing still is two samples inside one tick; a reading this old is still
// well inside the fleet's stale bound (orchestration.DefaultFleetHeartbeatTimeout).
const cpuUtilizationFreshFor = 30 * time.Second

// advanceCPU derives the utilisation from lastCPU to current, collected at
// at, and makes current the next baseline. Counters that have not advanced
// keep the baseline, so the next interval spans real ticks, and answer with
// the last derived utilisation while it is fresh. The caller holds cpuTurn.
func (s *hostMetricsSampler) advanceCPU(current cpuTimes, at time.Time) orchestration.Reading {
	previous := s.lastCPU
	if previous != nil && current.total == previous.total {
		if s.lastUtilization.Known() && at.Sub(s.lastUtilizationAt) <= cpuUtilizationFreshFor {
			return s.lastUtilization
		}
		return orchestration.Unread()
	}
	s.lastCPU = &current
	if previous == nil {
		return orchestration.Unread()
	}
	utilization := cpuUtilization(*previous, current)
	if utilization.Known() {
		s.lastUtilization, s.lastUtilizationAt = utilization, at
	}
	return utilization
}

// cpuUtilization is the busy fraction between two counter readings. No
// elapsed ticks, or counters that went backwards, give no utilisation: an
// unread reading rather than a division by zero.
func cpuUtilization(previous, current cpuTimes) orchestration.Reading {
	if current.total <= previous.total || current.busy < previous.busy {
		return orchestration.Unread()
	}
	busy := float64(current.busy - previous.busy)
	total := float64(current.total - previous.total)
	return orchestration.Derived(min(busy/total, 1))
}

// sampleLoad reads /proc/loadavg.
func (s *hostMetricsSampler) sampleLoad(sample *orchestration.HostMetrics) {
	raw, err := fs.ReadFile(s.procfs, "loadavg")
	if err != nil {
		s.warnOnce("loadavg", err)
		return
	}
	fields := strings.Fields(string(raw))
	loads := []*orchestration.Reading{&sample.Load1, &sample.Load5, &sample.Load15}
	for i, load := range loads {
		if i >= len(fields) {
			s.warnOnce("loadavg", ErrHostMetricsMalformed)
			return
		}
		value, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			s.warnOnce("loadavg", err)
			return
		}
		*load = orchestration.Measured(value)
	}
}

// sampleMemory reads MemTotal and MemAvailable from /proc/meminfo. Each is
// read independently, so a kernel missing one still reports the other.
func (s *hostMetricsSampler) sampleMemory(sample *orchestration.HostMetrics) {
	raw, err := fs.ReadFile(s.procfs, "meminfo")
	if err != nil {
		s.warnOnce("meminfo", err)
		return
	}
	fields := parseMeminfo(raw)
	if total, ok := fields["MemTotal"]; ok {
		sample.MemTotalBytes = orchestration.Measured(total)
	}
	if available, ok := fields["MemAvailable"]; ok {
		sample.MemAvailableBytes = orchestration.Measured(available)
	}
}

// sampleDisk reads the work directory's filesystem.
func (s *hostMetricsSampler) sampleDisk(sample *orchestration.HostMetrics) {
	usage, err := s.disk(s.workDir)
	if err != nil {
		s.warnOnce("statfs", err)
		return
	}
	sample.DiskTotalBytes = orchestration.Measured(usage.total)
	sample.DiskAvailableBytes = orchestration.Measured(usage.available)
}

// warnOnce logs the first failure to read source. The signal is unread for
// as long as the source stays unreadable.
func (s *hostMetricsSampler) warnOnce(source string, err error) {
	if _, logged := s.warned.LoadOrStore(source, struct{}{}); logged {
		return
	}
	s.logger.Warnf("host metrics: %s is unreadable; its signals are unread: %v", source, err)
}

// parseProcStat reads the aggregate "cpu" line and counts the per-CPU lines.
// Busy time is everything but idle and iowait; guest time is already inside
// user time, so it is not added again.
func parseProcStat(raw []byte) (cpuTimes, int, error) {
	var times cpuTimes
	found := false
	count := 0
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		if fields[0] != "cpu" {
			count++
			continue
		}
		// user nice system idle iowait irq softirq steal
		if len(fields) < 9 {
			return cpuTimes{}, 0, ErrHostMetricsMalformed
		}
		for i, field := range fields[1:9] {
			ticks, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return cpuTimes{}, 0, err
			}
			times.total += ticks
			if i != 3 && i != 4 {
				times.busy += ticks
			}
		}
		found = true
	}
	if err := scanner.Err(); err != nil {
		return cpuTimes{}, 0, err
	}
	if !found || count == 0 {
		return cpuTimes{}, 0, ErrHostMetricsMalformed
	}
	return times, count, nil
}

// parseMeminfo returns /proc/meminfo's kB fields in bytes. Lines that do not
// parse are left out rather than read as zero.
func parseMeminfo(raw []byte) map[string]float64 {
	fields := make(map[string]float64)
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		name, rest, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		parts := strings.Fields(rest)
		if len(parts) != 2 || parts[1] != "kB" {
			continue
		}
		kib, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil {
			continue
		}
		fields[name] = float64(kib) * 1024
	}
	return fields
}

var (
	// ErrHostMetricsSamplerNoLogger indicates a host-metrics sampler built
	// without a logger.
	ErrHostMetricsSamplerNoLogger = errors.New("agent host metrics: logger required")

	// ErrHostMetricsSamplerNoWorkDir indicates a host-metrics sampler built
	// without the work directory whose filesystem it reports.
	ErrHostMetricsSamplerNoWorkDir = errors.New("agent host metrics: work directory required")

	// ErrHostMetricsMalformed indicates a kernel interface whose contents
	// are not in the expected shape.
	ErrHostMetricsMalformed = errors.New("agent host metrics: malformed kernel interface")

	// ErrHostDiskUnsupported indicates a platform this sampler cannot read
	// filesystem space on.
	ErrHostDiskUnsupported = errors.New("agent host metrics: filesystem space is not readable on this platform")
)
