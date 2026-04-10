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

package service

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// BackupManagerAdapter wraps backup.BackupManager to implement interfaces.BackupManager.
// This adapter converts between backup package concrete types and interface package
// interface{} types to enable dependency injection without circular imports.
type BackupManagerAdapter struct {
	manager *backup.BackupManager
}

// NewBackupManagerAdapter creates a new adapter wrapping the given BackupManager.
func NewBackupManagerAdapter(manager *backup.BackupManager) *BackupManagerAdapter {
	return &BackupManagerAdapter{manager: manager}
}

// CreateBackup creates a new backup. The backupType parameter must be a backup.BackupType.
func (a *BackupManagerAdapter) CreateBackup(ctx context.Context, backupType interface{}) (interface{}, error) {
	bt, ok := backupType.(backup.BackupType)
	if !ok {
		return nil, errors.InvalidInput("BackupManagerAdapter.CreateBackup", "backupType must be backup.BackupType")
	}
	return a.manager.CreateBackup(ctx, bt)
}

// RestoreBackup restores from a backup. The opts parameter must be a *backup.RestoreOptions.
func (a *BackupManagerAdapter) RestoreBackup(ctx context.Context, backupID string, opts interface{}) error {
	restoreOpts, ok := opts.(*backup.RestoreOptions)
	if !ok {
		return errors.InvalidInput("BackupManagerAdapter.RestoreBackup", "opts must be *backup.RestoreOptions")
	}
	return a.manager.RestoreBackup(ctx, backupID, restoreOpts)
}

// ListBackups returns a list of available backups.
func (a *BackupManagerAdapter) ListBackups(ctx context.Context, limit int) (interface{}, error) {
	return a.manager.ListBackups(ctx, limit)
}

// DeleteBackup deletes a backup.
func (a *BackupManagerAdapter) DeleteBackup(ctx context.Context, backupID string) error {
	return a.manager.DeleteBackup(ctx, backupID)
}

// VerifyBackup verifies the integrity of a backup.
func (a *BackupManagerAdapter) VerifyBackup(ctx context.Context, backupID string) (interface{}, error) {
	return a.manager.VerifyBackup(ctx, backupID)
}

// Close closes the backup manager and releases resources.
func (a *BackupManagerAdapter) Close() error {
	return a.manager.Close()
}

// Unwrap returns the underlying BackupManager for cases where direct access is needed.
func (a *BackupManagerAdapter) Unwrap() *backup.BackupManager {
	return a.manager
}

// Compile-time check that BackupManagerAdapter implements interfaces.BackupManager.
var _ interfaces.BackupManager = (*BackupManagerAdapter)(nil)
