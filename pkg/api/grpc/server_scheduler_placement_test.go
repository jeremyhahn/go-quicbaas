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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement/placementtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// newPlacementServer builds a SchedulerServer with only its placement half,
// over a real placement engine on an in-memory store.
func newPlacementServer(t *testing.T) *SchedulerServer {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	return NewSchedulerServer(nil, service.NewPlacementService(placement.New(placementtest.New())), log)
}

func TestSchedulerServer_PlacementRoundTrip(t *testing.T) {
	// The tenant the transport's authentication binds for an authenticated caller.
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	s := newPlacementServer(t)

	reg, err := s.RegisterNode(ctx, &pb.RegisterPlacementNodeRequest{
		Facts: &pb.PlacementNodeFacts{
			NodeId:   "n1",
			Capacity: map[string]int64{"cpu": 8},
			Labels:   map[string]string{"zone": "a"},
		},
		TtlSeconds: 30,
	})
	require.NoError(t, err)
	assert.Equal(t, "n1", reg.Node.Facts.NodeId)
	assert.Equal(t, int64(30), reg.Node.TtlSeconds)
	assert.NotZero(t, reg.Node.RegisteredAtUnixNanos)

	renew, err := s.RenewNode(ctx, &pb.PlacementNodeRequest{NodeId: "n1"})
	require.NoError(t, err)
	assert.True(t, renew.Renewed)

	placed, err := s.Place(ctx, &pb.PlaceWorkloadRequest{
		WorkloadId:   "w1",
		Group:        "web",
		Resources:    map[string]int64{"cpu": 3},
		Constraints:  []*pb.PlacementConstraint{{Key: "zone", Operator: "In", Values: []string{"a"}}},
		Affinity:     []*pb.PlacementAffinityTerm{{Group: "cache", Weight: 10}},
		AntiAffinity: []*pb.PlacementAffinityTerm{{Group: "web", Required: true}},
		Spread:       &pb.PlacementSpreadConstraint{TopologyKey: "zone", MaxSkew: 1},
		Strategy:     "spread",
	})
	require.NoError(t, err)
	assert.Equal(t, "n1", placed.Placement.NodeId)
	assert.False(t, placed.Existing)

	// What the server recorded, read back through the server.
	node, err := s.GetNode(ctx, &pb.PlacementNodeRequest{NodeId: "n1"})
	require.NoError(t, err)
	assert.Equal(t, int64(3), node.Node.Allocated["cpu"])
	assert.Equal(t, int64(1), node.Node.Placements)

	got, err := s.GetPlacement(ctx, &pb.GetPlacementRequest{WorkloadId: "w1"})
	require.NoError(t, err)
	assert.Equal(t, "web", got.Placement.Group)
	assert.Equal(t, map[string]int64{"cpu": 3}, got.Placement.Resources)

	list, err := s.ListPlacements(ctx, &pb.ListPlacementsRequest{Group: "web"})
	require.NoError(t, err)
	require.Len(t, list.Placements, 1)

	nodes, err := s.ListNodes(ctx, &pb.ListPlacementNodesRequest{})
	require.NoError(t, err)
	require.Len(t, nodes.Nodes, 1)

	cordoned, err := s.CordonNode(ctx, &pb.PlacementNodeRequest{NodeId: "n1"})
	require.NoError(t, err)
	assert.True(t, cordoned.Node.Status.Unschedulable)

	uncordoned, err := s.UncordonNode(ctx, &pb.PlacementNodeRequest{NodeId: "n1"})
	require.NoError(t, err)
	assert.False(t, uncordoned.Node.Status.Unschedulable)

	drained, err := s.DrainNode(ctx, &pb.PlacementNodeRequest{NodeId: "n1"})
	require.NoError(t, err)
	require.Len(t, drained.Placements, 1)
	assert.Equal(t, "w1", drained.Placements[0].WorkloadId)

	released, err := s.Release(ctx, &pb.ReleaseWorkloadRequest{WorkloadId: "w1"})
	require.NoError(t, err)
	assert.True(t, released.Released)

	dereg, err := s.DeregisterNode(ctx, &pb.PlacementNodeRequest{NodeId: "n1"})
	require.NoError(t, err)
	assert.True(t, dereg.Deregistered)
}

func TestSchedulerServer_PlacementErrorsMapToStatusCodes(t *testing.T) {
	// The tenant the transport's authentication binds for an authenticated caller.
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	s := newPlacementServer(t)

	_, err := s.GetNode(ctx, &pb.PlacementNodeRequest{NodeId: "missing"})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = s.Place(ctx, &pb.PlaceWorkloadRequest{WorkloadId: "w"})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err), "no nodes registered")

	_, err = s.Place(ctx, &pb.PlaceWorkloadRequest{})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = s.Place(ctx, nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = s.RegisterNode(ctx, nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = s.Release(ctx, &pb.ReleaseWorkloadRequest{WorkloadId: "missing"})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = s.RenewNode(ctx, &pb.PlacementNodeRequest{NodeId: "missing"})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = s.DeregisterNode(ctx, &pb.PlacementNodeRequest{NodeId: "missing"})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = s.DrainNode(ctx, &pb.PlacementNodeRequest{NodeId: "missing"})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = s.GetPlacement(ctx, &pb.GetPlacementRequest{WorkloadId: "missing"})
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestSchedulerServer_AbsentHalvesAnswerUnavailable(t *testing.T) {
	// The tenant the transport's authentication binds for an authenticated caller.
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	s := NewSchedulerServer(nil, nil, log)

	calls := map[string]func() error{
		"RegisterNode":   func() error { _, err := s.RegisterNode(ctx, &pb.RegisterPlacementNodeRequest{}); return err },
		"RenewNode":      func() error { _, err := s.RenewNode(ctx, &pb.PlacementNodeRequest{}); return err },
		"DeregisterNode": func() error { _, err := s.DeregisterNode(ctx, &pb.PlacementNodeRequest{}); return err },
		"GetNode":        func() error { _, err := s.GetNode(ctx, &pb.PlacementNodeRequest{}); return err },
		"ListNodes":      func() error { _, err := s.ListNodes(ctx, &pb.ListPlacementNodesRequest{}); return err },
		"DrainNode":      func() error { _, err := s.DrainNode(ctx, &pb.PlacementNodeRequest{}); return err },
		"Place":          func() error { _, err := s.Place(ctx, &pb.PlaceWorkloadRequest{}); return err },
		"Release":        func() error { _, err := s.Release(ctx, &pb.ReleaseWorkloadRequest{}); return err },
		"GetPlacement":   func() error { _, err := s.GetPlacement(ctx, &pb.GetPlacementRequest{}); return err },
		"ListPlacements": func() error { _, err := s.ListPlacements(ctx, &pb.ListPlacementsRequest{}); return err },
		"GetJob":         func() error { _, err := s.GetJob(ctx, &pb.GetJobRequest{JobId: 1}); return err },
		"GetStatus":      func() error { _, err := s.GetSchedulerStatus(ctx, &pb.GetSchedulerStatusRequest{}); return err },
	}
	for name, call := range calls {
		assert.Equalf(t, codes.Unavailable, status.Code(call()), "%s", name)
	}
}

func TestPlacementConversions_NilSafe(t *testing.T) {
	assert.Nil(t, PlacementNodeInfoToProto(nil))
	assert.Nil(t, PlacementRecordToProto(nil))
	assert.Equal(t, placement.NodeFacts{}, PlacementFactsFromProto(nil))
	assert.Zero(t, unixNanos(placement.Placement{}.CreatedAt))
	assert.Nil(t, affinityTermsFromProto(nil))
}
