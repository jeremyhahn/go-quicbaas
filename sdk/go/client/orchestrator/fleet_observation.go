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

package orchestrator

import "time"

// FleetReading is one telemetry value with its provenance: "measured" or
// "derived". A reading with no Value, or any other provenance, is unread —
// the server never treats an absent reading as zero.
type FleetReading struct {
	Value      *float64 `json:"value,omitempty"`
	Provenance string   `json:"provenance"`
}

// FleetDeviceSample is one accelerator's telemetry on a heartbeat.
type FleetDeviceSample struct {
	Kind             string       `json:"kind"`
	ID               string       `json:"id"`
	Name             string       `json:"name,omitempty"`
	PowerWatts       FleetReading `json:"power_watts"`
	Utilization      FleetReading `json:"utilization"`
	MemoryUsedBytes  FleetReading `json:"memory_used_bytes"`
	MemoryTotalBytes FleetReading `json:"memory_total_bytes"`
}

// FleetHostMetrics is a node's host-wide sample. CollectedAt is required: the
// server ages the sample from when it was taken, and judges a node whose
// sample is older than the fleet heartbeat timeout unhealthy.
type FleetHostMetrics struct {
	CollectedAt        time.Time    `json:"collected_at"`
	CPUUtilization     FleetReading `json:"cpu_utilization"`
	CPUCount           FleetReading `json:"cpu_count"`
	Load1              FleetReading `json:"load1"`
	Load5              FleetReading `json:"load5"`
	Load15             FleetReading `json:"load15"`
	MemTotalBytes      FleetReading `json:"mem_total_bytes"`
	MemAvailableBytes  FleetReading `json:"mem_available_bytes"`
	DiskTotalBytes     FleetReading `json:"disk_total_bytes"`
	DiskAvailableBytes FleetReading `json:"disk_available_bytes"`
}

// FleetNodeObservation is what a fleet node reports about itself on a
// heartbeat. Presence is meaningful, which is why the lists are pointers or
// nil-able: an absent list reports nothing and reconciles nothing, while an
// empty list is a report. Backends is the observation's anchor — a nil
// Backends sends no observation at all.
type FleetNodeObservation struct {
	Backends          []string            `json:"backends"`
	MaxConcurrentJobs int                 `json:"max_concurrent_jobs,omitempty"`
	SigningAgents     *[]string           `json:"signing_agents,omitempty"`
	SigningRelay      *bool               `json:"signing_relay,omitempty"`
	Images            *[]string           `json:"images,omitempty"`
	Devices           []FleetDeviceSample `json:"devices"`
	HostMetrics       *FleetHostMetrics   `json:"host_metrics,omitempty"`
}
