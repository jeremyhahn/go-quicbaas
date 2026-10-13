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

package grpc

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func TestFleetObservationFromProto_AbsentObservationReportsNothing(t *testing.T) {
	if got := fleetObservationFromProto(nil); got != nil {
		t.Fatalf("an absent observation decoded to %#v", got)
	}
}

func TestFleetObservationFromProto_UnflaggedListsReconcileNothing(t *testing.T) {
	got := fleetObservationFromProto(&pb.FleetNodeObservation{
		Backends: []string{"claude"},
		// Entries without their *_reported flag are not a report.
		SigningAgents: []string{"root-a"},
		Images:        []string{"img:1"},
		Devices:       []*pb.FleetDeviceSample{{Kind: "gpu", Id: "g0"}},
	})
	if got == nil {
		t.Fatal("an observation with backends must observe")
	}
	if got.SigningAgents != nil || got.Images != nil || got.Devices != nil {
		t.Errorf("unflagged lists were reported: %#v", got)
	}
	if got.HostMetrics != nil {
		t.Errorf("absent host metrics were reported: %#v", got.HostMetrics)
	}
}

func TestFleetObservationFromProto_FlaggedEmptyListsAreReports(t *testing.T) {
	got := fleetObservationFromProto(&pb.FleetNodeObservation{
		SigningAgentsReported: true,
		ImagesReported:        true,
		DevicesReported:       true,
	})
	if got == nil || got.Backends == nil || len(got.Backends) != 0 {
		t.Fatalf("an empty served list must be carried through for the registry to refuse, got %#v", got)
	}
	if got.SigningAgents == nil || got.Images == nil || got.Devices == nil {
		t.Errorf("flagged empty lists must be empty reports, not absences: %#v", got)
	}
}

func TestFleetObservationFromProto_CarriesHostMetricsAndDevices(t *testing.T) {
	at := time.Date(2026, 10, 12, 9, 30, 0, 0, time.UTC)
	load := 1.5
	watts := 200.0
	got := fleetObservationFromProto(&pb.FleetNodeObservation{
		Backends:        []string{"claude"},
		DevicesReported: true,
		Devices: []*pb.FleetDeviceSample{{
			Kind: "gpu", Id: "g0", Name: "GPU",
			PowerWatts: &pb.FleetReading{Value: &watts, Provenance: "measured"},
		}},
		HostMetrics: &pb.FleetHostMetrics{
			CollectedAt:    timestamppb.New(at),
			Load1:          &pb.FleetReading{Value: &load, Provenance: "measured"},
			CpuUtilization: &pb.FleetReading{Provenance: "unknown"},
		},
	})
	if got == nil || got.HostMetrics == nil {
		t.Fatalf("host metrics lost: %#v", got)
	}
	if !got.HostMetrics.CollectedAt.Equal(at) {
		t.Errorf("collected_at = %v, want %v", got.HostMetrics.CollectedAt, at)
	}
	if v, ok := got.HostMetrics.Load1.Value(); !ok || v != load {
		t.Errorf("load1 = %v (known %v), want %v", v, ok, load)
	}
	if got.HostMetrics.CPUUtilization.Known() || got.HostMetrics.MemTotalBytes.Known() {
		t.Errorf("unread and absent readings must stay unread: %#v", got.HostMetrics)
	}
	if len(got.Devices) != 1 || got.Devices[0].PowerWatts != orchestration.Measured(watts) || got.Devices[0].Utilization.Known() {
		t.Errorf("device telemetry = %#v", got.Devices)
	}
}

func TestHostMetricsFromProto_UnsetCollectionTimeIsInvalid(t *testing.T) {
	got := hostMetricsFromProto(&pb.FleetHostMetrics{})
	if !got.CollectedAt.IsZero() {
		t.Fatalf("an unset collection time decoded to %v", got.CollectedAt)
	}
	if err := got.Validate(); err == nil {
		t.Fatal("a sample with no collection time must be refused, not aged from the epoch")
	}
}
