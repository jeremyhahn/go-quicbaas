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
	"math"
	"strconv"
	"time"
)

// FleetNodeHealthReason is why a fleet node is, or is not, healthy.
type FleetNodeHealthReason string

const (
	// FleetNodeHealthOK is a node measured recently and inside every bound.
	FleetNodeHealthOK FleetNodeHealthReason = "ok"

	// FleetNodeHealthUnmeasured is a node that has reported no host metrics,
	// or left a signal the verdict needs unread.
	FleetNodeHealthUnmeasured FleetNodeHealthReason = "unmeasured"

	// FleetNodeHealthInvalidSample is a node whose last sample cannot be a
	// measurement of a real host (HostMetrics.Validate).
	FleetNodeHealthInvalidSample FleetNodeHealthReason = "invalid_sample"

	// FleetNodeHealthStale is a node whose last sample is older than the
	// stale bound.
	FleetNodeHealthStale FleetNodeHealthReason = "stale"

	// FleetNodeHealthMemoryExhausted is a node with too little memory free.
	FleetNodeHealthMemoryExhausted FleetNodeHealthReason = "memory_exhausted"

	// FleetNodeHealthCPUSaturated is a node whose CPU is past its ceiling.
	FleetNodeHealthCPUSaturated FleetNodeHealthReason = "cpu_saturated"

	// FleetNodeHealthDiskExhausted is a node with too little disk free.
	FleetNodeHealthDiskExhausted FleetNodeHealthReason = "disk_exhausted"
)

// FleetNodeHealth is the verdict on one fleet node at one instant.
type FleetNodeHealth struct {
	NodeID  string                `json:"node_id"`
	Healthy bool                  `json:"healthy"`
	Reason  FleetNodeHealthReason `json:"reason"`
	Detail  string                `json:"detail,omitempty"`

	// SampleAge is how old the node's information is: the older of the time
	// since its last heartbeat arrived and the time since its last sample was
	// collected. Zero for a node never measured.
	SampleAge time.Duration `json:"sample_age"`
}

// FleetHealthThresholds are the bounds a measured node must sit inside to be
// healthy.
type FleetHealthThresholds struct {
	// MaxSampleAge is the stale bound. The control plane sets it to the fleet
	// heartbeat timeout (SetFleetHeartbeatTimeout), so a node is judged stale
	// at the same moment it is judged unreachable.
	MaxSampleAge time.Duration

	// MaxCPUUtilization is the host-wide CPU ceiling, in (0, 1].
	MaxCPUUtilization float64

	// MinMemAvailableBytes is the memory floor, and MaxMemUsedFraction the
	// ceiling on the fraction in use, in (0, 1]. Both apply: a floor alone
	// passes a large host nearly full, a fraction alone a small host with no
	// room for one more job.
	MinMemAvailableBytes int64
	MaxMemUsedFraction   float64

	// MinDiskAvailableBytes and MaxDiskUsedFraction bound the work
	// directory's filesystem the same way.
	MinDiskAvailableBytes int64
	MaxDiskUsedFraction   float64
}

// DefaultFleetHealthThresholds returns the bounds the control plane uses when
// it is given none. The stale bound is DefaultFleetHeartbeatTimeout.
func DefaultFleetHealthThresholds() FleetHealthThresholds {
	return FleetHealthThresholds{
		MaxSampleAge:          DefaultFleetHeartbeatTimeout,
		MaxCPUUtilization:     0.90,
		MinMemAvailableBytes:  2 << 30,
		MaxMemUsedFraction:    0.90,
		MinDiskAvailableBytes: 5 << 30,
		MaxDiskUsedFraction:   0.95,
	}
}

// Validate refuses bounds that would wave every node through or refuse every
// one: a non-positive stale bound, a negative floor, or a ceiling that is not
// a finite fraction in (0, 1].
func (t FleetHealthThresholds) Validate() error {
	if t.MaxSampleAge <= 0 {
		return &FleetHealthThresholdsError{Field: "max_sample_age", Message: "must be positive"}
	}
	floors := []struct {
		name  string
		value int64
	}{
		{"min_mem_available_bytes", t.MinMemAvailableBytes},
		{"min_disk_available_bytes", t.MinDiskAvailableBytes},
	}
	for _, f := range floors {
		if f.value < 0 {
			return &FleetHealthThresholdsError{Field: f.name, Message: "must not be negative"}
		}
	}
	ceilings := []struct {
		name  string
		value float64
	}{
		{"max_cpu_utilization", t.MaxCPUUtilization},
		{"max_mem_used_fraction", t.MaxMemUsedFraction},
		{"max_disk_used_fraction", t.MaxDiskUsedFraction},
	}
	for _, c := range ceilings {
		if math.IsNaN(c.value) || c.value <= 0 || c.value > 1 {
			return &FleetHealthThresholdsError{Field: c.name, Message: "must be a fraction in (0, 1]"}
		}
	}
	return nil
}

// FleetHealthJudge forms a health verdict for a fleet node from the host
// metrics its daemon last reported.
//
// FAIL-SAFE, NOT FAIL-OPEN. A node is healthy only when it has been measured,
// recently, with every signal the verdict reads, and every one is inside its
// bound. A node that never reported, reported late, left a signal unread or
// sent a sample that cannot be real is unhealthy, because "nothing is known to
// be wrong" is not the same claim as "it is known to be fine".
type FleetHealthJudge struct {
	thresholds FleetHealthThresholds
}

// NewFleetHealthJudge returns a judge applying the given bounds.
func NewFleetHealthJudge(thresholds FleetHealthThresholds) (*FleetHealthJudge, error) {
	if err := thresholds.Validate(); err != nil {
		return nil, err
	}
	return &FleetHealthJudge{thresholds: thresholds}, nil
}

// Thresholds returns the bounds the judge applies.
func (j *FleetHealthJudge) Thresholds() FleetHealthThresholds { return j.thresholds }

// Judge forms the verdict on node at now.
//
// ORDER MATTERS and it is fail-closed first: measurability, validity and
// freshness are settled before any bound is read, so no bound is ever
// compared against a signal nobody has.
func (j *FleetHealthJudge) Judge(node *FleetNode, now time.Time) FleetNodeHealth {
	if node == nil {
		return FleetNodeHealth{Reason: FleetNodeHealthUnmeasured, Detail: "no node record"}
	}
	verdict := FleetNodeHealth{NodeID: node.ID}
	sample := node.HostMetrics
	if sample == nil {
		verdict.Reason = FleetNodeHealthUnmeasured
		verdict.Detail = "this node has never reported host metrics"
		return verdict
	}
	if err := sample.Validate(); err != nil {
		verdict.Reason = FleetNodeHealthInvalidSample
		verdict.Detail = err.Error()
		return verdict
	}
	verdict.SampleAge = fleetSampleAge(node, now)
	if verdict.SampleAge > j.thresholds.MaxSampleAge {
		verdict.Reason = FleetNodeHealthStale
		verdict.Detail = "last sample is " + verdict.SampleAge.Round(time.Second).String() +
			" old, past the " + j.thresholds.MaxSampleAge.String() + " bound"
		return verdict
	}
	for _, check := range fleetHealthChecks {
		if reason, detail, failed := check(sample, j.thresholds); failed {
			verdict.Reason = reason
			verdict.Detail = detail
			return verdict
		}
	}
	verdict.Healthy = true
	verdict.Reason = FleetNodeHealthOK
	return verdict
}

// HealthyNodes returns the nodes judged healthy at now, in the order given.
func (j *FleetHealthJudge) HealthyNodes(nodes []*FleetNode, now time.Time) []*FleetNode {
	healthy := make([]*FleetNode, 0, len(nodes))
	for _, node := range nodes {
		if j.Judge(node, now).Healthy {
			healthy = append(healthy, node)
		}
	}
	return healthy
}

// fleetSampleAge is how old a node's information is, taking whichever of two
// clocks says it is older.
//
// Two clocks, because there are two ways to go stale and each hides the other.
// A node whose daemon stopped talking is caught by LastSeen, which stops
// advancing. A node whose samples arrive LATE — a queued heartbeat, a slow
// link, a daemon catching up after a pause — is caught only by CollectedAt:
// LastSeen is the moment of arrival, so a reading taken ten minutes ago would
// count as fresh for a further full window purely because it turned up now.
// The maximum is the fail-closed answer to both. A node never heard from
// (LastSeen zero) is as old as the epoch.
func fleetSampleAge(node *FleetNode, now time.Time) time.Duration {
	age := now.Sub(time.Unix(0, node.LastSeen))
	if collected := now.Sub(node.HostMetrics.CollectedAt); collected > age {
		age = collected
	}
	return age
}

// fleetHealthCheck tests one bound against a valid, fresh sample, returning
// the reason and detail when the node fails it.
type fleetHealthCheck func(sample *HostMetrics, t FleetHealthThresholds) (FleetNodeHealthReason, string, bool)

// fleetHealthChecks run in order; the first failure is the verdict.
var fleetHealthChecks = []fleetHealthCheck{
	checkFleetNodeMemory,
	checkFleetNodeCPU,
	checkFleetNodeDisk,
}

// unreadSignal is the verdict on a sample that left a judged signal unread.
func unreadSignal(name string) (FleetNodeHealthReason, string, bool) {
	return FleetNodeHealthUnmeasured, name + " was not read, and a node cannot be judged on a signal nobody has", true
}

func checkFleetNodeMemory(sample *HostMetrics, t FleetHealthThresholds) (FleetNodeHealthReason, string, bool) {
	return checkCapacity("memory", sample.MemAvailableBytes, sample.MemTotalBytes,
		t.MinMemAvailableBytes, t.MaxMemUsedFraction, FleetNodeHealthMemoryExhausted)
}

func checkFleetNodeDisk(sample *HostMetrics, t FleetHealthThresholds) (FleetNodeHealthReason, string, bool) {
	return checkCapacity("disk", sample.DiskAvailableBytes, sample.DiskTotalBytes,
		t.MinDiskAvailableBytes, t.MaxDiskUsedFraction, FleetNodeHealthDiskExhausted)
}

func checkFleetNodeCPU(sample *HostMetrics, t FleetHealthThresholds) (FleetNodeHealthReason, string, bool) {
	cpu, ok := sample.CPUUtilization.Value()
	if !ok {
		return unreadSignal("cpu_utilization")
	}
	if cpu > t.MaxCPUUtilization {
		return FleetNodeHealthCPUSaturated,
			formatFraction(cpu) + " CPU in use, past the " + formatFraction(t.MaxCPUUtilization) + " ceiling", true
	}
	return "", "", false
}

// checkCapacity applies a floor and a used-fraction ceiling to one resource.
func checkCapacity(resource string, available, total Reading, floor int64, ceiling float64, reason FleetNodeHealthReason) (FleetNodeHealthReason, string, bool) {
	free, ok := available.Value()
	if !ok {
		return unreadSignal(resource + "_available_bytes")
	}
	if free < float64(floor) {
		return reason, "only " + formatByteCount(free) + " " + resource + " available, below the " +
			formatByteCount(float64(floor)) + " floor", true
	}
	used, ok := usedFraction(available, total)
	if !ok {
		return unreadSignal(resource + "_total_bytes")
	}
	if used > ceiling {
		return reason, formatFraction(used) + " of " + resource + " in use, past the " + formatFraction(ceiling) + " ceiling", true
	}
	return "", "", false
}

func formatFraction(v float64) string {
	return strconv.FormatFloat(v*100, 'f', 1, 64) + "%"
}

func formatByteCount(v float64) string {
	return strconv.FormatFloat(v, 'f', 0, 64) + " B"
}

// JudgeFleetHealth judges every fleet node of a tenant, with the default
// bounds and the configured fleet heartbeat timeout as the stale bound.
func (s *agentServiceImpl) JudgeFleetHealth(ctx context.Context, tenantID string) ([]FleetNodeHealth, error) {
	nodes, err := s.ListFleetNodes(ctx, tenantID, nil)
	if err != nil {
		return nil, err
	}
	thresholds := DefaultFleetHealthThresholds()
	thresholds.MaxSampleAge = s.effectiveFleetHeartbeatTimeout()
	judge, err := NewFleetHealthJudge(thresholds)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	verdicts := make([]FleetNodeHealth, 0, len(nodes))
	for _, node := range nodes {
		verdicts = append(verdicts, judge.Judge(node, now))
	}
	return verdicts, nil
}

var (
	// ErrFleetHealthThresholdsInvalid indicates health bounds that cannot be
	// applied.
	ErrFleetHealthThresholdsInvalid = errors.New("fleet health: invalid thresholds")
)

// FleetHealthThresholdsError is a refused set of health bounds.
type FleetHealthThresholdsError struct {
	Field   string
	Message string
}

// Error implements error.
func (e *FleetHealthThresholdsError) Error() string {
	return ErrFleetHealthThresholdsInvalid.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns ErrFleetHealthThresholdsInvalid.
func (e *FleetHealthThresholdsError) Unwrap() error { return ErrFleetHealthThresholdsInvalid }
