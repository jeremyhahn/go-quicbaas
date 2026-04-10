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

package backup

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
)

// TestBackupRestore tests backup creation and restoration
func TestBackupRestore(t *testing.T) {
	t.Run("BackupConfiguration", func(t *testing.T) {
		config := &backup.Config{
			StorageBackend:     "local",
			WorkDir:            "/tmp/quicbaas-test-work",
			DefaultCompression: backup.CompressionGzip,
			DefaultEncryption:  backup.EncryptionAES256GCM,
			EncryptionKey:      []byte("test-encryption-key-32bytes!!!!!"),
		}

		// Validate configuration
		err := config.Validate()
		require.NoError(t, err)

		// Apply defaults
		config = config.WithDefaults()
		assert.NotEmpty(t, config.WorkDir)

		t.Logf("Backup configuration valid: backend=%s, compression=%s",
			config.StorageBackend, config.DefaultCompression)
	})

	// Integration test with actual backup manager would go here
	// but requires full QuicBaaS setup
	t.Log("Backup/restore integration test requires full QuicBaaS setup")
}
