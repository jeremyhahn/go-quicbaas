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
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// initVectorStorage is a no-op. The qrdb node always owns vector storage;
// startVector wires the node's SDK transport directly.
func (s *Server) initVectorStorage() error {
	if !s.config.Vector.Enabled {
		s.logger.Info("Vector storage is disabled, skipping initialization")
		return nil
	}
	// The qrdb engine node owns the vector storage and at-rest encryption.
	// Building a separate PebbleDB store here would create a disconnected
	// duplicate. startVector wires the node's SDK client transport.
	s.logger.Info("Vector storage owned by qrdb node, skipping standalone storage init")
	return nil
}

// startVector creates the vector service backed by the qrdb node's SDK
// client embedded transport. The node owns the vector engine (PebbleDB-backed)
// and its at-rest encryptor. If vector is not enabled in config, this is a no-op.
func (s *Server) startVector() error {
	if !s.config.Vector.Enabled {
		s.logger.Info("Vector service is disabled, skipping initialization")
		return nil
	}

	s.logger.Info("Initializing vector service...")

	nodeClient := s.qrdbNode.Client()
	nodeStorage := newNodeVectorStorage(nodeClient)
	logAdapter := logger.NewAdapter(s.logger)
	opts := []service.VectorServiceOption{
		service.WithVectorShardID(s.config.Vector.ShardID),
		service.WithVectorIndexFactory(NewVectorIndexFactory()),
	}
	vectorSvc, err := service.NewVectorService(nodeStorage, logAdapter, opts...)
	if err != nil {
		return &qberrors.QBError{
			Code: qberrors.ErrInternal,
			Op:   "server.startVector",
			Err:  &ServerCauseError{Message: "failed to create node-backed vector service", Cause: err},
		}
	}
	s.vectorService = vectorSvc
	s.logger.WithFields(map[string]interface{}{
		"shard_id": s.config.Vector.ShardID,
	}).Info("Vector service initialized via qrdb node SDK transport")
	return nil
}
