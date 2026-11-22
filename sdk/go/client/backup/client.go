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
	"math"
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
	// Create asks the backup service for a full backup in the service's
	// configured backup storage.
	Create(ctx context.Context, req *BackupRequest) (*BackupResponse, error)

	// Restore starts a restore of the backup named by BackupID and returns
	// the job id and status the service reports. A running restore is not
	// an error; Restored is set only once the restore has finished.
	Restore(ctx context.Context, req *RestoreRequest) (*RestoreResponse, error)

	// List returns the backups the service reports, up to its default page
	// of 100 backups.
	List(ctx context.Context) (*ListBackupsResponse, error)
}

// BackupRequest is a request to create a backup.
//
// The backup service writes to its configured backup storage and applies
// its configured compression and encryption, so Destination, Compress and
// Encrypt must stay at their zero values: Create refuses any of them with
// an invalid-input error before it calls the server. &BackupRequest{}
// requests a full backup.
type BackupRequest struct {
	Destination string
	Compress    bool
	Encrypt     bool
}

// BackupResponse is the response from creating a backup.
type BackupResponse struct {
	BackupID string
	Size     int64
	// Duration is always zero: the backup service does not report it.
	Duration  time.Duration
	CreatedAt time.Time
	// Status is the backup service's status for the backup, such as "completed".
	Status string
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
	// Encrypted is always false: the backup service does not report a backup's encryption state.
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

// Create asks the backup service for a full backup in the service's
// configured backup storage. It refuses a request that sets Destination,
// Compress or Encrypt, which the service does not honour.
func (c *client) Create(ctx context.Context, req *BackupRequest) (*BackupResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("backup.Create", "context is required")
	}

	if err := validateBackupRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "Create", &service.CreateBackupRequest{})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("backup.Create", "nil response from server")
	}
	created, ok := resp.(*service.CreateBackupResponse)
	if !ok {
		return nil, errors.NewInternal("backup.Create", "invalid response type")
	}
	if created == nil {
		return nil, errors.NewInternal("backup.Create", "nil response from server")
	}
	return &BackupResponse{
		BackupID:  created.BackupID,
		Size:      clampBackupSize(created.Size),
		CreatedAt: backupTime(created.Timestamp),
		Status:    created.Status,
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

	return &RestoreResponse{
		Restored: restoreResp.Status == restoreCompleted,
		JobID:    restoreResp.JobID,
		Status:   restoreResp.Status,
	}, nil
}

// List returns the backups the service reports. The service returns up to
// its default page of 100 backups.
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

	out := &ListBackupsResponse{Backups: []BackupInfo{}}
	if listResp == nil {
		return out, nil
	}
	for _, m := range listResp.Backups {
		if m == nil {
			continue
		}
		out.Backups = append(out.Backups, BackupInfo{
			ID:        m.BackupID,
			Size:      clampBackupSize(m.Size),
			CreatedAt: backupTime(m.Timestamp),
		})
	}
	return out, nil
}

// Validation functions

func validateBackupRequest(req *BackupRequest) error {
	if req == nil {
		return errors.NewInvalidInput("backup.Create", "request is required")
	}
	if req.Destination != "" {
		return errors.NewInvalidInput("backup.Create", "destination is not supported: the backup service writes to its configured backup storage")
	}
	if req.Compress {
		return errors.NewInvalidInput("backup.Create", "compression is not supported: the backup service applies its configured settings")
	}
	if req.Encrypt {
		return errors.NewInvalidInput("backup.Create", "encryption is not supported: the backup service applies its configured settings")
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

// clampBackupSize converts the service's uint64 size, clamping at math.MaxInt64.
func clampBackupSize(n uint64) int64 {
	if n > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(n)
}

// backupTime converts the service's UnixNano timestamp; zero stays the zero time.
func backupTime(ns int64) time.Time {
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}
