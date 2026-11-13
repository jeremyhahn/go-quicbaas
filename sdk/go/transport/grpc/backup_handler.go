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
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/backup"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isBackupService reports whether the service name is the Backup service.
func isBackupService(service string) bool {
	svc := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		svc = service[idx+1:]
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
	if _, err := sdkRequest[backup.BackupRequest](op, req); err != nil {
		return nil, err
	}
	return nil, errors.NewUnimplemented(op, "CreateBackupRequest has no destination field")
}

func handleBackupList(ctx context.Context, client pb.QuicBaaSClient, _ interface{}) (interface{}, error) {
	const op = "grpc.backup.List"
	resp, err := client.ListBackups(ctx, &pb.ListBackupsRequest{})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	out := &backup.ListBackupsResponse{Backups: make([]backup.BackupInfo, 0, len(resp.GetBackups()))}
	for _, info := range resp.GetBackups() {
		out.Backups = append(out.Backups, backup.BackupInfo{
			ID:        info.GetBackupId(),
			Size:      info.GetSize(),
			CreatedAt: backupTime(info.GetTimestamp()),
			Encrypted: info.GetEncryption() == pb.EncryptionType_ENCRYPTION_TYPE_AES256,
		})
	}
	return out, nil
}

func handleBackupRestore(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	const op = "grpc.backup.Restore"
	r, err := sdkRequest[backup.RestoreRequest](op, req)
	if err != nil {
		return nil, err
	}
	if r.Source != "" {
		return nil, errors.NewUnimplemented(op, "RestoreBackupRequest has no source field")
	}
	resp, err := client.RestoreBackup(ctx, &pb.RestoreBackupRequest{BackupId: r.BackupID})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &backup.RestoreResponse{Restored: true, Duration: time.Duration(resp.GetDuration())}, nil
}

// backupTime converts a wire Unix-nanosecond time; zero stays the zero time.
func backupTime(unixNano int64) time.Time {
	if unixNano == 0 {
		return time.Time{}
	}
	return time.Unix(0, unixNano).UTC()
}
