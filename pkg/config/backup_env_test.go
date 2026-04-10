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

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBackupEnvOverrides tests that backup-related environment variables
// are properly applied to the configuration via applyEnvironmentOverrides.
func TestBackupEnvOverrides(t *testing.T) {
	t.Run("QUICBAAS_BACKUP_ENABLED sets backup enabled", func(t *testing.T) {
		os.Setenv("QUICBAAS_BACKUP_ENABLED", "true")
		defer os.Unsetenv("QUICBAAS_BACKUP_ENABLED")

		loader := NewLoader()
		cfg, err := loader.Load("")

		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.True(t, cfg.Backup.Enabled)
	})

	t.Run("QUICBAAS_BACKUP_ENABLED=false disables backup", func(t *testing.T) {
		os.Setenv("QUICBAAS_BACKUP_ENABLED", "false")
		defer os.Unsetenv("QUICBAAS_BACKUP_ENABLED")

		loader := NewLoader()
		cfg, err := loader.Load("")

		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.False(t, cfg.Backup.Enabled)
	})

	t.Run("QUICBAAS_BACKUP_STORAGE_BACKEND sets storage backend", func(t *testing.T) {
		os.Setenv("QUICBAAS_BACKUP_STORAGE_BACKEND", "s3")
		defer os.Unsetenv("QUICBAAS_BACKUP_STORAGE_BACKEND")

		loader := NewLoader()
		cfg, err := loader.Load("")

		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, "s3", cfg.Backup.StorageBackend)
	})

	t.Run("QUICBAAS_BACKUP_WORK_DIR sets work directory", func(t *testing.T) {
		os.Setenv("QUICBAAS_BACKUP_WORK_DIR", "/custom/backup/work")
		defer os.Unsetenv("QUICBAAS_BACKUP_WORK_DIR")

		loader := NewLoader()
		cfg, err := loader.Load("")

		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, "/custom/backup/work", cfg.Backup.WorkDir)
	})

	t.Run("QUICBAAS_BACKUP_DEFAULT_COMPRESSION sets compression", func(t *testing.T) {
		os.Setenv("QUICBAAS_BACKUP_DEFAULT_COMPRESSION", "gzip")
		defer os.Unsetenv("QUICBAAS_BACKUP_DEFAULT_COMPRESSION")

		loader := NewLoader()
		cfg, err := loader.Load("")

		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, "gzip", cfg.Backup.DefaultCompression)
	})

	t.Run("QUICBAAS_BACKUP_DEFAULT_ENCRYPTION sets encryption", func(t *testing.T) {
		os.Setenv("QUICBAAS_BACKUP_DEFAULT_ENCRYPTION", "aes-256-gcm")
		defer os.Unsetenv("QUICBAAS_BACKUP_DEFAULT_ENCRYPTION")

		loader := NewLoader()
		cfg, err := loader.Load("")

		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, "aes-256-gcm", cfg.Backup.DefaultEncryption)
	})

	t.Run("QUICBAAS_BACKUP_TIMEOUT sets timeout", func(t *testing.T) {
		os.Setenv("QUICBAAS_BACKUP_TIMEOUT", "45m")
		defer os.Unsetenv("QUICBAAS_BACKUP_TIMEOUT")

		loader := NewLoader()
		cfg, err := loader.Load("")

		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, 45*time.Minute, cfg.Backup.Timeout)
	})

	t.Run("env var overrides YAML config", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "backup.yaml")

		// YAML sets backup.enabled to false
		content := `
backup:
  enabled: false
  storage_backend: "local"
`
		err := os.WriteFile(configFile, []byte(content), 0o644)
		require.NoError(t, err)

		// Env var overrides to true
		os.Setenv("QUICBAAS_BACKUP_ENABLED", "true")
		os.Setenv("QUICBAAS_BACKUP_STORAGE_BACKEND", "gcs")
		defer func() {
			os.Unsetenv("QUICBAAS_BACKUP_ENABLED")
			os.Unsetenv("QUICBAAS_BACKUP_STORAGE_BACKEND")
		}()

		loader := NewLoader()
		cfg, err := loader.Load(configFile)

		require.NoError(t, err)
		require.NotNil(t, cfg)
		// Env var should override YAML
		assert.True(t, cfg.Backup.Enabled)
		assert.Equal(t, "gcs", cfg.Backup.StorageBackend)
	})

	t.Run("all backup env vars set together", func(t *testing.T) {
		os.Setenv("QUICBAAS_BACKUP_ENABLED", "true")
		os.Setenv("QUICBAAS_BACKUP_STORAGE_BACKEND", "minio")
		os.Setenv("QUICBAAS_BACKUP_WORK_DIR", "/tmp/backup-work")
		os.Setenv("QUICBAAS_BACKUP_DEFAULT_COMPRESSION", "zstd")
		os.Setenv("QUICBAAS_BACKUP_DEFAULT_ENCRYPTION", "none")
		os.Setenv("QUICBAAS_BACKUP_TIMEOUT", "1h")
		defer func() {
			os.Unsetenv("QUICBAAS_BACKUP_ENABLED")
			os.Unsetenv("QUICBAAS_BACKUP_STORAGE_BACKEND")
			os.Unsetenv("QUICBAAS_BACKUP_WORK_DIR")
			os.Unsetenv("QUICBAAS_BACKUP_DEFAULT_COMPRESSION")
			os.Unsetenv("QUICBAAS_BACKUP_DEFAULT_ENCRYPTION")
			os.Unsetenv("QUICBAAS_BACKUP_TIMEOUT")
		}()

		loader := NewLoader()
		cfg, err := loader.Load("")

		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.True(t, cfg.Backup.Enabled)
		assert.Equal(t, "minio", cfg.Backup.StorageBackend)
		assert.Equal(t, "/tmp/backup-work", cfg.Backup.WorkDir)
		assert.Equal(t, "zstd", cfg.Backup.DefaultCompression)
		assert.Equal(t, "none", cfg.Backup.DefaultEncryption)
		assert.Equal(t, time.Hour, cfg.Backup.Timeout)
	})
}
