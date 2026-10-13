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

package fleetparity

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// The parity tests are only as strong as the reference: if the REST decoding
// dropped a field, every transport could drop it too and still agree. This
// pins the reference to the fields the fixture sends.
func TestReference_CarriesEveryHeartbeatField(t *testing.T) {
	ref := Reference(t, Observation())
	if ref == nil {
		t.Fatal("the fixture decoded to no observation")
	}
	if len(ref.Backends) != 2 || ref.MaxConcurrentJobs != 4 {
		t.Errorf("backends or capacity lost: %#v", ref)
	}
	if ref.SigningAgents == nil || len(ref.SigningAgents) != 0 {
		t.Errorf("an empty signing-agent report must stay an empty, non-nil list, got %#v", ref.SigningAgents)
	}
	if ref.SigningRelay == nil || !*ref.SigningRelay {
		t.Errorf("signing relay lost: %v", ref.SigningRelay)
	}
	if ref.Images == nil || len(*ref.Images) != 1 {
		t.Errorf("images lost: %v", ref.Images)
	}
	if len(ref.Devices) != 1 || ref.Devices[0].PowerWatts.Provenance() != orchestration.ProvenanceMeasured {
		t.Errorf("device telemetry lost: %#v", ref.Devices)
	}

	metrics := ref.HostMetrics
	if metrics == nil {
		t.Fatal("host metrics lost")
	}
	if !metrics.CollectedAt.Equal(CollectedAt) {
		t.Errorf("collected_at = %v, want %v", metrics.CollectedAt, CollectedAt)
	}
	if metrics.CPUUtilization.Known() {
		t.Errorf("an unread reading must stay unread, got %#v", metrics.CPUUtilization)
	}
	if v, ok := metrics.CPUCount.Value(); !ok || v != 16 {
		t.Errorf("cpu_count = %v (known %v), want 16", v, ok)
	}
	if metrics.DiskAvailableBytes.Provenance() != orchestration.ProvenanceDerived {
		t.Errorf("a derived reading lost its provenance: %#v", metrics.DiskAvailableBytes)
	}
	if err := metrics.Validate(); err != nil {
		t.Errorf("the fixture sample must be valid: %v", err)
	}
}

func TestRecorder_ObservedReturnsWhatTheHeartbeatDelivered(t *testing.T) {
	recorder := NewRecorder()
	want := &orchestration.FleetNodeObservation{Backends: []string{"claude"}}
	if err := recorder.Agent().FleetNodeHeartbeat(context.Background(), "t", "n", want); err != nil {
		t.Fatalf("FleetNodeHeartbeat: %v", err)
	}
	if got := recorder.Observed(t); got != want {
		t.Fatalf("Observed = %p, want %p", got, want)
	}
}
