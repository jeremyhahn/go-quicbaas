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
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/backup"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// fakeBackupServer records the backup RPCs it receives. A non-OK code makes
// every RPC fail with that code.
type fakeBackupServer struct {
	pb.UnimplementedQuicBaaSServer
	code codes.Code

	calls       int
	lastCreate  *pb.CreateBackupRequest
	lastList    *pb.ListBackupsRequest
	lastRestore *pb.RestoreBackupRequest
	backups     []*pb.BackupInfo
}

func (f *fakeBackupServer) fail() error {
	f.calls++
	if f.code != codes.OK {
		return status.Error(f.code, "refused")
	}
	return nil
}

func (f *fakeBackupServer) CreateBackup(_ context.Context, req *pb.CreateBackupRequest) (*pb.CreateBackupResponse, error) {
	f.lastCreate = req
	if err := f.fail(); err != nil {
		return nil, err
	}
	return &pb.CreateBackupResponse{BackupId: "b-1"}, nil
}

func (f *fakeBackupServer) ListBackups(_ context.Context, req *pb.ListBackupsRequest) (*pb.ListBackupsResponse, error) {
	f.lastList = req
	if err := f.fail(); err != nil {
		return nil, err
	}
	return &pb.ListBackupsResponse{Backups: f.backups}, nil
}

func (f *fakeBackupServer) RestoreBackup(_ context.Context, req *pb.RestoreBackupRequest) (*pb.RestoreBackupResponse, error) {
	f.lastRestore = req
	if err := f.fail(); err != nil {
		return nil, err
	}
	return &pb.RestoreBackupResponse{
		BackupId:     req.GetBackupId(),
		KeysRestored: 7,
		Duration:     int64(3 * time.Second),
	}, nil
}

// newBackupClient serves fake on a bufconn listener and returns the real SDK
// backup client over the gRPC transport.
func newBackupClient(t *testing.T, fake *fakeBackupServer) (backup.Client, *Transport) {
	t.Helper()
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterQuicBaaSServer(s, fake)
	})
	tr := &Transport{cfg: &transport.Config{Timeout: 5 * time.Second}, conn: srv.dial(t)}
	return backup.NewClient(tr), tr
}

func TestBackupHandler_RestoreCarriesTheBackupID(t *testing.T) {
	fake := &fakeBackupServer{}
	client, _ := newBackupClient(t, fake)

	resp, err := client.Restore(context.Background(), &backup.RestoreRequest{BackupID: "b-42"})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := fake.lastRestore.GetBackupId(); got != "b-42" {
		t.Errorf("RestoreBackup backup_id = %q, want b-42", got)
	}
	want := &backup.RestoreResponse{Restored: true, Duration: 3 * time.Second}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("Restore = %+v, want %+v", resp, want)
	}
}

func TestBackupHandler_ListMapsEveryBackup(t *testing.T) {
	created := time.Date(2026, 10, 19, 12, 0, 0, 5, time.UTC)
	fake := &fakeBackupServer{backups: []*pb.BackupInfo{
		{BackupId: "b-1", Size: 1024, Timestamp: created.UnixNano(), Encryption: pb.EncryptionType_ENCRYPTION_TYPE_AES256},
		{BackupId: "b-2", Size: 2048, Encryption: pb.EncryptionType_ENCRYPTION_TYPE_NONE},
	}}
	client, _ := newBackupClient(t, fake)

	resp, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if fake.lastList.GetLimit() != 0 || fake.lastList.GetContinuationToken() != "" {
		t.Errorf("ListBackups must ask for every backup, got %+v", fake.lastList)
	}
	want := &backup.ListBackupsResponse{Backups: []backup.BackupInfo{
		{ID: "b-1", Size: 1024, CreatedAt: created, Encrypted: true},
		{ID: "b-2", Size: 2048},
	}}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("List = %+v, want %+v", resp, want)
	}
}

// Create's required destination and Restore's source have no field on the
// gRPC wire; the handler refuses them rather than dropping them unseen.
func TestBackupHandler_RefusesFieldsTheWireCannotCarry(t *testing.T) {
	fake := &fakeBackupServer{}
	client, _ := newBackupClient(t, fake)
	ctx := context.Background()

	if _, err := client.Create(ctx, &backup.BackupRequest{Destination: "/backups", Compress: true}); !errors.IsUnimplemented(err) {
		t.Errorf("Create with a destination gave %v, want unimplemented", err)
	}
	if _, err := client.Restore(ctx, &backup.RestoreRequest{BackupID: "b-1", Source: "/backups/b-1"}); !errors.IsUnimplemented(err) {
		t.Errorf("Restore with a source gave %v, want unimplemented", err)
	}
	if fake.calls != 0 {
		t.Errorf("a refused request reached the server %d times", fake.calls)
	}
}

func TestBackupHandler_MapsServerRefusals(t *testing.T) {
	codesToPredicates := map[codes.Code]func(error) bool{
		codes.InvalidArgument:  errors.IsInvalidInput,
		codes.NotFound:         errors.IsNotFound,
		codes.PermissionDenied: errors.IsForbidden,
	}
	for code, is := range codesToPredicates {
		client, _ := newBackupClient(t, &fakeBackupServer{code: code})
		if _, err := client.Restore(context.Background(), &backup.RestoreRequest{BackupID: "b-1"}); !is(err) {
			t.Errorf("Restore answering %s gave %v", code, err)
		}
		if _, err := client.List(context.Background()); !is(err) {
			t.Errorf("List answering %s gave %v", code, err)
		}
	}
}

func TestBackupHandler_RefusesUnknownMethodsAndRequestTypes(t *testing.T) {
	fake := &fakeBackupServer{}
	_, tr := newBackupClient(t, fake)
	ctx := context.Background()

	if _, err := tr.Request(ctx, "quicbaas.Backup", "Purge", &backup.RestoreRequest{}); !errors.IsUnimplemented(err) {
		t.Errorf("an unknown backup method gave %v, want unimplemented", err)
	}
	if _, err := tr.Request(ctx, "quicbaas.Backup", "Restore", &backup.BackupRequest{}); !errors.IsInvalidInput(err) {
		t.Errorf("a mistyped Restore request gave %v, want invalid input", err)
	}
	if fake.calls != 0 {
		t.Errorf("a refused request reached the server %d times", fake.calls)
	}
}

func TestBackupHandler_CarriesEveryVerb(t *testing.T) {
	iface := reflect.TypeOf((*backup.Client)(nil)).Elem()
	declared := make(map[string]bool, iface.NumMethod())
	for i := 0; i < iface.NumMethod(); i++ {
		verb := iface.Method(i).Name
		declared[verb] = true
		if _, ok := backupMethodMap[verb]; !ok {
			t.Errorf("the gRPC transport does not route backup.%s", verb)
		}
	}
	for method := range backupMethodMap {
		if !declared[method] {
			t.Errorf("the gRPC transport routes %q, which the SDK interface does not declare", method)
		}
	}
	for _, name := range []string{"quicbaas.Backup", "Backup"} {
		if !isBackupService(name) {
			t.Errorf("isBackupService(%q) = false", name)
		}
	}
	if isBackupService("quicbaas.Cache") {
		t.Error("isBackupService claims the Cache service")
	}
}
