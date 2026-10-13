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
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// fleetObservationToProto encodes a heartbeat observation. Proto repeated
// fields cannot tell an absent list from an empty one, so each optional list
// travels with a *_reported flag; the server reads the flags back into the
// same presence the REST body carries. A nil Backends reports nothing, as it
// does over REST.
func fleetObservationToProto(in *orchestrator.FleetNodeObservation) *pb.FleetNodeObservation {
	if in == nil || in.Backends == nil {
		return nil
	}
	out := &pb.FleetNodeObservation{
		Backends:          in.Backends,
		MaxConcurrentJobs: int32(in.MaxConcurrentJobs),
		SigningRelay:      in.SigningRelay,
		HostMetrics:       fleetHostMetricsToProto(in.HostMetrics),
	}
	if in.SigningAgents != nil {
		out.SigningAgents = *in.SigningAgents
		out.SigningAgentsReported = true
	}
	if in.Images != nil {
		out.Images = *in.Images
		out.ImagesReported = true
	}
	if in.Devices != nil {
		out.DevicesReported = true
		out.Devices = make([]*pb.FleetDeviceSample, 0, len(in.Devices))
		for _, d := range in.Devices {
			out.Devices = append(out.Devices, &pb.FleetDeviceSample{
				Kind:             d.Kind,
				Id:               d.ID,
				Name:             d.Name,
				PowerWatts:       fleetReadingToProto(d.PowerWatts),
				Utilization:      fleetReadingToProto(d.Utilization),
				MemoryUsedBytes:  fleetReadingToProto(d.MemoryUsedBytes),
				MemoryTotalBytes: fleetReadingToProto(d.MemoryTotalBytes),
			})
		}
	}
	return out
}

func fleetHostMetricsToProto(in *orchestrator.FleetHostMetrics) *pb.FleetHostMetrics {
	if in == nil {
		return nil
	}
	out := &pb.FleetHostMetrics{
		CpuUtilization:     fleetReadingToProto(in.CPUUtilization),
		CpuCount:           fleetReadingToProto(in.CPUCount),
		Load1:              fleetReadingToProto(in.Load1),
		Load5:              fleetReadingToProto(in.Load5),
		Load15:             fleetReadingToProto(in.Load15),
		MemTotalBytes:      fleetReadingToProto(in.MemTotalBytes),
		MemAvailableBytes:  fleetReadingToProto(in.MemAvailableBytes),
		DiskTotalBytes:     fleetReadingToProto(in.DiskTotalBytes),
		DiskAvailableBytes: fleetReadingToProto(in.DiskAvailableBytes),
	}
	// A zero collection time stays unset, so the server refuses the sample
	// exactly as it refuses one with no collected_at over REST.
	if !in.CollectedAt.IsZero() {
		out.CollectedAt = timestamppb.New(in.CollectedAt)
	}
	return out
}

func fleetReadingToProto(in orchestrator.FleetReading) *pb.FleetReading {
	return &pb.FleetReading{Value: in.Value, Provenance: in.Provenance}
}
