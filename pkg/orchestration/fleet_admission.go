// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package orchestration

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// =============================================================================
// Fleet admission: a new job is placed only on a node measured to have room
// =============================================================================
//
// THREE GATES, IN ORDER, and a node must pass every one:
//
//  1. HEALTH. The node's latest host sample is judged by FleetHealthJudge. A
//     node unmeasured, stale, carrying an invalid sample or past a ceiling is
//     not admitted to. "Nothing is known to be wrong" is not "it is known to
//     be fine": the gate fails closed.
//  2. DECLARED WIDTH. A node never carries more jobs than its declared
//     FleetNode.MaxConcurrentJobs, however idle its metrics look. Measured
//     room is a guess about the next job; the declaration is the operator's
//     ceiling, and an idle sample taken before a burst of spawns started
//     consuming must not lift it.
//  3. MEASURED ROOM. The headroom left under the health ceilings, divided by
//     what one job costs (FleetJobProfile), must fit one more job after the
//     jobs placed since the sample was taken, which the sample cannot show.

// FleetJobProfile is what one orchestration job costs a fleet node: the CPU
// cores and memory a job is expected to take. Measured room is the headroom
// under the health ceilings divided by it.
//
// It starts as a seed (DefaultFleetJobProfile) and is replaced by what the
// fleet measured when FleetAdmission.Recalibrate has enough evidence.
type FleetJobProfile struct {
	CPUCores    float64 `json:"cpu_cores"`
	MemoryBytes int64   `json:"memory_bytes"`

	// Measured reports whether the values came from observation or are the
	// seed. An operator reading a profile needs to know which.
	Measured bool `json:"measured"`

	// SampleSize is how many jobs the measurement was drawn from, and
	// UpdatedAt when it was taken. Both zero for the seed.
	SampleSize int       `json:"sample_size,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}

// DefaultFleetJobProfile is the seed profile: one core and 2 GiB per job.
func DefaultFleetJobProfile() FleetJobProfile {
	return FleetJobProfile{CPUCores: 1, MemoryBytes: 2 << 30}
}

// Validate refuses a profile that would divide a node's room by nothing or
// by a number that is not real.
func (p FleetJobProfile) Validate() error {
	if math.IsNaN(p.CPUCores) || math.IsInf(p.CPUCores, 0) || p.CPUCores <= 0 {
		return &FleetJobProfileError{Field: "cpu_cores", Message: "must be a positive finite number"}
	}
	if p.MemoryBytes <= 0 {
		return &FleetJobProfileError{Field: "memory_bytes", Message: "must be positive"}
	}
	return nil
}

// FleetAdmissionReason is why a node was refused a new job.
type FleetAdmissionReason string

// The reasons a node is refused.
const (
	// FleetAdmissionUnhealthy is a node FleetHealthJudge did not find
	// healthy: unmeasured, stale, an invalid sample, or past a ceiling.
	FleetAdmissionUnhealthy FleetAdmissionReason = "unhealthy"
	// FleetAdmissionAtWidth is a node already carrying its declared
	// MaxConcurrentJobs.
	FleetAdmissionAtWidth FleetAdmissionReason = "at_declared_width"
	// FleetAdmissionNoRoom is a healthy node whose measured headroom does
	// not fit one more job.
	FleetAdmissionNoRoom FleetAdmissionReason = "no_measured_room"
)

// FleetNodeLoad is the work already on a node, as the control plane counts
// it.
type FleetNodeLoad struct {
	// Live is the jobs live on the node (FleetLoadReporter).
	Live int
	// PlacedSinceSample is the spawns placed on the node after its latest
	// host sample was collected: work the sample cannot show yet.
	PlacedSinceSample int
}

// FleetNodeRefusal is one node's refusal of a new job.
type FleetNodeRefusal struct {
	NodeID string               `json:"node_id"`
	Host   string               `json:"host"`
	Reason FleetAdmissionReason `json:"reason"`
	// Health is the health verdict's reason when Reason is
	// FleetAdmissionUnhealthy.
	Health FleetNodeHealthReason `json:"health,omitempty"`
	Detail string                `json:"detail"`
}

// String renders the refusal for an error message or a log line.
func (r FleetNodeRefusal) String() string {
	reason := string(r.Reason)
	if r.Health != "" {
		reason += " (" + string(r.Health) + ")"
	}
	return "node " + r.NodeID + " (" + r.Host + "): " + reason + ": " + r.Detail
}

// FleetAdmission decides whether a fleet node may take one more job. It is
// safe for concurrent use; the job profile is swapped atomically when
// recalibrated.
type FleetAdmission struct {
	judge   *FleetHealthJudge
	profile atomic.Pointer[FleetJobProfile]
}

// NewFleetAdmission returns an admission over judge, starting from profile.
func NewFleetAdmission(judge *FleetHealthJudge, profile FleetJobProfile) (*FleetAdmission, error) {
	if judge == nil {
		return nil, &FleetJobProfileError{Field: "judge", Message: "admission needs a health judge"}
	}
	admission := &FleetAdmission{judge: judge}
	if err := admission.SetProfile(profile); err != nil {
		return nil, err
	}
	return admission, nil
}

// NewDefaultFleetAdmission returns an admission with the default health
// bounds, staleAfter as the stale bound (DefaultFleetHeartbeatTimeout when
// not positive), and the seed job profile.
func NewDefaultFleetAdmission(staleAfter time.Duration) (*FleetAdmission, error) {
	thresholds := DefaultFleetHealthThresholds()
	if staleAfter > 0 {
		thresholds.MaxSampleAge = staleAfter
	}
	judge, err := NewFleetHealthJudge(thresholds)
	if err != nil {
		return nil, err
	}
	return NewFleetAdmission(judge, DefaultFleetJobProfile())
}

// Profile returns the job profile admission currently divides by.
func (a *FleetAdmission) Profile() FleetJobProfile {
	return *a.profile.Load()
}

// SetProfile replaces the job profile after validating it.
func (a *FleetAdmission) SetProfile(profile FleetJobProfile) error {
	if err := profile.Validate(); err != nil {
		return err
	}
	a.profile.Store(&profile)
	return nil
}

// MeasuredRoom returns how many more jobs a sample has room for under the
// health ceilings, at the current job profile: the smaller of the CPU room
// and the memory room. A signal the room is computed from that was not read
// leaves no room, never an assumed one.
func (a *FleetAdmission) MeasuredRoom(sample *HostMetrics) int {
	if sample == nil {
		return 0
	}
	profile := a.Profile()
	thresholds := a.judge.Thresholds()

	cpu, cpuRead := sample.CPUUtilization.Value()
	cores, coresRead := sample.CPUCount.Value()
	total, totalRead := sample.MemTotalBytes.Value()
	available, availableRead := sample.MemAvailableBytes.Value()
	if !cpuRead || !coresRead || !totalRead || !availableRead {
		return 0
	}

	freeCores := (thresholds.MaxCPUUtilization - cpu) * cores
	byCPU := math.Floor(freeCores / profile.CPUCores)

	// Memory must stay above both the floor and the used-fraction ceiling,
	// so the reserve is whichever of the two is larger.
	reserve := math.Max(float64(thresholds.MinMemAvailableBytes), (1-thresholds.MaxMemUsedFraction)*total)
	byMemory := math.Floor((available - reserve) / float64(profile.MemoryBytes))

	room := math.Min(byCPU, byMemory)
	if room <= 0 || math.IsNaN(room) {
		return 0
	}
	return int(room)
}

// Admit decides whether node may take one more job, given the work already
// on it. It returns nil when the node is admitted, and the refusal otherwise.
// AdmitContinuation decides whether node may take a spawn that continues a
// job already holding a slot there, such as the job's reviewer pinned to its
// lead's host. The job's slot was counted when it was admitted, so only the
// health gate applies: a node unhealthy, unmeasured or stale takes nothing.
func (a *FleetAdmission) AdmitContinuation(node *FleetNode, now time.Time) *FleetNodeRefusal {
	verdict := a.judge.Judge(node, now)
	if verdict.Healthy {
		return nil
	}
	refusal := &FleetNodeRefusal{
		Reason: FleetAdmissionUnhealthy,
		Health: verdict.Reason,
		Detail: verdict.Detail,
	}
	if node != nil {
		refusal.NodeID, refusal.Host = node.ID, node.Host
	}
	return refusal
}

func (a *FleetAdmission) Admit(node *FleetNode, load FleetNodeLoad, now time.Time) *FleetNodeRefusal {
	if refusal := a.AdmitContinuation(node, now); refusal != nil {
		return refusal
	}
	if node.MaxConcurrentJobs > 0 && load.Live >= node.MaxConcurrentJobs {
		return &FleetNodeRefusal{
			NodeID: node.ID,
			Host:   node.Host,
			Reason: FleetAdmissionAtWidth,
			Detail: strconv.Itoa(load.Live) + " of " + strconv.Itoa(node.MaxConcurrentJobs) + " declared job slots in use",
		}
	}
	room := a.MeasuredRoom(node.HostMetrics)
	if room-load.PlacedSinceSample <= 0 {
		return &FleetNodeRefusal{
			NodeID: node.ID,
			Host:   node.Host,
			Reason: FleetAdmissionNoRoom,
			Detail: "measured room for " + strconv.Itoa(room) + " more job(s), " +
				strconv.Itoa(load.PlacedSinceSample) + " placed since the sample",
		}
	}
	return nil
}

// Recalibrate measures what one job costs from the fleet as it runs now and
// adopts the measurement as the job profile. loadByHost is each host's load:
// the jobs live on it (FleetLoadReporter.LiveJobsByHost) and the spawns placed
// on it since its sample.
//
// Only healthy nodes carrying jobs are measured: an unhealthy, unmeasured or
// stale node has no usage to attribute, and attributing zero would drag the
// profile toward nothing. A host placed on since its sample is not measured
// either: its live count includes jobs its sample predates, and dividing the
// older usage by them would cost each job too little, widening every node's
// room on each placement of a burst. A host is counted once however many nodes it
// registered. A node's whole usage is attributed to its jobs, so the profile
// errs toward costing a job too much, which admits less rather than more.
//
// It returns false, nil when fewer than minimumJobs jobs were measured: a
// quiet fleet is not a fault. A measurement of zero cost is refused, since
// adopting it would divide every node's room by nothing.
func (a *FleetAdmission) Recalibrate(nodes []*FleetNode, loadByHost map[string]FleetNodeLoad, minimumJobs int, now time.Time) (bool, error) {
	if minimumJobs < 1 {
		return false, &FleetJobProfileError{
			Field:   "minimum_jobs",
			Message: "recalibrating on no evidence is not recalibration; one job is the smallest sample",
		}
	}

	var cpuCores, memoryBytes float64
	jobs := 0
	measuredHosts := make(map[string]bool)
	for _, node := range nodes {
		if node == nil || measuredHosts[node.Host] {
			continue
		}
		load := loadByHost[node.Host]
		if load.Live <= 0 || load.PlacedSinceSample > 0 {
			continue
		}
		if !a.judge.Judge(node, now).Healthy {
			continue
		}
		sample := node.HostMetrics
		cpu, cpuRead := sample.CPUUtilization.Value()
		cores, coresRead := sample.CPUCount.Value()
		total, totalRead := sample.MemTotalBytes.Value()
		available, availableRead := sample.MemAvailableBytes.Value()
		if !cpuRead || !coresRead || !totalRead || !availableRead {
			// A cost derived from an unread usage figure is a guess, and
			// this loop's output becomes the profile every node admits on.
			continue
		}
		measuredHosts[node.Host] = true
		cpuCores += cpu * cores
		memoryBytes += total - available
		jobs += load.Live
	}
	if jobs < minimumJobs {
		return false, nil
	}

	next := FleetJobProfile{
		CPUCores:    cpuCores / float64(jobs),
		MemoryBytes: int64(memoryBytes / float64(jobs)),
		Measured:    true,
		SampleSize:  jobs,
		UpdatedAt:   now,
	}
	if err := a.SetProfile(next); err != nil {
		return false, err
	}
	return true, nil
}

// =============================================================================
// Errors
// =============================================================================

var (
	// ErrFleetCapacityRefused indicates every fleet node that could otherwise
	// serve a spawn refused it on health or capacity. Classified as an
	// exhausted pool (AgentCatalogFaultExhausted), so every transport answers
	// it as it answers an exhausted Agent pool.
	ErrFleetCapacityRefused = errors.New("orchestration: no fleet node has measured capacity for this job")

	// ErrFleetJobProfileInvalid indicates a job profile or admission that
	// cannot be applied.
	ErrFleetJobProfileInvalid = errors.New("fleet admission: invalid job profile")
)

// FleetCapacityRefusedError is a spawn every candidate fleet node refused.
// The spawn does not fall through to a local backend: a refusal on measured
// capacity is the answer, and running the job somewhere unmeasured instead
// would be failing open.
type FleetCapacityRefusedError struct {
	Backend  string
	Refusals []FleetNodeRefusal
}

// Error implements error.
func (e *FleetCapacityRefusedError) Error() string {
	parts := make([]string, 0, len(e.Refusals))
	for _, refusal := range e.Refusals {
		parts = append(parts, refusal.String())
	}
	return ErrFleetCapacityRefused.Error() + ": backend=" + e.Backend + ": " + strings.Join(parts, "; ")
}

// Unwrap returns ErrFleetCapacityRefused.
func (e *FleetCapacityRefusedError) Unwrap() error { return ErrFleetCapacityRefused }

// FleetJobProfileError is a refused job profile or admission setup.
type FleetJobProfileError struct {
	Field   string
	Message string
}

// Error implements error.
func (e *FleetJobProfileError) Error() string {
	return ErrFleetJobProfileInvalid.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns ErrFleetJobProfileInvalid.
func (e *FleetJobProfileError) Unwrap() error { return ErrFleetJobProfileInvalid }
