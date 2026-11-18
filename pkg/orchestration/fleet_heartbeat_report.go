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
	"encoding/json"
	"errors"
)

// FleetHeartbeatReport is the wire form of a FleetNodeObservation: the JSON a
// fleet daemon reports about itself on every heartbeat. The REST heartbeat
// body embeds it, and the MCP, gRPC and embedded transports build one, so
// every transport decodes a heartbeat the same way and none can drop a field
// the others carry.
//
// Presence is meaningful, which is why the lists are pointers: an absent key
// reports nothing and reconciles nothing, while an empty list is a report.
type FleetHeartbeatReport struct {
	// Backends is the set of backend names the daemon's runtime is serving.
	// A POINTER so that "the daemon reported none" is a different answer
	// from "the daemon reported nothing". Both decode to a nil slice, and
	// they mean opposite things: a report with no backends key is an older
	// daemon or a bare heartbeat, which reconciles nothing; a report carrying
	// an empty list is a daemon that constructed no backend at all, which is
	// a fault to surface (go-quicbaas#699).
	Backends *[]string `json:"backends"`

	// MaxConcurrentJobs is how many orchestration jobs the daemon declares
	// its host can run at once. Zero, or an absent key, reports nothing; see
	// FleetNodeObservation.MaxConcurrentJobs.
	MaxConcurrentJobs int `json:"max_concurrent_jobs,omitempty"`

	// SigningAgents is the identifier of every control-plane sockets root the
	// daemon can mount a signing socket from. A POINTER for the reason
	// Backends is one.
	SigningAgents *[]string `json:"signing_agents"`

	// SigningRelay reports whether the daemon can serve a session's signing
	// socket from a relay (go-quicbaas#1013). Absent reconciles nothing.
	SigningRelay *bool `json:"signing_relay,omitempty"`

	// Images are the container images on the daemon's host. Absent is
	// unknown; placement treats only a reported list as authoritative.
	Images *[]string `json:"images,omitempty"`

	// Devices is the daemon's accelerator telemetry. Absent reports nothing.
	Devices []DeviceSample `json:"devices,omitempty"`

	// HostMetrics is the daemon's host-wide sample. Absent reports nothing,
	// and the node's previous sample ages toward stale.
	HostMetrics *HostMetrics `json:"host_metrics,omitempty"`
}

// Observation returns what the daemon reported about itself, or nil when it
// reported nothing at all. An empty served list is carried through rather
// than dropped, because refusing it is the point: the registry decides that,
// not the decoder.
func (r *FleetHeartbeatReport) Observation() *FleetNodeObservation {
	if r == nil || r.Backends == nil {
		return nil
	}
	// THE OTHER FIELDS RIDE ON THE SERVED LIST rather than standing alone. An
	// observation is refused outright when it reports no backend at all
	// (FleetNodeObservation.Validate), so a report carrying a capacity and no
	// backends key would be rejected rather than reconciled — and there is
	// no such report: pkg/orchestration/fleet writes every field in one payload.
	observation := &FleetNodeObservation{
		Backends:          append([]string{}, (*r.Backends)...),
		MaxConcurrentJobs: r.MaxConcurrentJobs,
		Devices:           r.Devices,
	}
	if r.SigningAgents != nil {
		observation.SigningAgents = append([]string{}, (*r.SigningAgents)...)
	}
	if r.SigningRelay != nil {
		relay := *r.SigningRelay
		observation.SigningRelay = &relay
	}
	if r.Images != nil {
		images := append([]string{}, (*r.Images)...)
		observation.Images = &images
	}
	if r.HostMetrics != nil {
		sample := *r.HostMetrics
		observation.HostMetrics = &sample
	}
	return observation
}

// ParseFleetHeartbeatReport decodes a FleetHeartbeatReport's JSON into the
// observation it carries. Empty input is a bare heartbeat and returns nil.
func ParseFleetHeartbeatReport(data []byte) (*FleetNodeObservation, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var report FleetHeartbeatReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, &FleetHeartbeatReportInvalidError{Cause: err}
	}
	return report.Observation(), nil
}

// DecodeFleetHeartbeatReport decodes any value whose JSON is a
// FleetHeartbeatReport — an MCP tool's decoded arguments, or an SDK
// observation — through the same path a REST body takes. Nil is a bare
// heartbeat and returns nil.
func DecodeFleetHeartbeatReport(report interface{}) (*FleetNodeObservation, error) {
	if report == nil {
		return nil, nil
	}
	data, err := json.Marshal(report)
	if err != nil {
		return nil, &FleetHeartbeatReportInvalidError{Cause: err}
	}
	return ParseFleetHeartbeatReport(data)
}

// ErrFleetHeartbeatReportInvalid indicates a heartbeat report that is not a
// FleetHeartbeatReport's JSON.
var ErrFleetHeartbeatReportInvalid = errors.New("fleet heartbeat: report is not valid JSON")

// FleetHeartbeatReportInvalidError is a heartbeat report that did not decode.
type FleetHeartbeatReportInvalidError struct {
	Cause error
}

// Error implements error.
func (e *FleetHeartbeatReportInvalidError) Error() string {
	return ErrFleetHeartbeatReportInvalid.Error() + ": " + e.Cause.Error()
}

// Unwrap returns ErrFleetHeartbeatReportInvalid and the decoding error.
func (e *FleetHeartbeatReportInvalidError) Unwrap() []error {
	return []error{ErrFleetHeartbeatReportInvalid, e.Cause}
}
