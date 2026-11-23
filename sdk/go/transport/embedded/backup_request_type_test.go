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

	"github.com/stretchr/testify/assert"
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

func newBackupClientOverEmbedded(t *testing.T) (backupclient.Client, *backupDouble) {
	t.Helper()
	double := &backupDouble{}
	transport, err := New(double)
	require.NoError(t, err)
	return backupclient.NewClient(transport), double
}

func TestBackupClient_Create_ReachesEmbeddedHandler(t *testing.T) {
	client, double := newBackupClientOverEmbedded(t)

	resp, err := client.Create(context.Background(), &backupclient.BackupRequest{
		Destination: "nightly",
		Compress:    true,
		Encrypt:     true,
	})
	require.NoError(t, err)

	require.NotNil(t, double.createReq, "the handler never received the request")
	assert.Equal(t, "nightly", double.createReq.Description)
	assert.Equal(t, "gzip", double.createReq.Compression)
	assert.Equal(t, "aes-256-gcm", double.createReq.Encryption)

	assert.Equal(t, "backup-7", resp.BackupID)
	assert.Equal(t, int64(4096), resp.Size)
	assert.True(t, backupDoubleCreatedAt.Equal(resp.CreatedAt))
}

func TestBackupClient_Restore_ReachesEmbeddedHandler(t *testing.T) {
	client, double := newBackupClientOverEmbedded(t)

	resp, err := client.Restore(context.Background(), &backupclient.RestoreRequest{BackupID: "backup-7"})
	require.NoError(t, err)

	require.NotNil(t, double.restoreReq, "the handler never received the request")
	assert.Equal(t, "backup-7", double.restoreReq.BackupID)
	assert.True(t, resp.Restored)
}

func TestBackupClient_List_ReachesEmbeddedHandler(t *testing.T) {
	client, double := newBackupClientOverEmbedded(t)

	resp, err := client.List(context.Background())
	require.NoError(t, err)

	require.NotNil(t, double.listReq, "the handler never received the request")
	require.Len(t, resp.Backups, 1)
	assert.Equal(t, "backup-7", resp.Backups[0].ID)
	assert.Equal(t, int64(4096), resp.Backups[0].Size)
	assert.True(t, backupDoubleCreatedAt.Equal(resp.Backups[0].CreatedAt))
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
