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
	"context"
	"testing"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/internal/fleetparity"
)

// heartbeatOverGRPC sends req through the SDK handler to the real server
// orchestrator service and returns what reached the agent service.
func heartbeatOverGRPC(t *testing.T, req *orchestrator.FleetNodeHeartbeatRequest) *fleetparity.Recorder {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	recorder := fleetparity.NewRecorder()
	// The server's authentication interceptor binds the caller's tenant;
	// this one binds the test's, so the heartbeat reaches the service.
	bindTenant := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(tenantctx.WithTenant(ctx, "tenant-1"), req)
	}
	server := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, grpcapi.NewOrchestratorServer(recorder.Agent(), log))
	}, grpc.UnaryInterceptor(bindTenant))
	resp, err := handleFleetHeartbeat(context.Background(), server.dial(t), req)
	if err != nil {
		t.Fatalf("FleetNodeHeartbeat: %v", err)
	}
	if ack, ok := resp.(*orchestrator.FleetNodeHeartbeatResponse); !ok || !ack.Heartbeat {
		t.Fatalf("FleetNodeHeartbeat answered %#v, want an acknowledgement", resp)
	}
	return recorder
}

func TestFleetHeartbeat_ObservationSurvivesGRPC(t *testing.T) {
	recorder := heartbeatOverGRPC(t, &orchestrator.FleetNodeHeartbeatRequest{
		TenantID:    "tenant-1",
		NodeID:      "node-1",
		Observation: fleetparity.Observation(),
	})
	fleetparity.RequireParity(t, recorder.Observed(t))
}

func TestFleetHeartbeat_BareHeartbeatOverGRPCObservesNothing(t *testing.T) {
	recorder := heartbeatOverGRPC(t, &orchestrator.FleetNodeHeartbeatRequest{TenantID: "tenant-1", NodeID: "node-1"})
	if observed := recorder.Observed(t); observed != nil {
		t.Fatalf("a bare heartbeat observed %#v, want nothing", observed)
	}
}

func TestFleetObservationToProto_PresenceFlags(t *testing.T) {
	if got := fleetObservationToProto(&orchestrator.FleetNodeObservation{MaxConcurrentJobs: 2}); got != nil {
		t.Fatalf("an observation without backends encoded to %v, want nil", got)
	}

	got := fleetObservationToProto(&orchestrator.FleetNodeObservation{Backends: []string{}})
	if got == nil {
		t.Fatal("an empty served list must still be sent: it is a report")
	}
	if got.GetSigningAgentsReported() || got.GetImagesReported() || got.GetDevicesReported() {
		t.Errorf("absent lists were flagged as reported: %v", got)
	}
	if got.GetHostMetrics() != nil {
		t.Errorf("absent host metrics were sent: %v", got.GetHostMetrics())
	}
}

func TestFleetHostMetricsToProto_ZeroCollectionTimeStaysUnset(t *testing.T) {
	got := fleetHostMetricsToProto(&orchestrator.FleetHostMetrics{})
	if got.GetCollectedAt() != nil {
		t.Fatalf("a zero collection time was sent as %v; the server must see it unset and refuse the sample", got.GetCollectedAt())
	}
}
