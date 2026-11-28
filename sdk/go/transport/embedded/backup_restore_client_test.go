// Copyright (c) 2025 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package embedded

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	backupclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/backup"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// restoreDouble records the restore request the embedded handler passes on
// and answers with the configured status.
type restoreDouble struct {
	mockQuicBaaS
	status     string
	restoreReq *service.RestoreBackupRequest
}

func (d *restoreDouble) RestoreBackup(_ context.Context, req *service.RestoreBackupRequest) (*service.RestoreBackupResponse, error) {
	d.restoreReq = req
	return &service.RestoreBackupResponse{JobID: "restore-" + req.BackupID, Status: d.status, Progress: 50}, nil
}

func TestBackupClient_Restore_ReachesEmbeddedHandler(t *testing.T) {
	tests := []struct {
		name         string
		status       string
		wantRestored bool
	}{
		{name: "running restore is accepted, not finished", status: "running", wantRestored: false},
		{name: "completed restore is finished", status: "completed", wantRestored: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			double := &restoreDouble{status: tt.status}
			transport, err := New(double)
			require.NoError(t, err)
			client := backupclient.NewClient(transport)

			resp, err := client.Restore(context.Background(), &backupclient.RestoreRequest{BackupID: "backup-7"})
			require.NoError(t, err)
			require.NotNil(t, double.restoreReq, "the handler never received the request")
			assert.Equal(t, "backup-7", double.restoreReq.BackupID)
			assert.Equal(t, "restore-backup-7", resp.JobID)
			assert.Equal(t, tt.status, resp.Status)
			assert.Equal(t, tt.wantRestored, resp.Restored)
		})
	}
}

func TestBackupClient_Restore_SourceNeverReachesEmbeddedHandler(t *testing.T) {
	double := &restoreDouble{status: "completed"}
	transport, err := New(double)
	require.NoError(t, err)
	client := backupclient.NewClient(transport)

	resp, err := client.Restore(context.Background(), &backupclient.RestoreRequest{BackupID: "backup-7", Source: "s3://elsewhere"})
	require.Error(t, err)
	assert.True(t, sdkerrors.IsInvalidInput(err), "want invalid input, got %v", err)
	assert.Nil(t, resp)
	assert.Nil(t, double.restoreReq, "a refused restore must not reach the handler")
}

// createListDouble records the create and list requests the embedded
// handlers pass on and answers in the service's shape.
type createListDouble struct {
	mockQuicBaaS
	createReq *service.CreateBackupRequest
	listReq   *service.ListBackupsRequest
}

func (d *createListDouble) CreateBackup(_ context.Context, req *service.CreateBackupRequest) (*service.CreateBackupResponse, error) {
	d.createReq = req
	return &service.CreateBackupResponse{BackupID: "backup-9", Size: 4096, Timestamp: 1700000000000000000, Status: "completed"}, nil
}

func (d *createListDouble) ListBackups(_ context.Context, req *service.ListBackupsRequest) (*service.ListBackupsResponse, error) {
	d.listReq = req
	return &service.ListBackupsResponse{
		Backups: []*service.BackupMetadata{
			{BackupID: "backup-9", Size: 4096, Timestamp: 1700000000000000000, Status: "completed"},
			nil,
		},
		TotalCount: 1,
	}, nil
}

func TestBackupClient_Create_ReachesEmbeddedHandler(t *testing.T) {
	double := &createListDouble{}
	transport, err := New(double)
	require.NoError(t, err)
	client := backupclient.NewClient(transport)

	resp, err := client.Create(context.Background(), &backupclient.BackupRequest{})
	require.NoError(t, err)
	require.NotNil(t, double.createReq, "the handler never received the request")
	assert.Equal(t, "backup-9", resp.BackupID)
	assert.Equal(t, int64(4096), resp.Size)
	assert.True(t, resp.CreatedAt.Equal(time.Unix(0, 1700000000000000000)), "CreatedAt = %v", resp.CreatedAt)
	assert.Equal(t, "completed", resp.Status)
}

func TestBackupClient_List_ReachesEmbeddedHandler(t *testing.T) {
	double := &createListDouble{}
	transport, err := New(double)
	require.NoError(t, err)
	client := backupclient.NewClient(transport)

	resp, err := client.List(context.Background())
	require.NoError(t, err)
	require.NotNil(t, double.listReq, "the handler never received the request")
	require.Len(t, resp.Backups, 1, "the nil entry must be skipped")
	assert.Equal(t, "backup-9", resp.Backups[0].ID)
	assert.Equal(t, int64(4096), resp.Backups[0].Size)
}

func TestBackupClient_Create_UnsupportedOptionsNeverReachEmbeddedHandler(t *testing.T) {
	tests := []struct {
		name string
		req  *backupclient.BackupRequest
	}{
		{name: "destination", req: &backupclient.BackupRequest{Destination: "/b"}},
		{name: "compress", req: &backupclient.BackupRequest{Compress: true}},
		{name: "encrypt", req: &backupclient.BackupRequest{Encrypt: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			double := &createListDouble{}
			transport, err := New(double)
			require.NoError(t, err)
			client := backupclient.NewClient(transport)

			resp, err := client.Create(context.Background(), tt.req)
			require.Error(t, err)
			assert.True(t, sdkerrors.IsInvalidInput(err), "want invalid input, got %v", err)
			assert.Nil(t, resp)
			assert.Nil(t, double.createReq, "a refused create must not reach the handler")
		})
	}
}
