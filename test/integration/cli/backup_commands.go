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
	"fmt"
	"regexp"
	"strings"
	"time"
)

// =============================================================================
// Backup Commands
// =============================================================================

// backupTransportsToSkip defines which transports to skip for backup operations.
// Backup operations use REST API for remote execution via the CLI.
// QUIC transport does not have backup service implementations.
var backupTransportsToSkip = []Transport{TransportQUIC}

// backupIDTablePattern matches "Backup ID:" followed by spaces/tabs and the ID value
// on the same line in CLI table output. Uses [ \t]+ instead of \s+ to prevent matching
// across newlines when the Backup ID field is empty.
// Example: "Backup ID:  backup-abc123-def456"
var backupIDTablePattern = regexp.MustCompile(`Backup ID:[ \t]+(\S+)`)

// backupIDJSONPattern matches "backup_id" in JSON output.
// Example: "backup_id": "backup-abc123-def456"
var backupIDJSONPattern = regexp.MustCompile(`"backup_id"\s*:\s*"([^"]+)"`)

// ExtractBackupID extracts the server-generated backup ID from the create command output.
// The server generates UUID-based IDs (e.g., "backup-{uuid}") that differ from the
// user-provided backup name. This function parses both table and JSON output formats.
// Returns empty string if the backup ID cannot be extracted or the value is empty.
func ExtractBackupID(stdout string) string {
	// Try table format first: "Backup ID:  backup-{uuid}"
	if matches := backupIDTablePattern.FindStringSubmatch(stdout); len(matches) > 1 {
		value := matches[1]
		// Guard against matching a field label from an adjacent line. Field labels
		// end with a colon (e.g., "Type:") and are never valid backup IDs.
		if strings.HasSuffix(value, ":") {
			return ""
		}
		return value
	}
	// Try JSON format: "backup_id": "backup-{uuid}"
	if matches := backupIDJSONPattern.FindStringSubmatch(stdout); len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// BackupCreateCommand returns a command to create a backup with default options.
// Backup operations are supported via gRPC, REST, Unix, and MCP transports.
func BackupCreateCommand(backupName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("backup create %s", backupName),
		Args:           []string{"backup", "create", backupName},
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupName},
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupCreateIncrementalCommand returns a command to create an incremental backup.
func BackupCreateIncrementalCommand(backupName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("backup create incremental %s", backupName),
		Args:           []string{"backup", "create", backupName, "--incremental"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupName, "incremental"},
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupCreateWithCompressionCommand returns a command to create a backup with specific compression.
func BackupCreateWithCompressionCommand(backupName, compression string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("backup create %s with %s compression", backupName, compression),
		Args:           []string{"backup", "create", backupName, "--compression", compression},
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupName},
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupCreateWithEncryptionCommand returns a command to create an encrypted backup.
func BackupCreateWithEncryptionCommand(backupName, encryption, encryptKey string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("backup create encrypted %s", backupName),
		Args:           []string{"backup", "create", backupName, "--encryption", encryption, "--encrypt-key", encryptKey},
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupName},
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupCreateWithStoragePathCommand returns a command to create a backup to specific path.
func BackupCreateWithStoragePathCommand(backupName, storagePath string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("backup create %s to %s", backupName, storagePath),
		Args:           []string{"backup", "create", backupName, "--storage-path", storagePath},
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupName},
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupCreateWithAllOptionsCommand returns a command to create a backup with all options.
func BackupCreateWithAllOptionsCommand(backupName string, incremental bool, compression, encryption, encryptKey, storagePath string) CommandDef {
	args := []string{"backup", "create", backupName}
	if incremental {
		args = append(args, "--incremental")
	}
	if compression != "" {
		args = append(args, "--compression", compression)
	}
	if encryption != "" {
		args = append(args, "--encryption", encryption)
	}
	if encryptKey != "" {
		args = append(args, "--encrypt-key", encryptKey)
	}
	if storagePath != "" {
		args = append(args, "--storage-path", storagePath)
	}
	return CommandDef{
		Name:           fmt.Sprintf("backup create %s with all options", backupName),
		Args:           args,
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupName},
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupCreateErrorInvalidCompressionCommand returns a backup create command with invalid compression.
func BackupCreateErrorInvalidCompressionCommand(backupName string) CommandDef {
	return CommandDef{
		Name:           "backup create invalid compression",
		Args:           []string{"backup", "create", backupName, "--compression", "invalid"},
		ExpectSuccess:  false,
		ExpectError:    []string{"unsupported compression"},
		Timeout:        10 * time.Second,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupCreateErrorInvalidEncryptionCommand returns a backup create command with invalid encryption.
func BackupCreateErrorInvalidEncryptionCommand(backupName string) CommandDef {
	return CommandDef{
		Name:           "backup create invalid encryption",
		Args:           []string{"backup", "create", backupName, "--encryption", "invalid"},
		ExpectSuccess:  false,
		ExpectError:    []string{"unsupported encryption"},
		Timeout:        10 * time.Second,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupCreateErrorEncryptionWithoutKeyCommand returns a backup create command with encryption but no key.
func BackupCreateErrorEncryptionWithoutKeyCommand(backupName string) CommandDef {
	return CommandDef{
		Name:           "backup create encryption without key",
		Args:           []string{"backup", "create", backupName, "--encryption", "aes256"},
		ExpectSuccess:  false,
		ExpectError:    []string{"encryption key is required"},
		Timeout:        10 * time.Second,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupCreateErrorEncryptionInvalidKeyLengthCommand returns a backup create with wrong key length.
func BackupCreateErrorEncryptionInvalidKeyLengthCommand(backupName string) CommandDef {
	return CommandDef{
		Name:           "backup create encryption invalid key length",
		Args:           []string{"backup", "create", backupName, "--encryption", "aes256", "--encrypt-key", "short"},
		ExpectSuccess:  false,
		ExpectError:    []string{"32 bytes"},
		Timeout:        10 * time.Second,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupListCommand returns a command to list all backups.
func BackupListCommand() CommandDef {
	return CommandDef{
		Name:           "backup list",
		Args:           []string{"backup", "list"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupListWithLimitCommand returns a command to list backups with a limit.
func BackupListWithLimitCommand(limit int) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("backup list limit=%d", limit),
		Args:           []string{"backup", "list", "--limit", fmt.Sprintf("%d", limit)},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupListWithStoragePathCommand returns a command to list backups from specific path.
func BackupListWithStoragePathCommand(storagePath string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("backup list from %s", storagePath),
		Args:           []string{"backup", "list", "--storage-path", storagePath},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupListWithOutputFormatCommand returns a command to list backups with specific output format.
func BackupListWithOutputFormatCommand(format string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("backup list output=%s", format),
		Args:           []string{"backup", "list", "--output", format},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupStatusCommand returns a command to get backup status for a specific backup.
func BackupStatusCommand(backupID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("backup status %s", backupID),
		Args:           []string{"backup", "status", backupID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupID},
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupStatusGeneralCommand returns a command to get general backup status.
func BackupStatusGeneralCommand() CommandDef {
	return CommandDef{
		Name:           "backup status general",
		Args:           []string{"backup", "status"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupStatusWithStoragePathCommand returns a command to get backup status with storage path.
func BackupStatusWithStoragePathCommand(backupID, storagePath string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("backup status %s from %s", backupID, storagePath),
		Args:           []string{"backup", "status", backupID, "--storage-path", storagePath},
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupID},
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupRestoreCommand returns a command to restore from a backup.
func BackupRestoreCommand(backupID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("backup restore %s", backupID),
		Args:           []string{"backup", "restore", backupID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupID},
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupRestoreWithVerifyCommand returns a command to restore with verification option.
func BackupRestoreWithVerifyCommand(backupID string, verify bool) CommandDef {
	args := []string{"backup", "restore", backupID}
	if verify {
		args = append(args, "--verify")
	} else {
		args = append(args, "--verify=false")
	}
	return CommandDef{
		Name:           fmt.Sprintf("backup restore %s verify=%v", backupID, verify),
		Args:           args,
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupID},
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupRestoreWithSkipExistingCommand returns a command to restore with skip-existing option.
func BackupRestoreWithSkipExistingCommand(backupID string, skipExisting bool) CommandDef {
	args := []string{"backup", "restore", backupID}
	if skipExisting {
		args = append(args, "--skip-existing")
	}
	return CommandDef{
		Name:           fmt.Sprintf("backup restore %s skip-existing=%v", backupID, skipExisting),
		Args:           args,
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupID},
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupRestoreWithStoragePathCommand returns a command to restore from specific storage path.
func BackupRestoreWithStoragePathCommand(backupID, storagePath string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("backup restore %s from %s", backupID, storagePath),
		Args:           []string{"backup", "restore", backupID, "--storage-path", storagePath},
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupID},
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupRestoreWithAllOptionsCommand returns a command to restore with all options.
func BackupRestoreWithAllOptionsCommand(backupID, storagePath string, verify, skipExisting bool) CommandDef {
	args := []string{"backup", "restore", backupID}
	if storagePath != "" {
		args = append(args, "--storage-path", storagePath)
	}
	if verify {
		args = append(args, "--verify")
	} else {
		args = append(args, "--verify=false")
	}
	if skipExisting {
		args = append(args, "--skip-existing")
	}
	return CommandDef{
		Name:           fmt.Sprintf("backup restore %s with all options", backupID),
		Args:           args,
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupID},
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupRestoreErrorInvalidPointInTimeCommand returns a restore command with invalid point-in-time.
func BackupRestoreErrorInvalidPointInTimeCommand(backupID string) CommandDef {
	return CommandDef{
		Name:           "backup restore invalid point-in-time",
		Args:           []string{"backup", "restore", backupID, "--point-in-time", "invalid-timestamp"},
		ExpectSuccess:  false,
		ExpectError:    []string{"invalid point-in-time"},
		Timeout:        10 * time.Second,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupDeleteCommand returns a command to delete a backup.
func BackupDeleteCommand(backupID string, force bool) CommandDef {
	args := []string{"backup", "delete", backupID}
	if force {
		args = append(args, "--yes")
	}
	return CommandDef{
		Name:           fmt.Sprintf("backup delete %s", backupID),
		Args:           args,
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupID},
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupDeleteWithStoragePathCommand returns a command to delete from specific path.
func BackupDeleteWithStoragePathCommand(backupID, storagePath string, force bool) CommandDef {
	args := []string{"backup", "delete", backupID, "--storage-path", storagePath}
	if force {
		args = append(args, "--yes")
	}
	return CommandDef{
		Name:           fmt.Sprintf("backup delete %s from %s", backupID, storagePath),
		Args:           args,
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupID},
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupDeleteWithOutputFormatCommand returns a command to delete with specific output format.
func BackupDeleteWithOutputFormatCommand(backupID, format string, force bool) CommandDef {
	args := []string{"backup", "delete", backupID, "--output", format}
	if force {
		args = append(args, "--yes")
	}
	return CommandDef{
		Name:           fmt.Sprintf("backup delete %s output=%s", backupID, format),
		Args:           args,
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupID},
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupVerifyCommand returns a command to verify backup integrity.
func BackupVerifyCommand(backupID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("backup verify %s", backupID),
		Args:           []string{"backup", "verify", backupID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupID},
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupVerifyWithStoragePathCommand returns a command to verify from specific path.
func BackupVerifyWithStoragePathCommand(backupID, storagePath string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("backup verify %s from %s", backupID, storagePath),
		Args:           []string{"backup", "verify", backupID, "--storage-path", storagePath},
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupID},
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupVerifyWithEncryptKeyCommand returns a command to verify encrypted backup.
func BackupVerifyWithEncryptKeyCommand(backupID, encryptKey string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("backup verify encrypted %s", backupID),
		Args:           []string{"backup", "verify", backupID, "--encrypt-key", encryptKey},
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupID},
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupVerifyWithOutputFormatCommand returns a command to verify with specific output format.
func BackupVerifyWithOutputFormatCommand(backupID, format string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("backup verify %s output=%s", backupID, format),
		Args:           []string{"backup", "verify", backupID, "--output", format},
		ExpectSuccess:  true,
		ExpectOutput:   []string{backupID},
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: backupTransportsToSkip,
	}
}

// BackupHelpCommand returns a command to show backup help.
func BackupHelpCommand() CommandDef {
	return CommandDef{
		Name:          "backup help",
		Args:          []string{"backup", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"create", "list", "restore", "delete", "verify"},
		Timeout:       10 * time.Second,
	}
}

// BackupCreateHelpCommand returns a command to show backup create help.
func BackupCreateHelpCommand() CommandDef {
	return CommandDef{
		Name:          "backup create help",
		Args:          []string{"backup", "create", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"compression", "encryption", "incremental"},
		Timeout:       10 * time.Second,
	}
}

// BackupListHelpCommand returns a command to show backup list help.
func BackupListHelpCommand() CommandDef {
	return CommandDef{
		Name:          "backup list help",
		Args:          []string{"backup", "list", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"limit", "storage-path"},
		Timeout:       10 * time.Second,
	}
}

// BackupRestoreHelpCommand returns a command to show backup restore help.
func BackupRestoreHelpCommand() CommandDef {
	return CommandDef{
		Name:          "backup restore help",
		Args:          []string{"backup", "restore", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"verify", "skip-existing", "point-in-time"},
		Timeout:       10 * time.Second,
	}
}

// BackupDeleteHelpCommand returns a command to show backup delete help.
func BackupDeleteHelpCommand() CommandDef {
	return CommandDef{
		Name:          "backup delete help",
		Args:          []string{"backup", "delete", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"yes"},
		Timeout:       10 * time.Second,
	}
}

// BackupVerifyHelpCommand returns a command to show backup verify help.
func BackupVerifyHelpCommand() CommandDef {
	return CommandDef{
		Name:          "backup verify help",
		Args:          []string{"backup", "verify", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"storage-path", "encrypt-key"},
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Backup Test Suites
// =============================================================================

// BackupSuite returns backup commands for testing a complete backup lifecycle.
// The suite creates a backup, lists backups, then uses the extracted backup ID
// for status, verify, and delete operations. The caller must extract the backup
// ID from the create command output and pass it to the ID-dependent commands.
// This suite only includes commands that do NOT require the server-generated
// backup ID. Use BackupSuiteWithID for ID-dependent commands.
func BackupSuite(backupName string) []CommandDef {
	return []CommandDef{
		BackupCreateCommand(backupName),
		BackupListCommand(),
	}
}

// BackupSuiteWithID returns backup commands that require the server-generated
// backup ID. These commands must be run after extracting the ID from the
// create command output.
func BackupSuiteWithID(backupID string) []CommandDef {
	return []CommandDef{
		BackupStatusCommand(backupID),
		BackupVerifyCommand(backupID),
		BackupDeleteCommand(backupID, true),
	}
}

// BackupHelpSuite returns help commands for backup.
func BackupHelpSuite() []CommandDef {
	return []CommandDef{
		BackupHelpCommand(),
		BackupCreateHelpCommand(),
		BackupListHelpCommand(),
		BackupRestoreHelpCommand(),
		BackupDeleteHelpCommand(),
		BackupVerifyHelpCommand(),
	}
}

// BackupErrorSuite returns error case commands for backup.
func BackupErrorSuite(backupName string) []CommandDef {
	return []CommandDef{
		BackupCreateErrorInvalidCompressionCommand(backupName),
		BackupCreateErrorInvalidEncryptionCommand(backupName),
		BackupCreateErrorEncryptionWithoutKeyCommand(backupName),
		BackupCreateErrorEncryptionInvalidKeyLengthCommand(backupName),
		BackupRestoreErrorInvalidPointInTimeCommand(backupName),
	}
}
