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
	"context"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// CreateDatabase creates a new database for the tenant in context.
func (t *baasTransport) CreateDatabase(ctx context.Context, req transport.CreateDatabaseRequest) (*transport.DatabaseInfo, error) {
	if t.dbService == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "CreateDatabase"}
	}
	svcReq := &service.CreateDatabaseRequest{
		Name:    req.Name,
		Engine:  req.Engine,
		Backend: req.Backend,
	}
	info, err := t.dbService.CreateDatabase(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertToTransportDatabaseInfo(info), nil
}

// GetDatabase returns information about a specific database.
func (t *baasTransport) GetDatabase(ctx context.Context, name string) (*transport.DatabaseInfo, error) {
	if t.dbService == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "GetDatabase"}
	}
	info, err := t.dbService.GetDatabase(ctx, name)
	if err != nil {
		return nil, err
	}
	return convertToTransportDatabaseInfo(info), nil
}

// ListDatabases returns information about all tenant databases.
func (t *baasTransport) ListDatabases(ctx context.Context) ([]transport.DatabaseInfo, error) {
	if t.dbService == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "ListDatabases"}
	}
	infos, err := t.dbService.ListDatabases(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]transport.DatabaseInfo, len(infos))
	for i, info := range infos {
		result[i] = *convertToTransportDatabaseInfo(info)
	}
	return result, nil
}

// DropDatabase removes a database by name.
func (t *baasTransport) DropDatabase(ctx context.Context, name string) error {
	if t.dbService == nil {
		return &BaasTransportNotSupportedError{Operation: "DropDatabase"}
	}
	return t.dbService.DropDatabase(ctx, name)
}

// UpdateDatabaseProtocol updates wire protocol configuration.
func (t *baasTransport) UpdateDatabaseProtocol(ctx context.Context, req transport.UpdateDatabaseProtocolRequest) (*transport.DatabaseInfo, error) {
	if t.dbService == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "UpdateDatabaseProtocol"}
	}
	protocol := service.DatabaseProtocolConfig{
		MySQL:      req.MySQL,
		PostgreSQL: req.PostgreSQL,
	}
	info, err := t.dbService.UpdateDatabaseProtocol(ctx, req.Name, protocol)
	if err != nil {
		return nil, err
	}
	return convertToTransportDatabaseInfo(info), nil
}

// UpdateDatabaseEtcd updates etcd wire protocol configuration.
func (t *baasTransport) UpdateDatabaseEtcd(ctx context.Context, req transport.UpdateDatabaseEtcdRequest) (*transport.DatabaseInfo, error) {
	if t.dbService == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "UpdateDatabaseEtcd"}
	}
	etcd := &service.DatabaseEtcdConfig{
		Enabled:    req.Enabled,
		ListenAddr: req.ListenAddr,
	}
	info, err := t.dbService.UpdateDatabaseEtcd(ctx, req.Name, etcd)
	if err != nil {
		return nil, err
	}
	return convertToTransportDatabaseInfo(info), nil
}

// UpdateDatabaseRaft updates Raft consensus configuration.
func (t *baasTransport) UpdateDatabaseRaft(ctx context.Context, req transport.UpdateDatabaseRaftRequest) (*transport.DatabaseInfo, error) {
	if t.dbService == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "UpdateDatabaseRaft"}
	}
	var raft *service.DatabaseRaftConfig
	if req.Raft != nil {
		raft = &service.DatabaseRaftConfig{
			ClusterMode: req.Raft.ClusterMode,
			ShardID:     req.Raft.ShardID,
		}
	}
	info, err := t.dbService.UpdateDatabaseRaft(ctx, req.Name, raft)
	if err != nil {
		return nil, err
	}
	return convertToTransportDatabaseInfo(info), nil
}

// UpdateDatabaseBarrier updates encryption barrier configuration.
func (t *baasTransport) UpdateDatabaseBarrier(ctx context.Context, req transport.UpdateDatabaseBarrierRequest) (*transport.DatabaseInfo, error) {
	if t.dbService == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "UpdateDatabaseBarrier"}
	}
	var barrier *service.DatabaseBarrierConfig
	if req.Barrier != nil {
		barrier = &service.DatabaseBarrierConfig{
			Enabled:        req.Barrier.Enabled,
			Mode:           req.Barrier.Mode,
			SharesRequired: req.Barrier.SharesRequired,
			SharesTotal:    req.Barrier.SharesTotal,
		}
	}
	info, err := t.dbService.UpdateDatabaseBarrier(ctx, req.Name, barrier)
	if err != nil {
		return nil, err
	}
	return convertToTransportDatabaseInfo(info), nil
}

// UpdateDatabaseConnection updates remote connection configuration.
func (t *baasTransport) UpdateDatabaseConnection(ctx context.Context, req transport.UpdateDatabaseConnectionRequest) (*transport.DatabaseInfo, error) {
	if t.dbService == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "UpdateDatabaseConnection"}
	}
	var conn *service.DatabaseConnectionConfig
	if req.Connection != nil {
		conn = &service.DatabaseConnectionConfig{
			Transport: req.Connection.Transport,
			Address:   req.Connection.Address,
		}
	}
	info, err := t.dbService.UpdateDatabaseConnection(ctx, req.Name, conn)
	if err != nil {
		return nil, err
	}
	return convertToTransportDatabaseInfo(info), nil
}

// convertToTransportDatabaseInfo converts a service DatabaseInfo to
// the SDK transport DatabaseInfo format.
func convertToTransportDatabaseInfo(info *service.DatabaseInfo) *transport.DatabaseInfo {
	tInfo := &transport.DatabaseInfo{
		TenantID: info.TenantID,
		Name:     info.Name,
		Engine:   info.Engine,
		Backend:  info.Backend,
		Status:   info.Status,
	}

	// Map protocol configuration.
	tInfo.Protocol.MySQL = info.Protocol.MySQL
	tInfo.Protocol.PostgreSQL = info.Protocol.PostgreSQL
	if info.Protocol.Etcd != nil {
		tInfo.Protocol.Etcd = &transport.EtcdProtocolConfig{
			Enabled:    info.Protocol.Etcd.Enabled,
			ListenAddr: info.Protocol.Etcd.ListenAddr,
		}
	}

	// Map Raft configuration.
	if info.Raft != nil {
		tInfo.Raft = &transport.DatabaseRaftConfig{
			ClusterMode: info.Raft.ClusterMode,
			ShardID:     info.Raft.ShardID,
		}
	}

	// Map barrier configuration.
	if info.Barrier != nil {
		tInfo.Barrier = &transport.DatabaseBarrierConfig{
			Enabled:        info.Barrier.Enabled,
			Mode:           info.Barrier.Mode,
			SharesRequired: info.Barrier.SharesRequired,
			SharesTotal:    info.Barrier.SharesTotal,
		}
	}

	// Map connection configuration.
	if info.Connection != nil {
		tInfo.Connection = &transport.DatabaseConnectionConfig{
			Transport: info.Connection.Transport,
			Address:   info.Connection.Address,
		}
	}

	return tInfo
}
