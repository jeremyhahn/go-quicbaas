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

package backup

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Backup"

	// restoreCompleted is the status the service reports for a finished restore.
	restoreCompleted = "completed"
)

// Client provides backup and restore operations.
type Client interface {
	// Create initiates a new backup operation.
	Create(ctx context.Context, req *BackupRequest) (*BackupResponse, error)

	// Restore starts a restore of the backup named by BackupID and returns
	// the job id and status the service reports. A running restore is not
	// an error; Restored is set only once the restore has finished.
	Restore(ctx context.Context, req *RestoreRequest) (*RestoreResponse, error)

	// List returns all available backups.
	List(ctx context.Context) (*ListBackupsResponse, error)
}

// BackupRequest is a request to create a backup.
type BackupRequest struct {
	Destination string
	Compress    bool
	Encrypt     bool
}

// BackupResponse is the response from creating a backup.
type BackupResponse struct {
	BackupID  string
	Size      int64
	Duration  time.Duration
	CreatedAt time.Time
}

// RestoreRequest is a request to restore from a backup.
type RestoreRequest struct {
	// BackupID identifies the backup to restore. It is required.
	BackupID string
	// Source names an external location to restore from. The backup
	// service restores only by backup ID and no transport carries a
	// source, so Restore refuses any request that sets Source with an
	// invalid-input error before it calls the server.
	Source string
}

// RestoreResponse is the response from restoring a backup.
type RestoreResponse struct {
	// Restored is true only when the service reports the restore as
	// finished (status "completed"). An accepted restore that is still
	// running is not an error: Restored is false and Status and JobID say
	// where it stands.
	Restored bool
	// JobID identifies the restore job, as the service reports it.
	JobID string
	// Status is the restore job's status, as the service reports it.
	Status string
	// Duration is always zero: the service reports no restore duration.
	Duration time.Duration
}

// ListBackupsResponse is the response from listing backups.
type ListBackupsResponse struct {
	Backups []BackupInfo
}

// BackupInfo contains information about a backup.
type BackupInfo struct {
	ID        string
	Size      int64
	CreatedAt time.Time
	Encrypted bool
}

// listRequest is the internal list request.
type listRequest struct{}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new backup client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// Create initiates a new backup operation.
func (c *client) Create(ctx context.Context, req *BackupRequest) (*BackupResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("backup.Create", "context is required")
	}

	if err := validateBackupRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "Create", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("backup.Create", "nil response from server")
	}

	backupResp, ok := resp.(*BackupResponse)
	if !ok {
		return nil, errors.NewInternal("backup.Create", "invalid response type")
	}

	return backupResp, nil
}

// Restore restores data from a backup.
func (c *client) Restore(ctx context.Context, req *RestoreRequest) (*RestoreResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("backup.Restore", "context is required")
	}

	if err := validateRestoreRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "Restore", &service.RestoreBackupRequest{BackupID: req.BackupID})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("backup.Restore", "nil response from server")
	}

	restoreResp, ok := resp.(*service.RestoreBackupResponse)
	if !ok {
		return nil, errors.NewInternal("backup.Restore", "invalid response type")
	}

	return &RestoreResponse{
		Restored: restoreResp.Status == restoreCompleted,
		JobID:    restoreResp.JobID,
		Status:   restoreResp.Status,
	}, nil
}

// List returns all available backups.
func (c *client) List(ctx context.Context) (*ListBackupsResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("backup.List", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "List", &listRequest{})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListBackupsResponse{Backups: []BackupInfo{}}, nil
	}

	listResp, ok := resp.(*ListBackupsResponse)
	if !ok {
		return nil, errors.NewInternal("backup.List", "invalid response type")
	}

	return listResp, nil
}

// Validation functions

func validateBackupRequest(req *BackupRequest) error {
	if req == nil {
		return errors.NewInvalidInput("backup.Create", "request is required")
	}
	if req.Destination == "" {
		return errors.NewInvalidInput("backup.Create", "destination is required")
	}
	return nil
}

func validateRestoreRequest(req *RestoreRequest) error {
	if req == nil {
		return errors.NewInvalidInput("backup.Restore", "request is required")
	}
	if req.Source != "" {
		return errors.NewInvalidInput("backup.Restore", "restore from a source is not supported; set backup ID")
	}
	if req.BackupID == "" {
		return errors.NewInvalidInput("backup.Restore", "backup ID is required")
	}
	return nil
}
