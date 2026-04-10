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

//go:build integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// resolveBackupID extracts the server-generated backup ID from create command
// output. The server generates UUID-based IDs (e.g., "backup-{uuid}") that
// differ from the user-provided name. Falls back to the user-provided name
// if extraction fails (e.g., when a transport does not return parseable output).
func resolveBackupID(t *testing.T, stdout, fallbackName string) string {
	t.Helper()
	backupID := ExtractBackupID(stdout)
	if backupID == "" {
		t.Logf("Could not extract backup ID from output, falling back to name: %s", fallbackName)
		return fallbackName
	}
	t.Logf("Extracted backup ID: %s (user name: %s)", backupID, fallbackName)
	return backupID
}

// runBackupCreate executes a backup create command using Run() instead of
// RunAndAssert(), extracts the server-generated backup ID from the output,
// and performs success assertions. Returns the real backup ID for use in
// subsequent operations. Falls back to backupName if ID extraction fails.
func runBackupCreate(t *testing.T, runner *TestRunner, cmd CommandDef, backupName string) string {
	t.Helper()

	// Skip check: if the transport is skipped for this command, return the
	// fallback name so callers can proceed without error.
	if runner.IsSkipped(cmd) {
		t.Logf("Command %q not supported for this transport, skipping", cmd.Name)
		return backupName
	}

	stdout, stderr, err := runner.Run(cmd)
	require.NoError(t, err, "backup create should succeed: stdout=%s stderr=%s", stdout, stderr)
	return resolveBackupID(t, stdout, backupName)
}

// =============================================================================
// Backup Lifecycle Tests
// =============================================================================

// TestBackupLifecycle tests the complete backup lifecycle across all transports.
// This includes create, list, status, verify, and delete operations.
func TestBackupLifecycle(t *testing.T) {
	RunForAllTransports(t, "BackupLifecycle", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		backupName := UniqueKey("test-backup")

		// Run create and list (no ID needed)
		createCmd := BackupCreateCommand(backupName)
		backupID := runBackupCreate(t, runner, createCmd, backupName)

		listCmd := BackupListCommand()
		runner.RunAndAssert(listCmd)

		// Run ID-dependent commands with the real backup ID
		idSuite := BackupSuiteWithID(backupID)
		runner.RunSuite(idSuite)
	})
}

// =============================================================================
// Backup Create Tests
// =============================================================================

// TestBackupCreate tests creating a backup with default options across all transports.
func TestBackupCreate(t *testing.T) {
	RunForAllTransports(t, "BackupCreate", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		backupName := UniqueKey("create-backup")
		cmd := BackupCreateCommand(backupName)
		backupID := runBackupCreate(t, runner, cmd, backupName)

		// Cleanup
		cleanup := BackupDeleteCommand(backupID, true)
		runner.Run(cleanup)
	})
}

// TestBackupCreateIncremental tests creating an incremental backup across all transports.
func TestBackupCreateIncremental(t *testing.T) {
	RunForAllTransports(t, "BackupCreateIncremental", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		// First create a full backup
		fullBackupName := UniqueKey("full-backup")
		fullCmd := BackupCreateCommand(fullBackupName)
		fullBackupID := runBackupCreate(t, runner, fullCmd, fullBackupName)

		// Then create an incremental backup
		incBackupName := UniqueKey("inc-backup")
		incCmd := BackupCreateIncrementalCommand(incBackupName)
		incBackupID := runBackupCreate(t, runner, incCmd, incBackupName)

		// Cleanup both
		cleanup1 := BackupDeleteCommand(fullBackupID, true)
		runner.Run(cleanup1)
		cleanup2 := BackupDeleteCommand(incBackupID, true)
		runner.Run(cleanup2)
	})
}

// TestBackupCreateWithGzipCompression tests creating a backup with gzip compression.
func TestBackupCreateWithGzipCompression(t *testing.T) {
	RunForAllTransports(t, "BackupCreateGzip", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		backupName := UniqueKey("gzip-backup")
		cmd := BackupCreateWithCompressionCommand(backupName, "gzip")
		backupID := runBackupCreate(t, runner, cmd, backupName)

		// Cleanup
		cleanup := BackupDeleteCommand(backupID, true)
		runner.Run(cleanup)
	})
}

// TestBackupCreateWithZstdCompression tests creating a backup with zstd compression.
func TestBackupCreateWithZstdCompression(t *testing.T) {
	RunForAllTransports(t, "BackupCreateZstd", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		backupName := UniqueKey("zstd-backup")
		cmd := BackupCreateWithCompressionCommand(backupName, "zstd")
		backupID := runBackupCreate(t, runner, cmd, backupName)

		// Cleanup
		cleanup := BackupDeleteCommand(backupID, true)
		runner.Run(cleanup)
	})
}

// TestBackupCreateWithNoCompression tests creating a backup without compression.
func TestBackupCreateWithNoCompression(t *testing.T) {
	RunForAllTransports(t, "BackupCreateNoCompression", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		backupName := UniqueKey("nocompress-backup")
		cmd := BackupCreateWithCompressionCommand(backupName, "none")
		backupID := runBackupCreate(t, runner, cmd, backupName)

		// Cleanup
		cleanup := BackupDeleteCommand(backupID, true)
		runner.Run(cleanup)
	})
}

// TestBackupCreateWithEncryption tests creating an encrypted backup.
func TestBackupCreateWithEncryption(t *testing.T) {
	RunForAllTransports(t, "BackupCreateEncrypted", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		backupName := UniqueKey("encrypted-backup")
		// AES-256 requires exactly 32 bytes
		encryptKey := "12345678901234567890123456789012"

		cmd := BackupCreateWithEncryptionCommand(backupName, "aes256", encryptKey)
		backupID := runBackupCreate(t, runner, cmd, backupName)

		// Cleanup
		cleanup := BackupDeleteCommand(backupID, true)
		runner.Run(cleanup)
	})
}

// TestBackupCreateWithStoragePath tests creating a backup to a specific storage path.
func TestBackupCreateWithStoragePath(t *testing.T) {
	RunForAllTransports(t, "BackupCreateStoragePath", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		backupName := UniqueKey("path-backup")
		storagePath := "/tmp/quicbaas-test-backups"

		cmd := BackupCreateWithStoragePathCommand(backupName, storagePath)
		backupID := runBackupCreate(t, runner, cmd, backupName)

		// Cleanup
		cleanup := BackupDeleteWithStoragePathCommand(backupID, storagePath, true)
		runner.Run(cleanup)
	})
}

// TestBackupCreateWithAllOptions tests creating a backup with all options combined.
func TestBackupCreateWithAllOptions(t *testing.T) {
	RunForAllTransports(t, "BackupCreateAllOptions", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		backupName := UniqueKey("allopts-backup")
		encryptKey := "12345678901234567890123456789012"
		storagePath := "/tmp/quicbaas-test-backups"

		cmd := BackupCreateWithAllOptionsCommand(
			backupName,
			true,   // incremental
			"zstd", // compression
			"aes256",
			encryptKey,
			storagePath,
		)
		backupID := runBackupCreate(t, runner, cmd, backupName)

		// Cleanup
		cleanup := BackupDeleteWithStoragePathCommand(backupID, storagePath, true)
		runner.Run(cleanup)
	})
}

// =============================================================================
// Backup Create Error Cases
// =============================================================================

// TestBackupCreateInvalidCompression tests that invalid compression type fails.
func TestBackupCreateInvalidCompression(t *testing.T) {
	RunForAllTransports(t, "BackupCreateInvalidCompression", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		backupName := UniqueKey("invalid-compress")
		cmd := BackupCreateErrorInvalidCompressionCommand(backupName)
		runner.RunAndAssert(cmd)
	})
}

// TestBackupCreateInvalidEncryption tests that invalid encryption type fails.
func TestBackupCreateInvalidEncryption(t *testing.T) {
	RunForAllTransports(t, "BackupCreateInvalidEncryption", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		backupName := UniqueKey("invalid-encrypt")
		cmd := BackupCreateErrorInvalidEncryptionCommand(backupName)
		runner.RunAndAssert(cmd)
	})
}

// TestBackupCreateEncryptionWithoutKey tests that encryption without key fails.
func TestBackupCreateEncryptionWithoutKey(t *testing.T) {
	RunForAllTransports(t, "BackupCreateEncryptionNoKey", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		backupName := UniqueKey("encrypt-nokey")
		cmd := BackupCreateErrorEncryptionWithoutKeyCommand(backupName)
		runner.RunAndAssert(cmd)
	})
}

// TestBackupCreateEncryptionInvalidKeyLength tests that encryption with wrong key length fails.
func TestBackupCreateEncryptionInvalidKeyLength(t *testing.T) {
	RunForAllTransports(t, "BackupCreateEncryptionBadKey", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		backupName := UniqueKey("encrypt-badkey")
		cmd := BackupCreateErrorEncryptionInvalidKeyLengthCommand(backupName)
		runner.RunAndAssert(cmd)
	})
}

// =============================================================================
// Backup List Tests
// =============================================================================

// TestBackupList tests listing backups across all transports.
func TestBackupList(t *testing.T) {
	RunForAllTransports(t, "BackupList", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		// Create a backup first
		backupName := UniqueKey("list-backup")
		createCmd := BackupCreateCommand(backupName)
		backupID := runBackupCreate(t, runner, createCmd, backupName)

		// List backups
		listCmd := BackupListCommand()
		runner.RunAndAssert(listCmd)

		// Cleanup
		cleanup := BackupDeleteCommand(backupID, true)
		runner.Run(cleanup)
	})
}

// TestBackupListWithLimit tests listing backups with a limit.
func TestBackupListWithLimit(t *testing.T) {
	RunForAllTransports(t, "BackupListWithLimit", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		// Create multiple backups and track their real IDs
		backupNames := []string{
			UniqueKey("limit-backup-1"),
			UniqueKey("limit-backup-2"),
			UniqueKey("limit-backup-3"),
		}
		backupIDs := make([]string, len(backupNames))

		for i, name := range backupNames {
			createCmd := BackupCreateCommand(name)
			backupIDs[i] = runBackupCreate(t, runner, createCmd, name)
		}

		// List with limit
		listCmd := BackupListWithLimitCommand(2)
		runner.RunAndAssert(listCmd)

		// Cleanup
		for _, id := range backupIDs {
			cleanup := BackupDeleteCommand(id, true)
			runner.Run(cleanup)
		}
	})
}

// TestBackupListWithStoragePath tests listing backups from specific storage path.
func TestBackupListWithStoragePath(t *testing.T) {
	RunForAllTransports(t, "BackupListWithStoragePath", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		storagePath := "/tmp/quicbaas-test-backups"

		// Create a backup first
		backupName := UniqueKey("listpath-backup")
		createCmd := BackupCreateWithStoragePathCommand(backupName, storagePath)
		backupID := runBackupCreate(t, runner, createCmd, backupName)

		// List backups from specific path
		listCmd := BackupListWithStoragePathCommand(storagePath)
		runner.RunAndAssert(listCmd)

		// Cleanup
		cleanup := BackupDeleteWithStoragePathCommand(backupID, storagePath, true)
		runner.Run(cleanup)
	})
}

// TestBackupListWithOutputFormats tests listing backups with different output formats.
func TestBackupListWithOutputFormats(t *testing.T) {
	RunForAllTransports(t, "BackupListOutputFormats", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		// Create a backup first
		backupName := UniqueKey("format-backup")
		createCmd := BackupCreateCommand(backupName)
		backupID := runBackupCreate(t, runner, createCmd, backupName)

		// Test different output formats
		formats := []string{"table", "json", "yaml"}
		for _, format := range formats {
			t.Run(format, func(t *testing.T) {
				listCmd := BackupListWithOutputFormatCommand(format)
				runner.RunAndAssert(listCmd)
			})
		}

		// Cleanup
		cleanup := BackupDeleteCommand(backupID, true)
		runner.Run(cleanup)
	})
}

// =============================================================================
// Backup Status/Get Tests
// =============================================================================

// TestBackupStatus tests getting backup status across all transports.
func TestBackupStatus(t *testing.T) {
	RunForAllTransports(t, "BackupStatus", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		// Create a backup first
		backupName := UniqueKey("status-backup")
		createCmd := BackupCreateCommand(backupName)
		backupID := runBackupCreate(t, runner, createCmd, backupName)

		// Get backup status
		statusCmd := BackupStatusCommand(backupID)
		runner.RunAndAssert(statusCmd)

		// Cleanup
		cleanup := BackupDeleteCommand(backupID, true)
		runner.Run(cleanup)
	})
}

// TestBackupStatusNoBackupID tests getting general backup status without specific ID.
func TestBackupStatusNoBackupID(t *testing.T) {
	RunForAllTransports(t, "BackupStatusGeneral", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		// Get general backup status
		statusCmd := BackupStatusGeneralCommand()
		runner.RunAndAssert(statusCmd)
	})
}

// TestBackupStatusWithStoragePath tests getting backup status with storage path.
func TestBackupStatusWithStoragePath(t *testing.T) {
	RunForAllTransports(t, "BackupStatusWithPath", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		storagePath := "/tmp/quicbaas-test-backups"

		// Create a backup first
		backupName := UniqueKey("statuspath-backup")
		createCmd := BackupCreateWithStoragePathCommand(backupName, storagePath)
		backupID := runBackupCreate(t, runner, createCmd, backupName)

		// Get backup status
		statusCmd := BackupStatusWithStoragePathCommand(backupID, storagePath)
		runner.RunAndAssert(statusCmd)

		// Cleanup
		cleanup := BackupDeleteWithStoragePathCommand(backupID, storagePath, true)
		runner.Run(cleanup)
	})
}

// =============================================================================
// Backup Restore Tests
// =============================================================================

// TestBackupRestore tests restoring from a backup across all transports.
func TestBackupRestore(t *testing.T) {
	RunForAllTransports(t, "BackupRestore", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		// Create a backup first
		backupName := UniqueKey("restore-backup")
		createCmd := BackupCreateCommand(backupName)
		backupID := runBackupCreate(t, runner, createCmd, backupName)

		// Restore from backup
		restoreCmd := BackupRestoreCommand(backupID)
		runner.RunAndAssert(restoreCmd)

		// Cleanup
		cleanup := BackupDeleteCommand(backupID, true)
		runner.Run(cleanup)
	})
}

// TestBackupRestoreWithVerify tests restoring with verification enabled.
func TestBackupRestoreWithVerify(t *testing.T) {
	RunForAllTransports(t, "BackupRestoreWithVerify", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		// Create a backup first
		backupName := UniqueKey("restorev-backup")
		createCmd := BackupCreateCommand(backupName)
		backupID := runBackupCreate(t, runner, createCmd, backupName)

		// Restore with verification
		restoreCmd := BackupRestoreWithVerifyCommand(backupID, true)
		runner.RunAndAssert(restoreCmd)

		// Cleanup
		cleanup := BackupDeleteCommand(backupID, true)
		runner.Run(cleanup)
	})
}

// TestBackupRestoreWithoutVerify tests restoring with verification disabled.
func TestBackupRestoreWithoutVerify(t *testing.T) {
	RunForAllTransports(t, "BackupRestoreNoVerify", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		// Create a backup first
		backupName := UniqueKey("restorenov-backup")
		createCmd := BackupCreateCommand(backupName)
		backupID := runBackupCreate(t, runner, createCmd, backupName)

		// Restore without verification
		restoreCmd := BackupRestoreWithVerifyCommand(backupID, false)
		runner.RunAndAssert(restoreCmd)

		// Cleanup
		cleanup := BackupDeleteCommand(backupID, true)
		runner.Run(cleanup)
	})
}

// TestBackupRestoreWithSkipExisting tests restoring with skip-existing option.
func TestBackupRestoreWithSkipExisting(t *testing.T) {
	RunForAllTransports(t, "BackupRestoreSkipExisting", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		// Create a backup first
		backupName := UniqueKey("restoresk-backup")
		createCmd := BackupCreateCommand(backupName)
		backupID := runBackupCreate(t, runner, createCmd, backupName)

		// Restore with skip-existing
		restoreCmd := BackupRestoreWithSkipExistingCommand(backupID, true)
		runner.RunAndAssert(restoreCmd)

		// Cleanup
		cleanup := BackupDeleteCommand(backupID, true)
		runner.Run(cleanup)
	})
}

// TestBackupRestoreWithStoragePath tests restoring from specific storage path.
func TestBackupRestoreWithStoragePath(t *testing.T) {
	RunForAllTransports(t, "BackupRestoreStoragePath", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		storagePath := "/tmp/quicbaas-test-backups"

		// Create a backup first
		backupName := UniqueKey("restorep-backup")
		createCmd := BackupCreateWithStoragePathCommand(backupName, storagePath)
		backupID := runBackupCreate(t, runner, createCmd, backupName)

		// Restore from specific path
		restoreCmd := BackupRestoreWithStoragePathCommand(backupID, storagePath)
		runner.RunAndAssert(restoreCmd)

		// Cleanup
		cleanup := BackupDeleteWithStoragePathCommand(backupID, storagePath, true)
		runner.Run(cleanup)
	})
}

// TestBackupRestoreWithAllOptions tests restoring with all options combined.
func TestBackupRestoreWithAllOptions(t *testing.T) {
	RunForAllTransports(t, "BackupRestoreAllOptions", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		storagePath := "/tmp/quicbaas-test-backups"

		// Create a backup first
		backupName := UniqueKey("restoreall-backup")
		createCmd := BackupCreateWithStoragePathCommand(backupName, storagePath)
		backupID := runBackupCreate(t, runner, createCmd, backupName)

		// Restore with all options
		restoreCmd := BackupRestoreWithAllOptionsCommand(
			backupID,
			storagePath,
			true,  // verify
			false, // skip-existing
		)
		runner.RunAndAssert(restoreCmd)

		// Cleanup
		cleanup := BackupDeleteWithStoragePathCommand(backupID, storagePath, true)
		runner.Run(cleanup)
	})
}

// =============================================================================
// Backup Restore Error Cases
// =============================================================================

// TestBackupRestoreInvalidPointInTime tests that invalid point-in-time timestamp fails.
func TestBackupRestoreInvalidPointInTime(t *testing.T) {
	RunForAllTransports(t, "BackupRestoreInvalidPIT", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		// Create a backup first
		backupName := UniqueKey("restorepiterr-backup")
		createCmd := BackupCreateCommand(backupName)
		backupID := runBackupCreate(t, runner, createCmd, backupName)

		// Try to restore with invalid point-in-time
		restoreCmd := BackupRestoreErrorInvalidPointInTimeCommand(backupID)
		runner.RunAndAssert(restoreCmd)

		// Cleanup
		cleanup := BackupDeleteCommand(backupID, true)
		runner.Run(cleanup)
	})
}

// =============================================================================
// Backup Delete Tests
// =============================================================================

// TestBackupDelete tests deleting a backup with force flag across all transports.
func TestBackupDelete(t *testing.T) {
	RunForAllTransports(t, "BackupDelete", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		// Create a backup first
		backupName := UniqueKey("delete-backup")
		createCmd := BackupCreateCommand(backupName)
		backupID := runBackupCreate(t, runner, createCmd, backupName)

		// Delete with force
		deleteCmd := BackupDeleteCommand(backupID, true)
		runner.RunAndAssert(deleteCmd)
	})
}

// TestBackupDeleteWithStoragePath tests deleting from specific storage path.
func TestBackupDeleteWithStoragePath(t *testing.T) {
	RunForAllTransports(t, "BackupDeleteStoragePath", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		storagePath := "/tmp/quicbaas-test-backups"

		// Create a backup first
		backupName := UniqueKey("deletep-backup")
		createCmd := BackupCreateWithStoragePathCommand(backupName, storagePath)
		backupID := runBackupCreate(t, runner, createCmd, backupName)

		// Delete from specific path
		deleteCmd := BackupDeleteWithStoragePathCommand(backupID, storagePath, true)
		runner.RunAndAssert(deleteCmd)
	})
}

// TestBackupDeleteWithOutputFormats tests deleting with different output formats.
func TestBackupDeleteWithOutputFormats(t *testing.T) {
	RunForAllTransports(t, "BackupDeleteOutputFormats", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		formats := []string{"table", "json", "yaml"}

		for _, format := range formats {
			t.Run(format, func(t *testing.T) {
				// Create a backup
				backupName := UniqueKey("deletefmt-backup")
				createCmd := BackupCreateCommand(backupName)
				backupID := runBackupCreate(t, runner, createCmd, backupName)

				// Delete with format
				deleteCmd := BackupDeleteWithOutputFormatCommand(backupID, format, true)
				runner.RunAndAssert(deleteCmd)
			})
		}
	})
}

// =============================================================================
// Backup Verify Tests
// =============================================================================

// TestBackupVerify tests verifying backup integrity across all transports.
func TestBackupVerify(t *testing.T) {
	RunForAllTransports(t, "BackupVerify", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		// Create a backup first
		backupName := UniqueKey("verify-backup")
		createCmd := BackupCreateCommand(backupName)
		backupID := runBackupCreate(t, runner, createCmd, backupName)

		// Verify backup
		verifyCmd := BackupVerifyCommand(backupID)
		runner.RunAndAssert(verifyCmd)

		// Cleanup
		cleanup := BackupDeleteCommand(backupID, true)
		runner.Run(cleanup)
	})
}

// TestBackupVerifyWithStoragePath tests verifying from specific storage path.
func TestBackupVerifyWithStoragePath(t *testing.T) {
	RunForAllTransports(t, "BackupVerifyStoragePath", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		storagePath := "/tmp/quicbaas-test-backups"

		// Create a backup first
		backupName := UniqueKey("verifyp-backup")
		createCmd := BackupCreateWithStoragePathCommand(backupName, storagePath)
		backupID := runBackupCreate(t, runner, createCmd, backupName)

		// Verify from specific path
		verifyCmd := BackupVerifyWithStoragePathCommand(backupID, storagePath)
		runner.RunAndAssert(verifyCmd)

		// Cleanup
		cleanup := BackupDeleteWithStoragePathCommand(backupID, storagePath, true)
		runner.Run(cleanup)
	})
}

// TestBackupVerifyEncrypted tests verifying an encrypted backup.
func TestBackupVerifyEncrypted(t *testing.T) {
	RunForAllTransports(t, "BackupVerifyEncrypted", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		encryptKey := "12345678901234567890123456789012"

		// Create an encrypted backup first
		backupName := UniqueKey("verifye-backup")
		createCmd := BackupCreateWithEncryptionCommand(backupName, "aes256", encryptKey)
		backupID := runBackupCreate(t, runner, createCmd, backupName)

		// Verify encrypted backup
		verifyCmd := BackupVerifyWithEncryptKeyCommand(backupID, encryptKey)
		runner.RunAndAssert(verifyCmd)

		// Cleanup
		cleanup := BackupDeleteCommand(backupID, true)
		runner.Run(cleanup)
	})
}

// TestBackupVerifyWithOutputFormats tests verifying with different output formats.
func TestBackupVerifyWithOutputFormats(t *testing.T) {
	RunForAllTransports(t, "BackupVerifyOutputFormats", func(t *testing.T, runner *TestRunner) {
		runner.RequireBackupAvailable()

		// Create a backup first
		backupName := UniqueKey("verifyfmt-backup")
		createCmd := BackupCreateCommand(backupName)
		backupID := runBackupCreate(t, runner, createCmd, backupName)

		// Test different output formats
		formats := []string{"table", "json", "yaml"}
		for _, format := range formats {
			t.Run(format, func(t *testing.T) {
				verifyCmd := BackupVerifyWithOutputFormatCommand(backupID, format)
				runner.RunAndAssert(verifyCmd)
			})
		}

		// Cleanup
		cleanup := BackupDeleteCommand(backupID, true)
		runner.Run(cleanup)
	})
}

// =============================================================================
// Backup Help Tests
// =============================================================================

// TestBackupHelp tests the backup help command.
func TestBackupHelp(t *testing.T) {
	RunForAllTransports(t, "BackupHelp", func(t *testing.T, runner *TestRunner) {
		cmd := BackupHelpCommand()
		runner.RunAndAssert(cmd)
	})
}

// TestBackupCreateHelp tests the backup create help command.
func TestBackupCreateHelp(t *testing.T) {
	RunForAllTransports(t, "BackupCreateHelp", func(t *testing.T, runner *TestRunner) {
		cmd := BackupCreateHelpCommand()
		runner.RunAndAssert(cmd)
	})
}

// TestBackupListHelp tests the backup list help command.
func TestBackupListHelp(t *testing.T) {
	RunForAllTransports(t, "BackupListHelp", func(t *testing.T, runner *TestRunner) {
		cmd := BackupListHelpCommand()
		runner.RunAndAssert(cmd)
	})
}

// TestBackupRestoreHelp tests the backup restore help command.
func TestBackupRestoreHelp(t *testing.T) {
	RunForAllTransports(t, "BackupRestoreHelp", func(t *testing.T, runner *TestRunner) {
		cmd := BackupRestoreHelpCommand()
		runner.RunAndAssert(cmd)
	})
}

// TestBackupDeleteHelp tests the backup delete help command.
func TestBackupDeleteHelp(t *testing.T) {
	RunForAllTransports(t, "BackupDeleteHelp", func(t *testing.T, runner *TestRunner) {
		cmd := BackupDeleteHelpCommand()
		runner.RunAndAssert(cmd)
	})
}

// TestBackupVerifyHelp tests the backup verify help command.
func TestBackupVerifyHelp(t *testing.T) {
	RunForAllTransports(t, "BackupVerifyHelp", func(t *testing.T, runner *TestRunner) {
		cmd := BackupVerifyHelpCommand()
		runner.RunAndAssert(cmd)
	})
}
