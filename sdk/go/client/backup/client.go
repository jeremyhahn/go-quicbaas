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

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Backup"

	// restoreStatusCompleted is the status the backup service reports for a
	// finished restore.
	restoreStatusCompleted = "completed"
)

// Client provides backup and restore operations.
type Client interface {
	// Create initiates a new backup operation.
	Create(ctx context.Context, req *BackupRequest) (*BackupResponse, error)

	// Restore restores data from a backup.
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
	// BackupID names the backup to restore. It is required.
	BackupID string
	// Source is not carried by the backup service: no transport's restore
	// request has a source. Restore refuses a request that sets it with an
	// invalid-input error before any call, rather than dropping it (#1726).
	Source string
}

// RestoreResponse is the response from restoring a backup.
//
// The service restore is asynchronous: it returns a job id, a status and a
// progress. Restored is true only for a finished restore (status
// "completed"). A restore the service accepted and is still running returns
// Restored false with its JobID and Status, not an error.
type RestoreResponse struct {
	Restored bool
	JobID    string
	Status   string
	Progress float64
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

	// The transports serve the service's restore request and response
	// types (the embedded transport refuses any other request type).
	resp, err := c.transport.Request(ctx, serviceName, "Restore", &service.RestoreBackupRequest{BackupID: req.BackupID})
	if err != nil {
		return nil, err
	}

	switch r := resp.(type) {
	case *service.RestoreBackupResponse:
		if r == nil {
			return nil, errors.NewInternal("backup.Restore", "nil response from server")
		}
		return &RestoreResponse{
			Restored: r.Status == restoreStatusCompleted,
			JobID:    r.JobID,
			Status:   r.Status,
			Progress: r.Progress,
		}, nil
	case *RestoreResponse:
		if r == nil {
			return nil, errors.NewInternal("backup.Restore", "nil response from server")
		}
		return r, nil
	case nil:
		return nil, errors.NewInternal("backup.Restore", "nil response from server")
	default:
		return nil, errors.NewInternal("backup.Restore", "invalid response type")
	}
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
		return errors.NewInvalidInput("backup.Restore", "source is not supported: the backup service restores by backup ID only")
	}
	if req.BackupID == "" {
		return errors.NewInvalidInput("backup.Restore", "backup ID is required")
	}
	return nil
}
