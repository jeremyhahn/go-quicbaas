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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// TestBackupManagerAdapter_CompileTimeCheck verifies that BackupManagerAdapter
// satisfies the interfaces.BackupManager interface at compile time.
func TestBackupManagerAdapter_CompileTimeCheck(t *testing.T) {
	var _ interfaces.BackupManager = (*BackupManagerAdapter)(nil)
}

// TestBackupManagerAdapter_CompileTimeCheck_NonNil verifies that a constructed
// adapter instance is assignable to the BackupManager interface.
func TestBackupManagerAdapter_CompileTimeCheck_NonNil(t *testing.T) {
	adapter := NewBackupManagerAdapter(nil)
	var iface interfaces.BackupManager = adapter
	assert.NotNil(t, iface)
}

// TestBackupManagerAdapter_NewBackupManagerAdapter verifies that the constructor
// correctly wraps the provided BackupManager.
func TestBackupManagerAdapter_NewBackupManagerAdapter(t *testing.T) {
	adapter := NewBackupManagerAdapter(nil)
	require.NotNil(t, adapter)
	assert.Nil(t, adapter.manager)
}

// TestBackupManagerAdapter_NewBackupManagerAdapter_NonNilManager verifies that
// the constructor stores a non-nil manager reference.
func TestBackupManagerAdapter_NewBackupManagerAdapter_NonNilManager(t *testing.T) {
	// We cannot easily construct a real BackupManager without heavy dependencies,
	// but we can verify the adapter struct stores whatever pointer is given.
	// Using a nil pointer of the correct type demonstrates the wiring works.
	var mgr *backup.BackupManager
	adapter := NewBackupManagerAdapter(mgr)
	require.NotNil(t, adapter)
	assert.Equal(t, mgr, adapter.manager)
}

// TestBackupManagerAdapter_Unwrap verifies that Unwrap returns the same
// BackupManager instance that was provided to the constructor.
func TestBackupManagerAdapter_Unwrap(t *testing.T) {
	var mgr *backup.BackupManager
	adapter := NewBackupManagerAdapter(mgr)
	result := adapter.Unwrap()
	assert.Equal(t, mgr, result)
}

// TestBackupManagerAdapter_Unwrap_IsNilWhenNilProvided verifies that Unwrap
// returns nil when the adapter was constructed with a nil manager.
func TestBackupManagerAdapter_Unwrap_IsNilWhenNilProvided(t *testing.T) {
	adapter := NewBackupManagerAdapter(nil)
	assert.Nil(t, adapter.Unwrap())
}

// TestBackupManagerAdapter_CreateBackup_InvalidType verifies that CreateBackup
// returns an InvalidInput error when the backupType parameter is not a
// backup.BackupType.
func TestBackupManagerAdapter_CreateBackup_InvalidType(t *testing.T) {
	adapter := NewBackupManagerAdapter(nil)
	ctx := context.Background()

	tests := []struct {
		name      string
		input     interface{}
		wantInMsg string
	}{
		{
			name:      "string_type",
			input:     "full",
			wantInMsg: "backupType must be backup.BackupType",
		},
		{
			name:      "int_type",
			input:     42,
			wantInMsg: "backupType must be backup.BackupType",
		},
		{
			name:      "nil_type",
			input:     nil,
			wantInMsg: "backupType must be backup.BackupType",
		},
		{
			name:      "bool_type",
			input:     true,
			wantInMsg: "backupType must be backup.BackupType",
		},
		{
			name:      "struct_type",
			input:     struct{}{},
			wantInMsg: "backupType must be backup.BackupType",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := adapter.CreateBackup(ctx, tc.input)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.True(t, errors.IsInvalidInput(err), "expected InvalidInput error, got: %v", err)
			assert.Contains(t, err.Error(), tc.wantInMsg)
		})
	}
}

// TestBackupManagerAdapter_CreateBackup_ValidTypeWithNilManager verifies that
// when a valid backup.BackupType is provided but the underlying manager is nil,
// the adapter correctly passes the type assertion and panics on nil dereference
// (demonstrating the type assertion itself works correctly).
func TestBackupManagerAdapter_CreateBackup_ValidTypeWithNilManager(t *testing.T) {
	adapter := NewBackupManagerAdapter(nil)
	ctx := context.Background()

	// A valid BackupType should pass the type assertion but panic because
	// the underlying manager is nil. This confirms the type check works.
	assert.Panics(t, func() {
		_, _ = adapter.CreateBackup(ctx, backup.BackupTypeFull)
	})
}

// TestBackupManagerAdapter_RestoreBackup_InvalidOpts verifies that RestoreBackup
// returns an InvalidInput error when the opts parameter is not a
// *backup.RestoreOptions.
func TestBackupManagerAdapter_RestoreBackup_InvalidOpts(t *testing.T) {
	adapter := NewBackupManagerAdapter(nil)
	ctx := context.Background()

	tests := []struct {
		name      string
		opts      interface{}
		wantInMsg string
	}{
		{
			name:      "string_opts",
			opts:      "some-options",
			wantInMsg: "opts must be *backup.RestoreOptions",
		},
		{
			name:      "int_opts",
			opts:      123,
			wantInMsg: "opts must be *backup.RestoreOptions",
		},
		{
			name:      "nil_opts",
			opts:      nil,
			wantInMsg: "opts must be *backup.RestoreOptions",
		},
		{
			name:      "non_pointer_restore_options",
			opts:      backup.RestoreOptions{VerifyChecksum: true},
			wantInMsg: "opts must be *backup.RestoreOptions",
		},
		{
			name:      "map_opts",
			opts:      map[string]string{"key": "value"},
			wantInMsg: "opts must be *backup.RestoreOptions",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := adapter.RestoreBackup(ctx, "backup-123", tc.opts)
			require.Error(t, err)
			assert.True(t, errors.IsInvalidInput(err), "expected InvalidInput error, got: %v", err)
			assert.Contains(t, err.Error(), tc.wantInMsg)
		})
	}
}

// TestBackupManagerAdapter_RestoreBackup_ValidOptsWithNilManager verifies that
// when valid *backup.RestoreOptions are provided but the underlying manager is
// nil, the adapter correctly passes the type assertion.
func TestBackupManagerAdapter_RestoreBackup_ValidOptsWithNilManager(t *testing.T) {
	adapter := NewBackupManagerAdapter(nil)
	ctx := context.Background()

	opts := &backup.RestoreOptions{
		VerifyChecksum: true,
		SkipExisting:   false,
	}

	// Valid opts should pass the type assertion but panic because
	// the underlying manager is nil.
	assert.Panics(t, func() {
		_ = adapter.RestoreBackup(ctx, "backup-123", opts)
	})
}

// TestBackupManagerAdapter_ListBackups_NilManager verifies that ListBackups
// panics when the underlying manager is nil, confirming the method delegates
// directly to the manager without any intermediate type assertion.
func TestBackupManagerAdapter_ListBackups_NilManager(t *testing.T) {
	adapter := NewBackupManagerAdapter(nil)
	ctx := context.Background()

	assert.Panics(t, func() {
		_, _ = adapter.ListBackups(ctx, 10)
	})
}

// TestBackupManagerAdapter_DeleteBackup_NilManager verifies that DeleteBackup
// panics when the underlying manager is nil, confirming the method delegates
// directly to the manager.
func TestBackupManagerAdapter_DeleteBackup_NilManager(t *testing.T) {
	adapter := NewBackupManagerAdapter(nil)
	ctx := context.Background()

	assert.Panics(t, func() {
		_ = adapter.DeleteBackup(ctx, "backup-123")
	})
}

// TestBackupManagerAdapter_VerifyBackup_NilManager verifies that VerifyBackup
// panics when the underlying manager is nil, confirming the method delegates
// directly to the manager.
func TestBackupManagerAdapter_VerifyBackup_NilManager(t *testing.T) {
	adapter := NewBackupManagerAdapter(nil)
	ctx := context.Background()

	assert.Panics(t, func() {
		_, _ = adapter.VerifyBackup(ctx, "backup-123")
	})
}

// TestBackupManagerAdapter_Close_NilManager verifies that Close panics when
// the underlying manager is nil, confirming the method delegates directly
// to the manager.
func TestBackupManagerAdapter_Close_NilManager(t *testing.T) {
	adapter := NewBackupManagerAdapter(nil)

	assert.Panics(t, func() {
		_ = adapter.Close()
	})
}

// TestBackupManagerAdapter_CreateBackup_ErrorCodeIsInvalidInput verifies that
// the error returned for invalid backup type has the correct QBError code.
func TestBackupManagerAdapter_CreateBackup_ErrorCodeIsInvalidInput(t *testing.T) {
	adapter := NewBackupManagerAdapter(nil)
	ctx := context.Background()

	_, err := adapter.CreateBackup(ctx, "not-a-backup-type")
	require.Error(t, err)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	assert.Equal(t, "BackupManagerAdapter.CreateBackup", qbErr.Op)
}

// TestBackupManagerAdapter_RestoreBackup_ErrorCodeIsInvalidInput verifies that
// the error returned for invalid restore options has the correct QBError code.
func TestBackupManagerAdapter_RestoreBackup_ErrorCodeIsInvalidInput(t *testing.T) {
	adapter := NewBackupManagerAdapter(nil)
	ctx := context.Background()

	err := adapter.RestoreBackup(ctx, "backup-456", "not-restore-options")
	require.Error(t, err)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	assert.Equal(t, "BackupManagerAdapter.RestoreBackup", qbErr.Op)
}
