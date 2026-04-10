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

// Package file provides a file-based storage backend for QuicBaaS.
// Files are stored with codec-appropriate extensions and configurable permissions.
package file

import (
	"os"

	"github.com/jeremyhahn/go-codec"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Default configuration values.
const (
	// DefaultFilePermissions is the default file mode for created files.
	DefaultFilePermissions os.FileMode = 0644

	// DefaultDirPermissions is the default directory mode for created directories.
	DefaultDirPermissions os.FileMode = 0755

	// DefaultSyncWrites enables fsync after each write for durability.
	DefaultSyncWrites = true
)

// Config holds FileStorage configuration options.
type Config struct {
	// DataDir is the root directory where files are stored.
	// This field is required and must be a valid path.
	DataDir string

	// CodecType specifies the serialization format for stored data.
	// Default: CodecJSON
	CodecType codec.Type

	// FilePermissions is the file mode applied to created files.
	// Default: 0644
	FilePermissions os.FileMode

	// DirPermissions is the directory mode applied to created directories.
	// Default: 0755
	DirPermissions os.FileMode

	// SyncWrites ensures writes are synced to disk before returning.
	// When true, each write operation calls fsync for durability.
	// Default: true
	SyncWrites bool
}

// DefaultConfig returns a Config with sensible default values.
// DataDir must still be set before use.
func DefaultConfig() *Config {
	return &Config{
		DataDir:         "",
		CodecType:       codec.JSON,
		FilePermissions: DefaultFilePermissions,
		DirPermissions:  DefaultDirPermissions,
		SyncWrites:      DefaultSyncWrites,
	}
}

// Validate validates the configuration and returns an error if invalid.
func (c *Config) Validate() error {
	if c.DataDir == "" {
		return errors.New(
			errors.ErrConfiguration,
			"file.Config.Validate",
			"data directory is required",
		)
	}

	// Validate file permissions - must not be zero
	if c.FilePermissions == 0 {
		return errors.New(
			errors.ErrConfiguration,
			"file.Config.Validate",
			"file permissions cannot be zero",
		)
	}

	// Validate directory permissions - must not be zero
	if c.DirPermissions == 0 {
		return errors.New(
			errors.ErrConfiguration,
			"file.Config.Validate",
			"directory permissions cannot be zero",
		)
	}

	// Validate codec type - ensure it's a known type
	switch c.CodecType {
	case codec.JSON, codec.YAML, codec.TOML, codec.MsgPack,
		codec.BSON, codec.ProtoBuf, codec.CBOR:
		// Valid codec types
	case "":
		return errors.New(
			errors.ErrConfiguration,
			"file.Config.Validate",
			"codec type is required",
		)
	default:
		return errors.Newf(
			errors.ErrConfiguration,
			"file.Config.Validate",
			"unsupported codec type: %s", c.CodecType,
		)
	}

	return nil
}

// ExtensionForCodec returns the file extension for the given codec type.
// The extension includes the leading dot (e.g., ".json").
func ExtensionForCodec(codecType codec.Type) string {
	switch codecType {
	case codec.JSON:
		return ".json"
	case codec.YAML:
		return ".yaml"
	case codec.TOML:
		return ".toml"
	case codec.MsgPack:
		return ".msgpack"
	case codec.BSON:
		return ".bson"
	case codec.ProtoBuf:
		return ".pb"
	case codec.CBOR:
		return ".cbor"
	default:
		return ".dat"
	}
}
