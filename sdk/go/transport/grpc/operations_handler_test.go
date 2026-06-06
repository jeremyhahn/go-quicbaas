// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"strconv"
	"testing"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/operations"
)

// fakeOperationsServer implements the subset of the QuicBaaS proto service that
// the operations handler exercises. It captures the last request it received so
// tests can assert the SDK->proto conversion, and echoes deterministic values
// back so tests can assert the proto->SDK conversion. The fail flag injects an
// Internal error to drive the error path.
type fakeOperationsServer struct {
	pb.UnimplementedQuicBaaSServer
	fail bool

	lastUpgrade  *pb.UpgradeNodeRequest
	lastStatus   *pb.GetUpgradeStatusRequest
	lastMigrate  *pb.MigrateDataRequest
	lastProgress *pb.GetMigrationProgressRequest
	lastReplace  *pb.ReplaceNodeRequest
}

func (f *fakeOperationsServer) e() error { return failErr(f.fail) }

func (f *fakeOperationsServer) UpgradeNode(_ context.Context, req *pb.UpgradeNodeRequest) (*pb.UpgradeNodeResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	f.lastUpgrade = req
	return &pb.UpgradeNodeResponse{OperationId: "upgrade-op-1", Started: 1234}, nil
}

func (f *fakeOperationsServer) GetUpgradeStatus(_ context.Context, req *pb.GetUpgradeStatusRequest) (*pb.GetUpgradeStatusResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	f.lastStatus = req
	return &pb.GetUpgradeStatusResponse{
		OperationId:     req.OperationId,
		PercentComplete: 42.5,
		CurrentVersion:  "v2.0.0",
		TargetVersion:   "v2.1.0",
		Status:          "in_progress",
	}, nil
}

func (f *fakeOperationsServer) MigrateData(_ context.Context, req *pb.MigrateDataRequest) (*pb.MigrateDataResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	f.lastMigrate = req
	return &pb.MigrateDataResponse{OperationId: "migrate-op-1", Started: 5678}, nil
}

func (f *fakeOperationsServer) GetMigrationProgress(_ context.Context, req *pb.GetMigrationProgressRequest) (*pb.GetMigrationProgressResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	f.lastProgress = req
	return &pb.GetMigrationProgressResponse{
		OperationId:        req.OperationId,
		KeysProcessed:      10,
		EstimatedTotal:     100,
		PercentComplete:    10,
		EstimatedRemaining: 90,
		Status:             "running",
	}, nil
}

func (f *fakeOperationsServer) ReplaceNode(_ context.Context, req *pb.ReplaceNodeRequest) (*pb.ReplaceNodeResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	f.lastReplace = req
	return &pb.ReplaceNodeResponse{Success: true, Message: "replace-job-1"}, nil
}

func newOperationsServer(t *testing.T, fail bool) (*operationsHandler, *fakeOperationsServer) {
	t.Helper()
	fake := &fakeOperationsServer{fail: fail}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterQuicBaaSServer(s, fake)
	})
	return newOperationsHandler(srv.dial(t)), fake
}

func TestIsOperationsService(t *testing.T) {
	tests := []struct {
		service  string
		expected bool
	}{
		{"quicbaas.Operations", true},
		{"quicbaas.Operations.Upgrade", true},
		{"quicbaas.Certificate", false},
		{"quicbaas.Cluster", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isOperationsService(tt.service); got != tt.expected {
			t.Errorf("isOperationsService(%q) = %v, want %v", tt.service, got, tt.expected)
		}
	}
}

// TestOperationsHandler_UpgradeNode_AllNodes verifies the canonical
// "upgrade all nodes" path: NodeID 0 must be forwarded verbatim as proto
// node_id == 0, never rejected, and the proto response converts back to the
// SDK response without a marshal error.
func TestOperationsHandler_UpgradeNode_AllNodes(t *testing.T) {
	h, fake := newOperationsServer(t, false)

	req := &operations.UpgradeNodeRequest{NodeID: 0, TargetVersion: "v2.1.0"}
	out, err := h.handleRequest(context.Background(), "quicbaas.Operations", "UpgradeNode", req)
	if err != nil {
		t.Fatalf("UpgradeNode returned error: %v", err)
	}

	if fake.lastUpgrade == nil {
		t.Fatal("server did not receive UpgradeNode request")
	}
	if fake.lastUpgrade.NodeId != 0 {
		t.Errorf("NodeId = %d, want 0 (all nodes)", fake.lastUpgrade.NodeId)
	}
	if fake.lastUpgrade.TargetVersion != "v2.1.0" {
		t.Errorf("TargetVersion = %q, want v2.1.0", fake.lastUpgrade.TargetVersion)
	}

	resp, ok := out.(*operations.UpgradeNodeResponse)
	if !ok {
		t.Fatalf("response type = %T, want *operations.UpgradeNodeResponse", out)
	}
	if resp.JobID != "upgrade-op-1" {
		t.Errorf("JobID = %q, want upgrade-op-1", resp.JobID)
	}
}

func TestOperationsHandler_UpgradeNode_SpecificNode(t *testing.T) {
	h, fake := newOperationsServer(t, false)

	req := &operations.UpgradeNodeRequest{NodeID: 7, TargetVersion: "v3.0.0"}
	if _, err := h.handleRequest(context.Background(), "quicbaas.Operations", "UpgradeNode", req); err != nil {
		t.Fatalf("UpgradeNode returned error: %v", err)
	}
	if fake.lastUpgrade.NodeId != 7 {
		t.Errorf("NodeId = %d, want 7", fake.lastUpgrade.NodeId)
	}
}

func TestOperationsHandler_GetUpgradeStatus(t *testing.T) {
	h, fake := newOperationsServer(t, false)

	req := &operations.GetUpgradeStatusRequest{JobID: "upgrade-op-1"}
	out, err := h.handleRequest(context.Background(), "quicbaas.Operations", "GetUpgradeStatus", req)
	if err != nil {
		t.Fatalf("GetUpgradeStatus returned error: %v", err)
	}
	if fake.lastStatus.OperationId != "upgrade-op-1" {
		t.Errorf("OperationId = %q, want upgrade-op-1", fake.lastStatus.OperationId)
	}

	resp, ok := out.(*operations.GetUpgradeStatusResponse)
	if !ok {
		t.Fatalf("response type = %T, want *operations.GetUpgradeStatusResponse", out)
	}
	if resp.JobID != "upgrade-op-1" {
		t.Errorf("JobID = %q, want upgrade-op-1", resp.JobID)
	}
	if resp.Progress != 42.5 {
		t.Errorf("Progress = %v, want 42.5", resp.Progress)
	}
	if resp.Status != "in_progress" {
		t.Errorf("Status = %q, want in_progress", resp.Status)
	}
	if resp.CurrentVersion != "v2.0.0" || resp.TargetVersion != "v2.1.0" {
		t.Errorf("versions = %q/%q, want v2.0.0/v2.1.0", resp.CurrentVersion, resp.TargetVersion)
	}
}

func TestOperationsHandler_MigrateData(t *testing.T) {
	h, fake := newOperationsServer(t, false)

	req := &operations.MigrateDataRequest{SourceShard: 3, TargetShard: 4, Concurrency: 8}
	out, err := h.handleRequest(context.Background(), "quicbaas.Operations", "MigrateData", req)
	if err != nil {
		t.Fatalf("MigrateData returned error: %v", err)
	}
	if fake.lastMigrate.SourceBackend != strconv.FormatUint(3, 10) {
		t.Errorf("SourceBackend = %q, want 3", fake.lastMigrate.SourceBackend)
	}
	if fake.lastMigrate.DestinationBackend != strconv.FormatUint(4, 10) {
		t.Errorf("DestinationBackend = %q, want 4", fake.lastMigrate.DestinationBackend)
	}
	if fake.lastMigrate.Concurrency != 8 {
		t.Errorf("Concurrency = %d, want 8", fake.lastMigrate.Concurrency)
	}

	resp, ok := out.(*operations.MigrateDataResponse)
	if !ok {
		t.Fatalf("response type = %T, want *operations.MigrateDataResponse", out)
	}
	if resp.MigrationID != "migrate-op-1" {
		t.Errorf("MigrationID = %q, want migrate-op-1", resp.MigrationID)
	}
	if resp.EstimatedDuration != 5678 {
		t.Errorf("EstimatedDuration = %d, want 5678", resp.EstimatedDuration)
	}
}

func TestOperationsHandler_GetMigrationProgress(t *testing.T) {
	h, fake := newOperationsServer(t, false)

	req := &operations.GetMigrationProgressRequest{MigrationID: "migrate-op-1"}
	out, err := h.handleRequest(context.Background(), "quicbaas.Operations", "GetMigrationProgress", req)
	if err != nil {
		t.Fatalf("GetMigrationProgress returned error: %v", err)
	}
	if fake.lastProgress.OperationId != "migrate-op-1" {
		t.Errorf("OperationId = %q, want migrate-op-1", fake.lastProgress.OperationId)
	}

	resp, ok := out.(*operations.GetMigrationProgressResponse)
	if !ok {
		t.Fatalf("response type = %T, want *operations.GetMigrationProgressResponse", out)
	}
	if resp.MigrationID != "migrate-op-1" {
		t.Errorf("MigrationID = %q, want migrate-op-1", resp.MigrationID)
	}
	if resp.ItemsMigrated != 10 || resp.TotalItems != 100 {
		t.Errorf("items = %d/%d, want 10/100", resp.ItemsMigrated, resp.TotalItems)
	}
	if resp.ETA != 90 {
		t.Errorf("ETA = %d, want 90", resp.ETA)
	}
}

func TestOperationsHandler_ReplaceNode(t *testing.T) {
	h, fake := newOperationsServer(t, false)

	req := &operations.ReplaceNodeRequest{
		ShardID:    1,
		OldNodeID:  2,
		NewNodeID:  3,
		NewAddress: "10.0.0.5:7000",
	}
	out, err := h.handleRequest(context.Background(), "quicbaas.Operations", "ReplaceNode", req)
	if err != nil {
		t.Fatalf("ReplaceNode returned error: %v", err)
	}
	if fake.lastReplace.FailedNodeId != 2 {
		t.Errorf("FailedNodeId = %d, want 2", fake.lastReplace.FailedNodeId)
	}
	if fake.lastReplace.ReplacementNodeId != 3 {
		t.Errorf("ReplacementNodeId = %d, want 3", fake.lastReplace.ReplacementNodeId)
	}
	if fake.lastReplace.ReplacementAddress != "10.0.0.5:7000" {
		t.Errorf("ReplacementAddress = %q, want 10.0.0.5:7000", fake.lastReplace.ReplacementAddress)
	}

	resp, ok := out.(*operations.ReplaceNodeResponse)
	if !ok {
		t.Fatalf("response type = %T, want *operations.ReplaceNodeResponse", out)
	}
	if resp.JobID != "replace-job-1" {
		t.Errorf("JobID = %q, want replace-job-1", resp.JobID)
	}
}

func TestOperationsHandler_UnsupportedMethod(t *testing.T) {
	h := newOperationsHandler(nil)
	if _, err := h.handleRequest(context.Background(), "quicbaas.Operations", "Bogus", nil); err == nil {
		t.Fatal("expected error for method not exposed by QuicBaaS operations proto")
	}
}

func TestOperationsHandler_InvalidRequestTypes(t *testing.T) {
	h := newOperationsHandler(nil)
	cases := []struct {
		name   string
		method string
	}{
		{"UpgradeNode", "UpgradeNode"},
		{"GetUpgradeStatus", "GetUpgradeStatus"},
		{"MigrateData", "MigrateData"},
		{"GetMigrationProgress", "GetMigrationProgress"},
		{"ReplaceNode", "ReplaceNode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.handleRequest(context.Background(), "quicbaas.Operations", tc.method, "wrong-type"); err == nil {
				t.Errorf("%s: expected error for invalid request type", tc.name)
			}
		})
	}
}

func TestOperationsHandler_ServerError(t *testing.T) {
	h, _ := newOperationsServer(t, true)
	cases := []struct {
		method string
		req    interface{}
	}{
		{"UpgradeNode", &operations.UpgradeNodeRequest{NodeID: 0, TargetVersion: "v2.1.0"}},
		{"GetUpgradeStatus", &operations.GetUpgradeStatusRequest{JobID: "x"}},
		{"MigrateData", &operations.MigrateDataRequest{SourceShard: 1, TargetShard: 2}},
		{"GetMigrationProgress", &operations.GetMigrationProgressRequest{MigrationID: "x"}},
		{"ReplaceNode", &operations.ReplaceNodeRequest{OldNodeID: 1, NewNodeID: 2, NewAddress: "a"}},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			if _, err := h.handleRequest(context.Background(), "quicbaas.Operations", tc.method, tc.req); err == nil {
				t.Errorf("%s: expected error from failing server", tc.method)
			}
		})
	}
}
