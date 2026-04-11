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

//go:build vector

package server

import (
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// initVectorStorage initializes the PebbleDB-backed vector storage engine.
// This must be called before startVector so that vectorStorage is available
// for the vector service to use.
func (s *Server) initVectorStorage() error {
	if !s.config.Vector.Enabled {
		s.logger.Info("Vector storage is disabled, skipping initialization")
		return nil
	}

	s.logger.Info("Initializing vector storage...")

	// Validate vector configuration
	if err := s.config.Vector.Validate(); err != nil {
		return &qberrors.QBError{
			Code: qberrors.ErrConfiguration,
			Op:   "server.initVectorStorage",
			Err:  &ServerCauseError{Message: "invalid vector configuration", Cause: err},
		}
	}

	// Build PebbleDB vector storage configuration from the server config
	storageConfig := xkmssdk.QRDBDefaultVectorStorageConfig(s.config.Vector.StoragePath)

	// Adapt the quicbaas logger to the qrdb logger interface
	qrdbLogger := newQRDBLoggerAdapter(s.logger)

	// Open PebbleDB vector storage
	pebbleStorage, err := xkmssdk.QRDBNewPebbleVectorStorage(storageConfig, qrdbLogger)
	if err != nil {
		return &qberrors.QBError{
			Code: qberrors.ErrInternal,
			Op:   "server.initVectorStorage",
			Err:  &ServerCauseError{Message: "failed to open vector storage", Cause: err},
		}
	}

	// Wrap the qrdb storage with the quicbaas interface adapter
	s.vectorStorage = newVectorStorageAdapter(pebbleStorage)
	s.logger.WithFields(map[string]interface{}{
		"storage_path": s.config.Vector.StoragePath,
		"shard_id":     s.config.Vector.ShardID,
	}).Info("Vector storage initialized successfully")

	return nil
}

// startVector creates the vector service using the pre-initialized storage.
// If vector is not enabled in config, this is a no-op.
// If vectorStorage is nil and vector is enabled, storage is initialized first.
func (s *Server) startVector() error {
	if !s.config.Vector.Enabled {
		s.logger.Info("Vector service is disabled, skipping initialization")
		return nil
	}

	s.logger.Info("Initializing vector service...")

	// If storage was not pre-initialized, create it now
	if s.vectorStorage == nil {
		if err := s.initVectorStorage(); err != nil {
			return err
		}
	}

	// Adapt the quicbaas logger to the quicbaas interfaces.Logger
	logAdapter := logger.NewAdapter(s.logger)

	// Build vector service options with vector index factory for similarity search
	opts := []service.VectorServiceOption{
		service.WithVectorShardID(s.config.Vector.ShardID),
		service.WithVectorIndexFactory(NewVectorIndexFactory()),
	}

	// Create the vector service with storage and options
	vectorSvc, err := service.NewVectorService(s.vectorStorage, logAdapter, opts...)
	if err != nil {
		return &qberrors.QBError{
			Code: qberrors.ErrInternal,
			Op:   "server.startVector",
			Err:  &ServerCauseError{Message: "failed to create vector service", Cause: err},
		}
	}

	s.vectorService = vectorSvc
	s.logger.WithFields(map[string]interface{}{
		"shard_id":     s.config.Vector.ShardID,
		"storage_path": s.config.Vector.StoragePath,
	}).Info("Vector service initialized successfully")

	return nil
}
