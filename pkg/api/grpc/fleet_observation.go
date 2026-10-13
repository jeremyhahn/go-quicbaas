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
	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// fleetObservationFromProto decodes a heartbeat's observation through the
// same FleetHeartbeatReport the REST body uses, so presence means the same on
// both: an absent observation reports nothing, and an unflagged list
// reconciles nothing.
func fleetObservationFromProto(in *pb.FleetNodeObservation) *orchestration.FleetNodeObservation {
	if in == nil {
		return nil
	}
	backends := append([]string{}, in.GetBackends()...)
	report := orchestration.FleetHeartbeatReport{
		Backends:          &backends,
		MaxConcurrentJobs: int(in.GetMaxConcurrentJobs()),
		SigningRelay:      in.SigningRelay,
		HostMetrics:       hostMetricsFromProto(in.GetHostMetrics()),
	}
	if in.GetSigningAgentsReported() {
		agents := append([]string{}, in.GetSigningAgents()...)
		report.SigningAgents = &agents
	}
	if in.GetImagesReported() {
		images := append([]string{}, in.GetImages()...)
		report.Images = &images
	}
	if in.GetDevicesReported() {
		report.Devices = make([]orchestration.DeviceSample, 0, len(in.GetDevices()))
		for _, d := range in.GetDevices() {
			report.Devices = append(report.Devices, orchestration.DeviceSample{
				Kind:             orchestration.DeviceKind(d.GetKind()),
				ID:               d.GetId(),
				Name:             d.GetName(),
				PowerWatts:       readingFromProto(d.GetPowerWatts()),
				Utilization:      readingFromProto(d.GetUtilization()),
				MemoryUsedBytes:  readingFromProto(d.GetMemoryUsedBytes()),
				MemoryTotalBytes: readingFromProto(d.GetMemoryTotalBytes()),
			})
		}
	}
	return report.Observation()
}

func hostMetricsFromProto(in *pb.FleetHostMetrics) *orchestration.HostMetrics {
	if in == nil {
		return nil
	}
	sample := &orchestration.HostMetrics{
		CPUUtilization:     readingFromProto(in.GetCpuUtilization()),
		CPUCount:           readingFromProto(in.GetCpuCount()),
		Load1:              readingFromProto(in.GetLoad1()),
		Load5:              readingFromProto(in.GetLoad5()),
		Load15:             readingFromProto(in.GetLoad15()),
		MemTotalBytes:      readingFromProto(in.GetMemTotalBytes()),
		MemAvailableBytes:  readingFromProto(in.GetMemAvailableBytes()),
		DiskTotalBytes:     readingFromProto(in.GetDiskTotalBytes()),
		DiskAvailableBytes: readingFromProto(in.GetDiskAvailableBytes()),
	}
	// An unset collection time stays zero, which HostMetrics.Validate
	// refuses and the health verdict judges invalid.
	if in.GetCollectedAt() != nil {
		sample.CollectedAt = in.GetCollectedAt().AsTime()
	}
	return sample
}

func readingFromProto(in *pb.FleetReading) orchestration.Reading {
	if in == nil {
		return orchestration.Unread()
	}
	return orchestration.ParseReading(in.Value, in.GetProvenance())
}
