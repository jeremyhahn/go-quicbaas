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
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Device telemetry — what this host's accelerators are doing
// =============================================================================
//
// Sampled once per heartbeat and sent with it, so the control plane can cost a
// local turn from what its device drew rather than from a configured wattage
// (pkg/orchestration turn_energy.go). Three sources, each read without cgo so
// the static, cross-compiled and FIPS daemon builds are unaffected:
//
//   - NVIDIA GPUs through `nvidia-smi --query-gpu`, bounded by a timeout. NVML
//     has no pure-Go binding (go-nvml is cgo), so the CLI is the only path a
//     CGO_ENABLED=0 build has.
//   - Intel NPUs through the intel_vpu driver's sysfs attributes under
//     /sys/class/accel: npu_busy_time_us is a cumulative busy counter, so the
//     busy fraction is DERIVED from two readings. The driver exposes no power
//     sensor.
//   - CPU packages through RAPL powercap zones (/sys/class/powercap/intel-rapl:N):
//     energy_uj is a cumulative, wrapping energy counter, so power is DERIVED
//     from two readings. Most distributions make it root-only; an unreadable
//     counter reports the package with its power unread.
//
// A signal that cannot be read is reported UNREAD, never as zero (see
// orchestration.Reading): the first sample of every counter-derived signal is
// unread because there is no earlier reading to difference against.

// DeviceSampler reads this host's devices once per heartbeat.
type DeviceSampler interface {
	// Sample returns one sample per readable device. Nil when the host has
	// no device any source recognises.
	Sample(ctx context.Context) []orchestration.DeviceSample
}

// deviceSource is one family of devices.
type deviceSource interface {
	sample(ctx context.Context, now time.Time) []orchestration.DeviceSample
}

// commandRunner runs a command and returns its standard output. It is the
// seam nvidia-smi is read through, so unit tests never execute it.
type commandRunner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

// execRunner is the production commandRunner.
type execRunner struct{}

// Output runs name with args and returns its standard output.
func (execRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// hostDeviceSampler combines every source.
type hostDeviceSampler struct {
	sources []deviceSource
	now     func() time.Time
}

// Compile-time interface check.
var _ DeviceSampler = (*hostDeviceSampler)(nil)

// NewHostDeviceSampler returns the sampler for this host: NVIDIA GPUs when
// nvidia-smi is on PATH, and the NPUs and CPU packages sysfs exposes.
func NewHostDeviceSampler(logger interfaces.Logger) (DeviceSampler, error) {
	if logger == nil {
		return nil, ErrDeviceSamplerNoLogger
	}
	sysfs := os.DirFS("/sys")
	sources := []deviceSource{
		newNPUSource(sysfs),
		newRAPLSource(sysfs, logger),
	}
	if path, err := exec.LookPath(nvidiaSMI); err == nil {
		sources = append(sources, newNVIDIASource(execRunner{}, path, logger))
	}
	return &hostDeviceSampler{sources: sources, now: time.Now}, nil
}

// Sample implements DeviceSampler.
func (h *hostDeviceSampler) Sample(ctx context.Context) []orchestration.DeviceSample {
	now := h.now()
	var out []orchestration.DeviceSample
	for _, source := range h.sources {
		out = append(out, source.sample(ctx, now)...)
	}
	return out
}

// =============================================================================
// NVIDIA
// =============================================================================

// nvidiaSMI is the NVIDIA management CLI.
const nvidiaSMI = "nvidia-smi"

// nvidiaQueryTimeout bounds one nvidia-smi run. A wedged driver can hang the
// CLI indefinitely, and a heartbeat must not wait on it.
const nvidiaQueryTimeout = 5 * time.Second

// nvidiaQueryFields are the columns read, in order.
const nvidiaQueryFields = "uuid,name,power.draw,utilization.gpu,memory.used,memory.total"

// mebibyte converts nvidia-smi's MiB to bytes.
const mebibyte = 1 << 20

// nvidiaSource reads NVIDIA GPUs through nvidia-smi.
type nvidiaSource struct {
	runner commandRunner
	path   string
	logger interfaces.Logger

	// failing is set while nvidia-smi is failing, so a persistent failure is
	// logged once rather than on every heartbeat.
	failing atomic.Bool
}

// newNVIDIASource returns a source that runs the nvidia-smi at path.
func newNVIDIASource(runner commandRunner, path string, logger interfaces.Logger) *nvidiaSource {
	return &nvidiaSource{runner: runner, path: path, logger: logger}
}

// sample runs one query. A failed run reports no GPUs this heartbeat: the
// devices exist, but nothing about them was read.
func (n *nvidiaSource) sample(ctx context.Context, _ time.Time) []orchestration.DeviceSample {
	ctx, cancel := context.WithTimeout(ctx, nvidiaQueryTimeout)
	defer cancel()
	out, err := n.runner.Output(ctx, n.path,
		"--query-gpu="+nvidiaQueryFields, "--format=csv,noheader,nounits")
	if err != nil {
		if !n.failing.Swap(true) {
			n.logger.Warnf("device telemetry: nvidia-smi failed; GPU telemetry is unread until it recovers: %v", err)
		}
		return nil
	}
	if n.failing.Swap(false) {
		n.logger.Infof("device telemetry: nvidia-smi recovered")
	}
	return parseNVIDIAQuery(string(out))
}

// parseNVIDIAQuery parses nvidia-smi's CSV, one GPU per line. A line with the
// wrong number of columns is skipped; a column nvidia-smi could not read
// ("[N/A]", "[Not Supported]") is unread.
func parseNVIDIAQuery(out string) []orchestration.DeviceSample {
	var samples []orchestration.DeviceSample
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		cols := strings.Split(line, ",")
		if len(cols) != 6 {
			continue
		}
		for i := range cols {
			cols[i] = strings.TrimSpace(cols[i])
		}
		if cols[0] == "" {
			continue
		}
		utilization := measuredColumn(cols[3], 0.01)
		samples = append(samples, orchestration.DeviceSample{
			Kind:             orchestration.DeviceGPU,
			ID:               cols[0],
			Name:             cols[1],
			PowerWatts:       measuredColumn(cols[2], 1),
			Utilization:      utilization,
			MemoryUsedBytes:  measuredColumn(cols[4], mebibyte),
			MemoryTotalBytes: measuredColumn(cols[5], mebibyte),
		})
	}
	return samples
}

// measuredColumn parses one numeric column scaled by scale, or returns an
// unread reading when the column is not a number.
func measuredColumn(col string, scale float64) orchestration.Reading {
	v, err := strconv.ParseFloat(col, 64)
	if err != nil {
		return orchestration.Unread()
	}
	return orchestration.Measured(v * scale)
}

// =============================================================================
// Intel NPU
// =============================================================================

// accelClassDir is the DRM accel class directory, relative to /sys.
const accelClassDir = "class/accel"

// counterReading is one reading of a cumulative counter and when it was taken.
type counterReading struct {
	value uint64
	at    time.Time
}

// npuSource reads Intel NPUs through the intel_vpu driver's sysfs attributes.
type npuSource struct {
	sysfs fs.FS

	mu   sync.Mutex
	last map[string]counterReading
}

// newNPUSource returns an NPU source over sysfs (a filesystem rooted at /sys).
func newNPUSource(sysfs fs.FS) *npuSource {
	return &npuSource{sysfs: sysfs, last: make(map[string]counterReading)}
}

// sample reads every accel device that exposes npu_busy_time_us.
func (n *npuSource) sample(_ context.Context, now time.Time) []orchestration.DeviceSample {
	entries, err := fs.ReadDir(n.sysfs, accelClassDir)
	if err != nil {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	var samples []orchestration.DeviceSample
	for _, entry := range entries {
		device := accelClassDir + "/" + entry.Name() + "/device/"
		busy, err := readUint(n.sysfs, device+"npu_busy_time_us")
		if err != nil {
			// Not an NPU this source knows how to read.
			continue
		}
		sample := orchestration.DeviceSample{
			Kind:        orchestration.DeviceNPU,
			ID:          entry.Name(),
			Name:        "Intel NPU",
			Utilization: n.busyFraction(entry.Name(), counterReading{value: busy, at: now}),
		}
		if used, err := readUint(n.sysfs, device+"npu_memory_utilization"); err == nil {
			sample.MemoryUsedBytes = orchestration.Measured(float64(used))
		}
		samples = append(samples, sample)
	}
	return samples
}

// busyFraction derives the busy fraction since the previous reading of id's
// counter and remembers this one. The first reading, a counter that went
// backwards (a driver reload), and a clock that did not advance are unread.
func (n *npuSource) busyFraction(id string, current counterReading) orchestration.Reading {
	previous, seen := n.last[id]
	n.last[id] = current
	if !seen || current.value < previous.value || !current.at.After(previous.at) {
		return orchestration.Unread()
	}
	busy := time.Duration(current.value-previous.value) * time.Microsecond
	fraction := busy.Seconds() / current.at.Sub(previous.at).Seconds()
	if fraction > 1 {
		// Busy time accrues in the driver and the wall clock here; the two
		// can disagree by a scheduling delay. The device was busy throughout.
		fraction = 1
	}
	return orchestration.Derived(fraction)
}

// =============================================================================
// CPU package (RAPL)
// =============================================================================

// powercapClassDir is the powercap class directory, relative to /sys.
const powercapClassDir = "class/powercap"

// raplZonePrefix names a top-level RAPL package zone ("intel-rapl:0"). Its
// subzones ("intel-rapl:0:0") and the MMIO mirror ("intel-rapl-mmio:0") are
// not read, because each would count the package's energy a second time.
const raplZonePrefix = "intel-rapl:"

// raplSource reads CPU package power from RAPL energy counters.
type raplSource struct {
	sysfs  fs.FS
	logger interfaces.Logger

	mu   sync.Mutex
	last map[string]counterReading

	// unreadable is set once an energy counter could not be read, so a
	// permanently root-only counter is logged once.
	unreadable atomic.Bool
}

// newRAPLSource returns a RAPL source over sysfs.
func newRAPLSource(sysfs fs.FS, logger interfaces.Logger) *raplSource {
	return &raplSource{sysfs: sysfs, logger: logger, last: make(map[string]counterReading)}
}

// sample reads every top-level package zone.
func (r *raplSource) sample(_ context.Context, now time.Time) []orchestration.DeviceSample {
	entries, err := fs.ReadDir(r.sysfs, powercapClassDir)
	if err != nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var samples []orchestration.DeviceSample
	for _, entry := range entries {
		zone := entry.Name()
		if !strings.HasPrefix(zone, raplZonePrefix) || strings.Count(zone, ":") != 1 {
			continue
		}
		dir := powercapClassDir + "/" + zone + "/"
		name, err := fs.ReadFile(r.sysfs, dir+"name")
		if err != nil {
			continue
		}
		id := strings.TrimSpace(string(name))
		sample := orchestration.DeviceSample{Kind: orchestration.DeviceCPU, ID: id, Name: "CPU " + id}
		energy, err := readUint(r.sysfs, dir+"energy_uj")
		if err != nil {
			if !r.unreadable.Swap(true) {
				r.logger.Warnf("device telemetry: RAPL energy counter %s is unreadable (usually root-only); CPU package power is unread: %v", dir+"energy_uj", err)
			}
			samples = append(samples, sample)
			continue
		}
		// The counter's range is what unwraps a wrapped counter. Unknown, a
		// wrap reads as a counter that went backwards, and is unread.
		wrap, wrapErr := readUint(r.sysfs, dir+"max_energy_range_uj")
		if wrapErr != nil {
			wrap = 0
		}
		sample.PowerWatts = r.power(id, counterReading{value: energy, at: now}, wrap)
		samples = append(samples, sample)
	}
	return samples
}

// power derives the package's mean power since the previous reading and
// remembers this one. A counter that wrapped is unwrapped with maxRange; one
// that went backwards with no known range, the first reading, and a clock
// that did not advance are unread.
func (r *raplSource) power(id string, current counterReading, maxRange uint64) orchestration.Reading {
	previous, seen := r.last[id]
	r.last[id] = current
	if !seen || !current.at.After(previous.at) {
		return orchestration.Unread()
	}
	var delta uint64
	switch {
	case current.value >= previous.value:
		delta = current.value - previous.value
	case maxRange > previous.value:
		delta = maxRange - previous.value + current.value
	default:
		return orchestration.Unread()
	}
	joules := float64(delta) / 1e6
	return orchestration.Derived(joules / current.at.Sub(previous.at).Seconds())
}

// readUint reads a sysfs attribute holding one unsigned integer.
func readUint(sysfs fs.FS, name string) (uint64, error) {
	raw, err := fs.ReadFile(sysfs, name)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
}

// ErrDeviceSamplerNoLogger reports a device sampler built without a logger.
var ErrDeviceSamplerNoLogger = errors.New("agent device telemetry: logger required")
