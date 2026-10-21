package unix

// The Unix transport serves quicbaas.ObjectStorageAdmin by delegating to the
// gRPC transport over the socket. These tests drive the
// objectstorageadmin client through a real Unix socket against a fake gRPC
// ObjectStorageAdmin server: every request field must reach the server,
// every response field must reach the caller, a server status must surface
// as a typed SDK error, and an unknown verb must be refused (#1551).

import (
	"context"
	"net"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	osadmin "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorageadmin"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

var osAdminUnixTime = time.Date(2026, 10, 21, 9, 15, 30, 987654321, time.UTC)

// osAdminUnixServer records the last request it received and answers each
// verb with a fully populated response, or with err when it is set.
type osAdminUnixServer struct {
	pb.UnimplementedObjectStorageAdminServer

	err error
	got proto.Message
}

func (f *osAdminUnixServer) record(req proto.Message) error {
	f.got = req
	return f.err
}

func (f *osAdminUnixServer) CreateBackend(_ context.Context, req *pb.CreateBackendRequest) (*pb.CreateBackendResponse, error) {
	if err := f.record(req); err != nil {
		return nil, err
	}
	return &pb.CreateBackendResponse{Name: req.GetName(), CreatedAt: timestamppb.New(osAdminUnixTime)}, nil
}

func (f *osAdminUnixServer) UpdateBackend(_ context.Context, req *pb.UpdateBackendRequest) (*pb.UpdateBackendResponse, error) {
	if err := f.record(req); err != nil {
		return nil, err
	}
	return &pb.UpdateBackendResponse{Name: req.GetName(), UpdatedAt: timestamppb.New(osAdminUnixTime)}, nil
}

func (f *osAdminUnixServer) DeleteBackend(_ context.Context, req *pb.DeleteBackendRequest) (*pb.DeleteBackendResponse, error) {
	if err := f.record(req); err != nil {
		return nil, err
	}
	return &pb.DeleteBackendResponse{Name: req.GetName(), Deleted: true, MigratedObjects: 17}, nil
}

func (f *osAdminUnixServer) SetBackendStatus(_ context.Context, req *pb.SetBackendStatusRequest) (*pb.SetBackendStatusResponse, error) {
	if err := f.record(req); err != nil {
		return nil, err
	}
	return &pb.SetBackendStatusResponse{
		Name: req.GetName(), Enabled: req.GetEnabled(), UpdatedAt: timestamppb.New(osAdminUnixTime),
	}, nil
}

func (f *osAdminUnixServer) GetStorageStats(_ context.Context, req *pb.GetStorageStatsRequest) (*pb.GetStorageStatsResponse, error) {
	if err := f.record(req); err != nil {
		return nil, err
	}
	return &pb.GetStorageStatsResponse{
		TotalBytes:      1 << 30,
		TotalObjects:    512,
		TotalBackends:   2,
		HealthyBackends: 1,
		BackendStats: []*pb.BackendInfo{{
			Name:            "archive",
			Type:            pb.BackendType_BACKEND_TYPE_AZURE,
			Enabled:         true,
			Healthy:         true,
			Tier:            pb.StorageTier_STORAGE_TIER_ARCHIVE,
			Region:          "westeurope",
			Endpoint:        "https://blob.example.test",
			Bucket:          "cold",
			CapacityBytes:   1 << 32,
			UsedBytes:       1 << 29,
			ObjectCount:     256,
			LastHealthCheck: timestamppb.New(osAdminUnixTime),
			Metadata:        map[string]string{"rack": "r7"},
		}},
		TenantStats: []*pb.ObjectStorageTenantUsage{{
			TenantId:     "globex",
			TotalBytes:   2000,
			ObjectCount:  20,
			HotBytes:     800,
			WarmBytes:    600,
			ColdBytes:    400,
			ArchiveBytes: 200,
			CalculatedAt: timestamppb.New(osAdminUnixTime),
		}},
		CalculatedAt: timestamppb.New(osAdminUnixTime),
	}, nil
}

func (f *osAdminUnixServer) RunGarbageCollection(_ context.Context, req *pb.RunGarbageCollectionRequest) (*pb.RunGarbageCollectionResponse, error) {
	if err := f.record(req); err != nil {
		return nil, err
	}
	return &pb.RunGarbageCollectionResponse{OperationId: "gc-9", StartedAt: timestamppb.New(osAdminUnixTime)}, nil
}

func (f *osAdminUnixServer) GetGarbageCollectionProgress(_ context.Context, req *pb.GetGarbageCollectionProgressRequest) (*pb.GetGarbageCollectionProgressResponse, error) {
	if err := f.record(req); err != nil {
		return nil, err
	}
	return &pb.GetGarbageCollectionProgressResponse{
		OperationId:     req.GetOperationId(),
		Status:          string(osadmin.GCStatusCompleted),
		ObjectsScanned:  300,
		ObjectsDeleted:  12,
		BytesReclaimed:  4096,
		PercentComplete: 100,
		Error:           "one object skipped",
		CompletedAt:     timestamppb.New(osAdminUnixTime),
	}, nil
}

func (f *osAdminUnixServer) RepairObject(_ context.Context, req *pb.RepairObjectRequest) (*pb.RepairObjectResponse, error) {
	if err := f.record(req); err != nil {
		return nil, err
	}
	return &pb.RepairObjectResponse{
		Key: req.GetKey(), Success: true, RepairedLocations: []string{"node-4"}, Error: "slow source",
	}, nil
}

func (f *osAdminUnixServer) CompactBackend(_ context.Context, req *pb.CompactBackendRequest) (*pb.CompactBackendResponse, error) {
	if err := f.record(req); err != nil {
		return nil, err
	}
	return &pb.CompactBackendResponse{
		Name: req.GetName(), OperationId: "compact-9", StartedAt: timestamppb.New(osAdminUnixTime),
	}, nil
}

// newOSAdminUnixTransport serves fake on a fresh Unix socket and returns a
// Unix transport dialled to it.
func newOSAdminUnixTransport(t *testing.T, fake *osAdminUnixServer) *Transport {
	t.Helper()
	serverTLS, clientTLS := unixTestTLS(t)
	socket := filepath.Join(t.TempDir(), "quicbaas.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	pb.RegisterObjectStorageAdminServer(server, fake)
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()

	tr, err := New(&transport.Config{Address: socket, Timeout: 5 * time.Second, TLSConfig: clientTLS})
	if err != nil {
		server.Stop()
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := tr.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
		server.Stop()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	return tr
}

type osAdminUnixCase struct {
	verb     string
	call     func(context.Context, osadmin.Client) (interface{}, error)
	wantReq  proto.Message
	wantResp interface{}
}

func osAdminUnixCases() []osAdminUnixCase {
	nanos := osAdminUnixTime.UnixNano()
	return []osAdminUnixCase{
		{
			verb: "CreateBackend",
			call: func(ctx context.Context, c osadmin.Client) (interface{}, error) {
				return c.CreateBackend(ctx, &osadmin.CreateBackendRequest{
					Name: "archive", Type: osadmin.BackendTypeAzure, Tier: osadmin.StorageTierArchive,
					Config: map[string]string{"container": "cold"}, Enabled: true,
				})
			},
			wantReq: &pb.CreateBackendRequest{
				Name: "archive", Type: pb.BackendType_BACKEND_TYPE_AZURE, Tier: pb.StorageTier_STORAGE_TIER_ARCHIVE,
				Config: map[string]string{"container": "cold"}, Enabled: true,
			},
			wantResp: &osadmin.CreateBackendResponse{Name: "archive", CreatedAt: nanos},
		},
		{
			verb: "UpdateBackend",
			call: func(ctx context.Context, c osadmin.Client) (interface{}, error) {
				return c.UpdateBackend(ctx, &osadmin.UpdateBackendRequest{
					Name: "archive", Config: map[string]string{"container": "colder"}, Tier: osadmin.StorageTierCold,
				})
			},
			wantReq: &pb.UpdateBackendRequest{
				Name: "archive", Config: map[string]string{"container": "colder"}, Tier: pb.StorageTier_STORAGE_TIER_COLD,
			},
			wantResp: &osadmin.UpdateBackendResponse{Name: "archive", UpdatedAt: nanos},
		},
		{
			verb: "DeleteBackend",
			call: func(ctx context.Context, c osadmin.Client) (interface{}, error) {
				return c.DeleteBackend(ctx, &osadmin.DeleteBackendRequest{Name: "archive", MigrateTo: "primary", Force: true})
			},
			wantReq:  &pb.DeleteBackendRequest{Name: "archive", MigrateTo: "primary", Force: true},
			wantResp: &osadmin.DeleteBackendResponse{Name: "archive", Deleted: true, MigratedObjects: 17},
		},
		{
			verb: "SetBackendStatus",
			call: func(ctx context.Context, c osadmin.Client) (interface{}, error) {
				return c.SetBackendStatus(ctx, &osadmin.SetBackendStatusRequest{Name: "archive", Enabled: true})
			},
			wantReq:  &pb.SetBackendStatusRequest{Name: "archive", Enabled: true},
			wantResp: &osadmin.SetBackendStatusResponse{Name: "archive", Enabled: true, UpdatedAt: nanos},
		},
		{
			verb: "GetStorageStats",
			call: func(ctx context.Context, c osadmin.Client) (interface{}, error) {
				return c.GetStorageStats(ctx, &osadmin.GetStorageStatsRequest{IncludeBackends: true, IncludeTenants: true})
			},
			wantReq: &pb.GetStorageStatsRequest{IncludeBackends: true, IncludeTenants: true},
			wantResp: &osadmin.GetStorageStatsResponse{
				TotalBytes:      1 << 30,
				TotalObjects:    512,
				TotalBackends:   2,
				HealthyBackends: 1,
				BackendStats: []*osadmin.BackendInfo{{
					Name:            "archive",
					Type:            osadmin.BackendTypeAzure,
					Enabled:         true,
					Healthy:         true,
					Tier:            osadmin.StorageTierArchive,
					Region:          "westeurope",
					Endpoint:        "https://blob.example.test",
					Bucket:          "cold",
					CapacityBytes:   1 << 32,
					UsedBytes:       1 << 29,
					ObjectCount:     256,
					LastHealthCheck: nanos,
					Metadata:        map[string]string{"rack": "r7"},
				}},
				TenantStats: []*osadmin.TenantUsage{{
					TenantID:     "globex",
					TotalBytes:   2000,
					ObjectCount:  20,
					HotBytes:     800,
					WarmBytes:    600,
					ColdBytes:    400,
					ArchiveBytes: 200,
					CalculatedAt: nanos,
				}},
				CalculatedAt: nanos,
			},
		},
		{
			verb: "RunGarbageCollection",
			call: func(ctx context.Context, c osadmin.Client) (interface{}, error) {
				return c.RunGarbageCollection(ctx, &osadmin.RunGarbageCollectionRequest{
					Backends: []string{"archive"}, DryRun: true, Aggressive: true,
				})
			},
			wantReq:  &pb.RunGarbageCollectionRequest{Backends: []string{"archive"}, DryRun: true, Aggressive: true},
			wantResp: &osadmin.RunGarbageCollectionResponse{OperationID: "gc-9", StartedAt: nanos},
		},
		{
			verb: "GetGarbageCollectionProgress",
			call: func(ctx context.Context, c osadmin.Client) (interface{}, error) {
				return c.GetGarbageCollectionProgress(ctx, &osadmin.GetGarbageCollectionProgressRequest{OperationID: "gc-9"})
			},
			wantReq: &pb.GetGarbageCollectionProgressRequest{OperationId: "gc-9"},
			wantResp: &osadmin.GetGarbageCollectionProgressResponse{
				OperationID:     "gc-9",
				Status:          osadmin.GCStatusCompleted,
				ObjectsScanned:  300,
				ObjectsDeleted:  12,
				BytesReclaimed:  4096,
				PercentComplete: 100,
				Error:           "one object skipped",
				CompletedAt:     nanos,
			},
		},
		{
			verb: "RepairObject",
			call: func(ctx context.Context, c osadmin.Client) (interface{}, error) {
				return c.RepairObject(ctx, &osadmin.RepairObjectRequest{
					Key: "tenants/globex/blob", SourceLocation: "node-1", TimeoutMs: 9000,
				})
			},
			wantReq: &pb.RepairObjectRequest{Key: "tenants/globex/blob", SourceLocation: "node-1", TimeoutMs: 9000},
			wantResp: &osadmin.RepairObjectResponse{
				Key: "tenants/globex/blob", Success: true, RepairedLocations: []string{"node-4"}, Error: "slow source",
			},
		},
		{
			verb: "CompactBackend",
			call: func(ctx context.Context, c osadmin.Client) (interface{}, error) {
				return c.CompactBackend(ctx, &osadmin.CompactBackendRequest{Name: "archive", Aggressive: true})
			},
			wantReq:  &pb.CompactBackendRequest{Name: "archive", Aggressive: true},
			wantResp: &osadmin.CompactBackendResponse{Name: "archive", OperationID: "compact-9", StartedAt: nanos},
		},
	}
}

func TestTransport_ObjectStorageAdminOverUnixSocket(t *testing.T) {
	fake := &osAdminUnixServer{}
	client := osadmin.NewClient(newOSAdminUnixTransport(t, fake))
	for _, tc := range osAdminUnixCases() {
		t.Run(tc.verb, func(t *testing.T) {
			got, err := tc.call(context.Background(), client)
			if err != nil {
				t.Fatalf("%s: %v", tc.verb, err)
			}
			if !proto.Equal(fake.got, tc.wantReq) {
				t.Errorf("server received %v, want %v", fake.got, tc.wantReq)
			}
			if !reflect.DeepEqual(got, tc.wantResp) {
				t.Errorf("reply = %#v, want %#v", got, tc.wantResp)
			}
		})
	}
}

func TestTransport_ObjectStorageAdminUnixServerErrors(t *testing.T) {
	failures := []struct {
		name  string
		code  codes.Code
		check func(error) bool
	}{
		{"NotFound", codes.NotFound, sdkerrors.IsNotFound},
		{"InvalidArgument", codes.InvalidArgument, sdkerrors.IsInvalidInput},
	}
	for _, failure := range failures {
		fake := &osAdminUnixServer{err: status.Error(failure.code, "refused")}
		client := osadmin.NewClient(newOSAdminUnixTransport(t, fake))
		for _, tc := range osAdminUnixCases() {
			t.Run(tc.verb+"/"+failure.name, func(t *testing.T) {
				_, err := tc.call(context.Background(), client)
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

func TestTransport_ObjectStorageAdminUnixRefusesUnknownVerb(t *testing.T) {
	tr := newOSAdminUnixTransport(t, &osAdminUnixServer{})
	_, err := tr.Request(context.Background(), "quicbaas.ObjectStorageAdmin", "Rebalance",
		&osadmin.CompactBackendRequest{Name: "archive"})
	if !sdkerrors.IsUnimplemented(err) {
		t.Fatalf("got %v, want a typed unimplemented error", err)
	}
}
