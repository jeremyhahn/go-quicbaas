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

package service

import "context"

// DatabaseService defines the service-layer interface for database
// lifecycle management. It provides create, read, update, and delete
// operations for tenant databases and their associated configuration.
type DatabaseService interface {
	// CreateDatabase provisions a new database for the tenant in context.
	CreateDatabase(ctx context.Context, req *CreateDatabaseRequest) (*DatabaseInfo, error)

	// GetDatabase returns information about a specific database by name.
	GetDatabase(ctx context.Context, name string) (*DatabaseInfo, error)

	// ListDatabases returns information about all databases owned by the
	// tenant in context.
	ListDatabases(ctx context.Context) ([]*DatabaseInfo, error)

	// DropDatabase removes a database by name.
	DropDatabase(ctx context.Context, name string) error

	// UpdateDatabaseProtocol updates wire protocol configuration for a database.
	UpdateDatabaseProtocol(ctx context.Context, name string, protocol DatabaseProtocolConfig) (*DatabaseInfo, error)

	// UpdateDatabaseEtcd updates etcd wire protocol configuration for a database.
	UpdateDatabaseEtcd(ctx context.Context, name string, etcd *DatabaseEtcdConfig) (*DatabaseInfo, error)

	// UpdateDatabaseRaft updates Raft consensus configuration for a database.
	UpdateDatabaseRaft(ctx context.Context, name string, raft *DatabaseRaftConfig) (*DatabaseInfo, error)

	// UpdateDatabaseBarrier updates encryption barrier configuration for a database.
	UpdateDatabaseBarrier(ctx context.Context, name string, barrier *DatabaseBarrierConfig) (*DatabaseInfo, error)

	// UpdateDatabaseConnection updates remote connection configuration for a database.
	UpdateDatabaseConnection(ctx context.Context, name string, conn *DatabaseConnectionConfig) (*DatabaseInfo, error)
}

// CreateDatabaseRequest holds the parameters for creating a new database.
type CreateDatabaseRequest struct {
	Name    string
	Engine  string
	Backend string
}

// DatabaseInfo holds metadata about a provisioned database.
type DatabaseInfo struct {
	TenantID   string
	Name       string
	Engine     string
	Backend    string
	Status     string
	Protocol   DatabaseProtocolConfig
	Raft       *DatabaseRaftConfig
	Barrier    *DatabaseBarrierConfig
	Connection *DatabaseConnectionConfig
}

// DatabaseProtocolConfig holds wire protocol settings for a database.
type DatabaseProtocolConfig struct {
	MySQL      bool
	PostgreSQL bool
	Etcd       *DatabaseEtcdConfig
}

// DatabaseEtcdConfig holds etcd wire protocol settings.
type DatabaseEtcdConfig struct {
	Enabled    bool
	ListenAddr string
}

// DatabaseRaftConfig holds Raft consensus settings for a database.
type DatabaseRaftConfig struct {
	ClusterMode string
	ShardID     uint64
}

// DatabaseBarrierConfig holds encryption barrier settings for a database.
type DatabaseBarrierConfig struct {
	Enabled        bool
	Mode           string
	SharesRequired int
	SharesTotal    int
}

// DatabaseConnectionConfig holds remote connection settings for a database.
type DatabaseConnectionConfig struct {
	Transport string
	Address   string
}
