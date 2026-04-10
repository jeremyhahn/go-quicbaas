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

//go:build !integration

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ---------------------------------------------------------------------------
// Mock DatabaseService for transport tests
// ---------------------------------------------------------------------------

// mockDatabaseService implements service.DatabaseService for testing.
type mockDatabaseService struct {
	createFn           func(ctx context.Context, req *service.CreateDatabaseRequest) (*service.DatabaseInfo, error)
	getFn              func(ctx context.Context, name string) (*service.DatabaseInfo, error)
	listFn             func(ctx context.Context) ([]*service.DatabaseInfo, error)
	dropFn             func(ctx context.Context, name string) error
	updateProtocolFn   func(ctx context.Context, name string, protocol service.DatabaseProtocolConfig) (*service.DatabaseInfo, error)
	updateEtcdFn       func(ctx context.Context, name string, etcd *service.DatabaseEtcdConfig) (*service.DatabaseInfo, error)
	updateRaftFn       func(ctx context.Context, name string, raft *service.DatabaseRaftConfig) (*service.DatabaseInfo, error)
	updateBarrierFn    func(ctx context.Context, name string, barrier *service.DatabaseBarrierConfig) (*service.DatabaseInfo, error)
	updateConnectionFn func(ctx context.Context, name string, conn *service.DatabaseConnectionConfig) (*service.DatabaseInfo, error)
}

func (m *mockDatabaseService) CreateDatabase(ctx context.Context, req *service.CreateDatabaseRequest) (*service.DatabaseInfo, error) {
	if m.createFn != nil {
		return m.createFn(ctx, req)
	}
	return &service.DatabaseInfo{Name: req.Name, Engine: req.Engine, Backend: req.Backend, Status: "open"}, nil
}

func (m *mockDatabaseService) GetDatabase(ctx context.Context, name string) (*service.DatabaseInfo, error) {
	if m.getFn != nil {
		return m.getFn(ctx, name)
	}
	return &service.DatabaseInfo{Name: name, Status: "open"}, nil
}

func (m *mockDatabaseService) ListDatabases(ctx context.Context) ([]*service.DatabaseInfo, error) {
	if m.listFn != nil {
		return m.listFn(ctx)
	}
	return nil, nil
}

func (m *mockDatabaseService) DropDatabase(ctx context.Context, name string) error {
	if m.dropFn != nil {
		return m.dropFn(ctx, name)
	}
	return nil
}

func (m *mockDatabaseService) UpdateDatabaseProtocol(ctx context.Context, name string, protocol service.DatabaseProtocolConfig) (*service.DatabaseInfo, error) {
	if m.updateProtocolFn != nil {
		return m.updateProtocolFn(ctx, name, protocol)
	}
	return &service.DatabaseInfo{Name: name, Status: "open"}, nil
}

func (m *mockDatabaseService) UpdateDatabaseEtcd(ctx context.Context, name string, etcd *service.DatabaseEtcdConfig) (*service.DatabaseInfo, error) {
	if m.updateEtcdFn != nil {
		return m.updateEtcdFn(ctx, name, etcd)
	}
	return &service.DatabaseInfo{Name: name, Status: "open"}, nil
}

func (m *mockDatabaseService) UpdateDatabaseRaft(ctx context.Context, name string, raft *service.DatabaseRaftConfig) (*service.DatabaseInfo, error) {
	if m.updateRaftFn != nil {
		return m.updateRaftFn(ctx, name, raft)
	}
	return &service.DatabaseInfo{Name: name, Status: "open"}, nil
}

func (m *mockDatabaseService) UpdateDatabaseBarrier(ctx context.Context, name string, barrier *service.DatabaseBarrierConfig) (*service.DatabaseInfo, error) {
	if m.updateBarrierFn != nil {
		return m.updateBarrierFn(ctx, name, barrier)
	}
	return &service.DatabaseInfo{Name: name, Status: "open"}, nil
}

func (m *mockDatabaseService) UpdateDatabaseConnection(ctx context.Context, name string, conn *service.DatabaseConnectionConfig) (*service.DatabaseInfo, error) {
	if m.updateConnectionFn != nil {
		return m.updateConnectionFn(ctx, name, conn)
	}
	return &service.DatabaseInfo{Name: name, Status: "open"}, nil
}

// ---------------------------------------------------------------------------
// Helper: build a baasTransport with an injected DatabaseService
// ---------------------------------------------------------------------------

func newTestTransportWithDB(dbSvc service.DatabaseService) *baasTransport {
	t := &baasTransport{
		dbService: dbSvc,
	}
	return t
}

func newTestTransportNoDB() *baasTransport {
	return &baasTransport{}
}

// ---------------------------------------------------------------------------
// CreateDatabase tests
// ---------------------------------------------------------------------------

func TestBaasTransport_CreateDatabase(t *testing.T) {
	mock := &mockDatabaseService{
		createFn: func(_ context.Context, req *service.CreateDatabaseRequest) (*service.DatabaseInfo, error) {
			return &service.DatabaseInfo{
				TenantID: "t1",
				Name:     req.Name,
				Engine:   req.Engine,
				Backend:  req.Backend,
				Status:   "open",
			}, nil
		},
	}
	tr := newTestTransportWithDB(mock)
	ctx := context.Background()

	req := transport.CreateDatabaseRequest{
		Name:    "mydb",
		Engine:  "kv",
		Backend: "pebble",
	}
	info, err := tr.CreateDatabase(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "t1", info.TenantID)
	assert.Equal(t, "mydb", info.Name)
	assert.Equal(t, "kv", info.Engine)
	assert.Equal(t, "pebble", info.Backend)
	assert.Equal(t, "open", info.Status)
}

func TestBaasTransport_CreateDatabase_NilService(t *testing.T) {
	tr := newTestTransportNoDB()
	ctx := context.Background()

	info, err := tr.CreateDatabase(ctx, transport.CreateDatabaseRequest{Name: "db"})

	require.Error(t, err)
	assert.Nil(t, info)
	var notSupported *BaasTransportNotSupportedError
	require.True(t, errors.As(err, &notSupported))
	assert.Equal(t, "CreateDatabase", notSupported.Operation)
}

func TestBaasTransport_CreateDatabase_ServiceError(t *testing.T) {
	svcErr := &service.DatabaseCreateError{Name: "bad", Err: &service.InvalidRequestError{Detail: "invalid"}}
	mock := &mockDatabaseService{
		createFn: func(_ context.Context, _ *service.CreateDatabaseRequest) (*service.DatabaseInfo, error) {
			return nil, svcErr
		},
	}
	tr := newTestTransportWithDB(mock)
	ctx := context.Background()

	info, err := tr.CreateDatabase(ctx, transport.CreateDatabaseRequest{Name: "bad"})

	require.Error(t, err)
	assert.Nil(t, info)
	assert.True(t, errors.Is(err, svcErr))
}

// ---------------------------------------------------------------------------
// GetDatabase tests
// ---------------------------------------------------------------------------

func TestBaasTransport_GetDatabase(t *testing.T) {
	mock := &mockDatabaseService{
		getFn: func(_ context.Context, name string) (*service.DatabaseInfo, error) {
			return &service.DatabaseInfo{
				TenantID: "t2",
				Name:     name,
				Engine:   "vector",
				Status:   "open",
			}, nil
		},
	}
	tr := newTestTransportWithDB(mock)
	ctx := context.Background()

	info, err := tr.GetDatabase(ctx, "testdb")

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "t2", info.TenantID)
	assert.Equal(t, "testdb", info.Name)
	assert.Equal(t, "vector", info.Engine)
}

func TestBaasTransport_GetDatabase_NilService(t *testing.T) {
	tr := newTestTransportNoDB()

	info, err := tr.GetDatabase(context.Background(), "db")

	require.Error(t, err)
	assert.Nil(t, info)
	var notSupported *BaasTransportNotSupportedError
	require.True(t, errors.As(err, &notSupported))
	assert.Equal(t, "GetDatabase", notSupported.Operation)
}

func TestBaasTransport_GetDatabase_ServiceError(t *testing.T) {
	svcErr := &service.DatabaseCreateError{Name: "missing", Err: &service.InvalidRequestError{Detail: "not found"}}
	mock := &mockDatabaseService{
		getFn: func(_ context.Context, _ string) (*service.DatabaseInfo, error) {
			return nil, svcErr
		},
	}
	tr := newTestTransportWithDB(mock)

	info, err := tr.GetDatabase(context.Background(), "missing")

	require.Error(t, err)
	assert.Nil(t, info)
}

// ---------------------------------------------------------------------------
// ListDatabases tests
// ---------------------------------------------------------------------------

func TestBaasTransport_ListDatabases(t *testing.T) {
	mock := &mockDatabaseService{
		listFn: func(_ context.Context) ([]*service.DatabaseInfo, error) {
			return []*service.DatabaseInfo{
				{TenantID: "t1", Name: "db1", Engine: "kv", Status: "open"},
				{TenantID: "t1", Name: "db2", Engine: "vector", Status: "open"},
			}, nil
		},
	}
	tr := newTestTransportWithDB(mock)

	infos, err := tr.ListDatabases(context.Background())

	require.NoError(t, err)
	require.Len(t, infos, 2)
	assert.Equal(t, "db1", infos[0].Name)
	assert.Equal(t, "db2", infos[1].Name)
	assert.Equal(t, "kv", infos[0].Engine)
	assert.Equal(t, "vector", infos[1].Engine)
}

func TestBaasTransport_ListDatabases_NilService(t *testing.T) {
	tr := newTestTransportNoDB()

	infos, err := tr.ListDatabases(context.Background())

	require.Error(t, err)
	assert.Nil(t, infos)
	var notSupported *BaasTransportNotSupportedError
	require.True(t, errors.As(err, &notSupported))
	assert.Equal(t, "ListDatabases", notSupported.Operation)
}

func TestBaasTransport_ListDatabases_ServiceError(t *testing.T) {
	svcErr := &service.DatabaseCreateError{Name: "fail", Err: &service.InvalidRequestError{Detail: "fail"}}
	mock := &mockDatabaseService{
		listFn: func(_ context.Context) ([]*service.DatabaseInfo, error) {
			return nil, svcErr
		},
	}
	tr := newTestTransportWithDB(mock)

	infos, err := tr.ListDatabases(context.Background())

	require.Error(t, err)
	assert.Nil(t, infos)
}

// ---------------------------------------------------------------------------
// DropDatabase tests
// ---------------------------------------------------------------------------

func TestBaasTransport_DropDatabase(t *testing.T) {
	var capturedName string
	mock := &mockDatabaseService{
		dropFn: func(_ context.Context, name string) error {
			capturedName = name
			return nil
		},
	}
	tr := newTestTransportWithDB(mock)

	err := tr.DropDatabase(context.Background(), "todelete")

	require.NoError(t, err)
	assert.Equal(t, "todelete", capturedName)
}

func TestBaasTransport_DropDatabase_NilService(t *testing.T) {
	tr := newTestTransportNoDB()

	err := tr.DropDatabase(context.Background(), "db")

	require.Error(t, err)
	var notSupported *BaasTransportNotSupportedError
	require.True(t, errors.As(err, &notSupported))
	assert.Equal(t, "DropDatabase", notSupported.Operation)
}

func TestBaasTransport_DropDatabase_ServiceError(t *testing.T) {
	svcErr := &service.DatabaseDropError{Name: "missing", Err: &service.InvalidRequestError{Detail: "not found"}}
	mock := &mockDatabaseService{
		dropFn: func(_ context.Context, _ string) error {
			return svcErr
		},
	}
	tr := newTestTransportWithDB(mock)

	err := tr.DropDatabase(context.Background(), "missing")

	require.Error(t, err)
	assert.True(t, errors.Is(err, svcErr))
}

// ---------------------------------------------------------------------------
// UpdateDatabaseProtocol tests
// ---------------------------------------------------------------------------

func TestBaasTransport_UpdateDatabaseProtocol(t *testing.T) {
	mock := &mockDatabaseService{
		updateProtocolFn: func(_ context.Context, name string, protocol service.DatabaseProtocolConfig) (*service.DatabaseInfo, error) {
			return &service.DatabaseInfo{
				Name:   name,
				Status: "open",
				Protocol: service.DatabaseProtocolConfig{
					MySQL:      protocol.MySQL,
					PostgreSQL: protocol.PostgreSQL,
				},
			}, nil
		},
	}
	tr := newTestTransportWithDB(mock)

	req := transport.UpdateDatabaseProtocolRequest{
		Name:       "mydb",
		MySQL:      true,
		PostgreSQL: false,
	}
	info, err := tr.UpdateDatabaseProtocol(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "mydb", info.Name)
	assert.True(t, info.Protocol.MySQL)
	assert.False(t, info.Protocol.PostgreSQL)
}

func TestBaasTransport_UpdateDatabaseProtocol_NilService(t *testing.T) {
	tr := newTestTransportNoDB()

	info, err := tr.UpdateDatabaseProtocol(context.Background(), transport.UpdateDatabaseProtocolRequest{Name: "db"})

	require.Error(t, err)
	assert.Nil(t, info)
	var notSupported *BaasTransportNotSupportedError
	require.True(t, errors.As(err, &notSupported))
	assert.Equal(t, "UpdateDatabaseProtocol", notSupported.Operation)
}

func TestBaasTransport_UpdateDatabaseProtocol_ServiceError(t *testing.T) {
	svcErr := &service.DatabaseUpdateError{Name: "db", Operation: "protocol", Err: &service.InvalidRequestError{Detail: "fail"}}
	mock := &mockDatabaseService{
		updateProtocolFn: func(_ context.Context, _ string, _ service.DatabaseProtocolConfig) (*service.DatabaseInfo, error) {
			return nil, svcErr
		},
	}
	tr := newTestTransportWithDB(mock)

	info, err := tr.UpdateDatabaseProtocol(context.Background(), transport.UpdateDatabaseProtocolRequest{Name: "db"})

	require.Error(t, err)
	assert.Nil(t, info)
}

// ---------------------------------------------------------------------------
// UpdateDatabaseEtcd tests
// ---------------------------------------------------------------------------

func TestBaasTransport_UpdateDatabaseEtcd(t *testing.T) {
	mock := &mockDatabaseService{
		updateEtcdFn: func(_ context.Context, name string, etcd *service.DatabaseEtcdConfig) (*service.DatabaseInfo, error) {
			return &service.DatabaseInfo{
				Name:   name,
				Status: "open",
				Protocol: service.DatabaseProtocolConfig{
					Etcd: &service.DatabaseEtcdConfig{
						Enabled:    etcd.Enabled,
						ListenAddr: etcd.ListenAddr,
					},
				},
			}, nil
		},
	}
	tr := newTestTransportWithDB(mock)

	req := transport.UpdateDatabaseEtcdRequest{
		Name:       "mydb",
		Enabled:    true,
		ListenAddr: ":2379",
	}
	info, err := tr.UpdateDatabaseEtcd(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "mydb", info.Name)
	require.NotNil(t, info.Protocol.Etcd)
	assert.True(t, info.Protocol.Etcd.Enabled)
	assert.Equal(t, ":2379", info.Protocol.Etcd.ListenAddr)
}

func TestBaasTransport_UpdateDatabaseEtcd_NilService(t *testing.T) {
	tr := newTestTransportNoDB()

	info, err := tr.UpdateDatabaseEtcd(context.Background(), transport.UpdateDatabaseEtcdRequest{Name: "db"})

	require.Error(t, err)
	assert.Nil(t, info)
	var notSupported *BaasTransportNotSupportedError
	require.True(t, errors.As(err, &notSupported))
	assert.Equal(t, "UpdateDatabaseEtcd", notSupported.Operation)
}

func TestBaasTransport_UpdateDatabaseEtcd_ServiceError(t *testing.T) {
	svcErr := &service.DatabaseUpdateError{Name: "db", Operation: "etcd", Err: &service.InvalidRequestError{Detail: "fail"}}
	mock := &mockDatabaseService{
		updateEtcdFn: func(_ context.Context, _ string, _ *service.DatabaseEtcdConfig) (*service.DatabaseInfo, error) {
			return nil, svcErr
		},
	}
	tr := newTestTransportWithDB(mock)

	info, err := tr.UpdateDatabaseEtcd(context.Background(), transport.UpdateDatabaseEtcdRequest{Name: "db"})

	require.Error(t, err)
	assert.Nil(t, info)
}

// ---------------------------------------------------------------------------
// UpdateDatabaseRaft tests
// ---------------------------------------------------------------------------

func TestBaasTransport_UpdateDatabaseRaft(t *testing.T) {
	mock := &mockDatabaseService{
		updateRaftFn: func(_ context.Context, name string, raft *service.DatabaseRaftConfig) (*service.DatabaseInfo, error) {
			return &service.DatabaseInfo{
				Name:   name,
				Status: "open",
				Raft: &service.DatabaseRaftConfig{
					ClusterMode: raft.ClusterMode,
					ShardID:     raft.ShardID,
				},
			}, nil
		},
	}
	tr := newTestTransportWithDB(mock)

	req := transport.UpdateDatabaseRaftRequest{
		Name: "mydb",
		Raft: &transport.DatabaseRaftConfig{
			ClusterMode: "local",
			ShardID:     42,
		},
	}
	info, err := tr.UpdateDatabaseRaft(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "mydb", info.Name)
	require.NotNil(t, info.Raft)
	assert.Equal(t, "local", info.Raft.ClusterMode)
	assert.Equal(t, uint64(42), info.Raft.ShardID)
}

func TestBaasTransport_UpdateDatabaseRaft_NilRaftConfig(t *testing.T) {
	var capturedRaft *service.DatabaseRaftConfig
	mock := &mockDatabaseService{
		updateRaftFn: func(_ context.Context, name string, raft *service.DatabaseRaftConfig) (*service.DatabaseInfo, error) {
			capturedRaft = raft
			return &service.DatabaseInfo{Name: name, Status: "open"}, nil
		},
	}
	tr := newTestTransportWithDB(mock)

	req := transport.UpdateDatabaseRaftRequest{
		Name: "mydb",
		Raft: nil,
	}
	info, err := tr.UpdateDatabaseRaft(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Nil(t, capturedRaft)
}

func TestBaasTransport_UpdateDatabaseRaft_NilService(t *testing.T) {
	tr := newTestTransportNoDB()

	info, err := tr.UpdateDatabaseRaft(context.Background(), transport.UpdateDatabaseRaftRequest{Name: "db"})

	require.Error(t, err)
	assert.Nil(t, info)
	var notSupported *BaasTransportNotSupportedError
	require.True(t, errors.As(err, &notSupported))
	assert.Equal(t, "UpdateDatabaseRaft", notSupported.Operation)
}

func TestBaasTransport_UpdateDatabaseRaft_ServiceError(t *testing.T) {
	svcErr := &service.DatabaseUpdateError{Name: "db", Operation: "raft", Err: &service.InvalidRequestError{Detail: "fail"}}
	mock := &mockDatabaseService{
		updateRaftFn: func(_ context.Context, _ string, _ *service.DatabaseRaftConfig) (*service.DatabaseInfo, error) {
			return nil, svcErr
		},
	}
	tr := newTestTransportWithDB(mock)

	info, err := tr.UpdateDatabaseRaft(context.Background(), transport.UpdateDatabaseRaftRequest{Name: "db"})

	require.Error(t, err)
	assert.Nil(t, info)
}

// ---------------------------------------------------------------------------
// UpdateDatabaseBarrier tests
// ---------------------------------------------------------------------------

func TestBaasTransport_UpdateDatabaseBarrier(t *testing.T) {
	mock := &mockDatabaseService{
		updateBarrierFn: func(_ context.Context, name string, barrier *service.DatabaseBarrierConfig) (*service.DatabaseInfo, error) {
			return &service.DatabaseInfo{
				Name:   name,
				Status: "open",
				Barrier: &service.DatabaseBarrierConfig{
					Enabled:        barrier.Enabled,
					Mode:           barrier.Mode,
					SharesRequired: barrier.SharesRequired,
					SharesTotal:    barrier.SharesTotal,
				},
			}, nil
		},
	}
	tr := newTestTransportWithDB(mock)

	req := transport.UpdateDatabaseBarrierRequest{
		Name: "mydb",
		Barrier: &transport.DatabaseBarrierConfig{
			Enabled:        true,
			Mode:           "shamir",
			SharesRequired: 3,
			SharesTotal:    5,
		},
	}
	info, err := tr.UpdateDatabaseBarrier(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "mydb", info.Name)
	require.NotNil(t, info.Barrier)
	assert.True(t, info.Barrier.Enabled)
	assert.Equal(t, "shamir", info.Barrier.Mode)
	assert.Equal(t, 3, info.Barrier.SharesRequired)
	assert.Equal(t, 5, info.Barrier.SharesTotal)
}

func TestBaasTransport_UpdateDatabaseBarrier_NilBarrierConfig(t *testing.T) {
	var capturedBarrier *service.DatabaseBarrierConfig
	mock := &mockDatabaseService{
		updateBarrierFn: func(_ context.Context, name string, barrier *service.DatabaseBarrierConfig) (*service.DatabaseInfo, error) {
			capturedBarrier = barrier
			return &service.DatabaseInfo{Name: name, Status: "open"}, nil
		},
	}
	tr := newTestTransportWithDB(mock)

	req := transport.UpdateDatabaseBarrierRequest{
		Name:    "mydb",
		Barrier: nil,
	}
	info, err := tr.UpdateDatabaseBarrier(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Nil(t, capturedBarrier)
}

func TestBaasTransport_UpdateDatabaseBarrier_NilService(t *testing.T) {
	tr := newTestTransportNoDB()

	info, err := tr.UpdateDatabaseBarrier(context.Background(), transport.UpdateDatabaseBarrierRequest{Name: "db"})

	require.Error(t, err)
	assert.Nil(t, info)
	var notSupported *BaasTransportNotSupportedError
	require.True(t, errors.As(err, &notSupported))
	assert.Equal(t, "UpdateDatabaseBarrier", notSupported.Operation)
}

func TestBaasTransport_UpdateDatabaseBarrier_ServiceError(t *testing.T) {
	svcErr := &service.DatabaseUpdateError{Name: "db", Operation: "barrier", Err: &service.InvalidRequestError{Detail: "fail"}}
	mock := &mockDatabaseService{
		updateBarrierFn: func(_ context.Context, _ string, _ *service.DatabaseBarrierConfig) (*service.DatabaseInfo, error) {
			return nil, svcErr
		},
	}
	tr := newTestTransportWithDB(mock)

	info, err := tr.UpdateDatabaseBarrier(context.Background(), transport.UpdateDatabaseBarrierRequest{Name: "db"})

	require.Error(t, err)
	assert.Nil(t, info)
}

// ---------------------------------------------------------------------------
// UpdateDatabaseConnection tests
// ---------------------------------------------------------------------------

func TestBaasTransport_UpdateDatabaseConnection(t *testing.T) {
	mock := &mockDatabaseService{
		updateConnectionFn: func(_ context.Context, name string, conn *service.DatabaseConnectionConfig) (*service.DatabaseInfo, error) {
			return &service.DatabaseInfo{
				Name:   name,
				Status: "open",
				Connection: &service.DatabaseConnectionConfig{
					Transport: conn.Transport,
					Address:   conn.Address,
				},
			}, nil
		},
	}
	tr := newTestTransportWithDB(mock)

	req := transport.UpdateDatabaseConnectionRequest{
		Name: "mydb",
		Connection: &transport.DatabaseConnectionConfig{
			Transport: "grpc",
			Address:   "localhost:9090",
		},
	}
	info, err := tr.UpdateDatabaseConnection(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "mydb", info.Name)
	require.NotNil(t, info.Connection)
	assert.Equal(t, "grpc", info.Connection.Transport)
	assert.Equal(t, "localhost:9090", info.Connection.Address)
}

func TestBaasTransport_UpdateDatabaseConnection_NilConnectionConfig(t *testing.T) {
	var capturedConn *service.DatabaseConnectionConfig
	mock := &mockDatabaseService{
		updateConnectionFn: func(_ context.Context, name string, conn *service.DatabaseConnectionConfig) (*service.DatabaseInfo, error) {
			capturedConn = conn
			return &service.DatabaseInfo{Name: name, Status: "open"}, nil
		},
	}
	tr := newTestTransportWithDB(mock)

	req := transport.UpdateDatabaseConnectionRequest{
		Name:       "mydb",
		Connection: nil,
	}
	info, err := tr.UpdateDatabaseConnection(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Nil(t, capturedConn)
}

func TestBaasTransport_UpdateDatabaseConnection_NilService(t *testing.T) {
	tr := newTestTransportNoDB()

	info, err := tr.UpdateDatabaseConnection(context.Background(), transport.UpdateDatabaseConnectionRequest{Name: "db"})

	require.Error(t, err)
	assert.Nil(t, info)
	var notSupported *BaasTransportNotSupportedError
	require.True(t, errors.As(err, &notSupported))
	assert.Equal(t, "UpdateDatabaseConnection", notSupported.Operation)
}

func TestBaasTransport_UpdateDatabaseConnection_ServiceError(t *testing.T) {
	svcErr := &service.DatabaseUpdateError{Name: "db", Operation: "connection", Err: &service.InvalidRequestError{Detail: "fail"}}
	mock := &mockDatabaseService{
		updateConnectionFn: func(_ context.Context, _ string, _ *service.DatabaseConnectionConfig) (*service.DatabaseInfo, error) {
			return nil, svcErr
		},
	}
	tr := newTestTransportWithDB(mock)

	info, err := tr.UpdateDatabaseConnection(context.Background(), transport.UpdateDatabaseConnectionRequest{Name: "db"})

	require.Error(t, err)
	assert.Nil(t, info)
}

// ---------------------------------------------------------------------------
// convertToTransportDatabaseInfo tests
// ---------------------------------------------------------------------------

func TestConvertToTransportDatabaseInfo_FullFields(t *testing.T) {
	svcInfo := &service.DatabaseInfo{
		TenantID: "t1",
		Name:     "fulldb",
		Engine:   "vector",
		Backend:  "pebble",
		Status:   "open",
		Protocol: service.DatabaseProtocolConfig{
			MySQL:      true,
			PostgreSQL: true,
			Etcd: &service.DatabaseEtcdConfig{
				Enabled:    true,
				ListenAddr: ":2379",
			},
		},
		Raft: &service.DatabaseRaftConfig{
			ClusterMode: "distributed",
			ShardID:     7,
		},
		Barrier: &service.DatabaseBarrierConfig{
			Enabled:        true,
			Mode:           "shamir",
			SharesRequired: 2,
			SharesTotal:    3,
		},
		Connection: &service.DatabaseConnectionConfig{
			Transport: "quic",
			Address:   "node1:4433",
		},
	}

	result := convertToTransportDatabaseInfo(svcInfo)

	assert.Equal(t, "t1", result.TenantID)
	assert.Equal(t, "fulldb", result.Name)
	assert.Equal(t, "vector", result.Engine)
	assert.Equal(t, "pebble", result.Backend)
	assert.Equal(t, "open", result.Status)
	assert.True(t, result.Protocol.MySQL)
	assert.True(t, result.Protocol.PostgreSQL)
	require.NotNil(t, result.Protocol.Etcd)
	assert.True(t, result.Protocol.Etcd.Enabled)
	assert.Equal(t, ":2379", result.Protocol.Etcd.ListenAddr)
	require.NotNil(t, result.Raft)
	assert.Equal(t, "distributed", result.Raft.ClusterMode)
	assert.Equal(t, uint64(7), result.Raft.ShardID)
	require.NotNil(t, result.Barrier)
	assert.True(t, result.Barrier.Enabled)
	assert.Equal(t, "shamir", result.Barrier.Mode)
	assert.Equal(t, 2, result.Barrier.SharesRequired)
	assert.Equal(t, 3, result.Barrier.SharesTotal)
	require.NotNil(t, result.Connection)
	assert.Equal(t, "quic", result.Connection.Transport)
	assert.Equal(t, "node1:4433", result.Connection.Address)
}

func TestConvertToTransportDatabaseInfo_MinimalFields(t *testing.T) {
	svcInfo := &service.DatabaseInfo{
		Name:   "simple",
		Engine: "kv",
		Status: "open",
	}

	result := convertToTransportDatabaseInfo(svcInfo)

	assert.Equal(t, "simple", result.Name)
	assert.Equal(t, "kv", result.Engine)
	assert.Equal(t, "open", result.Status)
	assert.Nil(t, result.Protocol.Etcd)
	assert.Nil(t, result.Raft)
	assert.Nil(t, result.Barrier)
	assert.Nil(t, result.Connection)
}
