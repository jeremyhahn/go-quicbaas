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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbdb "github.com/jeremyhahn/go-qrdb/pkg/database"
	"github.com/jeremyhahn/go-qrdb/pkg/seal"
)

// ---------------------------------------------------------------------------
// Mock qrdb DatabaseServicer
// ---------------------------------------------------------------------------

// mockDBServicer implements qrdbsvc.DatabaseServicer for unit tests.
type mockDBServicer struct {
	// Non-tenant methods (unused by databaseService but required by interface).
	createFn           func(cfg qrdbdb.DatabaseConfig) (*qrdbdb.DatabaseInfo, error)
	getFn              func(name string) (*qrdbdb.DatabaseInfo, error)
	listFn             func() ([]qrdbdb.DatabaseInfo, error)
	dropFn             func(name string) error
	updateProtocolFn   func(name string, p qrdbdb.ProtocolConfig) (*qrdbdb.DatabaseInfo, error)
	updateEtcdFn       func(name string, e *qrdbdb.EtcdConfig) (*qrdbdb.DatabaseInfo, error)
	updateRaftFn       func(name string, r *qrdbdb.DatabaseRaftConfig) (*qrdbdb.DatabaseInfo, error)
	updateBarrierFn    func(name string, b *qrdbdb.DatabaseBarrierConfig) (*qrdbdb.DatabaseInfo, error)
	updateConnectionFn func(name string, c *qrdbdb.DatabaseConnectionConfig) (*qrdbdb.DatabaseInfo, error)

	// Tenant-scoped methods (used by databaseService).
	createForTenantFn           func(ctx context.Context, cfg qrdbdb.DatabaseConfig) (*qrdbdb.DatabaseInfo, error)
	getForTenantFn              func(ctx context.Context, name string) (*qrdbdb.DatabaseInfo, error)
	listForTenantFn             func(ctx context.Context) ([]qrdbdb.DatabaseInfo, error)
	dropForTenantFn             func(ctx context.Context, name string) error
	updateProtocolForTenantFn   func(ctx context.Context, name string, p qrdbdb.ProtocolConfig) (*qrdbdb.DatabaseInfo, error)
	updateEtcdForTenantFn       func(ctx context.Context, name string, e *qrdbdb.EtcdConfig) (*qrdbdb.DatabaseInfo, error)
	updateRaftForTenantFn       func(ctx context.Context, name string, r *qrdbdb.DatabaseRaftConfig) (*qrdbdb.DatabaseInfo, error)
	updateBarrierForTenantFn    func(ctx context.Context, name string, b *qrdbdb.DatabaseBarrierConfig) (*qrdbdb.DatabaseInfo, error)
	updateConnectionForTenantFn func(ctx context.Context, name string, c *qrdbdb.DatabaseConnectionConfig) (*qrdbdb.DatabaseInfo, error)
}

// Non-tenant interface methods (stubs).
func (m *mockDBServicer) Create(cfg qrdbdb.DatabaseConfig) (*qrdbdb.DatabaseInfo, error) {
	if m.createFn != nil {
		return m.createFn(cfg)
	}
	return &qrdbdb.DatabaseInfo{Name: cfg.Name, Status: qrdbdb.StatusOpen}, nil
}

func (m *mockDBServicer) Get(name string) (*qrdbdb.DatabaseInfo, error) {
	if m.getFn != nil {
		return m.getFn(name)
	}
	return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen}, nil
}

func (m *mockDBServicer) List() ([]qrdbdb.DatabaseInfo, error) {
	if m.listFn != nil {
		return m.listFn()
	}
	return nil, nil
}

func (m *mockDBServicer) Drop(name string) error {
	if m.dropFn != nil {
		return m.dropFn(name)
	}
	return nil
}

func (m *mockDBServicer) UpdateProtocol(name string, p qrdbdb.ProtocolConfig) (*qrdbdb.DatabaseInfo, error) {
	if m.updateProtocolFn != nil {
		return m.updateProtocolFn(name, p)
	}
	return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen}, nil
}

func (m *mockDBServicer) UpdateEtcd(name string, e *qrdbdb.EtcdConfig) (*qrdbdb.DatabaseInfo, error) {
	if m.updateEtcdFn != nil {
		return m.updateEtcdFn(name, e)
	}
	return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen}, nil
}

func (m *mockDBServicer) UpdateRaft(name string, r *qrdbdb.DatabaseRaftConfig) (*qrdbdb.DatabaseInfo, error) {
	if m.updateRaftFn != nil {
		return m.updateRaftFn(name, r)
	}
	return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen}, nil
}

func (m *mockDBServicer) UpdateBarrier(name string, b *qrdbdb.DatabaseBarrierConfig) (*qrdbdb.DatabaseInfo, error) {
	if m.updateBarrierFn != nil {
		return m.updateBarrierFn(name, b)
	}
	return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen}, nil
}

func (m *mockDBServicer) UpdateConnection(name string, c *qrdbdb.DatabaseConnectionConfig) (*qrdbdb.DatabaseInfo, error) {
	if m.updateConnectionFn != nil {
		return m.updateConnectionFn(name, c)
	}
	return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen}, nil
}

func (m *mockDBServicer) SealDatabase(_ context.Context, _ string) error { return nil }
func (m *mockDBServicer) UnsealDatabase(_ context.Context, _ string, _ seal.Credentials) error {
	return nil
}
func (m *mockDBServicer) DatabaseBarrierStatus(_ string) (*seal.BarrierStatus, error) {
	return nil, nil
}

// Tenant-scoped interface methods.
func (m *mockDBServicer) CreateForTenant(ctx context.Context, cfg qrdbdb.DatabaseConfig) (*qrdbdb.DatabaseInfo, error) {
	if m.createForTenantFn != nil {
		return m.createForTenantFn(ctx, cfg)
	}
	return &qrdbdb.DatabaseInfo{
		Name:    cfg.Name,
		Engine:  cfg.Engine,
		Backend: cfg.Backend,
		Status:  qrdbdb.StatusOpen,
	}, nil
}

func (m *mockDBServicer) GetForTenant(ctx context.Context, name string) (*qrdbdb.DatabaseInfo, error) {
	if m.getForTenantFn != nil {
		return m.getForTenantFn(ctx, name)
	}
	return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen}, nil
}

func (m *mockDBServicer) ListForTenant(ctx context.Context) ([]qrdbdb.DatabaseInfo, error) {
	if m.listForTenantFn != nil {
		return m.listForTenantFn(ctx)
	}
	return nil, nil
}

func (m *mockDBServicer) DropForTenant(ctx context.Context, name string) error {
	if m.dropForTenantFn != nil {
		return m.dropForTenantFn(ctx, name)
	}
	return nil
}

func (m *mockDBServicer) UpdateProtocolForTenant(ctx context.Context, name string, p qrdbdb.ProtocolConfig) (*qrdbdb.DatabaseInfo, error) {
	if m.updateProtocolForTenantFn != nil {
		return m.updateProtocolForTenantFn(ctx, name, p)
	}
	return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen, Protocol: p}, nil
}

func (m *mockDBServicer) UpdateEtcdForTenant(ctx context.Context, name string, e *qrdbdb.EtcdConfig) (*qrdbdb.DatabaseInfo, error) {
	if m.updateEtcdForTenantFn != nil {
		return m.updateEtcdForTenantFn(ctx, name, e)
	}
	return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen}, nil
}

func (m *mockDBServicer) UpdateRaftForTenant(ctx context.Context, name string, r *qrdbdb.DatabaseRaftConfig) (*qrdbdb.DatabaseInfo, error) {
	if m.updateRaftForTenantFn != nil {
		return m.updateRaftForTenantFn(ctx, name, r)
	}
	return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen}, nil
}

func (m *mockDBServicer) UpdateBarrierForTenant(ctx context.Context, name string, b *qrdbdb.DatabaseBarrierConfig) (*qrdbdb.DatabaseInfo, error) {
	if m.updateBarrierForTenantFn != nil {
		return m.updateBarrierForTenantFn(ctx, name, b)
	}
	return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen}, nil
}

func (m *mockDBServicer) UpdateConnectionForTenant(ctx context.Context, name string, c *qrdbdb.DatabaseConnectionConfig) (*qrdbdb.DatabaseInfo, error) {
	if m.updateConnectionForTenantFn != nil {
		return m.updateConnectionForTenantFn(ctx, name, c)
	}
	return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen}, nil
}

// ---------------------------------------------------------------------------
// CreateDatabase tests
// ---------------------------------------------------------------------------

func TestDatabaseService_CreateDatabase(t *testing.T) {
	var capturedCfg qrdbdb.DatabaseConfig
	mock := &mockDBServicer{
		createForTenantFn: func(_ context.Context, cfg qrdbdb.DatabaseConfig) (*qrdbdb.DatabaseInfo, error) {
			capturedCfg = cfg
			return &qrdbdb.DatabaseInfo{
				TenantID: "tenant-1",
				Name:     cfg.Name,
				Engine:   cfg.Engine,
				Backend:  cfg.Backend,
				Status:   qrdbdb.StatusOpen,
			}, nil
		},
	}
	svc := NewDatabaseService(mock)

	req := &CreateDatabaseRequest{
		Name:    "mydb",
		Engine:  "kv",
		Backend: "memory",
	}
	info, err := svc.CreateDatabase(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "tenant-1", info.TenantID)
	assert.Equal(t, "mydb", info.Name)
	assert.Equal(t, "kv", info.Engine)
	assert.Equal(t, "memory", info.Backend)
	assert.Equal(t, "open", info.Status)
	assert.Equal(t, "mydb", capturedCfg.Name)
	assert.Equal(t, qrdbdb.EngineKV, capturedCfg.Engine)
	assert.Equal(t, qrdbdb.BackendMemory, capturedCfg.Backend)
}

func TestDatabaseService_CreateDatabase_NilRequest(t *testing.T) {
	svc := NewDatabaseService(&mockDBServicer{})

	info, err := svc.CreateDatabase(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, info)
	var createErr *DatabaseCreateError
	require.True(t, errors.As(err, &createErr))
	var reqErr *InvalidRequestError
	require.True(t, errors.As(err, &reqErr))
}

func TestDatabaseService_CreateDatabase_QRDBError(t *testing.T) {
	qrdbErr := &qrdbdb.InvalidNameError{Name: "bad name"}
	mock := &mockDBServicer{
		createForTenantFn: func(_ context.Context, _ qrdbdb.DatabaseConfig) (*qrdbdb.DatabaseInfo, error) {
			return nil, qrdbErr
		},
	}
	svc := NewDatabaseService(mock)

	info, err := svc.CreateDatabase(context.Background(), &CreateDatabaseRequest{Name: "bad name"})

	require.Error(t, err)
	assert.Nil(t, info)
	var createErr *DatabaseCreateError
	require.True(t, errors.As(err, &createErr))
	assert.Equal(t, "bad name", createErr.Name)
	assert.True(t, errors.Is(err, qrdbErr))
}

// ---------------------------------------------------------------------------
// GetDatabase tests
// ---------------------------------------------------------------------------

func TestDatabaseService_GetDatabase(t *testing.T) {
	mock := &mockDBServicer{
		getForTenantFn: func(_ context.Context, name string) (*qrdbdb.DatabaseInfo, error) {
			return &qrdbdb.DatabaseInfo{
				TenantID: "tenant-2",
				Name:     name,
				Engine:   qrdbdb.EngineKV,
				Backend:  qrdbdb.BackendMemory,
				Status:   qrdbdb.StatusOpen,
			}, nil
		},
	}
	svc := NewDatabaseService(mock)

	info, err := svc.GetDatabase(context.Background(), "testdb")

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "tenant-2", info.TenantID)
	assert.Equal(t, "testdb", info.Name)
	assert.Equal(t, "kv", info.Engine)
	assert.Equal(t, "memory", info.Backend)
}

func TestDatabaseService_GetDatabase_NotFound(t *testing.T) {
	notFoundErr := &qrdbdb.NotFoundError{Name: "missing"}
	mock := &mockDBServicer{
		getForTenantFn: func(_ context.Context, _ string) (*qrdbdb.DatabaseInfo, error) {
			return nil, notFoundErr
		},
	}
	svc := NewDatabaseService(mock)

	info, err := svc.GetDatabase(context.Background(), "missing")

	require.Error(t, err)
	assert.Nil(t, info)
	assert.True(t, errors.Is(err, notFoundErr))
}

// ---------------------------------------------------------------------------
// ListDatabases tests
// ---------------------------------------------------------------------------

func TestDatabaseService_ListDatabases(t *testing.T) {
	mock := &mockDBServicer{
		listForTenantFn: func(_ context.Context) ([]qrdbdb.DatabaseInfo, error) {
			return []qrdbdb.DatabaseInfo{
				{TenantID: "t1", Name: "db1", Engine: qrdbdb.EngineKV, Status: qrdbdb.StatusOpen},
				{TenantID: "t1", Name: "db2", Engine: qrdbdb.EngineVector, Status: qrdbdb.StatusOpen},
			}, nil
		},
	}
	svc := NewDatabaseService(mock)

	infos, err := svc.ListDatabases(context.Background())

	require.NoError(t, err)
	require.Len(t, infos, 2)
	assert.Equal(t, "db1", infos[0].Name)
	assert.Equal(t, "db2", infos[1].Name)
	assert.Equal(t, "t1", infos[0].TenantID)
	assert.Equal(t, "vector", infos[1].Engine)
}

func TestDatabaseService_ListDatabases_Empty(t *testing.T) {
	mock := &mockDBServicer{
		listForTenantFn: func(_ context.Context) ([]qrdbdb.DatabaseInfo, error) {
			return nil, nil
		},
	}
	svc := NewDatabaseService(mock)

	infos, err := svc.ListDatabases(context.Background())

	require.NoError(t, err)
	assert.Empty(t, infos)
}

func TestDatabaseService_ListDatabases_QRDBError(t *testing.T) {
	listErr := &qrdbdb.InvalidNameError{Name: "fail"}
	mock := &mockDBServicer{
		listForTenantFn: func(_ context.Context) ([]qrdbdb.DatabaseInfo, error) {
			return nil, listErr
		},
	}
	svc := NewDatabaseService(mock)

	infos, err := svc.ListDatabases(context.Background())

	require.Error(t, err)
	assert.Nil(t, infos)
}

// ---------------------------------------------------------------------------
// DropDatabase tests
// ---------------------------------------------------------------------------

func TestDatabaseService_DropDatabase(t *testing.T) {
	var capturedName string
	mock := &mockDBServicer{
		dropForTenantFn: func(_ context.Context, name string) error {
			capturedName = name
			return nil
		},
	}
	svc := NewDatabaseService(mock)

	err := svc.DropDatabase(context.Background(), "todelete")

	require.NoError(t, err)
	assert.Equal(t, "todelete", capturedName)
}

func TestDatabaseService_DropDatabase_QRDBError(t *testing.T) {
	notFoundErr := &qrdbdb.NotFoundError{Name: "missing"}
	mock := &mockDBServicer{
		dropForTenantFn: func(_ context.Context, _ string) error {
			return notFoundErr
		},
	}
	svc := NewDatabaseService(mock)

	err := svc.DropDatabase(context.Background(), "missing")

	require.Error(t, err)
	var dropErr *DatabaseDropError
	require.True(t, errors.As(err, &dropErr))
	assert.Equal(t, "missing", dropErr.Name)
	assert.True(t, errors.Is(err, notFoundErr))
}

// ---------------------------------------------------------------------------
// UpdateDatabaseProtocol tests
// ---------------------------------------------------------------------------

func TestDatabaseService_UpdateDatabaseProtocol(t *testing.T) {
	var capturedProto qrdbdb.ProtocolConfig
	mock := &mockDBServicer{
		updateProtocolForTenantFn: func(_ context.Context, name string, p qrdbdb.ProtocolConfig) (*qrdbdb.DatabaseInfo, error) {
			capturedProto = p
			return &qrdbdb.DatabaseInfo{
				Name:     name,
				Engine:   qrdbdb.EngineKV,
				Status:   qrdbdb.StatusOpen,
				Protocol: p,
			}, nil
		},
	}
	svc := NewDatabaseService(mock)

	proto := DatabaseProtocolConfig{
		MySQL:      true,
		PostgreSQL: false,
		Etcd: &DatabaseEtcdConfig{
			Enabled:    true,
			ListenAddr: ":2379",
		},
	}
	info, err := svc.UpdateDatabaseProtocol(context.Background(), "mydb", proto)

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.True(t, capturedProto.MySQL)
	assert.False(t, capturedProto.PostgreSQL)
	require.NotNil(t, capturedProto.Etcd)
	assert.True(t, capturedProto.Etcd.Enabled)
	assert.Equal(t, ":2379", capturedProto.Etcd.ListenAddr)
}

func TestDatabaseService_UpdateDatabaseProtocol_NilEtcd(t *testing.T) {
	var capturedProto qrdbdb.ProtocolConfig
	mock := &mockDBServicer{
		updateProtocolForTenantFn: func(_ context.Context, name string, p qrdbdb.ProtocolConfig) (*qrdbdb.DatabaseInfo, error) {
			capturedProto = p
			return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen, Protocol: p}, nil
		},
	}
	svc := NewDatabaseService(mock)

	info, err := svc.UpdateDatabaseProtocol(context.Background(), "db", DatabaseProtocolConfig{MySQL: true})

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Nil(t, capturedProto.Etcd)
}

func TestDatabaseService_UpdateDatabaseProtocol_QRDBError(t *testing.T) {
	mock := &mockDBServicer{
		updateProtocolForTenantFn: func(_ context.Context, _ string, _ qrdbdb.ProtocolConfig) (*qrdbdb.DatabaseInfo, error) {
			return nil, &qrdbdb.NotFoundError{Name: "nope"}
		},
	}
	svc := NewDatabaseService(mock)

	info, err := svc.UpdateDatabaseProtocol(context.Background(), "db", DatabaseProtocolConfig{})

	require.Error(t, err)
	assert.Nil(t, info)
	var updateErr *DatabaseUpdateError
	require.True(t, errors.As(err, &updateErr))
	assert.Equal(t, "protocol", updateErr.Operation)
}

// ---------------------------------------------------------------------------
// UpdateDatabaseEtcd tests
// ---------------------------------------------------------------------------

func TestDatabaseService_UpdateDatabaseEtcd(t *testing.T) {
	var capturedEtcd *qrdbdb.EtcdConfig
	mock := &mockDBServicer{
		updateEtcdForTenantFn: func(_ context.Context, name string, e *qrdbdb.EtcdConfig) (*qrdbdb.DatabaseInfo, error) {
			capturedEtcd = e
			return &qrdbdb.DatabaseInfo{
				Name:     name,
				Status:   qrdbdb.StatusOpen,
				Protocol: qrdbdb.ProtocolConfig{Etcd: e},
			}, nil
		},
	}
	svc := NewDatabaseService(mock)

	etcd := &DatabaseEtcdConfig{Enabled: true, ListenAddr: ":2379"}
	info, err := svc.UpdateDatabaseEtcd(context.Background(), "db", etcd)

	require.NoError(t, err)
	require.NotNil(t, info)
	require.NotNil(t, capturedEtcd)
	assert.True(t, capturedEtcd.Enabled)
	assert.Equal(t, ":2379", capturedEtcd.ListenAddr)
}

func TestDatabaseService_UpdateDatabaseEtcd_NilConfig(t *testing.T) {
	var capturedEtcd *qrdbdb.EtcdConfig
	mock := &mockDBServicer{
		updateEtcdForTenantFn: func(_ context.Context, name string, e *qrdbdb.EtcdConfig) (*qrdbdb.DatabaseInfo, error) {
			capturedEtcd = e
			return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen}, nil
		},
	}
	svc := NewDatabaseService(mock)

	info, err := svc.UpdateDatabaseEtcd(context.Background(), "db", nil)

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Nil(t, capturedEtcd)
}

func TestDatabaseService_UpdateDatabaseEtcd_QRDBError(t *testing.T) {
	mock := &mockDBServicer{
		updateEtcdForTenantFn: func(_ context.Context, _ string, _ *qrdbdb.EtcdConfig) (*qrdbdb.DatabaseInfo, error) {
			return nil, &qrdbdb.NotFoundError{Name: "x"}
		},
	}
	svc := NewDatabaseService(mock)

	info, err := svc.UpdateDatabaseEtcd(context.Background(), "db", &DatabaseEtcdConfig{})

	require.Error(t, err)
	assert.Nil(t, info)
	var updateErr *DatabaseUpdateError
	require.True(t, errors.As(err, &updateErr))
	assert.Equal(t, "etcd", updateErr.Operation)
}

// ---------------------------------------------------------------------------
// UpdateDatabaseRaft tests
// ---------------------------------------------------------------------------

func TestDatabaseService_UpdateDatabaseRaft(t *testing.T) {
	var capturedRaft *qrdbdb.DatabaseRaftConfig
	mock := &mockDBServicer{
		updateRaftForTenantFn: func(_ context.Context, name string, r *qrdbdb.DatabaseRaftConfig) (*qrdbdb.DatabaseInfo, error) {
			capturedRaft = r
			return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen, Raft: r}, nil
		},
	}
	svc := NewDatabaseService(mock)

	raftCfg := &DatabaseRaftConfig{ClusterMode: "local", ShardID: 42}
	info, err := svc.UpdateDatabaseRaft(context.Background(), "db", raftCfg)

	require.NoError(t, err)
	require.NotNil(t, info)
	require.NotNil(t, capturedRaft)
	assert.Equal(t, qrdbdb.RaftLocal, capturedRaft.ClusterMode)
	assert.Equal(t, uint64(42), capturedRaft.ShardID)
}

func TestDatabaseService_UpdateDatabaseRaft_NilConfig(t *testing.T) {
	var capturedRaft *qrdbdb.DatabaseRaftConfig
	mock := &mockDBServicer{
		updateRaftForTenantFn: func(_ context.Context, name string, r *qrdbdb.DatabaseRaftConfig) (*qrdbdb.DatabaseInfo, error) {
			capturedRaft = r
			return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen}, nil
		},
	}
	svc := NewDatabaseService(mock)

	info, err := svc.UpdateDatabaseRaft(context.Background(), "db", nil)

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Nil(t, capturedRaft)
}

func TestDatabaseService_UpdateDatabaseRaft_QRDBError(t *testing.T) {
	mock := &mockDBServicer{
		updateRaftForTenantFn: func(_ context.Context, _ string, _ *qrdbdb.DatabaseRaftConfig) (*qrdbdb.DatabaseInfo, error) {
			return nil, &qrdbdb.NotFoundError{Name: "x"}
		},
	}
	svc := NewDatabaseService(mock)

	info, err := svc.UpdateDatabaseRaft(context.Background(), "db", &DatabaseRaftConfig{})

	require.Error(t, err)
	assert.Nil(t, info)
	var updateErr *DatabaseUpdateError
	require.True(t, errors.As(err, &updateErr))
	assert.Equal(t, "raft", updateErr.Operation)
}

// ---------------------------------------------------------------------------
// UpdateDatabaseBarrier tests
// ---------------------------------------------------------------------------

func TestDatabaseService_UpdateDatabaseBarrier(t *testing.T) {
	var capturedBarrier *qrdbdb.DatabaseBarrierConfig
	mock := &mockDBServicer{
		updateBarrierForTenantFn: func(_ context.Context, name string, b *qrdbdb.DatabaseBarrierConfig) (*qrdbdb.DatabaseInfo, error) {
			capturedBarrier = b
			return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen, Barrier: b}, nil
		},
	}
	svc := NewDatabaseService(mock)

	barrier := &DatabaseBarrierConfig{
		Enabled:        true,
		Mode:           "shamir",
		SharesRequired: 3,
		SharesTotal:    5,
	}
	info, err := svc.UpdateDatabaseBarrier(context.Background(), "db", barrier)

	require.NoError(t, err)
	require.NotNil(t, info)
	require.NotNil(t, capturedBarrier)
	assert.True(t, capturedBarrier.Enabled)
	assert.Equal(t, "shamir", capturedBarrier.Mode)
	assert.Equal(t, 3, capturedBarrier.SharesRequired)
	assert.Equal(t, 5, capturedBarrier.SharesTotal)
}

func TestDatabaseService_UpdateDatabaseBarrier_NilConfig(t *testing.T) {
	var capturedBarrier *qrdbdb.DatabaseBarrierConfig
	mock := &mockDBServicer{
		updateBarrierForTenantFn: func(_ context.Context, name string, b *qrdbdb.DatabaseBarrierConfig) (*qrdbdb.DatabaseInfo, error) {
			capturedBarrier = b
			return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen}, nil
		},
	}
	svc := NewDatabaseService(mock)

	info, err := svc.UpdateDatabaseBarrier(context.Background(), "db", nil)

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Nil(t, capturedBarrier)
}

func TestDatabaseService_UpdateDatabaseBarrier_QRDBError(t *testing.T) {
	mock := &mockDBServicer{
		updateBarrierForTenantFn: func(_ context.Context, _ string, _ *qrdbdb.DatabaseBarrierConfig) (*qrdbdb.DatabaseInfo, error) {
			return nil, &qrdbdb.NotFoundError{Name: "x"}
		},
	}
	svc := NewDatabaseService(mock)

	info, err := svc.UpdateDatabaseBarrier(context.Background(), "db", &DatabaseBarrierConfig{})

	require.Error(t, err)
	assert.Nil(t, info)
	var updateErr *DatabaseUpdateError
	require.True(t, errors.As(err, &updateErr))
	assert.Equal(t, "barrier", updateErr.Operation)
}

// ---------------------------------------------------------------------------
// UpdateDatabaseConnection tests
// ---------------------------------------------------------------------------

func TestDatabaseService_UpdateDatabaseConnection(t *testing.T) {
	var capturedConn *qrdbdb.DatabaseConnectionConfig
	mock := &mockDBServicer{
		updateConnectionForTenantFn: func(_ context.Context, name string, c *qrdbdb.DatabaseConnectionConfig) (*qrdbdb.DatabaseInfo, error) {
			capturedConn = c
			return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen, Connection: c}, nil
		},
	}
	svc := NewDatabaseService(mock)

	conn := &DatabaseConnectionConfig{Transport: "grpc", Address: "localhost:9090"}
	info, err := svc.UpdateDatabaseConnection(context.Background(), "db", conn)

	require.NoError(t, err)
	require.NotNil(t, info)
	require.NotNil(t, capturedConn)
	assert.Equal(t, "grpc", capturedConn.Transport)
	assert.Equal(t, "localhost:9090", capturedConn.Address)
}

func TestDatabaseService_UpdateDatabaseConnection_NilConfig(t *testing.T) {
	var capturedConn *qrdbdb.DatabaseConnectionConfig
	mock := &mockDBServicer{
		updateConnectionForTenantFn: func(_ context.Context, name string, c *qrdbdb.DatabaseConnectionConfig) (*qrdbdb.DatabaseInfo, error) {
			capturedConn = c
			return &qrdbdb.DatabaseInfo{Name: name, Status: qrdbdb.StatusOpen}, nil
		},
	}
	svc := NewDatabaseService(mock)

	info, err := svc.UpdateDatabaseConnection(context.Background(), "db", nil)

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Nil(t, capturedConn)
}

func TestDatabaseService_UpdateDatabaseConnection_QRDBError(t *testing.T) {
	mock := &mockDBServicer{
		updateConnectionForTenantFn: func(_ context.Context, _ string, _ *qrdbdb.DatabaseConnectionConfig) (*qrdbdb.DatabaseInfo, error) {
			return nil, &qrdbdb.NotFoundError{Name: "x"}
		},
	}
	svc := NewDatabaseService(mock)

	info, err := svc.UpdateDatabaseConnection(context.Background(), "db", &DatabaseConnectionConfig{})

	require.Error(t, err)
	assert.Nil(t, info)
	var updateErr *DatabaseUpdateError
	require.True(t, errors.As(err, &updateErr))
	assert.Equal(t, "connection", updateErr.Operation)
}

// ---------------------------------------------------------------------------
// convertDatabaseInfo tests
// ---------------------------------------------------------------------------

func TestConvertDatabaseInfo_FullFields(t *testing.T) {
	qrdbInfo := &qrdbdb.DatabaseInfo{
		TenantID: "tenant-1",
		Name:     "fulldb",
		Engine:   qrdbdb.EngineVector,
		Backend:  qrdbdb.BackendPebble,
		Status:   qrdbdb.StatusOpen,
		Protocol: qrdbdb.ProtocolConfig{
			MySQL:      true,
			PostgreSQL: true,
			Etcd: &qrdbdb.EtcdConfig{
				Enabled:    true,
				ListenAddr: ":2379",
			},
		},
		Raft: &qrdbdb.DatabaseRaftConfig{
			ClusterMode: qrdbdb.RaftDistributed,
			ShardID:     7,
		},
		Barrier: &qrdbdb.DatabaseBarrierConfig{
			Enabled:        true,
			Mode:           "shamir",
			SharesRequired: 2,
			SharesTotal:    3,
		},
		Connection: &qrdbdb.DatabaseConnectionConfig{
			Transport: "quic",
			Address:   "node1:4433",
		},
	}

	result := convertDatabaseInfo(qrdbInfo)

	assert.Equal(t, "tenant-1", result.TenantID)
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

func TestConvertDatabaseInfo_MinimalFields(t *testing.T) {
	qrdbInfo := &qrdbdb.DatabaseInfo{
		Name:   "simple",
		Engine: qrdbdb.EngineKV,
		Status: qrdbdb.StatusOpen,
	}

	result := convertDatabaseInfo(qrdbInfo)

	assert.Equal(t, "simple", result.Name)
	assert.Equal(t, "kv", result.Engine)
	assert.Equal(t, "open", result.Status)
	assert.Nil(t, result.Protocol.Etcd)
	assert.Nil(t, result.Raft)
	assert.Nil(t, result.Barrier)
	assert.Nil(t, result.Connection)
}
