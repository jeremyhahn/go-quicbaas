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
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage/memory"
)

// setupSchedulerTestManager creates a backup manager for scheduler tests.
func setupSchedulerTestManager(t *testing.T, schedule *ScheduleConfig) (*BackupManager, *MockStorage, func()) {
	t.Helper()

	// Create temp work directory
	workDir, err := os.MkdirTemp("", "backup-scheduler-test-*")
	require.NoError(t, err)

	// Create mock storage
	mockBackend := NewMockStorage()

	// Create logger
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	// Create memory storage
	memStorage := memory.New(log)
	mockStor := &mockStorage{MemoryStorage: memStorage}

	// Add test data
	_ = mockStor.Set([]byte("key1"), []byte("value1"))
	_ = mockStor.Set([]byte("key2"), []byte("value2"))
	_ = mockStor.Set([]byte("key3"), []byte("value3"))

	// Create mock raft node
	mockRaft := &mockRaftNode{shardCount: 1}

	// Create config
	config := &Config{
		StorageBackend: "mock",
		StorageConfig: map[string]string{
			"path":   workDir,
			"prefix": "backups",
		},
		DefaultCompression: CompressionNone,
		DefaultEncryption:  EncryptionNone,
		WorkDir:            workDir,
		Timeout:            30 * time.Minute,
		Schedule:           schedule,
	}

	// Create manager directly without using NewBackupManager to inject mock backend
	bm := &BackupManager{
		config:     config,
		storage:    mockStor,
		raft:       mockRaft,
		backend:    mockBackend,
		prefix:     "backups",
		logger:     log.WithField("component", "backup-manager"),
		metrics:    initBackupMetrics(),
		cronStopCh: make(chan struct{}),
	}

	cleanup := func() {
		_ = bm.Close()
		os.RemoveAll(workDir)
	}

	return bm, mockBackend, cleanup
}

func TestScheduler_StartScheduledBackups_Valid(t *testing.T) {
	schedule := &ScheduleConfig{
		Enabled:        true,
		CronExpression: "*/5 * * * *", // Every 5 minutes
		BackupType:     BackupTypeFull,
	}

	bm, _, cleanup := setupSchedulerTestManager(t, schedule)
	defer cleanup()

	// Start scheduled backups
	err := bm.startScheduledBackups()
	require.NoError(t, err)

	// Verify scheduler is running
	assert.True(t, bm.IsSchedulerRunning())

	// Get next scheduled backup time
	nextRun, ok := bm.GetNextScheduledBackup()
	assert.True(t, ok)
	assert.True(t, nextRun.After(time.Now()))
}

func TestScheduler_StartScheduledBackups_EmptyCronExpression(t *testing.T) {
	schedule := &ScheduleConfig{
		Enabled:        true,
		CronExpression: "", // Empty cron expression
		BackupType:     BackupTypeFull,
	}

	bm, _, cleanup := setupSchedulerTestManager(t, schedule)
	defer cleanup()

	err := bm.startScheduledBackups()
	require.Error(t, err)

	var schedErr *SchedulerError
	assert.ErrorAs(t, err, &schedErr)
	assert.Equal(t, "parse", schedErr.Op)
}

func TestScheduler_StartScheduledBackups_InvalidCronExpression(t *testing.T) {
	schedule := &ScheduleConfig{
		Enabled:        true,
		CronExpression: "invalid cron", // Invalid cron expression
		BackupType:     BackupTypeFull,
	}

	bm, _, cleanup := setupSchedulerTestManager(t, schedule)
	defer cleanup()

	err := bm.startScheduledBackups()
	require.Error(t, err)

	var schedErr *SchedulerError
	assert.ErrorAs(t, err, &schedErr)
	assert.Equal(t, "schedule", schedErr.Op)
}

func TestScheduler_StartScheduledBackups_Disabled(t *testing.T) {
	schedule := &ScheduleConfig{
		Enabled:        false,
		CronExpression: "* * * * *",
		BackupType:     BackupTypeFull,
	}

	bm, _, cleanup := setupSchedulerTestManager(t, schedule)
	defer cleanup()

	err := bm.startScheduledBackups()
	require.NoError(t, err)

	// Scheduler should not be running when disabled
	assert.False(t, bm.IsSchedulerRunning())
}

func TestScheduler_StartScheduledBackups_NilSchedule(t *testing.T) {
	bm, _, cleanup := setupSchedulerTestManager(t, nil)
	defer cleanup()

	err := bm.startScheduledBackups()
	require.NoError(t, err)

	// Scheduler should not be running when no schedule is configured
	assert.False(t, bm.IsSchedulerRunning())
}

func TestScheduler_StopScheduledBackups(t *testing.T) {
	schedule := &ScheduleConfig{
		Enabled:        true,
		CronExpression: "* * * * *",
		BackupType:     BackupTypeFull,
	}

	bm, _, cleanup := setupSchedulerTestManager(t, schedule)
	defer cleanup()

	// Start scheduled backups
	err := bm.startScheduledBackups()
	require.NoError(t, err)
	assert.True(t, bm.IsSchedulerRunning())

	// Stop scheduled backups
	bm.StopScheduledBackups()
	assert.False(t, bm.IsSchedulerRunning())
}

func TestScheduler_StopScheduledBackups_NotRunning(t *testing.T) {
	bm, _, cleanup := setupSchedulerTestManager(t, nil)
	defer cleanup()

	// Stopping when not running should not panic
	bm.StopScheduledBackups()
	assert.False(t, bm.IsSchedulerRunning())
}

func TestScheduler_GetNextScheduledBackup_NotRunning(t *testing.T) {
	bm, _, cleanup := setupSchedulerTestManager(t, nil)
	defer cleanup()

	nextRun, ok := bm.GetNextScheduledBackup()
	assert.False(t, ok)
	assert.True(t, nextRun.IsZero())
}

func TestScheduler_CloseStopsScheduler(t *testing.T) {
	schedule := &ScheduleConfig{
		Enabled:        true,
		CronExpression: "* * * * *",
		BackupType:     BackupTypeFull,
	}

	bm, _, cleanup := setupSchedulerTestManager(t, schedule)
	defer cleanup()

	// Start scheduled backups
	err := bm.startScheduledBackups()
	require.NoError(t, err)
	assert.True(t, bm.IsSchedulerRunning())

	// Close the manager
	err = bm.Close()
	require.NoError(t, err)

	// Scheduler should be stopped
	assert.False(t, bm.IsSchedulerRunning())
}

func TestScheduler_CronExpressionFormats(t *testing.T) {
	testCases := []struct {
		name       string
		expression string
		valid      bool
	}{
		{"every minute", "* * * * *", true},
		{"every 5 minutes", "*/5 * * * *", true},
		{"daily at 2am", "0 2 * * *", true},
		{"weekly on sunday", "0 0 * * 0", true},
		{"monthly first day", "0 0 1 * *", true},
		{"with seconds", "0 * * * * *", true},
		{"every second", "@every 1s", true},
		{"hourly descriptor", "@hourly", true},
		{"daily descriptor", "@daily", true},
		{"weekly descriptor", "@weekly", true},
		{"monthly descriptor", "@monthly", true},
		{"invalid expression", "not-a-cron", false},
		{"too few fields", "* *", false},
		{"invalid field values", "99 99 99 99 99", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			schedule := &ScheduleConfig{
				Enabled:        true,
				CronExpression: tc.expression,
				BackupType:     BackupTypeFull,
			}

			bm, _, cleanup := setupSchedulerTestManager(t, schedule)
			defer cleanup()

			err := bm.startScheduledBackups()

			if tc.valid {
				require.NoError(t, err)
				assert.True(t, bm.IsSchedulerRunning())
			} else {
				require.Error(t, err)
				assert.False(t, bm.IsSchedulerRunning())
			}
		})
	}
}

func TestScheduler_DefaultBackupType(t *testing.T) {
	schedule := &ScheduleConfig{
		Enabled:        true,
		CronExpression: "* * * * *",
		BackupType:     "", // Empty backup type - should default to full
	}

	bm, _, cleanup := setupSchedulerTestManager(t, schedule)
	defer cleanup()

	err := bm.startScheduledBackups()
	require.NoError(t, err)
	assert.True(t, bm.IsSchedulerRunning())
}

func TestScheduler_WithRetentionPolicy(t *testing.T) {
	schedule := &ScheduleConfig{
		Enabled:        true,
		CronExpression: "* * * * *",
		BackupType:     BackupTypeFull,
		RetentionPolicy: &RetentionPolicy{
			MaxBackups: 5,
			MaxAge:     24 * time.Hour,
		},
	}

	bm, _, cleanup := setupSchedulerTestManager(t, schedule)
	defer cleanup()

	err := bm.startScheduledBackups()
	require.NoError(t, err)
	assert.True(t, bm.IsSchedulerRunning())
}

func TestSchedulerError_Error(t *testing.T) {
	t.Run("with cause", func(t *testing.T) {
		err := &SchedulerError{
			Op:      "parse",
			Message: "invalid cron expression",
			Cause:   assert.AnError,
		}
		assert.Contains(t, err.Error(), "scheduler parse")
		assert.Contains(t, err.Error(), "invalid cron expression")
		assert.Contains(t, err.Error(), assert.AnError.Error())
	})

	t.Run("without cause", func(t *testing.T) {
		err := &SchedulerError{
			Op:      "start",
			Message: "scheduler already running",
		}
		assert.Contains(t, err.Error(), "scheduler start")
		assert.Contains(t, err.Error(), "scheduler already running")
	})
}

func TestSchedulerError_Unwrap(t *testing.T) {
	cause := assert.AnError
	err := &SchedulerError{
		Op:      "test",
		Message: "test error",
		Cause:   cause,
	}
	assert.Equal(t, cause, err.Unwrap())
}

func TestBackupExecutionError_Error(t *testing.T) {
	t.Run("with cause", func(t *testing.T) {
		err := &BackupExecutionError{
			BackupType: BackupTypeFull,
			Schedule:   "* * * * *",
			Message:    "backup failed",
			Cause:      assert.AnError,
		}
		assert.Contains(t, err.Error(), "full")
		assert.Contains(t, err.Error(), "* * * * *")
		assert.Contains(t, err.Error(), "backup failed")
		assert.Contains(t, err.Error(), assert.AnError.Error())
	})

	t.Run("without cause", func(t *testing.T) {
		err := &BackupExecutionError{
			BackupType: BackupTypeIncremental,
			Schedule:   "0 2 * * *",
			Message:    "storage unavailable",
		}
		assert.Contains(t, err.Error(), "incremental")
		assert.Contains(t, err.Error(), "0 2 * * *")
		assert.Contains(t, err.Error(), "storage unavailable")
	})
}

func TestBackupExecutionError_Unwrap(t *testing.T) {
	cause := assert.AnError
	err := &BackupExecutionError{
		BackupType: BackupTypeFull,
		Schedule:   "* * * * *",
		Message:    "test error",
		Cause:      cause,
	}
	assert.Equal(t, cause, err.Unwrap())
}

func TestRetentionPolicyError_Error(t *testing.T) {
	t.Run("with cause", func(t *testing.T) {
		err := &RetentionPolicyError{
			Op:      "apply",
			Message: "failed to delete backup",
			Cause:   assert.AnError,
		}
		assert.Contains(t, err.Error(), "retention policy apply")
		assert.Contains(t, err.Error(), "failed to delete backup")
		assert.Contains(t, err.Error(), assert.AnError.Error())
	})

	t.Run("without cause", func(t *testing.T) {
		err := &RetentionPolicyError{
			Op:      "list",
			Message: "no backups found",
		}
		assert.Contains(t, err.Error(), "retention policy list")
		assert.Contains(t, err.Error(), "no backups found")
	})
}

func TestRetentionPolicyError_Unwrap(t *testing.T) {
	cause := assert.AnError
	err := &RetentionPolicyError{
		Op:      "test",
		Message: "test error",
		Cause:   cause,
	}
	assert.Equal(t, cause, err.Unwrap())
}

func TestManagerClosedError_Error(t *testing.T) {
	err := &ManagerClosedError{Op: "CreateBackup"}
	assert.Contains(t, err.Error(), "backup manager is closed")
	assert.Contains(t, err.Error(), "CreateBackup")
}

func TestInvalidConfigError_Error(t *testing.T) {
	t.Run("with cause", func(t *testing.T) {
		err := &InvalidConfigError{
			Field:   "cron_expression",
			Message: "invalid format",
			Cause:   assert.AnError,
		}
		assert.Contains(t, err.Error(), "cron_expression")
		assert.Contains(t, err.Error(), "invalid format")
		assert.Contains(t, err.Error(), assert.AnError.Error())
	})

	t.Run("without cause", func(t *testing.T) {
		err := &InvalidConfigError{
			Field:   "storage_backend",
			Message: "must be specified",
		}
		assert.Contains(t, err.Error(), "storage_backend")
		assert.Contains(t, err.Error(), "must be specified")
	})
}

func TestInvalidConfigError_Unwrap(t *testing.T) {
	cause := assert.AnError
	err := &InvalidConfigError{
		Field:   "test",
		Message: "test error",
		Cause:   cause,
	}
	assert.Equal(t, cause, err.Unwrap())
}

func TestNewBackupManager_WithScheduleEnabled(t *testing.T) {
	// Create temp work directory
	workDir, err := os.MkdirTemp("", "backup-manager-schedule-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(workDir)

	// Create logger
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	// Create memory storage
	memStorage := memory.New(log)
	mockStor := &mockStorage{MemoryStorage: memStorage}

	// Create mock raft node
	mockRaft := &mockRaftNode{shardCount: 1}

	// Create config with schedule enabled using mock backend
	config := &Config{
		StorageBackend: "mock", // Use mock backend for testing
		StorageConfig: map[string]string{
			"path":   workDir,
			"prefix": "backups",
		},
		DefaultCompression: CompressionNone,
		DefaultEncryption:  EncryptionNone,
		WorkDir:            workDir,
		Timeout:            30 * time.Minute,
		Schedule: &ScheduleConfig{
			Enabled:        true,
			CronExpression: "0 2 * * *", // Daily at 2am
			BackupType:     BackupTypeFull,
		},
	}

	manager, err := NewBackupManager(config, mockStor, mockRaft, log)
	require.NoError(t, err)
	defer manager.Close()

	// Verify scheduler started
	assert.True(t, manager.IsSchedulerRunning())

	// Verify next run is scheduled
	nextRun, ok := manager.GetNextScheduledBackup()
	assert.True(t, ok)
	assert.True(t, nextRun.After(time.Now()))
}

func TestNewBackupManager_WithInvalidSchedule(t *testing.T) {
	// Create temp work directory
	workDir, err := os.MkdirTemp("", "backup-manager-invalid-schedule-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(workDir)

	// Create logger
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	// Create memory storage
	memStorage := memory.New(log)
	mockStor := &mockStorage{MemoryStorage: memStorage}

	// Create mock raft node
	mockRaft := &mockRaftNode{shardCount: 1}

	// Create config with invalid schedule using mock backend
	config := &Config{
		StorageBackend: "mock", // Use mock backend for testing
		StorageConfig: map[string]string{
			"path":   workDir,
			"prefix": "backups",
		},
		DefaultCompression: CompressionNone,
		DefaultEncryption:  EncryptionNone,
		WorkDir:            workDir,
		Timeout:            30 * time.Minute,
		Schedule: &ScheduleConfig{
			Enabled:        true,
			CronExpression: "invalid-cron",
			BackupType:     BackupTypeFull,
		},
	}

	manager, err := NewBackupManager(config, mockStor, mockRaft, log)
	require.Error(t, err)
	assert.Nil(t, manager)

	var schedErr *SchedulerError
	assert.ErrorAs(t, err, &schedErr)
}

func TestScheduler_RunScheduledBackupsNoop(t *testing.T) {
	// Test that runScheduledBackups is a no-op for backward compatibility
	bm, _, cleanup := setupSchedulerTestManager(t, nil)
	defer cleanup()

	// Should not panic or cause any issues
	bm.runScheduledBackups()
}
