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
	"strings"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isBackupService reports whether the service name is the Backup service.
func isBackupService(name string) bool {
	svc := name
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		svc = name[idx+1:]
	}
	return svc == "Backup"
}

type backupHandlerFunc func(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error)

// backupMethodMap dispatches Backup methods onto the quicbaas.QuicBaaS
// backup RPCs: Create -> CreateBackup, List -> ListBackups and
// Restore -> RestoreBackup.
var backupMethodMap = map[string]backupHandlerFunc{
	"Create":  handleBackupCreate,
	"List":    handleBackupList,
	"Restore": handleBackupRestore,
}

// handleBackupRequest routes one Backup method over conn.
func handleBackupRequest(ctx context.Context, conn *grpc.ClientConn, method string, req interface{}) (interface{}, error) {
	handler, ok := backupMethodMap[method]
	if !ok {
		return nil, errors.NewUnimplemented("grpc.backup."+method, "method not exposed by the QuicBaaS backup RPCs")
	}
	return handler(ctx, pb.NewQuicBaaSClient(conn), req)
}

// handleBackupCreate refuses every Create: the SDK requires a destination,
// and CreateBackupRequest has no field to carry it.
func handleBackupCreate(_ context.Context, _ pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	const op = "grpc.backup.Create"
	if _, err := sdkRequest[service.CreateBackupRequest](op, req); err != nil {
		return nil, err
	}
	return nil, errors.NewUnimplemented(op, "CreateBackupRequest has no destination field")
}

// handleBackupList takes the service's list request the backup client sends
// and answers the service's list response. The wire BackupInfo carries no
// name, description or status, so those stay empty.
func handleBackupList(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	const op = "grpc.backup.List"
	if _, err := sdkRequest[service.ListBackupsRequest](op, req); err != nil {
		return nil, err
	}
	resp, err := client.ListBackups(ctx, &pb.ListBackupsRequest{})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	out := &service.ListBackupsResponse{Backups: make([]*service.BackupMetadata, 0, len(resp.GetBackups()))}
	for _, info := range resp.GetBackups() {
		size := info.GetSize()
		if size < 0 {
			size = 0
		}
		out.Backups = append(out.Backups, &service.BackupMetadata{
			BackupID:  info.GetBackupId(),
			Size:      uint64(size),
			Timestamp: info.GetTimestamp(),
		})
	}
	out.TotalCount = uint64(len(out.Backups))
	return out, nil
}

// handleBackupRestore takes the service's restore request the backup client
// sends and answers the service's restore response.
func handleBackupRestore(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	const op = "grpc.backup.Restore"
	r, err := sdkRequest[service.RestoreBackupRequest](op, req)
	if err != nil {
		return nil, err
	}
	if _, err := client.RestoreBackup(ctx, &pb.RestoreBackupRequest{BackupId: r.BackupID, TargetShards: r.TargetShards}); err != nil {
		return nil, convertGRPCError(err, op)
	}
	// RestoreBackup answers once the restore has finished.
	return &service.RestoreBackupResponse{Status: backupRestoreCompleted, Progress: 100}, nil
}

// backupRestoreCompleted is the service status of a finished restore.
const backupRestoreCompleted = "completed"
