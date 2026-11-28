// Copyright (c) 2025 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package embedded

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	backupclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/backup"
)

// backupDouble records the service requests the backup handlers pass on and
// answers with fixed results, so a test can prove the client's call reached
// the handler and came back as the client's own response type.
type backupDouble struct {
	mockQuicBaaS
	createReq  *service.CreateBackupRequest
	listReq    *service.ListBackupsRequest
	restoreReq *service.RestoreBackupRequest
}

var backupDoubleCreatedAt = time.Unix(1700000000, 0).UTC()

func (d *backupDouble) CreateBackup(_ context.Context, req *service.CreateBackupRequest) (*service.CreateBackupResponse, error) {
	d.createReq = req
	return &service.CreateBackupResponse{
		BackupID:  "backup-7",
		Size:      4096,
		Timestamp: backupDoubleCreatedAt.UnixNano(),
		Status:    "completed",
	}, nil
}

func (d *backupDouble) ListBackups(_ context.Context, req *service.ListBackupsRequest) (*service.ListBackupsResponse, error) {
	d.listReq = req
	return &service.ListBackupsResponse{
		Backups: []*service.BackupMetadata{
			{BackupID: "backup-7", Size: 4096, Timestamp: backupDoubleCreatedAt.UnixNano(), Status: "completed"},
		},
		TotalCount: 1,
	}, nil
}

func (d *backupDouble) RestoreBackup(_ context.Context, req *service.RestoreBackupRequest) (*service.RestoreBackupResponse, error) {
	d.restoreReq = req
	return &service.RestoreBackupResponse{JobID: "restore-" + req.BackupID, Status: "completed", Progress: 100}, nil
}

func TestBackupHandlers_WrongRequestType_ReturnInvalidRequestTypeError(t *testing.T) {
	transport, err := New(&backupDouble{})
	require.NoError(t, err)

	for _, method := range []string{"Create", "Restore", "List"} {
		t.Run(method, func(t *testing.T) {
			_, err := transport.Request(context.Background(), "quicbaas.Backup", method, &backupclient.BackupInfo{})
			var invalid *InvalidRequestTypeError
			require.True(t, errors.As(err, &invalid), "got %v", err)
		})
	}
}
