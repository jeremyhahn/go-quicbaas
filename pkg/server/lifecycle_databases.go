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

package server

import (
	"log/slog"
	"path/filepath"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// startDatabases initializes the database management subsystem.
//
// This creates a qrdb database.Manager for tenant database lifecycle
// management and wires it through the service layers:
//
//	qrdb database.Manager
//	  → qrdb service.DatabaseService (DatabaseServicer)
//	    → QuicBaaS service.DatabaseService
//	      → baasTransport.dbService (SDK transport layer)
//
// Database recovery of existing tenant databases occurs automatically
// via the qrdb Manager.OpenAll call during initialization.
func (s *Server) startDatabases() error {
	if !s.config.Databases.Enabled {
		s.logger.Info("Database management disabled")
		return nil
	}

	if s.quicBaaSService == nil {
		s.logger.Warn("Skipping database initialization: service not available")
		return nil
	}

	s.logger.Info("Initializing database management subsystem...")

	// Resolve the data directory for tenant databases.
	dataDir := s.config.Databases.DataDir
	if dataDir == "" {
		dataDir = filepath.Join(s.config.Storage.DataDir, "databases")
	}

	// Create the qrdb database.Manager that manages tenant database
	// lifecycle (create, open, drop, list). The Manager persists metadata
	// in meta.json files within each database directory.
	dbManager := qrdbsdk.NewManager(
		slog.Default(),
		dataDir,
	)

	// Open all previously created databases from disk.
	// Each database directory contains a meta.json that records the
	// database configuration including tenant ownership.
	if openErr := dbManager.OpenAll(); openErr != nil {
		s.logger.WithError(openErr).Warn("Some databases failed to open during recovery")
		// Non-fatal: individual database failures should not prevent
		// the server from starting. Failed databases will be in
		// StatusClosed and can be recovered manually.
	}

	opened := dbManager.List()
	s.logger.Infof("Database recovery complete: %d databases opened", len(opened))

	// Create the qrdb DatabaseServicer that provides tenant-scoped
	// database CRUD operations.
	qrdbDBSvc := qrdbsdk.NewDatabaseService(dbManager)

	// Create the QuicBaaS DatabaseService that wraps qrdb's
	// DatabaseServicer with BaaS-level type conversions.
	dbSvc := service.NewDatabaseService(qrdbDBSvc)

	// Wire into the service facade.
	s.quicBaaSService.SetDatabaseService(dbSvc)
	s.logger.Info("DatabaseService attached to service facade")

	s.logger.Info("Database management subsystem initialized")
	return nil
}
