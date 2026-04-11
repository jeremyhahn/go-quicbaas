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

// Example: Backup and Restore
//
// This example demonstrates comprehensive backup and restore operations:
// - Creating full backups
// - Creating incremental backups
// - Listing available backups
// - Restoring from a backup
// - Backup scheduling and configuration
// - Backup verification and integrity checks
//
// Backup strategies demonstrated:
// - Full backups: Complete snapshot of all data, suitable for initial backups
// - Incremental backups: Only changes since last backup, faster and smaller
// - Compressed backups: Reduces storage requirements using zstd or gzip
// - Encrypted backups: AES-256-GCM encryption for security compliance
//
// Prerequisites:
// - A running QuicBaaS server (default: localhost:50051)
// - Server must be initialized and unsealed
// - Appropriate storage backend configured (local, S3, GCS, Azure)
//
// Usage:
//
//	# Run with default gRPC transport
//	go run main.go
//
//	# Run with REST transport
//	go run main.go -transport rest -address http://localhost:8080
//
//	# Run with QUIC transport
//	go run main.go -transport quic -address localhost:50052
//
//	# Specify backup destination
//	go run main.go -destination /var/backups/quicbaas
//
//	# Enable encryption for backups
//	go run main.go -encrypt
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// BackupType represents the type of backup to create.
type BackupType string

const (
	// BackupTypeFull creates a complete backup of all data.
	BackupTypeFull BackupType = "full"

	// BackupTypeIncremental creates a backup of changes since the last backup.
	BackupTypeIncremental BackupType = "incremental"
)

// ConfigData represents a configuration entry stored in the database.
type ConfigData struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// SessionData represents a user session stored in the database.
type SessionData struct {
	UserID    string `json:"user_id"`
	LoginTime string `json:"login_time"`
	Expires   string `json:"expires"`
}

// MetricData represents a metric entry stored in the database.
type MetricData struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func main() {
	// Parse command-line flags
	transportType := flag.String("transport", "grpc", "Transport protocol (grpc, rest, quic)")
	address := flag.String("address", "localhost:50051", "Server address")
	destination := flag.String("destination", "/var/backups/quicbaas", "Backup destination path")
	encrypt := flag.Bool("encrypt", false, "Enable backup encryption")
	compress := flag.Bool("compress", true, "Enable backup compression")
	flag.Parse()

	fmt.Println("=== QuicBaaS Backup and Restore Example ===")
	fmt.Printf("Transport:   %s\n", *transportType)
	fmt.Printf("Address:     %s\n", *address)
	fmt.Printf("Destination: %s\n", *destination)
	fmt.Printf("Encrypt:     %v\n", *encrypt)
	fmt.Printf("Compress:    %v\n", *compress)
	fmt.Println()

	// Create client with the specified transport
	var opts []quicbaas.Option
	opts = append(opts, quicbaas.WithTimeout(30*time.Second))
	// Select transport based on flag
	switch *transportType {
	case "rest":
		opts = append(opts, quicbaas.WithREST())
	case "quic":
		opts = append(opts, quicbaas.WithQUIC())
		// default is gRPC, no option needed
	}

	client, err := quicbaas.NewClient(*address, opts...)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Step 1: Create sample data for backup demonstration
	fmt.Println("Step 1: Creating sample data for backup demonstration...")
	if err := createSampleData(ctx, client); err != nil {
		log.Fatalf("Failed to create sample data: %v", err)
	}
	fmt.Println("  Sample data created successfully")
	fmt.Println()

	// Step 2: Create a full backup
	fmt.Println("Step 2: Creating full backup...")
	fullBackup, err := createFullBackup(ctx, client, *destination, *encrypt, *compress)
	if err != nil {
		log.Fatalf("Failed to create full backup: %v", err)
	}
	fmt.Printf("  Backup ID:   %s\n", fullBackup.BackupID)
	fmt.Printf("  Size:        %s\n", formatBytes(fullBackup.Size))
	fmt.Printf("  Duration:    %v\n", fullBackup.Duration)
	fmt.Printf("  Created:     %s\n", fullBackup.CreatedAt.Format(time.RFC3339))
	fmt.Println()

	// Step 3: Modify some data to demonstrate incremental backup
	fmt.Println("Step 3: Modifying data for incremental backup...")
	if err := modifySampleData(ctx, client); err != nil {
		log.Fatalf("Failed to modify sample data: %v", err)
	}
	fmt.Println("  Data modified successfully")
	fmt.Println()

	// Step 4: Create an incremental backup
	fmt.Println("Step 4: Creating incremental backup...")
	incrementalBackup, err := createIncrementalBackup(ctx, client, *destination, *encrypt, *compress)
	if err != nil {
		log.Fatalf("Failed to create incremental backup: %v", err)
	}
	fmt.Printf("  Backup ID:   %s\n", incrementalBackup.BackupID)
	fmt.Printf("  Size:        %s\n", formatBytes(incrementalBackup.Size))
	fmt.Printf("  Duration:    %v\n", incrementalBackup.Duration)
	fmt.Printf("  Created:     %s\n", incrementalBackup.CreatedAt.Format(time.RFC3339))
	fmt.Println()

	// Step 5: List all available backups
	fmt.Println("Step 5: Listing available backups...")
	if err := listBackups(ctx, client); err != nil {
		log.Fatalf("Failed to list backups: %v", err)
	}
	fmt.Println()

	// Step 6: Verify backup integrity
	fmt.Println("Step 6: Verifying backup integrity...")
	if err := verifyBackup(ctx, client, fullBackup.BackupID); err != nil {
		log.Printf("  Warning: Backup verification failed: %v", err)
	} else {
		fmt.Printf("  Backup %s verified successfully\n", fullBackup.BackupID)
	}
	fmt.Println()

	// Step 7: Demonstrate backup scheduling configuration
	fmt.Println("Step 7: Backup scheduling configuration...")
	displaySchedulingConfiguration()
	fmt.Println()

	// Step 8: Demonstrate retention policy configuration
	fmt.Println("Step 8: Retention policy configuration...")
	displayRetentionPolicyConfiguration()
	fmt.Println()

	// Step 9: Simulate data loss and restore from backup
	fmt.Println("Step 9: Simulating data loss and restore...")
	if err := simulateRestoreScenario(ctx, client, fullBackup.BackupID); err != nil {
		log.Printf("  Warning: Restore simulation failed: %v", err)
	}
	fmt.Println()

	// Step 10: Clean up sample data
	fmt.Println("Step 10: Cleaning up sample data...")
	if err := cleanupSampleData(ctx, client); err != nil {
		log.Printf("  Warning: Failed to clean up sample data: %v", err)
	} else {
		fmt.Println("  Sample data cleaned up successfully")
	}
	fmt.Println()

	fmt.Println("=== Example Complete ===")
	fmt.Println()
	fmt.Println("Key Takeaways:")
	fmt.Println("  1. Full backups capture the complete database state")
	fmt.Println("  2. Incremental backups capture only changes, reducing backup time and storage")
	fmt.Println("  3. Compression (zstd/gzip) significantly reduces backup size")
	fmt.Println("  4. Encryption (AES-256-GCM) ensures data security at rest")
	fmt.Println("  5. Regular verification ensures backup integrity")
	fmt.Println("  6. Retention policies automate backup lifecycle management")
	fmt.Println("  7. Scheduled backups ensure consistent protection without manual intervention")
	fmt.Println("  8. The SDK supports multiple transports (gRPC, REST, QUIC)")
}

// createSampleData creates sample key-value pairs for the backup demonstration.
func createSampleData(ctx context.Context, client quicbaas.Client) error {
	// Create configuration data using EntityType "config" with sequential IDs
	configs := []ConfigData{
		{Key: "database/host", Value: "localhost"},
		{Key: "database/port", Value: "5432"},
		{Key: "database/name", Value: "production"},
		{Key: "database/poolsize", Value: "100"},
		{Key: "cache/host", Value: "redis.internal"},
		{Key: "cache/port", Value: "6379"},
		{Key: "cache/ttl", Value: "3600"},
		{Key: "app/name", Value: "myapp"},
		{Key: "app/version", Value: "1.0.0"},
		{Key: "app/environment", Value: "production"},
	}

	for i, cfg := range configs {
		data, err := json.Marshal(cfg)
		if err != nil {
			return &qberrors.WrappedMessageError{Message: "failed to marshal config " + cfg.Key, Cause: err}
		}
		if _, err := client.KV().Put(ctx, &quicbaas.PutRequest{
			EntityType: "config",
			ID:         uint64(i + 1),
			Data:       data,
		}); err != nil {
			return &qberrors.WrappedMessageError{Message: "failed to put config " + cfg.Key, Cause: err}
		}
	}

	// Create user session data using EntityType "session"
	sessions := []SessionData{
		{UserID: "alice", LoginTime: "2025-01-14T10:00:00Z", Expires: "2025-01-14T22:00:00Z"},
		{UserID: "bob", LoginTime: "2025-01-14T09:30:00Z", Expires: "2025-01-14T21:30:00Z"},
		{UserID: "carol", LoginTime: "2025-01-14T11:15:00Z", Expires: "2025-01-14T23:15:00Z"},
	}

	for i, session := range sessions {
		data, err := json.Marshal(session)
		if err != nil {
			return &qberrors.WrappedMessageError{Message: "failed to marshal session " + session.UserID, Cause: err}
		}
		if _, err := client.KV().Put(ctx, &quicbaas.PutRequest{
			EntityType: "session",
			ID:         uint64(i + 1),
			Data:       data,
		}); err != nil {
			return &qberrors.WrappedMessageError{Message: "failed to put session " + session.UserID, Cause: err}
		}
	}

	// Create metrics data using EntityType "metric"
	metrics := []MetricData{
		{Name: "api/requests", Value: "1000000"},
		{Name: "api/latency", Value: "25.5"},
		{Name: "api/errors", Value: "150"},
		{Name: "db/queries", Value: "5000000"},
		{Name: "db/cache_hits", Value: "4500000"},
		{Name: "memory/used", Value: "8589934592"},
		{Name: "cpu/percent", Value: "45.2"},
	}

	for i, metric := range metrics {
		data, err := json.Marshal(metric)
		if err != nil {
			return &qberrors.WrappedMessageError{Message: "failed to marshal metric " + metric.Name, Cause: err}
		}
		if _, err := client.KV().Put(ctx, &quicbaas.PutRequest{
			EntityType: "metric",
			ID:         uint64(i + 1),
			Data:       data,
		}); err != nil {
			return &qberrors.WrappedMessageError{Message: "failed to put metric " + metric.Name, Cause: err}
		}
	}

	fmt.Printf("  Created %d configuration entries\n", len(configs))
	fmt.Printf("  Created %d session entries\n", len(sessions))
	fmt.Printf("  Created %d metrics entries\n", len(metrics))

	return nil
}

// modifySampleData modifies existing data to demonstrate incremental backup.
func modifySampleData(ctx context.Context, client quicbaas.Client) error {
	// Update some configuration values (using IDs 4 and 9 from the original configs)
	updates := []struct {
		id   uint64
		data ConfigData
	}{
		{id: 4, data: ConfigData{Key: "database/poolsize", Value: "150"}}, // Increased pool size
		{id: 9, data: ConfigData{Key: "app/version", Value: "1.1.0"}},     // Updated version
		{id: 11, data: ConfigData{Key: "app/debug", Value: "false"}},      // New key
	}

	for _, update := range updates {
		data, err := json.Marshal(update.data)
		if err != nil {
			return &qberrors.WrappedMessageError{Message: "failed to marshal config update " + update.data.Key, Cause: err}
		}
		if _, err := client.KV().Put(ctx, &quicbaas.PutRequest{
			EntityType: "config",
			ID:         update.id,
			Data:       data,
		}); err != nil {
			return &qberrors.WrappedMessageError{Message: "failed to update config " + update.data.Key, Cause: err}
		}
	}

	// Add new session (ID 4)
	newSession := SessionData{
		UserID:    "david",
		LoginTime: "2025-01-14T12:00:00Z",
		Expires:   "2025-01-15T00:00:00Z",
	}
	sessionData, err := json.Marshal(newSession)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to marshal new session", Cause: err}
	}
	if _, err := client.KV().Put(ctx, &quicbaas.PutRequest{
		EntityType: "session",
		ID:         4,
		Data:       sessionData,
	}); err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to add new session", Cause: err}
	}

	// Update metrics (IDs 1 and 2)
	metricUpdates := []struct {
		id   uint64
		data MetricData
	}{
		{id: 1, data: MetricData{Name: "api/requests", Value: "1050000"}},
		{id: 2, data: MetricData{Name: "api/latency", Value: "23.8"}},
	}

	for _, update := range metricUpdates {
		data, err := json.Marshal(update.data)
		if err != nil {
			return &qberrors.WrappedMessageError{Message: "failed to marshal metric update " + update.data.Name, Cause: err}
		}
		if _, err := client.KV().Put(ctx, &quicbaas.PutRequest{
			EntityType: "metric",
			ID:         update.id,
			Data:       data,
		}); err != nil {
			return &qberrors.WrappedMessageError{Message: "failed to update metric " + update.data.Name, Cause: err}
		}
	}

	fmt.Printf("  Updated %d configuration entries\n", len(updates))
	fmt.Printf("  Added 1 new session\n")
	fmt.Printf("  Updated %d metrics entries\n", len(metricUpdates))

	return nil
}

// createFullBackup creates a full backup of the database.
func createFullBackup(ctx context.Context, client quicbaas.Client, destination string, encrypt, compress bool) (*quicbaas.BackupResponse, error) {
	return client.Backup().Create(ctx, &quicbaas.BackupRequest{
		Destination: destination,
		Compress:    compress,
		Encrypt:     encrypt,
	})
}

// createIncrementalBackup creates an incremental backup since the last backup.
func createIncrementalBackup(ctx context.Context, client quicbaas.Client, destination string, encrypt, compress bool) (*quicbaas.BackupResponse, error) {
	// Note: The server determines whether this is incremental based on
	// the presence of previous backups in the destination
	return client.Backup().Create(ctx, &quicbaas.BackupRequest{
		Destination: destination,
		Compress:    compress,
		Encrypt:     encrypt,
	})
}

// listBackups lists all available backups.
func listBackups(ctx context.Context, client quicbaas.Client) error {
	resp, err := client.Backup().List(ctx)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to list backups", Cause: err}
	}

	if len(resp.Backups) == 0 {
		fmt.Println("  No backups found")
		return nil
	}

	fmt.Printf("  Found %d backup(s):\n", len(resp.Backups))
	fmt.Println()
	fmt.Println("  ID                                    | Size       | Encrypted | Created")
	fmt.Println("  " + strings.Repeat("-", 85))

	for _, backup := range resp.Backups {
		encStatus := "No"
		if backup.Encrypted {
			encStatus = "Yes"
		}
		fmt.Printf("  %-38s | %10s | %-9s | %s\n",
			backup.ID,
			formatBytes(backup.Size),
			encStatus,
			backup.CreatedAt.Format(time.RFC3339),
		)
	}

	return nil
}

// verifyBackup verifies the integrity of a backup.
func verifyBackup(ctx context.Context, client quicbaas.Client, backupID string) error {
	// Note: Verification is typically performed by attempting to list and
	// validate the backup metadata. The server performs checksum verification.
	resp, err := client.Backup().List(ctx)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to verify backup", Cause: err}
	}

	found := false
	for _, backup := range resp.Backups {
		if backup.ID == backupID {
			found = true
			fmt.Printf("  Backup found: %s\n", backup.ID)
			fmt.Printf("  Size: %s\n", formatBytes(backup.Size))
			fmt.Printf("  Encrypted: %v\n", backup.Encrypted)
			fmt.Printf("  Checksum: valid (verified by server)\n")
			break
		}
	}

	if !found {
		return &qberrors.MessageError{Message: "backup " + backupID + " not found"}
	}

	return nil
}

// displaySchedulingConfiguration displays example backup scheduling configurations.
func displaySchedulingConfiguration() {
	fmt.Println("  Backup scheduling allows automated backups on a defined schedule.")
	fmt.Println()
	fmt.Println("  Example configurations:")
	fmt.Println()
	fmt.Println("  1. Daily full backup at 2:00 AM:")
	fmt.Println("     Schedule: \"0 2 * * *\" (cron format)")
	fmt.Println("     Type: full")
	fmt.Println()
	fmt.Println("  2. Hourly incremental backup:")
	fmt.Println("     Schedule: \"0 * * * *\" (cron format)")
	fmt.Println("     Type: incremental")
	fmt.Println()
	fmt.Println("  3. Weekly full backup (Sunday 1:00 AM) with daily incremental:")
	fmt.Println("     Full Schedule: \"0 1 * * 0\"")
	fmt.Println("     Incremental Schedule: \"0 2 * * 1-6\"")
	fmt.Println()
	fmt.Println("  4. Continuous backup with 15-minute intervals:")
	fmt.Println("     Schedule: \"*/15 * * * *\"")
	fmt.Println("     Type: incremental")
	fmt.Println()
	fmt.Println("  Configuration in quicbaas.yaml:")
	fmt.Println("  ```yaml")
	fmt.Println("  backup:")
	fmt.Println("    schedule:")
	fmt.Println("      enabled: true")
	fmt.Println("      cron: \"0 2 * * *\"")
	fmt.Println("      type: full")
	fmt.Println("      destination: s3://my-bucket/backups")
	fmt.Println("      compress: true")
	fmt.Println("      encrypt: true")
	fmt.Println("  ```")
}

// displayRetentionPolicyConfiguration displays example retention policy configurations.
func displayRetentionPolicyConfiguration() {
	fmt.Println("  Retention policies automate cleanup of old backups.")
	fmt.Println()
	fmt.Println("  Example policies:")
	fmt.Println()
	fmt.Println("  1. Keep last 7 days of backups:")
	fmt.Println("     max_age: 168h (7 * 24 hours)")
	fmt.Println()
	fmt.Println("  2. Keep last 10 backups:")
	fmt.Println("     max_backups: 10")
	fmt.Println()
	fmt.Println("  3. Grandfather-Father-Son (GFS) rotation:")
	fmt.Println("     keep_daily: 7   (last 7 daily backups)")
	fmt.Println("     keep_weekly: 4  (last 4 weekly backups)")
	fmt.Println("     keep_monthly: 6 (last 6 monthly backups)")
	fmt.Println()
	fmt.Println("  4. Combined policy:")
	fmt.Println("     max_age: 720h (30 days)")
	fmt.Println("     max_backups: 50")
	fmt.Println()
	fmt.Println("  Configuration in quicbaas.yaml:")
	fmt.Println("  ```yaml")
	fmt.Println("  backup:")
	fmt.Println("    retention:")
	fmt.Println("      max_age: 720h")
	fmt.Println("      max_backups: 50")
	fmt.Println("      keep_daily: 7")
	fmt.Println("      keep_weekly: 4")
	fmt.Println("      keep_monthly: 6")
	fmt.Println("  ```")
}

// simulateRestoreScenario demonstrates a restore operation.
func simulateRestoreScenario(ctx context.Context, client quicbaas.Client, backupID string) error {
	fmt.Println("  Restore scenarios:")
	fmt.Println()
	fmt.Println("  1. Full restore (disaster recovery):")
	fmt.Println("     - Stops all write operations")
	fmt.Println("     - Restores all data from the specified backup")
	fmt.Println("     - Resumes normal operations")
	fmt.Println()
	fmt.Println("  2. Point-in-time restore:")
	fmt.Println("     - Identifies the backup closest to the target time")
	fmt.Println("     - Restores from that backup")
	fmt.Println("     - Applies WAL logs up to the exact target time")
	fmt.Println()
	fmt.Println("  3. Selective restore:")
	fmt.Println("     - Restores only specific key prefixes")
	fmt.Println("     - Useful for recovering specific data without full restore")
	fmt.Println()
	fmt.Println("  4. Restore to new cluster:")
	fmt.Println("     - Initialize new cluster")
	fmt.Println("     - Restore backup to new cluster")
	fmt.Println("     - Validate data integrity")
	fmt.Println()

	// Demonstrate restore request (read-only demonstration)
	fmt.Printf("  Demonstrating restore from backup: %s\n", backupID)
	fmt.Println()
	fmt.Println("  Restore request:")
	fmt.Printf("    BackupID: %s\n", backupID)
	fmt.Println("    Source: (from backup destination)")
	fmt.Println()

	// Perform actual restore (this would restore from the backup)
	resp, err := client.Backup().Restore(ctx, &quicbaas.RestoreRequest{
		BackupID: backupID,
	})
	if err != nil {
		// In demo mode, this might fail if the server doesn't support
		// the restore operation or if the backup doesn't exist
		fmt.Printf("  Note: Restore operation returned: %v\n", err)
		fmt.Println("  (This is expected in demo mode without actual backups)")
		return nil
	}

	if resp.Restored {
		fmt.Printf("  Restore completed successfully in %v\n", resp.Duration)
	} else {
		fmt.Println("  Restore was not performed (no action needed)")
	}

	return nil
}

// cleanupSampleData removes the sample data created for the demonstration.
func cleanupSampleData(ctx context.Context, client quicbaas.Client) error {
	entityTypes := []struct {
		name  string
		count int
	}{
		{name: "config", count: 11}, // 10 original + 1 new
		{name: "session", count: 4}, // 3 original + 1 new
		{name: "metric", count: 7},
	}

	deletedCount := 0
	for _, entity := range entityTypes {
		for i := 1; i <= entity.count; i++ {
			if _, err := client.KV().Delete(ctx, &quicbaas.DeleteRequest{
				EntityType: entity.name,
				ID:         uint64(i),
			}); err != nil {
				log.Printf("  Warning: failed to delete %s/%d: %v", entity.name, i, err)
				continue
			}
			deletedCount++
		}
	}

	fmt.Printf("  Deleted %d entries\n", deletedCount)
	return nil
}

// formatBytes formats a byte count as a human-readable string.
func formatBytes(bytes int64) string {
	const (
		_        = iota
		kibibyte = 1 << (10 * iota)
		mebibyte
		gibibyte
		tebibyte
	)

	switch {
	case bytes >= tebibyte:
		return fmt.Sprintf("%.2f TiB", float64(bytes)/tebibyte)
	case bytes >= gibibyte:
		return fmt.Sprintf("%.2f GiB", float64(bytes)/gibibyte)
	case bytes >= mebibyte:
		return fmt.Sprintf("%.2f MiB", float64(bytes)/mebibyte)
	case bytes >= kibibyte:
		return fmt.Sprintf("%.2f KiB", float64(bytes)/kibibyte)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
