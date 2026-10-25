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

import (
	"context"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// databaseService implements DatabaseService by delegating to the qrdb
// DatabaseServicer tenant-scoped methods. Tenant enforcement is handled
// by qrdb using the tenant ID embedded in the request context.
type databaseService struct {
	qrdb qrdbsdk.DatabaseServicer
}

// Compile-time interface check.
var _ DatabaseService = (*databaseService)(nil)

// NewDatabaseService creates a new DatabaseService backed by a qrdb DatabaseServicer.
func NewDatabaseService(qrdb qrdbsdk.DatabaseServicer) DatabaseService {
	return &databaseService{qrdb: qrdb}
}

// CreateDatabase provisions a new database for the tenant in context.
func (s *databaseService) CreateDatabase(ctx context.Context, req *CreateDatabaseRequest) (*DatabaseInfo, error) {
	if req == nil {
		return nil, &DatabaseCreateError{
			Name: "",
			Err:  &InvalidRequestError{Detail: "create database request is nil"},
		}
	}
	cfg := qrdbsdk.DatabaseConfig{
		Name:    req.Name,
		Backend: qrdbsdk.BackendType(req.Backend),
	}
	info, err := s.qrdb.CreateForTenant(ctx, cfg)
	if err != nil {
		return nil, &DatabaseCreateError{Name: req.Name, Err: err}
	}
	return convertDatabaseInfo(info), nil
}

// GetDatabase returns information about a specific database by name.
func (s *databaseService) GetDatabase(ctx context.Context, name string) (*DatabaseInfo, error) {
	info, err := s.qrdb.GetForTenant(ctx, name)
	if err != nil {
		return nil, err
	}
	return convertDatabaseInfo(info), nil
}

// ListDatabases returns information about all databases owned by the tenant in context.
func (s *databaseService) ListDatabases(ctx context.Context) ([]*DatabaseInfo, error) {
	infos, err := s.qrdb.ListForTenant(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*DatabaseInfo, len(infos))
	for i := range infos {
		result[i] = convertDatabaseInfo(&infos[i])
	}
	return result, nil
}

// DropDatabase removes a database by name.
func (s *databaseService) DropDatabase(ctx context.Context, name string) error {
	if err := s.qrdb.DropForTenant(ctx, name); err != nil {
		return &DatabaseDropError{Name: name, Err: err}
	}
	return nil
}

// UpdateDatabaseProtocol updates wire protocol configuration for a database.
func (s *databaseService) UpdateDatabaseProtocol(ctx context.Context, name string, protocol DatabaseProtocolConfig) (*DatabaseInfo, error) {
	qrdbProto := qrdbsdk.ProtocolConfig{
		MySQL:      protocol.MySQL,
		PostgreSQL: protocol.PostgreSQL,
	}
	if protocol.Etcd != nil {
		qrdbProto.Etcd = &qrdbsdk.EtcdConfig{
			Enabled:    protocol.Etcd.Enabled,
			ListenAddr: protocol.Etcd.ListenAddr,
		}
	}
	info, err := s.qrdb.UpdateProtocolForTenant(ctx, name, qrdbProto)
	if err != nil {
		return nil, &DatabaseUpdateError{Name: name, Operation: "protocol", Err: err}
	}
	return convertDatabaseInfo(info), nil
}

// UpdateDatabaseEtcd updates etcd wire protocol configuration for a database.
func (s *databaseService) UpdateDatabaseEtcd(ctx context.Context, name string, etcd *DatabaseEtcdConfig) (*DatabaseInfo, error) {
	var qrdbEtcd *qrdbsdk.EtcdConfig
	if etcd != nil {
		qrdbEtcd = &qrdbsdk.EtcdConfig{
			Enabled:    etcd.Enabled,
			ListenAddr: etcd.ListenAddr,
		}
	}
	info, err := s.qrdb.UpdateEtcdForTenant(ctx, name, qrdbEtcd)
	if err != nil {
		return nil, &DatabaseUpdateError{Name: name, Operation: "etcd", Err: err}
	}
	return convertDatabaseInfo(info), nil
}

// UpdateDatabaseRaft updates Raft consensus configuration for a database.
func (s *databaseService) UpdateDatabaseRaft(ctx context.Context, name string, raft *DatabaseRaftConfig) (*DatabaseInfo, error) {
	var qrdbRaft *qrdbsdk.DatabaseRaftConfig
	if raft != nil {
		qrdbRaft = &qrdbsdk.DatabaseRaftConfig{
			ClusterMode: qrdbsdk.RaftClusterMode(raft.ClusterMode),
			ShardID:     raft.ShardID,
		}
	}
	info, err := s.qrdb.UpdateRaftForTenant(ctx, name, qrdbRaft)
	if err != nil {
		return nil, &DatabaseUpdateError{Name: name, Operation: "raft", Err: err}
	}
	return convertDatabaseInfo(info), nil
}

// UpdateDatabaseBarrier updates encryption barrier configuration for a database.
func (s *databaseService) UpdateDatabaseBarrier(ctx context.Context, name string, barrier *DatabaseBarrierConfig) (*DatabaseInfo, error) {
	var qrdbBarrier *qrdbsdk.DatabaseBarrierConfig
	if barrier != nil {
		qrdbBarrier = &qrdbsdk.DatabaseBarrierConfig{
			Enabled:        barrier.Enabled,
			Mode:           barrier.Mode,
			SharesRequired: barrier.SharesRequired,
			SharesTotal:    barrier.SharesTotal,
		}
	}
	info, err := s.qrdb.UpdateBarrierForTenant(ctx, name, qrdbBarrier)
	if err != nil {
		return nil, &DatabaseUpdateError{Name: name, Operation: "barrier", Err: err}
	}
	return convertDatabaseInfo(info), nil
}

// UpdateDatabaseConnection updates remote connection configuration for a database.
func (s *databaseService) UpdateDatabaseConnection(ctx context.Context, name string, conn *DatabaseConnectionConfig) (*DatabaseInfo, error) {
	var qrdbConn *qrdbsdk.DatabaseConnectionConfig
	if conn != nil {
		qrdbConn = &qrdbsdk.DatabaseConnectionConfig{
			Transport: conn.Transport,
			Address:   conn.Address,
		}
	}
	info, err := s.qrdb.UpdateConnectionForTenant(ctx, name, qrdbConn)
	if err != nil {
		return nil, &DatabaseUpdateError{Name: name, Operation: "connection", Err: err}
	}
	return convertDatabaseInfo(info), nil
}

// convertDatabaseInfo converts a qrdb DatabaseInfo to a BaaS DatabaseInfo.
func convertDatabaseInfo(info *qrdbsdk.DatabaseInfo) *DatabaseInfo {
	di := &DatabaseInfo{
		TenantID: info.TenantID,
		Name:     info.Name,
		Backend:  string(info.Backend),
		Status:   string(info.Status),
		Protocol: DatabaseProtocolConfig{
			MySQL:      info.Protocol.MySQL,
			PostgreSQL: info.Protocol.PostgreSQL,
		},
	}
	if info.Protocol.Etcd != nil {
		di.Protocol.Etcd = &DatabaseEtcdConfig{
			Enabled:    info.Protocol.Etcd.Enabled,
			ListenAddr: info.Protocol.Etcd.ListenAddr,
		}
	}
	if info.Raft != nil {
		di.Raft = &DatabaseRaftConfig{
			ClusterMode: string(info.Raft.ClusterMode),
			ShardID:     info.Raft.ShardID,
		}
	}
	if info.Barrier != nil {
		di.Barrier = &DatabaseBarrierConfig{
			Enabled:        info.Barrier.Enabled,
			Mode:           info.Barrier.Mode,
			SharesRequired: info.Barrier.SharesRequired,
			SharesTotal:    info.Barrier.SharesTotal,
		}
	}
	if info.Connection != nil {
		di.Connection = &DatabaseConnectionConfig{
			Transport: info.Connection.Transport,
			Address:   info.Connection.Address,
		}
	}
	return di
}
