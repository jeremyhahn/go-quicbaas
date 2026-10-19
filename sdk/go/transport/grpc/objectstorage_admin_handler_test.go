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

// Tests for the ObjectStorageAdmin gRPC handler. Each verb is driven through
// the real objectstorageadmin SDK client over this transport, against an
// in-process gRPC server on bufconn that registers a fake
// ObjectStorageAdminServer. The fake records every request it receives so the
// tests can prove no request field is dropped on the way in, and returns a
// fully populated response so they can prove none is dropped on the way out.

package grpc

import (
	"context"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	osadmin "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorageadmin"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// osAdminTime is the instant every fake response timestamp carries.
var osAdminTime = time.Date(2026, 10, 18, 12, 30, 45, 123456789, time.UTC)

// fakeObjectStorageAdminServer records the last request of each verb and
// answers with fixed, fully populated responses. When err is set every verb
// fails with it.
type fakeObjectStorageAdminServer struct {
	pb.UnimplementedObjectStorageAdminServer

	err error

	createReq   *pb.CreateBackendRequest
	updateReq   *pb.UpdateBackendRequest
	deleteReq   *pb.DeleteBackendRequest
	statusReq   *pb.SetBackendStatusRequest
	statsReq    *pb.GetStorageStatsRequest
	gcReq       *pb.RunGarbageCollectionRequest
	progressReq *pb.GetGarbageCollectionProgressRequest
	repairReq   *pb.RepairObjectRequest
	compactReq  *pb.CompactBackendRequest
}

func (f *fakeObjectStorageAdminServer) CreateBackend(_ context.Context, req *pb.CreateBackendRequest) (*pb.CreateBackendResponse, error) {
	f.createReq = req
	if f.err != nil {
		return nil, f.err
	}
	return &pb.CreateBackendResponse{Name: req.GetName(), CreatedAt: timestamppb.New(osAdminTime)}, nil
}

func (f *fakeObjectStorageAdminServer) UpdateBackend(_ context.Context, req *pb.UpdateBackendRequest) (*pb.UpdateBackendResponse, error) {
	f.updateReq = req
	if f.err != nil {
		return nil, f.err
	}
	return &pb.UpdateBackendResponse{Name: req.GetName(), UpdatedAt: timestamppb.New(osAdminTime)}, nil
}

func (f *fakeObjectStorageAdminServer) DeleteBackend(_ context.Context, req *pb.DeleteBackendRequest) (*pb.DeleteBackendResponse, error) {
	f.deleteReq = req
	if f.err != nil {
		return nil, f.err
	}
	return &pb.DeleteBackendResponse{Name: req.GetName(), Deleted: true, MigratedObjects: 42}, nil
}

func (f *fakeObjectStorageAdminServer) SetBackendStatus(_ context.Context, req *pb.SetBackendStatusRequest) (*pb.SetBackendStatusResponse, error) {
	f.statusReq = req
	if f.err != nil {
		return nil, f.err
	}
	return &pb.SetBackendStatusResponse{
		Name:      req.GetName(),
		Enabled:   req.GetEnabled(),
		UpdatedAt: timestamppb.New(osAdminTime),
	}, nil
}

func (f *fakeObjectStorageAdminServer) GetStorageStats(_ context.Context, req *pb.GetStorageStatsRequest) (*pb.GetStorageStatsResponse, error) {
	f.statsReq = req
	if f.err != nil {
		return nil, f.err
	}
	return &pb.GetStorageStatsResponse{
		TotalBytes:      1 << 40,
		TotalObjects:    9000,
		TotalBackends:   3,
		HealthyBackends: 2,
		BackendStats: []*pb.BackendInfo{{
			Name:            "primary",
			Type:            pb.BackendType_BACKEND_TYPE_S3,
			Enabled:         true,
			Healthy:         true,
			Tier:            pb.StorageTier_STORAGE_TIER_COLD,
			Region:          "eu-west-1",
			Endpoint:        "https://s3.example.test",
			Bucket:          "objects",
			CapacityBytes:   1 << 41,
			UsedBytes:       1 << 39,
			ObjectCount:     4500,
			LastHealthCheck: timestamppb.New(osAdminTime),
			Metadata:        map[string]string{"zone": "b"},
		}},
		TenantStats: []*pb.ObjectStorageTenantUsage{{
			TenantId:     "acme",
			TotalBytes:   1000,
			ObjectCount:  10,
			HotBytes:     400,
			WarmBytes:    300,
			ColdBytes:    200,
			ArchiveBytes: 100,
			CalculatedAt: timestamppb.New(osAdminTime),
		}},
		CalculatedAt: timestamppb.New(osAdminTime),
	}, nil
}

func (f *fakeObjectStorageAdminServer) RunGarbageCollection(_ context.Context, req *pb.RunGarbageCollectionRequest) (*pb.RunGarbageCollectionResponse, error) {
	f.gcReq = req
	if f.err != nil {
		return nil, f.err
	}
	return &pb.RunGarbageCollectionResponse{OperationId: "gc-1", StartedAt: timestamppb.New(osAdminTime)}, nil
}

func (f *fakeObjectStorageAdminServer) GetGarbageCollectionProgress(_ context.Context, req *pb.GetGarbageCollectionProgressRequest) (*pb.GetGarbageCollectionProgressResponse, error) {
	f.progressReq = req
	if f.err != nil {
		return nil, f.err
	}
	return &pb.GetGarbageCollectionProgressResponse{
		OperationId:     req.GetOperationId(),
		Status:          string(osadmin.GCStatusFailed),
		ObjectsScanned:  100,
		ObjectsDeleted:  7,
		BytesReclaimed:  2048,
		PercentComplete: 62.5,
		Error:           "disk offline",
		CompletedAt:     timestamppb.New(osAdminTime),
	}, nil
}

func (f *fakeObjectStorageAdminServer) RepairObject(_ context.Context, req *pb.RepairObjectRequest) (*pb.RepairObjectResponse, error) {
	f.repairReq = req
	if f.err != nil {
		return nil, f.err
	}
	return &pb.RepairObjectResponse{
		Key:               req.GetKey(),
		Success:           true,
		RepairedLocations: []string{"node-2", "node-3"},
		Error:             "partial",
	}, nil
}

func (f *fakeObjectStorageAdminServer) CompactBackend(_ context.Context, req *pb.CompactBackendRequest) (*pb.CompactBackendResponse, error) {
	f.compactReq = req
	if f.err != nil {
		return nil, f.err
	}
	return &pb.CompactBackendResponse{
		Name:        req.GetName(),
		OperationId: "compact-1",
		StartedAt:   timestamppb.New(osAdminTime),
	}, nil
}

// newObjectStorageAdminClientFor returns the real SDK client wired to this
// transport, talking to fake over bufconn.
func newObjectStorageAdminClientFor(t *testing.T, fake *fakeObjectStorageAdminServer) osadmin.Client {
	t.Helper()
	hs := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterObjectStorageAdminServer(s, fake)
	})
	return osadmin.NewClient(&Transport{cfg: &transport.Config{Timeout: 5 * time.Second}, conn: hs.dial(t)})
}

// assertProtoEqual fails the test when the server did not receive want.
func assertProtoEqual(t *testing.T, verb string, got, want proto.Message) {
	t.Helper()
	if !proto.Equal(got, want) {
		t.Errorf("%s: server received %v, want %v", verb, got, want)
	}
}

// assertDeepEqual fails the test when the client did not read back want.
func assertDeepEqual(t *testing.T, verb string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: client read %+v, want %+v", verb, got, want)
	}
}

func TestIsObjectStorageAdminService(t *testing.T) {
	tests := []struct {
		svc  string
		want bool
	}{
		{"quicbaas.ObjectStorageAdmin", true},
		{"quicbaas.objectstorage.v1.ObjectStorageAdmin", true},
		{"quicbaas.ObjectStorage", false},
		{"quicbaas.Crypto", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isObjectStorageAdminService(tt.svc); got != tt.want {
			t.Errorf("isObjectStorageAdminService(%q) = %v, want %v", tt.svc, got, tt.want)
		}
	}
}

func TestObjectStorageAdminHandler_AllVerbs(t *testing.T) {
	ctx := context.Background()
	fake := &fakeObjectStorageAdminServer{}
	client := newObjectStorageAdminClientFor(t, fake)
	nanos := osAdminTime.UnixNano()

	t.Run("CreateBackend", func(t *testing.T) {
		resp, err := client.CreateBackend(ctx, &osadmin.CreateBackendRequest{
			Name:    "primary",
			Type:    osadmin.BackendTypeMinio,
			Tier:    osadmin.StorageTierWarm,
			Config:  map[string]string{"bucket": "objects", "region": "eu-west-1"},
			Enabled: true,
		})
		if err != nil {
			t.Fatalf("CreateBackend: %v", err)
		}
		assertProtoEqual(t, "CreateBackend", fake.createReq, &pb.CreateBackendRequest{
			Name:    "primary",
			Type:    pb.BackendType_BACKEND_TYPE_MINIO,
			Tier:    pb.StorageTier_STORAGE_TIER_WARM,
			Config:  map[string]string{"bucket": "objects", "region": "eu-west-1"},
			Enabled: true,
		})
		assertDeepEqual(t, "CreateBackend", resp, &osadmin.CreateBackendResponse{Name: "primary", CreatedAt: nanos})
	})

	t.Run("UpdateBackend", func(t *testing.T) {
		resp, err := client.UpdateBackend(ctx, &osadmin.UpdateBackendRequest{
			Name:   "primary",
			Config: map[string]string{"region": "us-east-2"},
			Tier:   osadmin.StorageTierArchive,
		})
		if err != nil {
			t.Fatalf("UpdateBackend: %v", err)
		}
		assertProtoEqual(t, "UpdateBackend", fake.updateReq, &pb.UpdateBackendRequest{
			Name:   "primary",
			Config: map[string]string{"region": "us-east-2"},
			Tier:   pb.StorageTier_STORAGE_TIER_ARCHIVE,
		})
		assertDeepEqual(t, "UpdateBackend", resp, &osadmin.UpdateBackendResponse{Name: "primary", UpdatedAt: nanos})
	})

	t.Run("DeleteBackend", func(t *testing.T) {
		resp, err := client.DeleteBackend(ctx, &osadmin.DeleteBackendRequest{
			Name:      "primary",
			MigrateTo: "secondary",
			Force:     true,
		})
		if err != nil {
			t.Fatalf("DeleteBackend: %v", err)
		}
		assertProtoEqual(t, "DeleteBackend", fake.deleteReq, &pb.DeleteBackendRequest{
			Name:      "primary",
			MigrateTo: "secondary",
			Force:     true,
		})
		assertDeepEqual(t, "DeleteBackend", resp, &osadmin.DeleteBackendResponse{
			Name:            "primary",
			Deleted:         true,
			MigratedObjects: 42,
		})
	})

	t.Run("SetBackendStatus", func(t *testing.T) {
		resp, err := client.SetBackendStatus(ctx, &osadmin.SetBackendStatusRequest{Name: "primary", Enabled: true})
		if err != nil {
			t.Fatalf("SetBackendStatus: %v", err)
		}
		assertProtoEqual(t, "SetBackendStatus", fake.statusReq, &pb.SetBackendStatusRequest{Name: "primary", Enabled: true})
		assertDeepEqual(t, "SetBackendStatus", resp, &osadmin.SetBackendStatusResponse{
			Name:      "primary",
			Enabled:   true,
			UpdatedAt: nanos,
		})
	})

	t.Run("GetStorageStats", func(t *testing.T) {
		resp, err := client.GetStorageStats(ctx, &osadmin.GetStorageStatsRequest{IncludeBackends: true, IncludeTenants: true})
		if err != nil {
			t.Fatalf("GetStorageStats: %v", err)
		}
		assertProtoEqual(t, "GetStorageStats", fake.statsReq, &pb.GetStorageStatsRequest{IncludeBackends: true, IncludeTenants: true})
		assertDeepEqual(t, "GetStorageStats", resp, &osadmin.GetStorageStatsResponse{
			TotalBytes:      1 << 40,
			TotalObjects:    9000,
			TotalBackends:   3,
			HealthyBackends: 2,
			BackendStats: []*osadmin.BackendInfo{{
				Name:            "primary",
				Type:            osadmin.BackendTypeS3,
				Enabled:         true,
				Healthy:         true,
				Tier:            osadmin.StorageTierCold,
				Region:          "eu-west-1",
				Endpoint:        "https://s3.example.test",
				Bucket:          "objects",
				CapacityBytes:   1 << 41,
				UsedBytes:       1 << 39,
				ObjectCount:     4500,
				LastHealthCheck: nanos,
				Metadata:        map[string]string{"zone": "b"},
			}},
			TenantStats: []*osadmin.TenantUsage{{
				TenantID:     "acme",
				TotalBytes:   1000,
				ObjectCount:  10,
				HotBytes:     400,
				WarmBytes:    300,
				ColdBytes:    200,
				ArchiveBytes: 100,
				CalculatedAt: nanos,
			}},
			CalculatedAt: nanos,
		})
	})

	t.Run("RunGarbageCollection", func(t *testing.T) {
		resp, err := client.RunGarbageCollection(ctx, &osadmin.RunGarbageCollectionRequest{
			Backends:   []string{"primary", "secondary"},
			DryRun:     true,
			Aggressive: true,
		})
		if err != nil {
			t.Fatalf("RunGarbageCollection: %v", err)
		}
		assertProtoEqual(t, "RunGarbageCollection", fake.gcReq, &pb.RunGarbageCollectionRequest{
			Backends:   []string{"primary", "secondary"},
			DryRun:     true,
			Aggressive: true,
		})
		assertDeepEqual(t, "RunGarbageCollection", resp, &osadmin.RunGarbageCollectionResponse{
			OperationID: "gc-1",
			StartedAt:   nanos,
		})
	})

	t.Run("GetGarbageCollectionProgress", func(t *testing.T) {
		resp, err := client.GetGarbageCollectionProgress(ctx, &osadmin.GetGarbageCollectionProgressRequest{OperationID: "gc-1"})
		if err != nil {
			t.Fatalf("GetGarbageCollectionProgress: %v", err)
		}
		assertProtoEqual(t, "GetGarbageCollectionProgress", fake.progressReq,
			&pb.GetGarbageCollectionProgressRequest{OperationId: "gc-1"})
		assertDeepEqual(t, "GetGarbageCollectionProgress", resp, &osadmin.GetGarbageCollectionProgressResponse{
			OperationID:     "gc-1",
			Status:          osadmin.GCStatusFailed,
			ObjectsScanned:  100,
			ObjectsDeleted:  7,
			BytesReclaimed:  2048,
			PercentComplete: 62.5,
			Error:           "disk offline",
			CompletedAt:     nanos,
		})
	})

	t.Run("RepairObject", func(t *testing.T) {
		resp, err := client.RepairObject(ctx, &osadmin.RepairObjectRequest{
			Key:            "tenants/acme/blob",
			SourceLocation: "node-1",
			TimeoutMs:      15000,
		})
		if err != nil {
			t.Fatalf("RepairObject: %v", err)
		}
		assertProtoEqual(t, "RepairObject", fake.repairReq, &pb.RepairObjectRequest{
			Key:            "tenants/acme/blob",
			SourceLocation: "node-1",
			TimeoutMs:      15000,
		})
		assertDeepEqual(t, "RepairObject", resp, &osadmin.RepairObjectResponse{
			Key:               "tenants/acme/blob",
			Success:           true,
			RepairedLocations: []string{"node-2", "node-3"},
			Error:             "partial",
		})
	})

	t.Run("CompactBackend", func(t *testing.T) {
		resp, err := client.CompactBackend(ctx, &osadmin.CompactBackendRequest{Name: "primary", Aggressive: true})
		if err != nil {
			t.Fatalf("CompactBackend: %v", err)
		}
		assertProtoEqual(t, "CompactBackend", fake.compactReq, &pb.CompactBackendRequest{Name: "primary", Aggressive: true})
		assertDeepEqual(t, "CompactBackend", resp, &osadmin.CompactBackendResponse{
			Name:        "primary",
			OperationID: "compact-1",
			StartedAt:   nanos,
		})
	})
}

func TestObjectStorageAdminHandler_ServerErrors(t *testing.T) {
	ctx := context.Background()

	// Every verb runs once against a NotFound server and once against an
	// InvalidArgument server; the caller must see the typed SDK error.
	verbs := map[string]func(osadmin.Client) error{
		"CreateBackend": func(c osadmin.Client) error {
			_, err := c.CreateBackend(ctx, &osadmin.CreateBackendRequest{
				Name: "primary", Type: osadmin.BackendTypeLocal, Tier: osadmin.StorageTierHot,
				Config: map[string]string{"path": "data"},
			})
			return err
		},
		"UpdateBackend": func(c osadmin.Client) error {
			_, err := c.UpdateBackend(ctx, &osadmin.UpdateBackendRequest{Name: "primary", Tier: osadmin.StorageTierCold})
			return err
		},
		"DeleteBackend": func(c osadmin.Client) error {
			_, err := c.DeleteBackend(ctx, &osadmin.DeleteBackendRequest{Name: "primary"})
			return err
		},
		"SetBackendStatus": func(c osadmin.Client) error {
			_, err := c.SetBackendStatus(ctx, &osadmin.SetBackendStatusRequest{Name: "primary"})
			return err
		},
		"GetStorageStats": func(c osadmin.Client) error {
			_, err := c.GetStorageStats(ctx, &osadmin.GetStorageStatsRequest{})
			return err
		},
		"RunGarbageCollection": func(c osadmin.Client) error {
			_, err := c.RunGarbageCollection(ctx, &osadmin.RunGarbageCollectionRequest{})
			return err
		},
		"GetGarbageCollectionProgress": func(c osadmin.Client) error {
			_, err := c.GetGarbageCollectionProgress(ctx, &osadmin.GetGarbageCollectionProgressRequest{OperationID: "gc-1"})
			return err
		},
		"RepairObject": func(c osadmin.Client) error {
			_, err := c.RepairObject(ctx, &osadmin.RepairObjectRequest{Key: "tenants/acme/blob"})
			return err
		},
		"CompactBackend": func(c osadmin.Client) error {
			_, err := c.CompactBackend(ctx, &osadmin.CompactBackendRequest{Name: "primary"})
			return err
		},
	}

	failures := []struct {
		name  string
		code  codes.Code
		check func(error) bool
	}{
		{"NotFound", codes.NotFound, sdkerrors.IsNotFound},
		{"InvalidArgument", codes.InvalidArgument, sdkerrors.IsInvalidInput},
	}

	for _, failure := range failures {
		client := newObjectStorageAdminClientFor(t, &fakeObjectStorageAdminServer{
			err: status.Error(failure.code, "refused"),
		})
		for verb, call := range verbs {
			t.Run(verb+"/"+failure.name, func(t *testing.T) {
				err := call(client)
				if err == nil {
					t.Fatal("expected an error")
				}
				if !failure.check(err) {
					t.Errorf("got %v, want a typed %s error", err, failure.name)
				}
			})
		}
	}
}

func TestObjectStorageAdminHandler_RequestErrors(t *testing.T) {
	ctx := context.Background()
	hs := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterObjectStorageAdminServer(s, &fakeObjectStorageAdminServer{})
	})
	h := newObjectStorageAdminHandler(hs.dial(t))

	t.Run("unknown method", func(t *testing.T) {
		_, err := h.handleRequest(ctx, objectStorageAdminService, "NotAVerb", nil)
		if !sdkerrors.IsUnimplemented(err) {
			t.Errorf("got %v, want an Unimplemented error", err)
		}
	})

	t.Run("wrong request type", func(t *testing.T) {
		for method := range objectStorageAdminMethods {
			_, err := h.handleRequest(ctx, objectStorageAdminService, method, "not-a-request")
			if !sdkerrors.IsInvalidInput(err) {
				t.Errorf("%s: got %v, want an InvalidInput error", method, err)
			}
		}
	})

	t.Run("nil request", func(t *testing.T) {
		_, err := h.handleRequest(ctx, objectStorageAdminService, "CreateBackend", (*osadmin.CreateBackendRequest)(nil))
		if !sdkerrors.IsInvalidInput(err) {
			t.Errorf("got %v, want an InvalidInput error", err)
		}
	})
}

func TestObjectStorageAdminEnumConversion(t *testing.T) {
	t.Run("backend types round trip", func(t *testing.T) {
		for v := range pb.BackendType_name {
			sdkType := osadmin.BackendType(v)
			got := osAdminBackendTypeFromProto(osAdminBackendTypeToProto(sdkType))
			if got != sdkType {
				t.Errorf("backend type %v round-tripped to %v", sdkType, got)
			}
		}
	})

	t.Run("storage tiers round trip", func(t *testing.T) {
		for v := range pb.StorageTier_name {
			sdkTier := osadmin.StorageTier(v)
			got := osAdminStorageTierFromProto(osAdminStorageTierToProto(sdkTier))
			if got != sdkTier {
				t.Errorf("storage tier %v round-tripped to %v", sdkTier, got)
			}
		}
	})

	t.Run("unknown values map to unspecified", func(t *testing.T) {
		if got := osAdminBackendTypeToProto(osadmin.BackendType(99)); got != pb.BackendType_BACKEND_TYPE_UNSPECIFIED {
			t.Errorf("backend type 99 = %v, want UNSPECIFIED", got)
		}
		if got := osAdminStorageTierFromProto(pb.StorageTier(99)); got != osadmin.StorageTierUnspecified {
			t.Errorf("storage tier 99 = %v, want unspecified", got)
		}
	})
}
