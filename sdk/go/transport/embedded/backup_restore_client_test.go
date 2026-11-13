// Copyright (c) 2025 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package embedded

import (
	"context"
	"testing"

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
