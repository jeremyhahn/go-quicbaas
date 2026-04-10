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

package file

import (
	"github.com/jeremyhahn/go-codec"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	pkgStorage "github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// PartitionConfig holds configuration for a storage partition.
// This replaces the deleted partition.Config type.
type PartitionConfig struct {
	// DataDir is the directory path for partition data.
	DataDir string
}

// Factory creates file storage instances for partitions.
type Factory struct {
	logger    *logger.Logger
	codecType codec.Type
}

// FactoryOption is a functional option for configuring the factory.
type FactoryOption func(*Factory)

// WithCodecType sets the codec type for file storage.
func WithCodecType(codecType codec.Type) FactoryOption {
	return func(f *Factory) {
		f.codecType = codecType
	}
}

// NewFactory creates a new file partition factory.
func NewFactory(log *logger.Logger, opts ...FactoryOption) *Factory {
	f := &Factory{
		logger:    log,
		codecType: codec.JSON, // Default
	}

	for _, opt := range opts {
		opt(f)
	}

	return f
}

// Create creates a storage instance for the given partition config.
// The partitionName is used for logging and identification.
func (f *Factory) Create(partitionName string, config *PartitionConfig) (pkgStorage.Storage, error) {
	if config == nil {
		return nil, errors.New(errors.ErrInvalidInput, "FileFactory.Create", "config cannot be nil")
	}

	if config.DataDir == "" {
		return nil, errors.New(errors.ErrInvalidInput, "FileFactory.Create", "data directory is required")
	}

	log := f.logger
	if log == nil {
		var err error
		log, err = logger.New(logger.Default())
		if err != nil {
			return nil, errors.Wrap(err, errors.ErrInternal, "FileFactory.Create")
		}
	}

	fileConfig := &Config{
		DataDir:         config.DataDir,
		CodecType:       f.codecType,
		FilePermissions: DefaultFilePermissions,
		DirPermissions:  DefaultDirPermissions,
		SyncWrites:      DefaultSyncWrites,
	}

	storage, err := New(fileConfig, log.WithField("partition", partitionName))
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrStorage, "FileFactory.Create")
	}

	return storage, nil
}

// BackendType returns the storage backend type.
func (f *Factory) BackendType() string {
	return "file"
}
