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

// Package backup provides the backup client for the QuicBaaS SDK.
//
// The backup client supports creating and restoring database backups with
// optional compression and encryption.
//
// # Creating a Backup
//
//	client := backup.NewClient(transport)
//
//	resp, err := client.Create(ctx, &backup.BackupRequest{
//	    Destination: "s3://my-bucket/backups/",
//	    Compress:    true,
//	    Encrypt:     true,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Backup created: %s (size: %d bytes)\n", resp.BackupID, resp.Size)
//
// # Restoring from a Backup
//
//	resp, err := client.Restore(ctx, &backup.RestoreRequest{
//	    BackupID: "backup-123",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	// The restore runs asynchronously: a running restore is not an
//	// error, and Restored is set only once it has finished.
//	if resp.Restored {
//	    fmt.Printf("Restore %s completed\n", resp.JobID)
//	} else {
//	    fmt.Printf("Restore %s is %s\n", resp.JobID, resp.Status)
//	}
//
// # Listing Backups
//
//	resp, err := client.List(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for _, backup := range resp.Backups {
//	    fmt.Printf("ID: %s, Size: %d, Created: %v\n",
//	        backup.ID, backup.Size, backup.CreatedAt)
//	}
package backup
