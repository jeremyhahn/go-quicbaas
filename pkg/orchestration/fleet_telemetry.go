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
	"sort"
	"strings"
	"time"
)

// =============================================================================
// Fleet device telemetry
// =============================================================================
//
// Every fleet node reports what its accelerators are doing on each heartbeat:
// an NVIDIA GPU's power draw, utilisation and memory, an Intel NPU's busy
// fraction, and the CPU package's power. The control plane files each report
// as a time series per node (fleet.device.*), and the turn recorder integrates
// the series over a turn's window to cost local inference from what the device
// actually drew rather than from a configured wattage (turn_energy.go).
//
// A READING THE NODE COULD NOT TAKE IS NOT FILED. Every value travels as a
// Reading, so an unread signal has no number to store; a gap in a series is a
// gap, and the integration refuses to bridge one wider than telemetryMaxGap.

// DeviceKind names the class of device a sample describes, and the class an
// engine's turns draw on (config orchestration.pricing.engine_devices).
type DeviceKind string

const (
	// DeviceGPU is a discrete GPU (NVIDIA, read through nvidia-smi).
	DeviceGPU DeviceKind = "gpu"

	// DeviceNPU is a neural processing unit (Intel NPU, read through the
	// intel_vpu driver's sysfs attributes).
	DeviceNPU DeviceKind = "npu"

	// DeviceCPU is a CPU package (read through RAPL powercap zones).
	DeviceCPU DeviceKind = "cpu"
)

// validDeviceKinds is the exhaustive set; a kind outside it is refused rather
// than filed under a series no reader asks for.
var validDeviceKinds = map[DeviceKind]struct{}{
	DeviceGPU: {},
	DeviceNPU: {},
	DeviceCPU: {},
}

// ParseDeviceKind returns the DeviceKind named by s, or false when s names
// none.
func ParseDeviceKind(s string) (DeviceKind, bool) {
	kind := DeviceKind(s)
	_, ok := validDeviceKinds[kind]
	return kind, ok
}

// DeviceSample is one device's state at one heartbeat.
type DeviceSample struct {
	// Kind is the class of device.
	Kind DeviceKind `json:"kind"`

	// ID identifies the device on its node, stably across heartbeats: an
	// NVIDIA GPU's UUID, an accel node's name ("accel0"), a RAPL zone's name
	// ("package-0"). Required: two devices of one kind are two series.
	ID string `json:"id"`

	// Name is the device's human name ("NVIDIA RTX PRO 6000"), when it has one.
	Name string `json:"name,omitempty"`

	// PowerWatts is the device's draw. Measured for a GPU; derived from an
	// energy counter for a CPU package; unread for an NPU, whose driver
	// exposes no power sensor.
	PowerWatts Reading `json:"power_watts"`

	// Utilization is the fraction of the interval the device was busy, in
	// [0, 1]. Measured for a GPU; derived from the busy-time counter for an
	// NPU.
	Utilization Reading `json:"utilization"`

	// MemoryUsedBytes and MemoryTotalBytes are the device's memory, when it
	// reports it.
	MemoryUsedBytes  Reading `json:"memory_used_bytes"`
	MemoryTotalBytes Reading `json:"memory_total_bytes"`
}

// Validate refuses a sample that cannot be filed honestly: an unknown kind, a
// missing ID, a negative power or memory figure, or a utilisation outside
// [0, 1].
func (d DeviceSample) Validate() error {
	if _, ok := validDeviceKinds[d.Kind]; !ok {
		return &DeviceTelemetryInvalidError{Field: "kind", Message: "must be one of gpu, npu, cpu; got " + string(d.Kind)}
	}
	if strings.TrimSpace(d.ID) == "" {
		return &DeviceTelemetryInvalidError{Field: "id", Message: "must not be empty"}
	}
	nonNegative := []struct {
		field   string
		reading Reading
	}{
		{"power_watts", d.PowerWatts},
		{"memory_used_bytes", d.MemoryUsedBytes},
		{"memory_total_bytes", d.MemoryTotalBytes},
	}
	for _, n := range nonNegative {
		if v, ok := n.reading.Value(); ok && v < 0 {
			return &DeviceTelemetryInvalidError{Field: n.field, Message: "must not be negative"}
		}
	}
	if v, ok := d.Utilization.Value(); ok && (v < 0 || v > 1) {
		return &DeviceTelemetryInvalidError{Field: "utilization", Message: "must be a fraction in [0, 1]"}
	}
	return nil
}

// Series names, one per device signal. The node, host, kind and device travel
// as tags, so a reader selects a host's GPUs with one query.
const (
	MetricDevicePowerWatts       = "fleet.device.power_watts"
	MetricDeviceUtilization      = "fleet.device.utilization"
	MetricDeviceMemoryUsedBytes  = "fleet.device.memory_used_bytes"
	MetricDeviceMemoryTotalBytes = "fleet.device.memory_total_bytes"
)

// DeviceMetrics lists every series device telemetry is filed under, for a
// wiring that sets their retention.
func DeviceMetrics() []string {
	return []string{
		MetricDevicePowerWatts,
		MetricDeviceUtilization,
		MetricDeviceMemoryUsedBytes,
		MetricDeviceMemoryTotalBytes,
	}
}

// Series tags.
const (
	telemetryTagNode       = "node"
	telemetryTagHost       = "host"
	telemetryTagKind       = "kind"
	telemetryTagDevice     = "device"
	telemetryTagProvenance = "provenance"
)

// TelemetryPoint is one value of one series.
type TelemetryPoint struct {
	Metric string
	Tags   map[string]string
	Value  float64
	At     time.Time
}

// TelemetrySeries is the time-series capability device telemetry is filed in.
// The server satisfies it over the platform's tenant-scoped, Raft-replicated
// time-series service; nothing here knows which store that is.
type TelemetrySeries interface {
	// WritePoints files points under tenantID.
	WritePoints(ctx context.Context, tenantID string, points []TelemetryPoint) error

	// QueryPoints returns every point of metric under tenantID whose tags
	// include every tag in match, taken in [from, to].
	QueryPoints(ctx context.Context, tenantID, metric string, match map[string]string, from, to time.Time) ([]TelemetryPoint, error)
}

// FleetTelemetryStore files fleet device telemetry and reads it back per host
// and device kind.
type FleetTelemetryStore struct {
	series TelemetrySeries
}

// NewFleetTelemetryStore returns a store over series.
func NewFleetTelemetryStore(series TelemetrySeries) (*FleetTelemetryStore, error) {
	if series == nil {
		return nil, &DeviceTelemetryInvalidError{Field: "series", Message: "time-series store must not be nil"}
	}
	return &FleetTelemetryStore{series: series}, nil
}

// Record files one heartbeat's device samples for node, reachable at host,
// taken at at. Every sample is validated before anything is written, so a
// malformed report files nothing rather than half of itself.
func (s *FleetTelemetryStore) Record(ctx context.Context, tenantID, node, host string, at time.Time, devices []DeviceSample) error {
	if strings.TrimSpace(host) == "" {
		return &DeviceTelemetryInvalidError{Field: "host", Message: "must not be empty; telemetry is read back by host"}
	}
	for _, d := range devices {
		if err := d.Validate(); err != nil {
			return err
		}
	}
	points := make([]TelemetryPoint, 0, len(devices)*4)
	for _, d := range devices {
		signals := []struct {
			metric  string
			reading Reading
		}{
			{MetricDevicePowerWatts, d.PowerWatts},
			{MetricDeviceUtilization, d.Utilization},
			{MetricDeviceMemoryUsedBytes, d.MemoryUsedBytes},
			{MetricDeviceMemoryTotalBytes, d.MemoryTotalBytes},
		}
		for _, sig := range signals {
			value, known := sig.reading.Value()
			if !known {
				continue
			}
			points = append(points, TelemetryPoint{
				Metric: sig.metric,
				Tags: map[string]string{
					telemetryTagNode:       node,
					telemetryTagHost:       host,
					telemetryTagKind:       string(d.Kind),
					telemetryTagDevice:     d.ID,
					telemetryTagProvenance: sig.reading.Provenance().String(),
				},
				Value: value,
				At:    at.UTC(),
			})
		}
	}
	if len(points) == 0 {
		return nil
	}
	if err := s.series.WritePoints(ctx, tenantID, points); err != nil {
		return &DeviceTelemetryStoreError{Op: "write", Host: host, Cause: err}
	}
	return nil
}

// telemetryPoint is one timed value of one device signal, with how it was
// known.
type telemetryPoint struct {
	at         time.Time
	value      float64
	provenance Provenance
}

// deviceSeries is one device's power and utilisation series over a window,
// each sorted by time.
type deviceSeries struct {
	device      string
	power       []telemetryPoint
	utilization []telemetryPoint
}

// deviceSeries reads every kind device on host over [from, to].
func (s *FleetTelemetryStore) deviceSeries(ctx context.Context, tenantID, host string, kind DeviceKind, from, to time.Time) ([]*deviceSeries, error) {
	match := map[string]string{telemetryTagHost: host, telemetryTagKind: string(kind)}
	byDevice := make(map[string]*deviceSeries)
	signals := []struct {
		metric string
		into   func(*deviceSeries, telemetryPoint)
	}{
		{MetricDevicePowerWatts, func(d *deviceSeries, p telemetryPoint) { d.power = append(d.power, p) }},
		{MetricDeviceUtilization, func(d *deviceSeries, p telemetryPoint) { d.utilization = append(d.utilization, p) }},
	}
	for _, sig := range signals {
		points, err := s.series.QueryPoints(ctx, tenantID, sig.metric, match, from, to)
		if err != nil {
			return nil, &DeviceTelemetryStoreError{Op: "query " + sig.metric, Host: host, Cause: err}
		}
		for _, p := range points {
			device := p.Tags[telemetryTagDevice]
			if device == "" {
				continue
			}
			series, ok := byDevice[device]
			if !ok {
				series = &deviceSeries{device: device}
				byDevice[device] = series
			}
			provenance := provenanceByName[p.Tags[telemetryTagProvenance]]
			if !provenance.FromTelemetry() {
				// A point that does not say how it was known is not a
				// reading this layer can vouch for.
				continue
			}
			sig.into(series, telemetryPoint{at: p.At.UTC(), value: p.Value, provenance: provenance})
		}
	}
	out := make([]*deviceSeries, 0, len(byDevice))
	for _, series := range byDevice {
		sortTelemetry(series.power)
		sortTelemetry(series.utilization)
		out = append(out, series)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].device < out[j].device })
	return out, nil
}

// sortTelemetry orders points by time.
func sortTelemetry(points []telemetryPoint) {
	sort.Slice(points, func(i, j int) bool { return points[i].at.Before(points[j].at) })
}

// Device telemetry sentinels.
var (
	// ErrDeviceTelemetryInvalid indicates a sample or store that cannot be
	// filed or read as given.
	ErrDeviceTelemetryInvalid = errors.New("fleet telemetry: invalid device telemetry")

	// ErrDeviceTelemetryStore indicates the time-series store failed.
	ErrDeviceTelemetryStore = errors.New("fleet telemetry: store failed")
)

// DeviceTelemetryInvalidError is a refused sample or store.
type DeviceTelemetryInvalidError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *DeviceTelemetryInvalidError) Error() string {
	return ErrDeviceTelemetryInvalid.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns ErrDeviceTelemetryInvalid.
func (e *DeviceTelemetryInvalidError) Unwrap() error { return ErrDeviceTelemetryInvalid }

// DeviceTelemetryStoreError is a failed write or read of a host's telemetry.
type DeviceTelemetryStoreError struct {
	Op    string
	Host  string
	Cause error
}

// Error implements the error interface.
func (e *DeviceTelemetryStoreError) Error() string {
	return ErrDeviceTelemetryStore.Error() + ": " + e.Op + " host=" + e.Host + ": " + e.Cause.Error()
}

// Unwrap exposes both the sentinel and the cause.
func (e *DeviceTelemetryStoreError) Unwrap() []error {
	return []error{ErrDeviceTelemetryStore, e.Cause}
}
