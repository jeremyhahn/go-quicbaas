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

	qbbackup "github.com/jeremyhahn/go-quicbaas/pkg/backup"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
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
	BackupID string
	Source   string
}

// RestoreResponse is the response from restoring a backup.
type RestoreResponse struct {
	Restored bool
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

	resp, err := c.transport.Request(ctx, serviceName, "Create", toCreateBackupRequest(req))
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("backup.Create", "nil response from server")
	}

	backupResp, ok := resp.(*service.CreateBackupResponse)
	if !ok {
		return nil, errors.NewInternal("backup.Create", "invalid response type")
	}

	return &BackupResponse{
		BackupID:  backupResp.BackupID,
		Size:      conv.ClampUint64ToInt64(backupResp.Size),
		CreatedAt: time.Unix(0, backupResp.Timestamp).UTC(),
	}, nil
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

	return &RestoreResponse{Restored: restoreResp.Status == restoreCompleted}, nil
}

// List returns all available backups.
func (c *client) List(ctx context.Context) (*ListBackupsResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("backup.List", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "List", &service.ListBackupsRequest{})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListBackupsResponse{Backups: []BackupInfo{}}, nil
	}

	listResp, ok := resp.(*service.ListBackupsResponse)
	if !ok {
		return nil, errors.NewInternal("backup.List", "invalid response type")
	}

	backups := make([]BackupInfo, 0, len(listResp.Backups))
	for _, b := range listResp.Backups {
		if b == nil {
			continue
		}
		backups = append(backups, BackupInfo{
			ID:        b.BackupID,
			Size:      conv.ClampUint64ToInt64(b.Size),
			CreatedAt: time.Unix(0, b.Timestamp).UTC(),
		})
	}
	return &ListBackupsResponse{Backups: backups}, nil
}

// toCreateBackupRequest maps the client's request onto the service request
// every transport handler takes. The service has no destination field, so the
// destination travels as the backup's description.
func toCreateBackupRequest(req *BackupRequest) *service.CreateBackupRequest {
	out := &service.CreateBackupRequest{
		Description: req.Destination,
		Compression: string(qbbackup.CompressionNone),
		Encryption:  string(qbbackup.EncryptionNone),
	}
	if req.Compress {
		out.Compression = string(qbbackup.CompressionGzip)
	}
	if req.Encrypt {
		out.Encryption = string(qbbackup.EncryptionAES256GCM)
	}
	return out
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
	if req.BackupID == "" && req.Source == "" {
		return errors.NewInvalidInput("backup.Restore", "backup ID or source is required")
	}
	return nil
}
