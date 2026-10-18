package embedded

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/operations"
)

// operationsClientOver builds the SDK operations client over an embedded
// transport serving svc.
func operationsClientOver(t *testing.T, svc *mockQuicBaaS) operations.Client {
	t.Helper()
	tr, err := New(svc)
	if err != nil {
		t.Fatal(err)
	}
	return operations.NewClient(tr)
}

// requireInvalidRequestType asserts that calling method with a request of the
// wrong type returns the typed InvalidRequestTypeError naming the client type.
func requireInvalidRequestType(t *testing.T, method, expected string) {
	t.Helper()
	tr, err := New(&mockQuicBaaS{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = tr.Request(context.Background(), "quicbaas.Operations", method, "invalid")
	var typeErr *InvalidRequestTypeError
	if !errors.As(err, &typeErr) {
		t.Fatalf("%s error = %v, want *InvalidRequestTypeError", method, err)
	}
	if typeErr.ExpectedType != expected || typeErr.ActualType != "string" {
		t.Fatalf("%s error = %+v, want expected %s and actual string", method, typeErr, expected)
	}
}

func TestMigrateData_SDKClientReachesTheService(t *testing.T) {
	var got *service.MigrateDataRequest
	client := operationsClientOver(t, &mockQuicBaaS{migrateDataFunc: func(_ context.Context, req *service.MigrateDataRequest) (*service.MigrateDataResponse, error) {
		got = req
		return &service.MigrateDataResponse{MigrationID: "mig-7", Status: "running", EstimatedDuration: 90}, nil
	}})

	resp, err := client.MigrateData(context.Background(), &operations.MigrateDataRequest{
		SourceShard: 3, TargetShard: 4, EntityTypes: []string{"users", "orders"}, Concurrency: 8,
	})
	if err != nil {
		t.Fatalf("MigrateData: %v", err)
	}
	want := &service.MigrateDataRequest{SourceShard: 3, TargetShard: 4, EntityTypes: []string{"users", "orders"}, Concurrency: 8}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("service received %+v, want %+v", got, want)
	}
	if *resp != (operations.MigrateDataResponse{MigrationID: "mig-7", Status: "running", EstimatedDuration: 90}) {
		t.Fatalf("response = %+v", resp)
	}
}

func TestMigrateData_WrongRequestTypeIsTyped(t *testing.T) {
	requireInvalidRequestType(t, "MigrateData", "*operations.MigrateDataRequest")
}

func TestGetMigrationProgress_SDKClientReachesTheService(t *testing.T) {
	var got *service.GetMigrationProgressRequest
	client := operationsClientOver(t, &mockQuicBaaS{migrationProgressFunc: func(_ context.Context, req *service.GetMigrationProgressRequest) (*service.GetMigrationProgressResponse, error) {
		got = req
		return &service.GetMigrationProgressResponse{
			MigrationID: "mig-7", Progress: 42.5, Status: "running", ItemsMigrated: 425, TotalItems: 1000, ETA: 1760000000,
		}, nil
	}})

	resp, err := client.GetMigrationProgress(context.Background(), &operations.GetMigrationProgressRequest{MigrationID: "mig-7"})
	if err != nil {
		t.Fatalf("GetMigrationProgress: %v", err)
	}
	if got == nil || got.MigrationID != "mig-7" {
		t.Fatalf("service received %+v, want migration mig-7", got)
	}
	want := operations.GetMigrationProgressResponse{
		MigrationID: "mig-7", Progress: 42.5, Status: "running", ItemsMigrated: 425, TotalItems: 1000, ETA: 1760000000,
	}
	if *resp != want {
		t.Fatalf("response = %+v, want %+v", resp, want)
	}
}

func TestGetMigrationProgress_WrongRequestTypeIsTyped(t *testing.T) {
	requireInvalidRequestType(t, "GetMigrationProgress", "*operations.GetMigrationProgressRequest")
}

func TestReplaceNode_SDKClientReachesTheService(t *testing.T) {
	var got *service.ReplaceNodeRequest
	client := operationsClientOver(t, &mockQuicBaaS{replaceNodeFunc: func(_ context.Context, req *service.ReplaceNodeRequest) (*service.ReplaceNodeResponse, error) {
		got = req
		return &service.ReplaceNodeResponse{JobID: "replace-9", Status: "pending"}, nil
	}})

	resp, err := client.ReplaceNode(context.Background(), &operations.ReplaceNodeRequest{
		ShardID: 2, OldNodeID: 5, NewNodeID: 6, NewAddress: "node-6.example.com:63001",
	})
	if err != nil {
		t.Fatalf("ReplaceNode: %v", err)
	}
	want := service.ReplaceNodeRequest{ShardID: 2, OldNodeID: 5, NewNodeID: 6, NewAddress: "node-6.example.com:63001"}
	if got == nil || *got != want {
		t.Fatalf("service received %+v, want %+v", got, want)
	}
	if *resp != (operations.ReplaceNodeResponse{JobID: "replace-9", Status: "pending"}) {
		t.Fatalf("response = %+v", resp)
	}
}

func TestReplaceNode_WrongRequestTypeIsTyped(t *testing.T) {
	requireInvalidRequestType(t, "ReplaceNode", "*operations.ReplaceNodeRequest")
}

func TestUpgradeNode_SDKClientReachesTheService(t *testing.T) {
	var got *service.UpgradeNodeRequest
	client := operationsClientOver(t, &mockQuicBaaS{upgradeNodeFunc: func(_ context.Context, req *service.UpgradeNodeRequest) (*service.UpgradeNodeResponse, error) {
		got = req
		return &service.UpgradeNodeResponse{JobID: "upgrade-3", Status: "started"}, nil
	}})

	// NodeID 0 is the "upgrade all nodes" sentinel and must reach the service.
	resp, err := client.UpgradeNode(context.Background(), &operations.UpgradeNodeRequest{NodeID: 0, TargetVersion: "v0.17.0"})
	if err != nil {
		t.Fatalf("UpgradeNode: %v", err)
	}
	want := service.UpgradeNodeRequest{NodeID: 0, TargetVersion: "v0.17.0"}
	if got == nil || *got != want {
		t.Fatalf("service received %+v, want %+v", got, want)
	}
	if *resp != (operations.UpgradeNodeResponse{JobID: "upgrade-3", Status: "started"}) {
		t.Fatalf("response = %+v", resp)
	}
}

func TestUpgradeNode_WrongRequestTypeIsTyped(t *testing.T) {
	requireInvalidRequestType(t, "UpgradeNode", "*operations.UpgradeNodeRequest")
}

func TestGetUpgradeStatus_SDKClientReachesTheService(t *testing.T) {
	var got *service.GetUpgradeStatusRequest
	client := operationsClientOver(t, &mockQuicBaaS{upgradeStatusFunc: func(_ context.Context, req *service.GetUpgradeStatusRequest) (*service.GetUpgradeStatusResponse, error) {
		got = req
		return &service.GetUpgradeStatusResponse{
			JobID: "upgrade-3", Progress: 75, Status: "running", CurrentVersion: "v0.16.0", TargetVersion: "v0.17.0",
		}, nil
	}})

	resp, err := client.GetUpgradeStatus(context.Background(), &operations.GetUpgradeStatusRequest{JobID: "upgrade-3"})
	if err != nil {
		t.Fatalf("GetUpgradeStatus: %v", err)
	}
	if got == nil || got.JobID != "upgrade-3" {
		t.Fatalf("service received %+v, want job upgrade-3", got)
	}
	want := operations.GetUpgradeStatusResponse{
		JobID: "upgrade-3", Progress: 75, Status: "running", CurrentVersion: "v0.16.0", TargetVersion: "v0.17.0",
	}
	if *resp != want {
		t.Fatalf("response = %+v, want %+v", resp, want)
	}
}

func TestGetUpgradeStatus_WrongRequestTypeIsTyped(t *testing.T) {
	requireInvalidRequestType(t, "GetUpgradeStatus", "*operations.GetUpgradeStatusRequest")
}

// TestOperations_BackendErrorReachesTheSDKClient checks that a service error
// is returned to the client unchanged rather than masked by the conversion.
func TestOperations_BackendErrorReachesTheSDKClient(t *testing.T) {
	backendErr := errors.New("raft host not started")
	client := operationsClientOver(t, &mockQuicBaaS{replaceNodeFunc: func(context.Context, *service.ReplaceNodeRequest) (*service.ReplaceNodeResponse, error) {
		return nil, backendErr
	}})

	_, err := client.ReplaceNode(context.Background(), &operations.ReplaceNodeRequest{
		ShardID: 2, OldNodeID: 5, NewNodeID: 6, NewAddress: "node-6.example.com:63001",
	})
	if !errors.Is(err, backendErr) {
		t.Fatalf("ReplaceNode error = %v, want %v", err, backendErr)
	}
}

// TestOperations_NilClientRequestIsTyped checks that a typed nil SDK request
// passed straight to the transport is refused, not dereferenced.
func TestOperations_NilClientRequestIsTyped(t *testing.T) {
	tr, err := New(&mockQuicBaaS{})
	if err != nil {
		t.Fatal(err)
	}
	requests := map[string]interface{}{
		"MigrateData":          (*operations.MigrateDataRequest)(nil),
		"GetMigrationProgress": (*operations.GetMigrationProgressRequest)(nil),
		"ReplaceNode":          (*operations.ReplaceNodeRequest)(nil),
		"UpgradeNode":          (*operations.UpgradeNodeRequest)(nil),
		"GetUpgradeStatus":     (*operations.GetUpgradeStatusRequest)(nil),
	}
	for method, req := range requests {
		_, err := tr.Request(context.Background(), "quicbaas.Operations", method, req)
		var typeErr *InvalidRequestTypeError
		if !errors.As(err, &typeErr) {
			t.Errorf("%s error = %v, want *InvalidRequestTypeError", method, err)
		}
	}
}
