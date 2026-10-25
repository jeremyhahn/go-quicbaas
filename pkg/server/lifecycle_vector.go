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
	"context"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
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

	// A request's vector calls run as its authenticated caller, in its
	// tenant; calls with no caller run as the node's service principal. The
	// service confines every namespace to the tenant either way.
	nodeStorage := newNodeVectorStorage(s.serviceVector(s.getQRDBNode().Client().Vector()))
	logAdapter := logger.NewAdapter(s.logger)
	// The startup namespace load has no end user behind it: it runs as the
	// node's service principal in the system tenant.
	loadCtx, err := s.ServiceContext(tenantctx.WithSystem(context.Background()))
	if err != nil {
		return &qberrors.QBError{
			Code: qberrors.ErrInternal,
			Op:   "server.startVector",
			Err:  &ServerCauseError{Message: "failed to bind the vector load to the service principal", Cause: err},
		}
	}
	opts := []service.VectorServiceOption{
		service.WithVectorShardID(s.config.Vector.ShardID),
		service.WithVectorIndexFactory(NewVectorIndexFactory()),
		service.WithVectorLoadContext(loadCtx),
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
