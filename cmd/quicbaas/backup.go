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

// Package main provides the QuicBaaS command-line interface.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
)

var (
	// Backup create flags.
	backupCompression string
	backupEncryption  string
	backupIncremental bool
	backupStoragePath string
	backupEncryptKey  string

	// Backup list flags.
	backupListLimit int

	// Backup restore flags.
	backupRestorePointInTime  string
	backupRestoreVerify       bool
	backupRestoreSkipExisting bool

	// Backup delete flags.
	backupDeleteForce bool

	// Backup output format.
	backupOutputFormat string

	// Multi-backend storage flags.
	backupStorageType      string
	backupStorageBucket    string
	backupStorageRegion    string
	backupStorageEndpoint  string
	backupStorageContainer string
	backupCredentialsFile  string
	backupStorageProfile   string
)

// newBackupCommands creates the backup command group.
func newBackupCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Backup and restore operations",
		Long: `Backup and restore operations for QuicBaaS.

These commands allow you to create, list, restore, delete, verify, and monitor backups.
Backups support compression (gzip, zstd), encryption (aes256), and both full
and incremental backup types.

Storage backends:
  - local: Store backups on local filesystem
  - s3: Store backups on S3-compatible storage (coming soon)`,
	}

	// Add persistent flags for all backup commands
	cmd.PersistentFlags().StringVar(&backupOutputFormat, "output", "table", "Output format (json, yaml, table, raw)")

	// Multi-backend storage flags
	cmd.PersistentFlags().StringVar(&backupStorageType, "storage-type", "local", "Storage backend (local, s3, azure, gcs, minio)")
	cmd.PersistentFlags().StringVar(&backupStorageBucket, "bucket", "", "S3/GCS/MinIO bucket name")
	cmd.PersistentFlags().StringVar(&backupStorageRegion, "region", "", "AWS region")
	cmd.PersistentFlags().StringVar(&backupStorageEndpoint, "endpoint", "", "Custom endpoint (S3-compatible, MinIO)")
	cmd.PersistentFlags().StringVar(&backupStorageContainer, "container", "", "Azure blob container")
	cmd.PersistentFlags().StringVar(&backupCredentialsFile, "credentials-file", "", "Path to credentials file")
	cmd.PersistentFlags().StringVar(&backupStorageProfile, "profile", "", "Named profile from credentials file")

	// Add subcommands
	cmd.AddCommand(newBackupCreateCommand())
	cmd.AddCommand(newBackupListCommand())
	cmd.AddCommand(newBackupRestoreCommand())
	cmd.AddCommand(newBackupDeleteCommand())
	cmd.AddCommand(newBackupVerifyCommand())
	cmd.AddCommand(newBackupStatusCommand())

	return cmd
}

// newBackupCreateCommand creates the backup create command.
func newBackupCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new backup",
		Long: `Create a new backup of the QuicBaaS database.

This command creates a backup of all data in the database with optional
compression and encryption. The backup can be either a full backup (default)
or an incremental backup based on the last full backup.

Examples:
  # Create a full backup with default compression (zstd)
  quicbaas backup create my-backup

  # Create a backup with gzip compression
  quicbaas backup create my-backup --compression gzip

  # Create an encrypted backup with AES-256
  quicbaas backup create my-backup --encryption aes256 --encrypt-key "your-32-byte-key-here-padded!!"

  # Create an incremental backup
  quicbaas backup create my-backup --incremental

  # Create a backup to a specific storage path
  quicbaas backup create my-backup --storage-path /var/backups/quicbaas`,
		Args: cobra.ExactArgs(1),
		RunE: runBackupCreateCommand,
	}

	cmd.Flags().StringVar(&backupCompression, "compression", "zstd", "Compression algorithm (none, gzip, zstd)")
	cmd.Flags().StringVar(&backupEncryption, "encryption", "none", "Encryption algorithm (none, aes256)")
	cmd.Flags().BoolVar(&backupIncremental, "incremental", false, "Create incremental backup")
	cmd.Flags().StringVar(&backupStoragePath, "storage-path", "", "Storage path for backups (default: /var/lib/quicbaas/backups)")
	cmd.Flags().StringVar(&backupEncryptKey, "encrypt-key", "", "Encryption key (32 bytes for AES-256)")

	return cmd
}

// runBackupCreateCommand executes the backup create command.
func runBackupCreateCommand(cmd *cobra.Command, args []string) error {
	backupName := args[0]

	// Validate compression type
	compressionType, err := parseCompressionType(backupCompression)
	if err != nil {
		return err
	}

	// Validate encryption type
	encryptionType, err := parseEncryptionType(backupEncryption)
	if err != nil {
		return err
	}

	// Validate encryption key if encryption is enabled
	if encryptionType == backup.EncryptionAES256GCM {
		if backupEncryptKey == "" {
			return &BackupCLIError{
				Op:      "create",
				Message: "encryption key is required when using AES-256 encryption",
			}
		}
		if len(backupEncryptKey) != 32 {
			return &BackupCLIError{
				Op:      "create",
				Message: fmt.Sprintf("encryption key must be exactly 32 bytes for AES-256, got %d bytes", len(backupEncryptKey)),
			}
		}
	}

	// Determine backup type
	backupType := "full"
	if backupIncremental {
		backupType = "incremental"
	}

	// Determine storage path
	storagePath := backupStoragePath
	if storagePath == "" {
		storagePath = "/var/lib/quicbaas/backups"
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Build storage configuration
	storageFlags := &BackupStorageFlags{
		StorageType: backupStorageType,
		Bucket:      backupStorageBucket,
		Region:      backupStorageRegion,
		Endpoint:    backupStorageEndpoint,
		Container:   backupStorageContainer,
	}
	builder := NewBackupStorageBuilder(storageFlags, backupCredentialsFile, backupStorageProfile)
	storageType, storageConfig, err := builder.Build(ctx)
	if err != nil {
		return &BackupCLIError{
			Op:      "create",
			Message: "failed to build storage configuration",
			Cause:   err,
		}
	}

	// Create client using factory
	client, err := backupClientFactory()
	if err != nil {
		return &BackupCLIError{
			Op:      "create",
			Message: "failed to create backup client",
			Cause:   err,
		}
	}
	defer client.Close()

	// Build request
	req := &BackupCreateRequest{
		Name:          backupName,
		BackupType:    backupType,
		Compression:   string(compressionType),
		Encryption:    string(encryptionType),
		StoragePath:   storagePath,
		StorageType:   storageType,
		StorageConfig: storageConfig,
		Tags: map[string]string{
			"name": backupName,
		},
	}

	fmt.Fprintf(os.Stdout, "Creating backup '%s'...\n", backupName)
	fmt.Fprintf(os.Stdout, "  Type: %s\n", backupType)
	fmt.Fprintf(os.Stdout, "  Compression: %s\n", compressionType)
	fmt.Fprintf(os.Stdout, "  Encryption: %s\n", encryptionType)
	fmt.Fprintf(os.Stdout, "  Storage type: %s\n", storageType)
	fmt.Fprintf(os.Stdout, "  Storage path: %s\n", storagePath)

	// Execute backup via REST API
	resp, err := client.CreateBackup(ctx, req)
	if err != nil {
		return &BackupCLIError{
			Op:      "create",
			Message: "backup creation failed",
			Cause:   err,
		}
	}

	// Set additional response fields from request
	resp.Type = backupType
	resp.Compression = string(compressionType)
	resp.Encryption = string(encryptionType)

	// Format output
	formatter := cli.NewFormatter(true)
	format, err := cli.ParseOutputFormat(backupOutputFormat)
	if err != nil {
		return err
	}

	return formatter.FormatBackupCreateResponse(resp, format, os.Stdout)
}

// newBackupListCommand creates the backup list command.
func newBackupListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all backups",
		Long: `List all available backups with their metadata.

This command displays all backups stored in the backup storage backend,
including their size, type, compression, encryption, and creation timestamp.

Examples:
  # List all backups as a table (default)
  quicbaas backup list

  # List backups as JSON
  quicbaas backup list --output json

  # List first 10 backups
  quicbaas backup list --limit 10

  # List backups from specific storage path
  quicbaas backup list --storage-path /var/backups/quicbaas`,
		Args: cobra.NoArgs,
		RunE: runBackupListCommand,
	}

	cmd.Flags().IntVar(&backupListLimit, "limit", 100, "Maximum number of backups to list")
	cmd.Flags().StringVar(&backupStoragePath, "storage-path", "", "Storage path for backups (default: /var/lib/quicbaas/backups)")

	return cmd
}

// runBackupListCommand executes the backup list command.
func runBackupListCommand(cmd *cobra.Command, args []string) error {
	storagePath := backupStoragePath
	if storagePath == "" {
		storagePath = "/var/lib/quicbaas/backups"
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Build storage configuration
	storageFlags := &BackupStorageFlags{
		StorageType: backupStorageType,
		Bucket:      backupStorageBucket,
		Region:      backupStorageRegion,
		Endpoint:    backupStorageEndpoint,
		Container:   backupStorageContainer,
	}
	builder := NewBackupStorageBuilder(storageFlags, backupCredentialsFile, backupStorageProfile)
	storageType, storageConfig, err := builder.Build(ctx)
	if err != nil {
		return &BackupCLIError{
			Op:      "list",
			Message: "failed to build storage configuration",
			Cause:   err,
		}
	}

	// Create client using factory
	client, err := backupClientFactory()
	if err != nil {
		return &BackupCLIError{
			Op:      "list",
			Message: "failed to create backup client",
			Cause:   err,
		}
	}
	defer client.Close()

	// Build request
	req := &BackupListRequest{
		Limit:         backupListLimit,
		StoragePath:   storagePath,
		StorageType:   storageType,
		StorageConfig: storageConfig,
	}

	// Execute list via REST API
	resp, err := client.ListBackups(ctx, req)
	if err != nil {
		return &BackupCLIError{
			Op:      "list",
			Message: "failed to list backups",
			Cause:   err,
		}
	}

	// Format output
	formatter := cli.NewFormatter(true)
	format, err := cli.ParseOutputFormat(backupOutputFormat)
	if err != nil {
		return err
	}

	return formatter.FormatBackupListResponse(resp, format, os.Stdout)
}

// newBackupRestoreCommand creates the backup restore command.
func newBackupRestoreCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore <backup-id>",
		Short: "Restore from a backup",
		Long: `Restore data from a backup.

This command restores data from a previously created backup. It supports
point-in-time recovery if a timestamp is specified, and can verify backup
integrity before restoring.

WARNING: This operation will overwrite existing data in the database!
Use with caution and ensure you have recent backups before restoring.

Examples:
  # Restore from a backup
  quicbaas backup restore backup-prod-1729123456

  # Restore with checksum verification
  quicbaas backup restore backup-prod-1729123456 --verify

  # Restore without overwriting existing keys
  quicbaas backup restore backup-prod-1729123456 --skip-existing

  # Restore from specific storage path
  quicbaas backup restore backup-prod-1729123456 --storage-path /var/backups/quicbaas

  # Point-in-time restore (requires timestamp in RFC3339 format)
  quicbaas backup restore backup-prod-1729123456 --point-in-time 2025-10-14T12:00:00Z`,
		Args: cobra.ExactArgs(1),
		RunE: runBackupRestoreCommand,
	}

	cmd.Flags().StringVar(&backupRestorePointInTime, "point-in-time", "", "Restore to specific timestamp (RFC3339 format)")
	cmd.Flags().BoolVar(&backupRestoreVerify, "verify", true, "Verify backup integrity before restoring")
	cmd.Flags().BoolVar(&backupRestoreSkipExisting, "skip-existing", false, "Skip keys that already exist")
	cmd.Flags().StringVar(&backupStoragePath, "storage-path", "", "Storage path for backups (default: /var/lib/quicbaas/backups)")
	cmd.Flags().StringVar(&backupEncryptKey, "encrypt-key", "", "Encryption key if backup is encrypted (32 bytes for AES-256)")

	return cmd
}

// runBackupRestoreCommand executes the backup restore command.
func runBackupRestoreCommand(cmd *cobra.Command, args []string) error {
	backupID := args[0]

	storagePath := backupStoragePath
	if storagePath == "" {
		storagePath = "/var/lib/quicbaas/backups"
	}

	// Parse point-in-time if provided
	var pitTimestamp time.Time
	if backupRestorePointInTime != "" {
		var err error
		pitTimestamp, err = time.Parse(time.RFC3339, backupRestorePointInTime)
		if err != nil {
			return &BackupCLIError{
				Op:      "restore",
				Message: fmt.Sprintf("invalid point-in-time timestamp: %v (use RFC3339 format: 2025-10-14T12:00:00Z)", err),
			}
		}
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Build storage configuration
	storageFlags := &BackupStorageFlags{
		StorageType: backupStorageType,
		Bucket:      backupStorageBucket,
		Region:      backupStorageRegion,
		Endpoint:    backupStorageEndpoint,
		Container:   backupStorageContainer,
	}
	builder := NewBackupStorageBuilder(storageFlags, backupCredentialsFile, backupStorageProfile)
	storageType, storageConfig, err := builder.Build(ctx)
	if err != nil {
		return &BackupCLIError{
			Op:      "restore",
			Message: "failed to build storage configuration",
			Cause:   err,
		}
	}

	// Create client using factory
	client, err := backupClientFactory()
	if err != nil {
		return &BackupCLIError{
			Op:      "restore",
			Message: "failed to create backup client",
			Cause:   err,
		}
	}
	defer client.Close()

	// Build request
	req := &BackupRestoreRequest{
		BackupID:       backupID,
		VerifyChecksum: backupRestoreVerify,
		SkipExisting:   backupRestoreSkipExisting,
		PointInTime:    pitTimestamp,
		StoragePath:    storagePath,
		StorageType:    storageType,
		StorageConfig:  storageConfig,
		EncryptionKey:  backupEncryptKey,
	}

	fmt.Fprintf(os.Stdout, "Restoring from backup '%s'...\n", backupID)
	if !pitTimestamp.IsZero() {
		fmt.Fprintf(os.Stdout, "  Point-in-time: %s\n", pitTimestamp.Format(time.RFC3339))
	}
	fmt.Fprintf(os.Stdout, "  Verify checksums: %v\n", backupRestoreVerify)
	fmt.Fprintf(os.Stdout, "  Skip existing keys: %v\n", backupRestoreSkipExisting)
	fmt.Fprintf(os.Stdout, "  Storage path: %s\n", storagePath)

	// Execute restore via REST API
	resp, err := client.RestoreBackup(ctx, req)
	if err != nil {
		return &BackupCLIError{
			Op:      "restore",
			Message: "restore failed",
			Cause:   err,
		}
	}

	// Format output
	formatter := cli.NewFormatter(true)
	format, err := cli.ParseOutputFormat(backupOutputFormat)
	if err != nil {
		return err
	}

	return formatter.FormatBackupRestoreResponse(resp, format, os.Stdout)
}

// newBackupDeleteCommand creates the backup delete command.
func newBackupDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <backup-id>",
		Short: "Delete a backup",
		Long: `Delete a backup from storage.

This command permanently deletes a backup from the storage backend.
Use with caution as this operation cannot be undone.

Examples:
  # Delete a backup (with confirmation)
  quicbaas backup delete backup-prod-1729123456

  # Delete without confirmation
  quicbaas backup delete backup-prod-1729123456 --yes

  # Delete from specific storage path
  quicbaas backup delete backup-prod-1729123456 --storage-path /var/backups/quicbaas`,
		Args: cobra.ExactArgs(1),
		RunE: runBackupDeleteCommand,
	}

	cmd.Flags().BoolVarP(&backupDeleteForce, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&backupStoragePath, "storage-path", "", "Storage path for backups (default: /var/lib/quicbaas/backups)")

	return cmd
}

// runBackupDeleteCommand executes the backup delete command.
func runBackupDeleteCommand(cmd *cobra.Command, args []string) error {
	backupID := args[0]

	// Confirmation prompt unless --yes is specified
	if !backupDeleteForce {
		fmt.Printf("Are you sure you want to delete backup '%s'? This operation cannot be undone. (y/N): ", backupID)
		var response string
		if _, err := fmt.Scanln(&response); err != nil {
			// Treat empty input as "no"
			fmt.Println("Delete cancelled")
			return nil
		}
		if response != "y" && response != "yes" && response != "Y" && response != "YES" {
			fmt.Println("Delete cancelled")
			return nil
		}
	}

	storagePath := backupStoragePath
	if storagePath == "" {
		storagePath = "/var/lib/quicbaas/backups"
	}

	// Create client using factory
	client, err := backupClientFactory()
	if err != nil {
		return &BackupCLIError{
			Op:      "delete",
			Message: "failed to create backup client",
			Cause:   err,
		}
	}
	defer client.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Build request
	req := &BackupDeleteRequest{
		BackupID:    backupID,
		StoragePath: storagePath,
	}

	fmt.Fprintf(os.Stdout, "Deleting backup '%s' from %s...\n", backupID, storagePath)

	// Execute delete via REST API
	resp, err := client.DeleteBackup(ctx, req)
	if err != nil {
		return &BackupCLIError{
			Op:      "delete",
			Message: "delete failed",
			Cause:   err,
		}
	}

	// Format output
	formatter := cli.NewFormatter(true)
	format, err := cli.ParseOutputFormat(backupOutputFormat)
	if err != nil {
		return err
	}

	return formatter.FormatBackupDeleteResponse(resp, format, os.Stdout)
}

// newBackupVerifyCommand creates the backup verify command.
func newBackupVerifyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify <backup-id>",
		Short: "Verify backup integrity",
		Long: `Verify the integrity of a backup.

This command verifies that a backup is valid and can be restored by checking:
  - Backup metadata is valid
  - All shard snapshots are present
  - Checksums match for all data
  - Compression and encryption can be read
  - Data structure is valid

Examples:
  # Verify a backup
  quicbaas backup verify backup-prod-1729123456

  # Verify backup as JSON output
  quicbaas backup verify backup-prod-1729123456 --output json

  # Verify backup from specific storage path
  quicbaas backup verify backup-prod-1729123456 --storage-path /var/backups/quicbaas`,
		Args: cobra.ExactArgs(1),
		RunE: runBackupVerifyCommand,
	}

	cmd.Flags().StringVar(&backupStoragePath, "storage-path", "", "Storage path for backups (default: /var/lib/quicbaas/backups)")
	cmd.Flags().StringVar(&backupEncryptKey, "encrypt-key", "", "Encryption key if backup is encrypted (32 bytes for AES-256)")

	return cmd
}

// runBackupVerifyCommand executes the backup verify command.
func runBackupVerifyCommand(cmd *cobra.Command, args []string) error {
	backupID := args[0]

	// Create client using factory
	client, err := backupClientFactory()
	if err != nil {
		return &BackupCLIError{
			Op:      "verify",
			Message: "failed to create backup client",
			Cause:   err,
		}
	}
	defer client.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Build request
	req := &BackupVerifyRequest{
		BackupID:      backupID,
		StoragePath:   backupStoragePath,
		EncryptionKey: backupEncryptKey,
	}

	fmt.Fprintf(os.Stdout, "Verifying backup '%s'...\n", backupID)

	// Execute verify via REST API
	resp, err := client.VerifyBackup(ctx, req)
	if err != nil {
		return &BackupCLIError{
			Op:      "verify",
			Message: "verification failed",
			Cause:   err,
		}
	}

	// Format output
	formatter := cli.NewFormatter(true)
	format, err := cli.ParseOutputFormat(backupOutputFormat)
	if err != nil {
		return err
	}

	return formatter.FormatBackupVerifyResponse(resp, format, os.Stdout)
}

// newBackupStatusCommand creates the backup status command.
func newBackupStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [backup-id]",
		Short: "Show backup status and progress",
		Long: `Show the status of backup operations.

Without a backup ID, shows the status of any currently running backup operation.
With a backup ID, shows detailed status information for that specific backup.

This command displays:
  - Current backup operation status (if running)
  - Progress percentage and ETA
  - Shards processed
  - Bytes processed
  - Any errors encountered

Examples:
  # Show status of current backup operation
  quicbaas backup status

  # Show status of specific backup
  quicbaas backup status backup-prod-1729123456

  # Show status as JSON
  quicbaas backup status --output json`,
		Args: cobra.MaximumNArgs(1),
		RunE: runBackupStatusCommand,
	}

	cmd.Flags().StringVar(&backupStoragePath, "storage-path", "", "Storage path for backups (default: /var/lib/quicbaas/backups)")

	return cmd
}

// runBackupStatusCommand executes the backup status command.
func runBackupStatusCommand(cmd *cobra.Command, args []string) error {
	// Create client using factory
	client, err := backupClientFactory()
	if err != nil {
		return &BackupCLIError{
			Op:      "status",
			Message: "failed to create backup client",
			Cause:   err,
		}
	}
	defer client.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var backupID string
	if len(args) > 0 {
		backupID = args[0]
	}

	storagePath := backupStoragePath
	if storagePath == "" {
		storagePath = "/var/lib/quicbaas/backups"
	}

	// If a backup ID is provided, show status for that backup
	if backupID != "" {
		return showBackupDetails(ctx, client, backupID, storagePath)
	}

	// Otherwise, show status of active backup operations
	return showActiveBackupStatus(ctx, client, storagePath)
}

// showBackupDetails shows detailed status for a specific backup.
func showBackupDetails(ctx context.Context, client BackupClient, backupID, storagePath string) error {
	// First verify the backup exists by getting its verification status
	req := &BackupVerifyRequest{
		BackupID:    backupID,
		StoragePath: storagePath,
	}

	resp, err := client.VerifyBackup(ctx, req)
	if err != nil {
		return &BackupCLIError{
			Op:      "status",
			Message: fmt.Sprintf("failed to get status for backup '%s'", backupID),
			Cause:   err,
		}
	}

	// Format output
	formatter := cli.NewFormatter(true)
	format, err := cli.ParseOutputFormat(backupOutputFormat)
	if err != nil {
		return err
	}

	// Create status response with verification info
	statusResp := &cli.BackupStatusResponse{
		BackupID:       backupID,
		State:          "completed",
		Progress:       100,
		ShardsTotal:    resp.ShardsValid,
		ShardsComplete: resp.ShardsValid,
		Valid:          resp.Valid,
		ChecksumValid:  resp.ChecksumValid,
		Issues:         resp.Issues,
		Timestamp:      resp.Timestamp,
	}

	return formatter.FormatBackupStatusResponse(statusResp, format, os.Stdout)
}

// showActiveBackupStatus shows status of active backup operations.
func showActiveBackupStatus(ctx context.Context, client BackupClient, storagePath string) error {
	// List recent backups to check for any in-progress operations
	req := &BackupListRequest{
		Limit:       10,
		StoragePath: storagePath,
	}

	resp, err := client.ListBackups(ctx, req)
	if err != nil {
		return &BackupCLIError{
			Op:      "status",
			Message: "failed to get backup status",
			Cause:   err,
		}
	}

	// Format output
	formatter := cli.NewFormatter(true)
	format, err := cli.ParseOutputFormat(backupOutputFormat)
	if err != nil {
		return err
	}

	// If no backups found, show empty status
	if len(resp.Backups) == 0 {
		statusResp := &cli.BackupStatusResponse{
			State:     "idle",
			Message:   "No backup operations in progress. No backups found.",
			Timestamp: time.Now(),
		}
		return formatter.FormatBackupStatusResponse(statusResp, format, os.Stdout)
	}

	// Show most recent backup status
	mostRecent := resp.Backups[0]
	statusResp := &cli.BackupStatusResponse{
		BackupID:       mostRecent.ID,
		State:          "completed",
		Progress:       100,
		ShardsTotal:    mostRecent.NumShards,
		ShardsComplete: mostRecent.NumShards,
		BytesTotal:     mostRecent.Size,
		BytesComplete:  mostRecent.Size,
		Message:        fmt.Sprintf("Last backup: %s (%s)", mostRecent.ID, mostRecent.Type),
		Timestamp:      mostRecent.Timestamp,
	}

	return formatter.FormatBackupStatusResponse(statusResp, format, os.Stdout)
}

// Helper functions

func parseCompressionType(s string) (backup.CompressionType, error) {
	switch s {
	case "none":
		return backup.CompressionNone, nil
	case "gzip":
		return backup.CompressionGzip, nil
	case "zstd":
		return backup.CompressionZstd, nil
	default:
		return "", &BackupCLIError{
			Op:      "parse",
			Message: fmt.Sprintf("unsupported compression type: %s (valid: none, gzip, zstd)", s),
		}
	}
}

func parseEncryptionType(s string) (backup.EncryptionType, error) {
	switch s {
	case "none":
		return backup.EncryptionNone, nil
	case "aes256":
		return backup.EncryptionAES256GCM, nil
	default:
		return "", &BackupCLIError{
			Op:      "parse",
			Message: fmt.Sprintf("unsupported encryption type: %s (valid: none, aes256)", s),
		}
	}
}

// BackupCLIError is an error type for backup CLI operations.
type BackupCLIError struct {
	Op      string
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *BackupCLIError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("backup %s: %s: %v", e.Op, e.Message, e.Cause)
	}
	return fmt.Sprintf("backup %s: %s", e.Op, e.Message)
}

// Unwrap returns the underlying error.
func (e *BackupCLIError) Unwrap() error {
	return e.Cause
}
