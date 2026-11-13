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
	"errors"
	"time"
)

// HostMetrics is one fleet node's host-wide state at one heartbeat: CPU, load,
// memory and the disk its work lives on. The daemon collects it
// (pkg/orchestration/fleet HostMetricsSampler) and reports it on the heartbeat
// (FleetNodeObservation.HostMetrics); the control plane keeps the latest
// sample on the node's record and judges the node's health from it
// (fleet_health.go).
//
// EVERY SIGNAL IS A Reading, so an absent signal is reported as absent and
// never as zero. A host whose memory could not be read has not reported that
// it has no memory free, and a health verdict that read the zero as a value
// would call a busy host idle, or an idle one exhausted.
type HostMetrics struct {
	// CollectedAt is when the daemon took the sample. It is what lets a
	// sample delivered late be aged from when it was TAKEN rather than from
	// when it arrived (FleetHealthJudge). Required.
	CollectedAt time.Time `json:"collected_at"`

	// CPUUtilization is the fraction of host-wide CPU time spent busy over
	// the last heartbeat interval, in [0, 1]. Derived from the kernel's
	// cumulative CPU counters, so it is unread on the daemon's first sample:
	// a utilisation needs two readings.
	CPUUtilization Reading `json:"cpu_utilization"`

	// CPUCount is the number of online CPUs, the scale load is read against.
	CPUCount Reading `json:"cpu_count"`

	// Load1, Load5 and Load15 are the 1-, 5- and 15-minute load averages.
	Load1  Reading `json:"load1"`
	Load5  Reading `json:"load5"`
	Load15 Reading `json:"load15"`

	// MemTotalBytes is the host's memory, and MemAvailableBytes how much of
	// it new work can have without swapping. Available rather than free:
	// free memory on a working Linux host is near zero by design, because
	// the page cache uses what nothing else needs.
	MemTotalBytes     Reading `json:"mem_total_bytes"`
	MemAvailableBytes Reading `json:"mem_available_bytes"`

	// DiskTotalBytes and DiskAvailableBytes are the size of, and the space an
	// unprivileged writer can still use on, the filesystem holding the
	// daemon's work directory.
	DiskTotalBytes     Reading `json:"disk_total_bytes"`
	DiskAvailableBytes Reading `json:"disk_available_bytes"`
}

// hostMetricField names one of a sample's readings for validation and for the
// health verdict's messages.
type hostMetricField struct {
	name    string
	reading Reading
}

// counts are the readings that are sizes or counts and so cannot be negative.
func (m *HostMetrics) counts() []hostMetricField {
	return []hostMetricField{
		{"cpu_count", m.CPUCount},
		{"load1", m.Load1},
		{"load5", m.Load5},
		{"load15", m.Load15},
		{"mem_total_bytes", m.MemTotalBytes},
		{"mem_available_bytes", m.MemAvailableBytes},
		{"disk_total_bytes", m.DiskTotalBytes},
		{"disk_available_bytes", m.DiskAvailableBytes},
	}
}

// Validate refuses a sample that cannot be a measurement of a real host: no
// collection time, a negative size, a utilisation outside [0, 1], or more
// available than there is. Unread signals are not faults; they are absent.
//
// NaN and the infinities cannot reach a Reading (Measured and Derived refuse
// them, and JSON cannot carry them), so a failed calculation arrives as an
// unread signal rather than as a number that compares false against every
// threshold.
func (m *HostMetrics) Validate() error {
	if m == nil {
		return &HostMetricsInvalidError{Field: "host_metrics", Message: "must not be nil"}
	}
	if m.CollectedAt.IsZero() {
		return &HostMetricsInvalidError{Field: "collected_at", Message: "must be set; a sample with no collection time cannot be aged"}
	}
	for _, f := range m.counts() {
		if v, ok := f.reading.Value(); ok && v < 0 {
			return &HostMetricsInvalidError{Field: f.name, Message: "must not be negative"}
		}
	}
	if v, ok := m.CPUUtilization.Value(); ok && (v < 0 || v > 1) {
		return &HostMetricsInvalidError{Field: "cpu_utilization", Message: "must be a fraction in [0, 1]"}
	}
	pairs := []struct {
		name             string
		available, total Reading
	}{
		{"mem_available_bytes", m.MemAvailableBytes, m.MemTotalBytes},
		{"disk_available_bytes", m.DiskAvailableBytes, m.DiskTotalBytes},
	}
	for _, p := range pairs {
		available, aok := p.available.Value()
		total, tok := p.total.Value()
		if aok && tok && available > total {
			return &HostMetricsInvalidError{Field: p.name, Message: "exceeds the total, which is a mismatched pair of reads"}
		}
	}
	return nil
}

// usedFraction is the fraction of total not available, and whether both
// halves were read and the total is non-zero.
func usedFraction(available, total Reading) (float64, bool) {
	a, aok := available.Value()
	t, tok := total.Value()
	if !aok || !tok || t <= 0 {
		return 0, false
	}
	return 1 - a/t, true
}

var (
	// ErrHostMetricsInvalid indicates a host-metrics sample that cannot be a
	// measurement of a real host.
	ErrHostMetricsInvalid = errors.New("fleet host metrics: invalid sample")
)

// HostMetricsInvalidError is a refused host-metrics sample.
type HostMetricsInvalidError struct {
	Field   string
	Message string
}

// Error implements error.
func (e *HostMetricsInvalidError) Error() string {
	return ErrHostMetricsInvalid.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns ErrHostMetricsInvalid.
func (e *HostMetricsInvalidError) Unwrap() error { return ErrHostMetricsInvalid }
