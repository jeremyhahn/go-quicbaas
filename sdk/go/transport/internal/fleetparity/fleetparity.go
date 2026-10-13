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

// Package fleetparity is the shared fixture for the heartbeat parity tests:
// every transport must deliver a fleet node's observation to the agent
// service exactly as the REST heartbeat body decodes it. Each transport's
// test sends Observation() through its own SDK encoder and real server
// decoder, captures what reached the service with a Recorder, and checks it
// with RequireParity.
package fleetparity

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// CollectedAt is the fixture sample's collection time.
var CollectedAt = time.Date(2026, 10, 12, 9, 30, 0, 0, time.UTC)

// Observation returns an observation that exercises every heartbeat field:
// host metrics with one unread reading, an empty signing-agent list (a
// report, not an absence), a device, and the optional scalars.
func Observation() *orchestrator.FleetNodeObservation {
	relay := true
	agents := []string{}
	images := []string{"registry.example/agent:1"}
	return &orchestrator.FleetNodeObservation{
		Backends:          []string{"claude", "codex"},
		MaxConcurrentJobs: 4,
		SigningAgents:     &agents,
		SigningRelay:      &relay,
		Images:            &images,
		Devices: []orchestrator.FleetDeviceSample{{
			Kind:             "gpu",
			ID:               "GPU-1",
			Name:             "Example GPU",
			PowerWatts:       measured(210),
			Utilization:      measured(0.5),
			MemoryUsedBytes:  measured(1 << 30),
			MemoryTotalBytes: measured(8 << 30),
		}},
		HostMetrics: &orchestrator.FleetHostMetrics{
			CollectedAt: CollectedAt,
			// The first sample of a daemon has no utilisation yet.
			CPUUtilization:     orchestrator.FleetReading{Provenance: "unknown"},
			CPUCount:           measured(16),
			Load1:              measured(1.5),
			Load5:              measured(1.25),
			Load15:             measured(1),
			MemTotalBytes:      measured(64 << 30),
			MemAvailableBytes:  measured(32 << 30),
			DiskTotalBytes:     measured(1 << 40),
			DiskAvailableBytes: derived(1 << 39),
		},
	}
}

func measured(v float64) orchestrator.FleetReading {
	return orchestrator.FleetReading{Value: &v, Provenance: "measured"}
}

func derived(v float64) orchestrator.FleetReading {
	return orchestrator.FleetReading{Value: &v, Provenance: "derived"}
}

// Reference is what the REST heartbeat body decodes the observation to: the
// answer every other transport must reproduce.
func Reference(t testing.TB, observation *orchestrator.FleetNodeObservation) *orchestration.FleetNodeObservation {
	t.Helper()
	body, err := json.Marshal(observation)
	if err != nil {
		t.Fatalf("marshal observation: %v", err)
	}
	reference, err := orchestration.ParseFleetHeartbeatReport(body)
	if err != nil {
		t.Fatalf("decode REST body: %v", err)
	}
	return reference
}

// RequireParity fails the test unless got is the fixture's REST decoding.
func RequireParity(t testing.TB, got *orchestration.FleetNodeObservation) {
	t.Helper()
	want := Reference(t, Observation())
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("observation did not survive the transport:\n got  %#v\n want %#v", got, want)
	}
}

// Recorder is a QuicBaaS whose agent service records each heartbeat's
// observation. Every other service behaves as the testutil mock does.
type Recorder struct {
	*testutil.MockQuicBaaS
	agent *recordingAgent
}

// NewRecorder returns a Recorder with room for one heartbeat.
func NewRecorder() *Recorder {
	return &Recorder{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		agent:        &recordingAgent{observed: make(chan *orchestration.FleetNodeObservation, 1)},
	}
}

// Agent returns the recording agent service.
func (r *Recorder) Agent() orchestration.AgentService {
	return r.agent
}

// Observed returns the observation the last heartbeat delivered, failing the
// test if no heartbeat reached the service.
func (r *Recorder) Observed(t testing.TB) *orchestration.FleetNodeObservation {
	t.Helper()
	select {
	case observed := <-r.agent.observed:
		return observed
	default:
		t.Fatal("no heartbeat reached the agent service")
		return nil
	}
}

// recordingAgent implements only FleetNodeHeartbeat; the embedded interface
// is nil, so any other call fails the test loudly with a nil dereference.
type recordingAgent struct {
	orchestration.AgentService
	observed chan *orchestration.FleetNodeObservation
}

func (a *recordingAgent) FleetNodeHeartbeat(_ context.Context, _, _ string, observed *orchestration.FleetNodeObservation) error {
	a.observed <- observed
	return nil
}
