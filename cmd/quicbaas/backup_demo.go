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

//go:build ignore
// +build ignore

// Package main demonstrates the backup CLI commands.
// This is a standalone demo that can be run with: go run backup_demo.go
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func main() {
	fmt.Println("=== QuicBaaS Backup CLI Demo ===\n")

	// Demo 1: Backup Create Response
	fmt.Println("1. Backup Create Command Output")
	fmt.Println("----------------------------------------")
	demoBackupCreate()

	fmt.Println("\n2. Backup List Command Output")
	fmt.Println("----------------------------------------")
	demoBackupList()

	fmt.Println("\n3. Backup Restore Command Output")
	fmt.Println("----------------------------------------")
	demoBackupRestore()

	fmt.Println("\n4. Backup Delete Command Output")
	fmt.Println("----------------------------------------")
	demoBackupDelete()

	fmt.Println("\n5. Backup Verify Command Output")
	fmt.Println("----------------------------------------")
	demoBackupVerify()

	fmt.Println("\n6. Compression Type Parsing")
	fmt.Println("----------------------------------------")
	demoCompressionParsing()

	fmt.Println("\n7. Encryption Type Parsing")
	fmt.Println("----------------------------------------")
	demoEncryptionParsing()
}

func demoBackupCreate() {
	formatter := cli.NewFormatter(true)
	resp := &cli.BackupCreateResponse{
		BackupID:    "backup-prod-1729123456",
		Type:        "full",
		Size:        1024 * 1024 * 100, // 100 MB
		Compression: "zstd",
		Encryption:  "none",
		Duration:    5 * time.Second,
		Timestamp:   time.Now(),
		Success:     true,
	}

	fmt.Println("\nTable Format:")
	formatter.FormatBackupCreateResponse(resp, cli.OutputFormatTable, os.Stdout)

	fmt.Println("\nJSON Format:")
	formatter.FormatBackupCreateResponse(resp, cli.OutputFormatJSON, os.Stdout)
}

func demoBackupList() {
	formatter := cli.NewFormatter(true)
	resp := &cli.BackupListResponse{
		Backups: []cli.BackupInfo{
			{
				ID:          "backup-prod-1729123456",
				Type:        "full",
				Size:        1024 * 1024 * 100,
				Compression: "zstd",
				Encryption:  "none",
				NumShards:   3,
				NumKeys:     10000,
				Timestamp:   time.Now().Add(-24 * time.Hour),
			},
			{
				ID:          "backup-prod-1729209856",
				Type:        "incremental",
				Size:        1024 * 1024 * 10,
				Compression: "gzip",
				Encryption:  "aes256",
				NumShards:   3,
				NumKeys:     1000,
				Timestamp:   time.Now().Add(-1 * time.Hour),
			},
		},
		TotalCount:  2,
		StoragePath: "/var/lib/quicbaas/backups",
	}

	formatter.FormatBackupListResponse(resp, cli.OutputFormatTable, os.Stdout)
}

func demoBackupRestore() {
	formatter := cli.NewFormatter(true)
	resp := &cli.BackupRestoreResponse{
		BackupID:       "backup-prod-1729123456",
		KeysRestored:   10000,
		ShardsRestored: 3,
		Duration:       30 * time.Second,
		Timestamp:      time.Now(),
		Success:        true,
	}

	formatter.FormatBackupRestoreResponse(resp, cli.OutputFormatTable, os.Stdout)
}

func demoBackupDelete() {
	formatter := cli.NewFormatter(true)
	resp := &cli.BackupDeleteResponse{
		BackupID:  "backup-prod-1729123456",
		Deleted:   true,
		Timestamp: time.Now(),
	}

	formatter.FormatBackupDeleteResponse(resp, cli.OutputFormatTable, os.Stdout)
}

func demoBackupVerify() {
	formatter := cli.NewFormatter(true)
	resp := &cli.BackupVerifyResponse{
		BackupID:      "backup-prod-1729123456",
		Valid:         true,
		ChecksumValid: true,
		ShardsValid:   3,
		Issues:        []string{},
		Timestamp:     time.Now(),
	}

	formatter.FormatBackupVerifyResponse(resp, cli.OutputFormatTable, os.Stdout)
}

func demoCompressionParsing() {
	tests := []string{"none", "gzip", "zstd", "invalid"}
	for _, test := range tests {
		ctype, err := parseCompressionType(test)
		if err != nil {
			fmt.Printf("  %-10s -> ERROR: %v\n", test, err)
		} else {
			fmt.Printf("  %-10s -> %s\n", test, ctype)
		}
	}
}

func demoEncryptionParsing() {
	tests := []string{"none", "aes256", "invalid"}
	for _, test := range tests {
		etype, err := parseEncryptionType(test)
		if err != nil {
			fmt.Printf("  %-10s -> ERROR: %v\n", test, err)
		} else {
			fmt.Printf("  %-10s -> %s\n", test, etype)
		}
	}
}

func parseCompressionType(s string) (backup.CompressionType, error) {
	switch s {
	case "none":
		return backup.CompressionNone, nil
	case "gzip":
		return backup.CompressionGzip, nil
	case "zstd":
		return backup.CompressionZstd, nil
	default:
		return "", &dberrors.MessageError{Message: "unsupported compression type: " + s}
	}
}

func parseEncryptionType(s string) (backup.EncryptionType, error) {
	switch s {
	case "none":
		return backup.EncryptionNone, nil
	case "aes256":
		return backup.EncryptionAES256GCM, nil
	default:
		return "", &dberrors.MessageError{Message: "unsupported encryption type: " + s}
	}
}
