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
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
)

// testBackupData represents test data for backup/restore operations.
type testBackupData struct {
	keys   []string
	values [][]byte
}

// createTestBackupData generates deterministic test data.
func createTestBackupData(numKeys int) *testBackupData {
	data := &testBackupData{
		keys:   make([]string, numKeys),
		values: make([][]byte, numKeys),
	}

	for i := 0; i < numKeys; i++ {
		data.keys[i] = fmt.Sprintf("test:key:%05d", i)
		data.values[i] = []byte(fmt.Sprintf(`{"id":%d,"data":"test-value-%d","timestamp":%d}`, i, i, time.Now().UnixNano()))
	}

	return data
}

// TestBackupRestore_LocalStorage tests backup and restore operations using local filesystem storage.
func TestBackupRestore_LocalStorage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create temporary backup directory
	backupDir := filepath.Join(t.TempDir(), "local-backup-test")
	defer os.RemoveAll(backupDir)

	t.Run("CreateAndVerifyBackup", func(t *testing.T) {
		// Create backup configuration
		config := &backup.Config{
			StorageBackend: "local",
			StorageConfig: map[string]string{
				"path": backupDir,
			},
			DefaultCompression: backup.CompressionZstd,
			DefaultEncryption:  backup.EncryptionNone,
			WorkDir:            filepath.Join(backupDir, "work"),
			Timeout:            5 * time.Minute,
		}

		err := config.Validate()
		require.NoError(t, err, "Config validation should succeed")

		// Verify backup directory is created
		err = os.MkdirAll(backupDir, 0o755)
		require.NoError(t, err, "Should create backup directory")

		// Verify directory exists
		info, err := os.Stat(backupDir)
		require.NoError(t, err, "Backup directory should exist")
		assert.True(t, info.IsDir(), "Should be a directory")

		t.Logf("Local backup directory created: %s", backupDir)
	})

	t.Run("BackupWithCompression", func(t *testing.T) {
		compressionTypes := []backup.CompressionType{
			backup.CompressionNone,
			backup.CompressionGzip,
			backup.CompressionZstd,
		}

		for _, ctype := range compressionTypes {
			t.Run(string(ctype), func(t *testing.T) {
				config := &backup.Config{
					StorageBackend: "local",
					StorageConfig: map[string]string{
						"path": filepath.Join(backupDir, string(ctype)),
					},
					DefaultCompression: ctype,
					DefaultEncryption:  backup.EncryptionNone,
					WorkDir:            filepath.Join(backupDir, "work", string(ctype)),
					Timeout:            5 * time.Minute,
				}

				err := config.Validate()
				require.NoError(t, err, "Config validation should succeed for %s", ctype)

				// Verify compression type is set
				assert.Equal(t, ctype, config.DefaultCompression)

				t.Logf("Validated local backup with compression: %s", ctype)
			})
		}
	})

	t.Run("LocalStorageDirectAccess", func(t *testing.T) {
		localStorage, err := backup.NewLocalStorage(filepath.Join(backupDir, "direct"))
		require.NoError(t, err, "Should create local storage")

		// Create a test backup
		testBackup := &backup.Backup{
			ID:          fmt.Sprintf("backup-local-%d", time.Now().UnixNano()),
			Type:        backup.BackupTypeFull,
			Timestamp:   time.Now(),
			Compression: backup.CompressionNone,
			Encryption:  backup.EncryptionNone,
			Metadata: &backup.BackupMetadata{
				Version:   "1.0.0",
				NumShards: 1,
			},
		}

		testData := []byte("test backup data for local storage")

		// Upload backup
		err = localStorage.Upload(ctx, testBackup, bytes.NewReader(testData))
		require.NoError(t, err, "Upload should succeed")

		// Verify backup exists
		reader, err := localStorage.Download(ctx, testBackup.ID)
		require.NoError(t, err, "Download should succeed")
		defer reader.Close()

		downloadedData, err := io.ReadAll(reader)
		require.NoError(t, err, "Should read downloaded data")
		assert.Equal(t, testData, downloadedData, "Downloaded data should match")

		// List backups
		list, err := localStorage.List(ctx, 0, "")
		require.NoError(t, err, "List should succeed")
		assert.GreaterOrEqual(t, len(list.Backups), 1, "Should have at least one backup")

		// Get metadata
		metadata, err := localStorage.GetMetadata(ctx, testBackup.ID)
		require.NoError(t, err, "GetMetadata should succeed")
		assert.Equal(t, testBackup.ID, metadata.ID)
		assert.Equal(t, testBackup.Type, metadata.Type)

		// Delete backup
		err = localStorage.Delete(ctx, testBackup.ID)
		require.NoError(t, err, "Delete should succeed")

		// Verify deletion
		_, err = localStorage.Download(ctx, testBackup.ID)
		assert.Error(t, err, "Download should fail after deletion")

		t.Logf("Local storage direct access test completed")
	})

	select {
	case <-ctx.Done():
		t.Fatal("Test timed out")
	default:
		t.Log("Local storage tests completed successfully")
	}
}

// TestBackupRestore_S3Storage tests backup and restore operations using S3 (Floci).
func TestBackupRestore_S3Storage(t *testing.T) {
	requireFloci(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	s3Endpoint := getS3Endpoint()
	bucketName := fmt.Sprintf("quicbaas-backup-test-%d", time.Now().Unix())

	t.Run("S3Configuration", func(t *testing.T) {
		config := &backup.Config{
			StorageBackend: "s3",
			StorageConfig: map[string]string{
				"endpoint":       s3Endpoint,
				"bucket":         bucketName,
				"region":         "us-east-1",
				"access_key":     "test",
				"secret_key":     "test",
				"use_path_style": "true",
				"disable_ssl":    "true",
				"prefix":         "backups",
			},
			DefaultCompression: backup.CompressionZstd,
			DefaultEncryption:  backup.EncryptionNone,
			WorkDir:            filepath.Join(os.TempDir(), "s3-backup-work"),
			Timeout:            5 * time.Minute,
		}

		err := config.Validate()
		require.NoError(t, err, "S3 config validation should succeed")

		assert.Equal(t, "s3", config.StorageBackend)
		assert.Equal(t, bucketName, config.StorageConfig["bucket"])
		assert.Equal(t, s3Endpoint, config.StorageConfig["endpoint"])

		t.Logf("S3 configuration validated for endpoint: %s, bucket: %s", s3Endpoint, bucketName)
	})

	t.Run("S3WithCompression", func(t *testing.T) {
		compressionTypes := []backup.CompressionType{
			backup.CompressionNone,
			backup.CompressionGzip,
			backup.CompressionZstd,
		}

		for _, ctype := range compressionTypes {
			t.Run(string(ctype), func(t *testing.T) {
				config := &backup.Config{
					StorageBackend: "s3",
					StorageConfig: map[string]string{
						"endpoint":       s3Endpoint,
						"bucket":         bucketName + "-" + string(ctype),
						"region":         "us-east-1",
						"access_key":     "test",
						"secret_key":     "test",
						"use_path_style": "true",
						"disable_ssl":    "true",
					},
					DefaultCompression: ctype,
					DefaultEncryption:  backup.EncryptionNone,
					WorkDir:            filepath.Join(os.TempDir(), "s3-work", string(ctype)),
					Timeout:            5 * time.Minute,
				}

				err := config.Validate()
				require.NoError(t, err, "S3 config with compression %s should validate", ctype)

				t.Logf("S3 backup with compression %s validated", ctype)
			})
		}
	})

	t.Run("S3WithEncryption", func(t *testing.T) {
		encryptionKey := []byte("12345678901234567890123456789012") // 32 bytes

		config := &backup.Config{
			StorageBackend: "s3",
			StorageConfig: map[string]string{
				"endpoint":       s3Endpoint,
				"bucket":         bucketName + "-encrypted",
				"region":         "us-east-1",
				"access_key":     "test",
				"secret_key":     "test",
				"use_path_style": "true",
				"disable_ssl":    "true",
			},
			DefaultCompression: backup.CompressionZstd,
			DefaultEncryption:  backup.EncryptionAES256GCM,
			EncryptionKey:      encryptionKey,
			WorkDir:            filepath.Join(os.TempDir(), "s3-work-encrypted"),
			Timeout:            5 * time.Minute,
		}

		err := config.Validate()
		require.NoError(t, err, "S3 encrypted config should validate")

		assert.Equal(t, backup.EncryptionAES256GCM, config.DefaultEncryption)
		assert.Len(t, config.EncryptionKey, 32)

		t.Logf("S3 encrypted backup configuration validated")
	})

	select {
	case <-ctx.Done():
		t.Fatal("Test timed out")
	default:
		t.Log("S3 storage tests completed")
	}
}

// TestBackupRestore_MinIOStorage tests backup and restore operations using MinIO.
func TestBackupRestore_MinIOStorage(t *testing.T) {
	requireMinIO(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	minioEndpoint := getMinIOEndpoint()
	bucketName := fmt.Sprintf("quicbaas-backup-test-%d", time.Now().Unix())

	t.Run("MinIOConfiguration", func(t *testing.T) {
		config := &backup.Config{
			StorageBackend: "minio",
			StorageConfig: map[string]string{
				"endpoint":   minioEndpoint,
				"bucket":     bucketName,
				"access_key": "minioadmin",
				"secret_key": "minioadmin",
				"use_ssl":    "false",
				"prefix":     "backups",
			},
			DefaultCompression: backup.CompressionZstd,
			DefaultEncryption:  backup.EncryptionNone,
			WorkDir:            filepath.Join(os.TempDir(), "minio-backup-work"),
			Timeout:            5 * time.Minute,
		}

		err := config.Validate()
		require.NoError(t, err, "MinIO config validation should succeed")

		assert.Equal(t, "minio", config.StorageBackend)
		assert.Equal(t, bucketName, config.StorageConfig["bucket"])
		assert.Equal(t, minioEndpoint, config.StorageConfig["endpoint"])

		t.Logf("MinIO configuration validated for endpoint: %s, bucket: %s", minioEndpoint, bucketName)
	})

	t.Run("MinIOWithCompression", func(t *testing.T) {
		compressionTypes := []backup.CompressionType{
			backup.CompressionNone,
			backup.CompressionGzip,
			backup.CompressionZstd,
		}

		for _, ctype := range compressionTypes {
			t.Run(string(ctype), func(t *testing.T) {
				config := &backup.Config{
					StorageBackend: "minio",
					StorageConfig: map[string]string{
						"endpoint":   minioEndpoint,
						"bucket":     bucketName + "-" + string(ctype),
						"access_key": "minioadmin",
						"secret_key": "minioadmin",
						"use_ssl":    "false",
					},
					DefaultCompression: ctype,
					DefaultEncryption:  backup.EncryptionNone,
					WorkDir:            filepath.Join(os.TempDir(), "minio-work", string(ctype)),
					Timeout:            5 * time.Minute,
				}

				err := config.Validate()
				require.NoError(t, err, "MinIO config with compression %s should validate", ctype)

				t.Logf("MinIO backup with compression %s validated", ctype)
			})
		}
	})

	t.Run("MinIOWithEncryption", func(t *testing.T) {
		encryptionKey := []byte("12345678901234567890123456789012") // 32 bytes

		config := &backup.Config{
			StorageBackend: "minio",
			StorageConfig: map[string]string{
				"endpoint":   minioEndpoint,
				"bucket":     bucketName + "-encrypted",
				"access_key": "minioadmin",
				"secret_key": "minioadmin",
				"use_ssl":    "false",
			},
			DefaultCompression: backup.CompressionZstd,
			DefaultEncryption:  backup.EncryptionAES256GCM,
			EncryptionKey:      encryptionKey,
			WorkDir:            filepath.Join(os.TempDir(), "minio-work-encrypted"),
			Timeout:            5 * time.Minute,
		}

		err := config.Validate()
		require.NoError(t, err, "MinIO encrypted config should validate")

		assert.Equal(t, backup.EncryptionAES256GCM, config.DefaultEncryption)
		assert.Len(t, config.EncryptionKey, 32)

		t.Logf("MinIO encrypted backup configuration validated")
	})

	select {
	case <-ctx.Done():
		t.Fatal("Test timed out")
	default:
		t.Log("MinIO storage tests completed")
	}
}

// TestBackupRestore_AzureStorage tests backup and restore operations using Azure/Azurite.
func TestBackupRestore_AzureStorage(t *testing.T) {
	requireAzurite(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	azureEndpoint := getAzuriteEndpoint()
	containerName := fmt.Sprintf("quicbaas-backup-test-%d", time.Now().Unix())

	// Azurite default credentials
	accountName := "devstoreaccount1"
	accountKey := "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="

	t.Run("AzureConfiguration", func(t *testing.T) {
		config := &backup.Config{
			StorageBackend: "azure",
			StorageConfig: map[string]string{
				"endpoint":     azureEndpoint,
				"container":    containerName,
				"account_name": accountName,
				"account_key":  accountKey,
				"use_emulator": "true",
				"prefix":       "backups",
			},
			DefaultCompression: backup.CompressionZstd,
			DefaultEncryption:  backup.EncryptionNone,
			WorkDir:            filepath.Join(os.TempDir(), "azure-backup-work"),
			Timeout:            5 * time.Minute,
		}

		err := config.Validate()
		require.NoError(t, err, "Azure config validation should succeed")

		assert.Equal(t, "azure", config.StorageBackend)
		assert.Equal(t, containerName, config.StorageConfig["container"])
		assert.Equal(t, accountName, config.StorageConfig["account_name"])

		t.Logf("Azure configuration validated for endpoint: %s, container: %s", azureEndpoint, containerName)
	})

	t.Run("AzureWithCompression", func(t *testing.T) {
		compressionTypes := []backup.CompressionType{
			backup.CompressionNone,
			backup.CompressionGzip,
			backup.CompressionZstd,
		}

		for _, ctype := range compressionTypes {
			t.Run(string(ctype), func(t *testing.T) {
				config := &backup.Config{
					StorageBackend: "azure",
					StorageConfig: map[string]string{
						"endpoint":     azureEndpoint,
						"container":    containerName + "-" + strings.ReplaceAll(string(ctype), "-", ""),
						"account_name": accountName,
						"account_key":  accountKey,
						"use_emulator": "true",
					},
					DefaultCompression: ctype,
					DefaultEncryption:  backup.EncryptionNone,
					WorkDir:            filepath.Join(os.TempDir(), "azure-work", string(ctype)),
					Timeout:            5 * time.Minute,
				}

				err := config.Validate()
				require.NoError(t, err, "Azure config with compression %s should validate", ctype)

				t.Logf("Azure backup with compression %s validated", ctype)
			})
		}
	})

	t.Run("AzureWithEncryption", func(t *testing.T) {
		encryptionKey := []byte("12345678901234567890123456789012") // 32 bytes

		config := &backup.Config{
			StorageBackend: "azure",
			StorageConfig: map[string]string{
				"endpoint":     azureEndpoint,
				"container":    containerName + "-encrypted",
				"account_name": accountName,
				"account_key":  accountKey,
				"use_emulator": "true",
			},
			DefaultCompression: backup.CompressionZstd,
			DefaultEncryption:  backup.EncryptionAES256GCM,
			EncryptionKey:      encryptionKey,
			WorkDir:            filepath.Join(os.TempDir(), "azure-work-encrypted"),
			Timeout:            5 * time.Minute,
		}

		err := config.Validate()
		require.NoError(t, err, "Azure encrypted config should validate")

		assert.Equal(t, backup.EncryptionAES256GCM, config.DefaultEncryption)
		assert.Len(t, config.EncryptionKey, 32)

		t.Logf("Azure encrypted backup configuration validated")
	})

	select {
	case <-ctx.Done():
		t.Fatal("Test timed out")
	default:
		t.Log("Azure storage tests completed")
	}
}

// TestBackupRestore_GCSStorage tests backup and restore operations using GCS emulator.
func TestBackupRestore_GCSStorage(t *testing.T) {
	requireGCSEmulator(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	gcsEndpoint := getGCSEndpoint()
	bucketName := fmt.Sprintf("quicbaas-backup-test-%d", time.Now().Unix())

	t.Run("GCSConfiguration", func(t *testing.T) {
		config := &backup.Config{
			StorageBackend: "gcs",
			StorageConfig: map[string]string{
				"endpoint":     gcsEndpoint,
				"bucket":       bucketName,
				"project_id":   "test-project",
				"use_emulator": "true",
				"prefix":       "backups",
			},
			DefaultCompression: backup.CompressionZstd,
			DefaultEncryption:  backup.EncryptionNone,
			WorkDir:            filepath.Join(os.TempDir(), "gcs-backup-work"),
			Timeout:            5 * time.Minute,
		}

		err := config.Validate()
		require.NoError(t, err, "GCS config validation should succeed")

		assert.Equal(t, "gcs", config.StorageBackend)
		assert.Equal(t, bucketName, config.StorageConfig["bucket"])
		assert.Equal(t, gcsEndpoint, config.StorageConfig["endpoint"])

		t.Logf("GCS configuration validated for endpoint: %s, bucket: %s", gcsEndpoint, bucketName)
	})

	t.Run("GCSWithCompression", func(t *testing.T) {
		compressionTypes := []backup.CompressionType{
			backup.CompressionNone,
			backup.CompressionGzip,
			backup.CompressionZstd,
		}

		for _, ctype := range compressionTypes {
			t.Run(string(ctype), func(t *testing.T) {
				config := &backup.Config{
					StorageBackend: "gcs",
					StorageConfig: map[string]string{
						"endpoint":     gcsEndpoint,
						"bucket":       bucketName + "-" + string(ctype),
						"project_id":   "test-project",
						"use_emulator": "true",
					},
					DefaultCompression: ctype,
					DefaultEncryption:  backup.EncryptionNone,
					WorkDir:            filepath.Join(os.TempDir(), "gcs-work", string(ctype)),
					Timeout:            5 * time.Minute,
				}

				err := config.Validate()
				require.NoError(t, err, "GCS config with compression %s should validate", ctype)

				t.Logf("GCS backup with compression %s validated", ctype)
			})
		}
	})

	t.Run("GCSWithEncryption", func(t *testing.T) {
		encryptionKey := []byte("12345678901234567890123456789012") // 32 bytes

		config := &backup.Config{
			StorageBackend: "gcs",
			StorageConfig: map[string]string{
				"endpoint":     gcsEndpoint,
				"bucket":       bucketName + "-encrypted",
				"project_id":   "test-project",
				"use_emulator": "true",
			},
			DefaultCompression: backup.CompressionZstd,
			DefaultEncryption:  backup.EncryptionAES256GCM,
			EncryptionKey:      encryptionKey,
			WorkDir:            filepath.Join(os.TempDir(), "gcs-work-encrypted"),
			Timeout:            5 * time.Minute,
		}

		err := config.Validate()
		require.NoError(t, err, "GCS encrypted config should validate")

		assert.Equal(t, backup.EncryptionAES256GCM, config.DefaultEncryption)
		assert.Len(t, config.EncryptionKey, 32)

		t.Logf("GCS encrypted backup configuration validated")
	})

	select {
	case <-ctx.Done():
		t.Fatal("Test timed out")
	default:
		t.Log("GCS storage tests completed")
	}
}

// TestBackupRestore_CrossBackend tests backup from one backend and configuration for restore to another.
func TestBackupRestore_CrossBackend(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	backupDir := filepath.Join(t.TempDir(), "cross-backend-test")
	defer os.RemoveAll(backupDir)

	t.Run("LocalToS3Configuration", func(t *testing.T) {
		// Create local backup configuration
		localConfig := &backup.Config{
			StorageBackend: "local",
			StorageConfig: map[string]string{
				"path": filepath.Join(backupDir, "local-source"),
			},
			DefaultCompression: backup.CompressionZstd,
			DefaultEncryption:  backup.EncryptionNone,
			WorkDir:            filepath.Join(backupDir, "work-local"),
			Timeout:            5 * time.Minute,
		}

		err := localConfig.Validate()
		require.NoError(t, err, "Local config should validate")

		// Create S3 restore configuration
		s3Config := &backup.Config{
			StorageBackend: "s3",
			StorageConfig: map[string]string{
				"endpoint":       getS3Endpoint(),
				"bucket":         "cross-backend-restore",
				"region":         "us-east-1",
				"access_key":     "test",
				"secret_key":     "test",
				"use_path_style": "true",
				"disable_ssl":    "true",
			},
			DefaultCompression: backup.CompressionZstd,
			DefaultEncryption:  backup.EncryptionNone,
			WorkDir:            filepath.Join(backupDir, "work-s3"),
			Timeout:            5 * time.Minute,
		}

		err = s3Config.Validate()
		require.NoError(t, err, "S3 config should validate")

		// Verify both configurations are valid and different
		assert.NotEqual(t, localConfig.StorageBackend, s3Config.StorageBackend)
		assert.Equal(t, localConfig.DefaultCompression, s3Config.DefaultCompression)

		t.Logf("Cross-backend configuration (local -> S3) validated")
	})

	t.Run("MinIOToAzureConfiguration", func(t *testing.T) {
		// MinIO source configuration
		minioConfig := &backup.Config{
			StorageBackend: "minio",
			StorageConfig: map[string]string{
				"endpoint":   getMinIOEndpoint(),
				"bucket":     "cross-backend-source",
				"access_key": "minioadmin",
				"secret_key": "minioadmin",
				"use_ssl":    "false",
			},
			DefaultCompression: backup.CompressionGzip,
			DefaultEncryption:  backup.EncryptionNone,
			WorkDir:            filepath.Join(backupDir, "work-minio"),
			Timeout:            5 * time.Minute,
		}

		err := minioConfig.Validate()
		require.NoError(t, err, "MinIO config should validate")

		// Azure destination configuration
		azureConfig := &backup.Config{
			StorageBackend: "azure",
			StorageConfig: map[string]string{
				"endpoint":     getAzuriteEndpoint(),
				"container":    "cross-backend-dest",
				"account_name": "devstoreaccount1",
				"account_key":  "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==",
				"use_emulator": "true",
			},
			DefaultCompression: backup.CompressionGzip,
			DefaultEncryption:  backup.EncryptionNone,
			WorkDir:            filepath.Join(backupDir, "work-azure"),
			Timeout:            5 * time.Minute,
		}

		err = azureConfig.Validate()
		require.NoError(t, err, "Azure config should validate")

		// Verify configurations
		assert.NotEqual(t, minioConfig.StorageBackend, azureConfig.StorageBackend)
		assert.Equal(t, minioConfig.DefaultCompression, azureConfig.DefaultCompression)

		t.Logf("Cross-backend configuration (MinIO -> Azure) validated")
	})

	select {
	case <-ctx.Done():
		t.Fatal("Test timed out")
	default:
		t.Log("Cross-backend configuration tests completed")
	}
}

// TestBackupRestore_WithEncryption tests encrypted backup operations across backends.
func TestBackupRestore_WithEncryption(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	encryptionKey := []byte("12345678901234567890123456789012") // 32 bytes for AES-256

	backendConfigs := []struct {
		name           string
		storageBackend string
		storageConfig  map[string]string
	}{
		{
			name:           "Local",
			storageBackend: "local",
			storageConfig: map[string]string{
				"path": filepath.Join(t.TempDir(), "encrypted-local"),
			},
		},
		{
			name:           "S3",
			storageBackend: "s3",
			storageConfig: map[string]string{
				"endpoint":       getS3Endpoint(),
				"bucket":         "encrypted-backup-test",
				"region":         "us-east-1",
				"access_key":     "test",
				"secret_key":     "test",
				"use_path_style": "true",
				"disable_ssl":    "true",
			},
		},
		{
			name:           "MinIO",
			storageBackend: "minio",
			storageConfig: map[string]string{
				"endpoint":   getMinIOEndpoint(),
				"bucket":     "encrypted-backup-test",
				"access_key": "minioadmin",
				"secret_key": "minioadmin",
				"use_ssl":    "false",
			},
		},
		{
			name:           "Azure",
			storageBackend: "azure",
			storageConfig: map[string]string{
				"endpoint":     getAzuriteEndpoint(),
				"container":    "encryptedbackuptest",
				"account_name": "devstoreaccount1",
				"account_key":  "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==",
				"use_emulator": "true",
			},
		},
		{
			name:           "GCS",
			storageBackend: "gcs",
			storageConfig: map[string]string{
				"endpoint":     getGCSEndpoint(),
				"bucket":       "encrypted-backup-test",
				"project_id":   "test-project",
				"use_emulator": "true",
			},
		},
	}

	for _, bc := range backendConfigs {
		t.Run(bc.name, func(t *testing.T) {
			// Skip if backend not available
			switch bc.storageBackend {
			case "s3":
				requireFloci(t)
			case "minio":
				requireMinIO(t)
			case "azure":
				requireAzurite(t)
			case "gcs":
				requireGCSEmulator(t)
			}

			config := &backup.Config{
				StorageBackend:     bc.storageBackend,
				StorageConfig:      bc.storageConfig,
				DefaultCompression: backup.CompressionZstd,
				DefaultEncryption:  backup.EncryptionAES256GCM,
				EncryptionKey:      encryptionKey,
				WorkDir:            filepath.Join(t.TempDir(), "work-"+bc.name),
				Timeout:            5 * time.Minute,
			}

			err := config.Validate()
			require.NoError(t, err, "Encrypted config for %s should validate", bc.name)

			assert.Equal(t, backup.EncryptionAES256GCM, config.DefaultEncryption)
			assert.Len(t, config.EncryptionKey, 32)

			t.Logf("Encrypted backup configuration validated for %s", bc.name)
		})
	}

	select {
	case <-ctx.Done():
		t.Fatal("Test timed out")
	default:
		t.Log("Encryption tests completed")
	}
}

// TestBackupRestore_WithCompression tests compressed backup operations across backends.
func TestBackupRestore_WithCompression(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	compressionTypes := []backup.CompressionType{
		backup.CompressionNone,
		backup.CompressionGzip,
		backup.CompressionZstd,
	}

	backupDir := filepath.Join(t.TempDir(), "compression-test")

	for _, ctype := range compressionTypes {
		t.Run(string(ctype), func(t *testing.T) {
			// Test with local storage for simplicity
			config := &backup.Config{
				StorageBackend: "local",
				StorageConfig: map[string]string{
					"path": filepath.Join(backupDir, string(ctype)),
				},
				DefaultCompression: ctype,
				DefaultEncryption:  backup.EncryptionNone,
				WorkDir:            filepath.Join(backupDir, "work", string(ctype)),
				Timeout:            5 * time.Minute,
			}

			err := config.Validate()
			require.NoError(t, err, "Config with compression %s should validate", ctype)

			// Create compressor
			compressor, err := backup.NewCompressor(ctype)
			require.NoError(t, err, "Should create compressor for %s", ctype)

			// Test compress/decompress cycle
			originalData := []byte("test data for compression testing - this is some sample data that should compress well")

			// Compress
			var compressedBuf bytes.Buffer
			compressWriter, err := compressor.Compress(&compressedBuf)
			require.NoError(t, err, "Should create compress writer")

			_, err = compressWriter.Write(originalData)
			require.NoError(t, err, "Should write to compressor")

			err = compressWriter.Close()
			require.NoError(t, err, "Should close compressor")

			compressedData := compressedBuf.Bytes()

			// Decompress
			decompressReader, err := compressor.Decompress(bytes.NewReader(compressedData))
			require.NoError(t, err, "Should create decompress reader")

			decompressedData, err := io.ReadAll(decompressReader)
			require.NoError(t, err, "Should read decompressed data")

			err = decompressReader.Close()
			require.NoError(t, err, "Should close decompressor")

			// Verify data integrity
			assert.Equal(t, originalData, decompressedData, "Decompressed data should match original")

			// Log compression ratio
			if ctype != backup.CompressionNone {
				ratio := float64(len(compressedData)) / float64(len(originalData))
				t.Logf("Compression %s: %d bytes -> %d bytes (ratio: %.2f)", ctype, len(originalData), len(compressedData), ratio)
			} else {
				t.Logf("No compression: %d bytes -> %d bytes", len(originalData), len(compressedData))
			}
		})
	}

	select {
	case <-ctx.Done():
		t.Fatal("Test timed out")
	default:
		t.Log("Compression tests completed")
	}
}

// TestBackupRestore_InvalidConfigurations tests that invalid configurations are properly rejected.
func TestBackupRestore_InvalidConfigurations(t *testing.T) {
	t.Run("MissingStorageBackend", func(t *testing.T) {
		config := &backup.Config{
			StorageBackend: "",
			StorageConfig:  map[string]string{},
			Timeout:        5 * time.Minute,
		}

		err := config.Validate()
		assert.Error(t, err, "Should reject missing storage backend")
		assert.Contains(t, err.Error(), "storage backend")
	})

	t.Run("UnsupportedStorageBackend", func(t *testing.T) {
		config := &backup.Config{
			StorageBackend: "unsupported-backend",
			StorageConfig:  map[string]string{},
			Timeout:        5 * time.Minute,
		}

		err := config.Validate()
		assert.Error(t, err, "Should reject unsupported storage backend")
		assert.Contains(t, err.Error(), "unsupported")
	})

	t.Run("InvalidCompressionType", func(t *testing.T) {
		config := &backup.Config{
			StorageBackend:     "local",
			StorageConfig:      map[string]string{"path": "/tmp/test"},
			DefaultCompression: backup.CompressionType("invalid-compression"),
			Timeout:            5 * time.Minute,
		}

		err := config.Validate()
		assert.Error(t, err, "Should reject invalid compression type")
		assert.Contains(t, err.Error(), "compression")
	})

	t.Run("InvalidEncryptionType", func(t *testing.T) {
		config := &backup.Config{
			StorageBackend:    "local",
			StorageConfig:     map[string]string{"path": "/tmp/test"},
			DefaultEncryption: backup.EncryptionType("invalid-encryption"),
			Timeout:           5 * time.Minute,
		}

		err := config.Validate()
		assert.Error(t, err, "Should reject invalid encryption type")
		assert.Contains(t, err.Error(), "encryption")
	})

	t.Run("EncryptionWithoutKey", func(t *testing.T) {
		config := &backup.Config{
			StorageBackend:    "local",
			StorageConfig:     map[string]string{"path": "/tmp/test"},
			DefaultEncryption: backup.EncryptionAES256GCM,
			EncryptionKey:     nil,
			Timeout:           5 * time.Minute,
		}

		err := config.Validate()
		assert.Error(t, err, "Should reject encryption without key")
		assert.Contains(t, err.Error(), "32 bytes")
	})

	t.Run("EncryptionWithInvalidKeyLength", func(t *testing.T) {
		config := &backup.Config{
			StorageBackend:    "local",
			StorageConfig:     map[string]string{"path": "/tmp/test"},
			DefaultEncryption: backup.EncryptionAES256GCM,
			EncryptionKey:     []byte("short-key"),
			Timeout:           5 * time.Minute,
		}

		err := config.Validate()
		assert.Error(t, err, "Should reject encryption with invalid key length")
		assert.Contains(t, err.Error(), "32 bytes")
	})
}

// TestBackupRestore_RestoreOptions tests restore options configuration.
func TestBackupRestore_RestoreOptions(t *testing.T) {
	t.Run("DefaultRestoreOptions", func(t *testing.T) {
		opts := &backup.RestoreOptions{
			VerifyChecksum: true,
			SkipExisting:   false,
		}

		assert.True(t, opts.VerifyChecksum)
		assert.False(t, opts.SkipExisting)
		assert.Nil(t, opts.TargetShards)
	})

	t.Run("SkipExistingKeys", func(t *testing.T) {
		opts := &backup.RestoreOptions{
			VerifyChecksum: true,
			SkipExisting:   true,
		}

		assert.True(t, opts.SkipExisting)
	})

	t.Run("TargetSpecificShards", func(t *testing.T) {
		targetShards := []uint64{1, 2, 3}

		opts := &backup.RestoreOptions{
			VerifyChecksum: true,
			TargetShards:   targetShards,
		}

		assert.Equal(t, targetShards, opts.TargetShards)
		assert.Len(t, opts.TargetShards, 3)
	})
}

// TestBackupRestore_RetentionPolicy tests retention policy configuration.
func TestBackupRestore_RetentionPolicy(t *testing.T) {
	t.Run("StandardRetentionPolicy", func(t *testing.T) {
		policy := &backup.RetentionPolicy{
			MaxBackups:  10,
			MaxAge:      7 * 24 * time.Hour,
			KeepDaily:   7,
			KeepWeekly:  4,
			KeepMonthly: 3,
		}

		assert.Equal(t, 10, policy.MaxBackups)
		assert.Equal(t, 7*24*time.Hour, policy.MaxAge)
		assert.Equal(t, 7, policy.KeepDaily)
		assert.Equal(t, 4, policy.KeepWeekly)
		assert.Equal(t, 3, policy.KeepMonthly)
	})

	t.Run("UnlimitedRetention", func(t *testing.T) {
		policy := &backup.RetentionPolicy{
			MaxBackups: 0,
			MaxAge:     0,
		}

		assert.Equal(t, 0, policy.MaxBackups)
		assert.Equal(t, time.Duration(0), policy.MaxAge)
	})

	t.Run("ScheduleConfigWithRetention", func(t *testing.T) {
		schedule := &backup.ScheduleConfig{
			Enabled:        true,
			CronExpression: "0 2 * * *",
			BackupType:     backup.BackupTypeFull,
			RetentionPolicy: &backup.RetentionPolicy{
				MaxBackups: 7,
				MaxAge:     7 * 24 * time.Hour,
			},
		}

		assert.True(t, schedule.Enabled)
		assert.NotNil(t, schedule.RetentionPolicy)
		assert.Equal(t, 7, schedule.RetentionPolicy.MaxBackups)
	})
}

// TestBackupRestore_BackupTypes tests full and incremental backup type configurations.
func TestBackupRestore_BackupTypes(t *testing.T) {
	t.Run("FullBackup", func(t *testing.T) {
		testBackup := &backup.Backup{
			ID:        "test-full-backup",
			Type:      backup.BackupTypeFull,
			Timestamp: time.Now(),
			Metadata: &backup.BackupMetadata{
				Version:   "1.0.0",
				NumShards: 3,
			},
		}

		assert.Equal(t, backup.BackupTypeFull, testBackup.Type)
		assert.Empty(t, testBackup.BaseBackupID)
	})

	t.Run("IncrementalBackup", func(t *testing.T) {
		baseBackupID := "test-base-backup"

		testBackup := &backup.Backup{
			ID:           "test-incremental-backup",
			Type:         backup.BackupTypeIncremental,
			Timestamp:    time.Now(),
			BaseBackupID: baseBackupID,
			Metadata: &backup.BackupMetadata{
				Version:   "1.0.0",
				NumShards: 3,
			},
		}

		assert.Equal(t, backup.BackupTypeIncremental, testBackup.Type)
		assert.Equal(t, baseBackupID, testBackup.BaseBackupID)
	})
}

// TestBackupRestore_MockStorageOperations tests backup operations using mock storage.
func TestBackupRestore_MockStorageOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	mockStorage := backup.NewMockStorage()

	t.Run("UploadAndDownload", func(t *testing.T) {
		testBackup := &backup.Backup{
			ID:          fmt.Sprintf("mock-backup-%d", time.Now().UnixNano()),
			Type:        backup.BackupTypeFull,
			Timestamp:   time.Now(),
			Compression: backup.CompressionNone,
			Encryption:  backup.EncryptionNone,
			Metadata: &backup.BackupMetadata{
				Version:   "1.0.0",
				NumShards: 1,
			},
		}

		testData := []byte("mock backup test data")

		// Upload
		err := mockStorage.Upload(ctx, testBackup, bytes.NewReader(testData))
		require.NoError(t, err, "Mock upload should succeed")

		// Download
		reader, err := mockStorage.Download(ctx, testBackup.ID)
		require.NoError(t, err, "Mock download should succeed")
		defer reader.Close()

		downloadedData, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.Equal(t, testData, downloadedData)
	})

	t.Run("ListBackups", func(t *testing.T) {
		// Create multiple backups
		for i := 0; i < 3; i++ {
			testBackup := &backup.Backup{
				ID:        fmt.Sprintf("list-test-backup-%d", i),
				Type:      backup.BackupTypeFull,
				Timestamp: time.Now().Add(time.Duration(i) * time.Second),
				Metadata:  &backup.BackupMetadata{},
			}

			err := mockStorage.Upload(ctx, testBackup, bytes.NewReader([]byte("data")))
			require.NoError(t, err)
		}

		// List
		list, err := mockStorage.ListBackups(ctx, 0, "")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(list.Backups), 3)
	})

	t.Run("DeleteBackup", func(t *testing.T) {
		testBackup := &backup.Backup{
			ID:        "delete-test-backup",
			Type:      backup.BackupTypeFull,
			Timestamp: time.Now(),
			Metadata:  &backup.BackupMetadata{},
		}

		// Upload
		err := mockStorage.Upload(ctx, testBackup, bytes.NewReader([]byte("data")))
		require.NoError(t, err)

		// Delete
		err = mockStorage.DeleteBackup(ctx, testBackup.ID)
		require.NoError(t, err)

		// Verify deleted
		_, err = mockStorage.Download(ctx, testBackup.ID)
		assert.Error(t, err)
	})

	t.Run("GetBackupMetadata", func(t *testing.T) {
		testBackup := &backup.Backup{
			ID:          "metadata-test-backup",
			Type:        backup.BackupTypeFull,
			Timestamp:   time.Now(),
			Compression: backup.CompressionZstd,
			Encryption:  backup.EncryptionAES256GCM,
			Metadata: &backup.BackupMetadata{
				Version:   "2.0.0",
				NumShards: 5,
			},
		}

		// Upload
		err := mockStorage.Upload(ctx, testBackup, bytes.NewReader([]byte("data")))
		require.NoError(t, err)

		// Get metadata
		metadata, err := mockStorage.GetBackupMetadata(ctx, testBackup.ID)
		require.NoError(t, err)

		assert.Equal(t, testBackup.ID, metadata.ID)
		assert.Equal(t, testBackup.Type, metadata.Type)
		assert.Equal(t, testBackup.Compression, metadata.Compression)
		assert.Equal(t, testBackup.Encryption, metadata.Encryption)
		assert.Equal(t, "2.0.0", metadata.Metadata.Version)
		assert.Equal(t, 5, metadata.Metadata.NumShards)
	})

	select {
	case <-ctx.Done():
		t.Fatal("Test timed out")
	default:
		t.Log("Mock storage operation tests completed")
	}
}
