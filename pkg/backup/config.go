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
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// Config contains configuration for the backup manager.
type Config struct {
	// StorageBackend specifies the storage backend type.
	// Supported: "s3", "gcs", "azure", "local"
	StorageBackend string

	// StorageConfig contains backend-specific configuration.
	StorageConfig map[string]string

	// DefaultCompression specifies the default compression algorithm.
	DefaultCompression CompressionType

	// DefaultEncryption specifies the default encryption algorithm.
	DefaultEncryption EncryptionType

	// EncryptionKey is the encryption key (32 bytes for AES-256).
	EncryptionKey []byte

	// Schedule contains scheduled backup configuration.
	Schedule *ScheduleConfig

	// WorkDir is the temporary directory for backup operations.
	WorkDir string

	// Timeout for backup operations.
	Timeout time.Duration
}

// Validate validates the backup configuration.
func (c *Config) Validate() error {
	if c.StorageBackend == "" {
		return api.NewRequiredFieldError("storage backend")
	}

	validBackends := map[string]bool{
		"s3":    true,
		"gcs":   true,
		"azure": true,
		"local": true,
		"minio": true,
		"mock":  true, // For testing
	}
	if !validBackends[c.StorageBackend] {
		return api.NewUnsupportedValueError("backup.Config.Validate", "storage backend", c.StorageBackend)
	}

	if c.DefaultCompression != "" {
		validCompression := map[CompressionType]bool{
			CompressionNone: true,
			CompressionGzip: true,
			CompressionZstd: true,
		}
		if !validCompression[c.DefaultCompression] {
			return api.NewUnsupportedValueError("backup.Config.Validate", "compression type", string(c.DefaultCompression))
		}
	}

	if c.DefaultEncryption != "" {
		validEncryption := map[EncryptionType]bool{
			EncryptionNone:      true,
			EncryptionAES256GCM: true,
		}
		if !validEncryption[c.DefaultEncryption] {
			return api.NewUnsupportedValueError("backup.Config.Validate", "encryption type", string(c.DefaultEncryption))
		}
	}

	if c.DefaultEncryption == EncryptionAES256GCM {
		if len(c.EncryptionKey) != 32 {
			return api.NewKeySizeError("backup.Config.Validate", "AES-256", 32, len(c.EncryptionKey))
		}
	}

	if c.Timeout <= 0 {
		c.Timeout = 30 * time.Minute // Default timeout
	}

	if c.WorkDir == "" {
		c.WorkDir = "/tmp/quicbaas-backups"
	}

	return nil
}

// WithDefaults returns a config with default values applied.
func (c *Config) WithDefaults() *Config {
	cfg := *c

	if cfg.DefaultCompression == "" {
		cfg.DefaultCompression = CompressionZstd
	}

	if cfg.DefaultEncryption == "" {
		cfg.DefaultEncryption = EncryptionNone
	}

	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Minute
	}

	if cfg.WorkDir == "" {
		cfg.WorkDir = "/tmp/quicbaas-backups"
	}

	return &cfg
}
