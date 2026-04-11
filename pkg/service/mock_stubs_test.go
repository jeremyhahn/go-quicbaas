// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// SPDX-License-Identifier: GPL-3.0-only
// See the LICENSE file in the root of this repository.

package service

// This file contains missing method stubs for mock types that were
// separated from their method definitions during test file consolidation.

import (
	"context"
	"fmt"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// errorReturningStorage implements storage.Storage
func (s *errorReturningStorage) Get(key []byte) ([]byte, error) {
	if s.getError {
		return nil, fmt.Errorf("get error")
	}
	return nil, nil
}
func (s *errorReturningStorage) Set(key, value []byte) error {
	if s.setError {
		return fmt.Errorf("set error")
	}
	return nil
}
func (s *errorReturningStorage) Delete(_ []byte) error                                   { return nil }
func (s *errorReturningStorage) List(_ []byte) ([][]byte, error)                         { return nil, nil }
func (s *errorReturningStorage) Exists(_ []byte) (bool, error)                           { return false, nil }
func (s *errorReturningStorage) Batch() storage.Batch                                    { return nil }
func (s *errorReturningStorage) NewIterator(_ *storage.IteratorOptions) storage.Iterator { return nil }
func (s *errorReturningStorage) Snapshot() (storage.Snapshot, error)                     { return nil, nil }
func (s *errorReturningStorage) Close() error                                            { return nil }

// coverageEmbeddingManager implements interfaces.EmbeddingManager
func (m *coverageEmbeddingManager) Embed(ctx context.Context, text string) ([]float32, error) {
	args := m.Called(ctx, text)
	if v := args.Get(0); v != nil {
		return v.([]float32), args.Error(1)
	}
	return nil, args.Error(1)
}
func (m *coverageEmbeddingManager) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	args := m.Called(ctx, texts)
	if v := args.Get(0); v != nil {
		return v.([][]float32), args.Error(1)
	}
	return nil, args.Error(1)
}
func (m *coverageEmbeddingManager) Dimensions() (int, error) {
	args := m.Called()
	return args.Int(0), args.Error(1)
}
func (m *coverageEmbeddingManager) GetProvider(name string) (interfaces.EmbeddingProvider, error) {
	args := m.Called(name)
	if v := args.Get(0); v != nil {
		return v.(interfaces.EmbeddingProvider), args.Error(1)
	}
	return nil, args.Error(1)
}
func (m *coverageEmbeddingManager) GetStats() interfaces.EmbeddingStats {
	args := m.Called()
	return args.Get(0).(interfaces.EmbeddingStats)
}

// coverageEmbeddingLogger implements interfaces.Logger
func (l *coverageEmbeddingLogger) Debug(_ string, _ ...any)                                        {}
func (l *coverageEmbeddingLogger) Debugf(_ string, _ ...interface{})                     {}
func (l *coverageEmbeddingLogger) Info(_ string, _ ...any)                                         {}
func (l *coverageEmbeddingLogger) Infof(_ string, _ ...interface{})                      {}
func (l *coverageEmbeddingLogger) Warn(_ string, _ ...any)                                         {}
func (l *coverageEmbeddingLogger) Warnf(_ string, _ ...interface{})                      {}
func (l *coverageEmbeddingLogger) Error(_ string, _ ...any)                                        {}
func (l *coverageEmbeddingLogger) Errorf(_ string, _ ...interface{})                     {}
func (l *coverageEmbeddingLogger) Fatal(_ string)                                        {}
func (l *coverageEmbeddingLogger) Fatalf(_ string, _ ...interface{})                     {}
func (l *coverageEmbeddingLogger) WithField(_ string, _ interface{}) interfaces.Logger   { return l }
func (l *coverageEmbeddingLogger) WithFields(_ map[string]interface{}) interfaces.Logger { return l }
func (l *coverageEmbeddingLogger) WithError(_ error) interfaces.Logger                   { return l }

// boost81RaftNode - missing Close
func (n *boost81RaftNode) Close() error { return nil }

// boost82MockBackupManager - missing Close
func (m *boost82MockBackupManager) Close() error { return m.closeErr }

// boost82MockCacheManager implements interfaces.CacheManager
func (m *boost82MockCacheManager) Get(_ context.Context, _ string) ([]byte, error) { return nil, nil }
func (m *boost82MockCacheManager) Set(_ context.Context, _ string, _ []byte) error { return nil }
func (m *boost82MockCacheManager) Delete(_ context.Context, _ string) error        { return nil }
func (m *boost82MockCacheManager) Clear()                                          {}
func (m *boost82MockCacheManager) Close() error                                    { return m.closeErr }

// cov90MockBackupManager implements interfaces.BackupManager
func (m *cov90MockBackupManager) CreateBackup(_ context.Context, _ interface{}) (interface{}, error) {
	return nil, nil
}
func (m *cov90MockBackupManager) RestoreBackup(_ context.Context, _ string, _ interface{}) error {
	return nil
}
func (m *cov90MockBackupManager) ListBackups(_ context.Context, _ int) (interface{}, error) {
	return nil, nil
}
func (m *cov90MockBackupManager) DeleteBackup(_ context.Context, _ string) error { return nil }
func (m *cov90MockBackupManager) VerifyBackup(_ context.Context, _ string) (interface{}, error) {
	return nil, nil
}
func (m *cov90MockBackupManager) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

// cov90MockCacheManager implements interfaces.CacheManager
func (m *cov90MockCacheManager) Get(ctx context.Context, key string) ([]byte, error) {
	if m.getFn != nil {
		return m.getFn(ctx, key)
	}
	return nil, nil
}
func (m *cov90MockCacheManager) Set(ctx context.Context, key string, value []byte) error {
	if m.setFn != nil {
		return m.setFn(ctx, key, value)
	}
	return nil
}
func (m *cov90MockCacheManager) Delete(ctx context.Context, key string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, key)
	}
	return nil
}
func (m *cov90MockCacheManager) Clear() {}
func (m *cov90MockCacheManager) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

// cov90MockClosingQRDBProvider - missing Close
func (m *cov90MockClosingQRDBProvider) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

// cov90MockStorage implements interfaces.Storage - missing Get, Set, Delete, Close
func (m *cov90MockStorage) Get(_ []byte) ([]byte, error)    { return nil, nil }
func (m *cov90MockStorage) Set(_, _ []byte) error           { return nil }
func (m *cov90MockStorage) Delete(_ []byte) error           { return nil }
func (m *cov90MockStorage) List(_ []byte) ([][]byte, error) { return nil, nil }
func (m *cov90MockStorage) Exists(_ []byte) (bool, error)   { return false, nil }
func (m *cov90MockStorage) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

// covMockCryptoManager - missing Close
func (m *covMockCryptoManager) Close() error { return nil }

// covNonCacheMgr implements interfaces.CacheManager
func (m *covNonCacheMgr) Get(_ context.Context, _ string) ([]byte, error) { return nil, nil }
func (m *covNonCacheMgr) Set(_ context.Context, _ string, _ []byte) error { return nil }
func (m *covNonCacheMgr) Delete(_ context.Context, _ string) error        { return nil }
func (m *covNonCacheMgr) Close() error                                    { return nil }

// mockUser2 implements interfaces.UserInfo
func (u *mockUser2) GetID() string          { return u.id }
func (u *mockUser2) GetUsername() string    { return u.username }
func (u *mockUser2) GetRoles() []string     { return nil }
func (u *mockUser2) GetCreated() time.Time  { return time.Time{} }
func (u *mockUser2) GetLastAuth() time.Time { return time.Time{} }

// mockUserStore2 implements interfaces.UserStore
func (s *mockUserStore2) AddUser(_ interfaces.UserInfo)              {}
func (s *mockUserStore2) GetByID(_ string) interfaces.UserInfo       { return nil }
func (s *mockUserStore2) GetByUsername(_ string) interfaces.UserInfo { return nil }
func (s *mockUserStore2) UpdateUser(_ interfaces.UserInfo)           {}
func (s *mockUserStore2) DeleteUser(_ string)                        {}
func (s *mockUserStore2) ListUsers() []interfaces.UserInfo           { return s.users }
func (s *mockUserStore2) AssignRole(_, _ string) error               { return nil }
func (s *mockUserStore2) RevokeRole(_, _ string) error               { return nil }
func (s *mockUserStore2) SetRoleManager(_ interfaces.RoleManager)    {}

// mockKMSScheduler implements KMSScheduler
func (m *mockKMSScheduler) ScheduleJob(_ context.Context, _ *SchedulerJob) error { return nil }

// rotateKeyTestScheduler implements KMSScheduler
func (s *rotateKeyTestScheduler) ScheduleJob(ctx context.Context, job *SchedulerJob) error {
	s.lastJob = job
	if s.scheduleJobFn != nil {
		return s.scheduleJobFn(ctx, job)
	}
	return nil
}

// svcBoostAuthManager implements interfaces.AuthManager
func (m *svcBoostAuthManager) Authenticate(_ context.Context, _, _ string) (interface{}, error) {
	return nil, nil
}
func (m *svcBoostAuthManager) IssueTokenForUser(_ context.Context, _ interfaces.UserInfo) (interface{}, error) {
	return nil, nil
}
func (m *svcBoostAuthManager) Authorize(_ context.Context, _, _, _ string) error { return nil }
func (m *svcBoostAuthManager) RefreshToken(_ context.Context, _ string) (interface{}, error) {
	return nil, nil
}
func (m *svcBoostAuthManager) RevokeToken(_ context.Context, _ string) error { return nil }
func (m *svcBoostAuthManager) ValidateToken(_ context.Context, _ string) (interface{}, error) {
	return nil, nil
}
func (m *svcBoostAuthManager) CreateUser(_, _ string, _ []string) (interface{}, error) {
	return nil, nil
}
func (m *svcBoostAuthManager) DeleteUser(_ string) error { return nil }
func (m *svcBoostAuthManager) GetUserStore() interfaces.UserStore {
	return m.userStoreReturn
}
func (m *svcBoostAuthManager) GetRoleManager() interfaces.RoleManager {
	return m.roleManagerReturn
}

// svcBoostRaftNode implements interfaces.RaftNode
func (n *svcBoostRaftNode) ID() string          { return "boost-node" }
func (n *svcBoostRaftNode) RaftAddress() string { return "localhost:0" }
func (n *svcBoostRaftNode) ShardCount() int     { return 1 }
func (n *svcBoostRaftNode) GetNodeHostInfo(_ interfaces.NodeHostInfoOption) *interfaces.NodeHostInfo {
	return &interfaces.NodeHostInfo{}
}
func (n *svcBoostRaftNode) HealthCheck() *interfaces.HealthStatus {
	return &interfaces.HealthStatus{}
}
func (n *svcBoostRaftNode) Drain() error     { return nil }
func (n *svcBoostRaftNode) IsDraining() bool { return false }
func (n *svcBoostRaftNode) SyncPropose(_ context.Context, _ uint64, _ []byte) (interfaces.Result, error) {
	return interfaces.Result{}, nil
}
func (n *svcBoostRaftNode) ProposeWithMode(_ context.Context, _ uint64, _ []byte, _ interfaces.WriteMode) (interfaces.Result, error) {
	return interfaces.Result{}, nil
}
func (n *svcBoostRaftNode) SyncRead(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
	if n.syncReadFn != nil {
		return n.syncReadFn(ctx, shardID, query)
	}
	return n.syncReadResult, n.syncReadErr
}
func (n *svcBoostRaftNode) StaleRead(_ context.Context, _ uint64, _ interface{}) (interface{}, error) {
	return nil, nil
}
func (n *svcBoostRaftNode) ReadWithConsistency(_ context.Context, _ uint64, _ interface{}, _ interfaces.ReadOptions) (interface{}, error) {
	return nil, nil
}
func (n *svcBoostRaftNode) GetLeaderID(_ uint64) (uint64, bool, error) { return 1, true, nil }
func (n *svcBoostRaftNode) GetShardRaftState(_ uint64) (uint64, uint64, error) {
	return 0, 0, nil
}
func (n *svcBoostRaftNode) RequestAddNode(_ context.Context, _, _ uint64, _ string, _ uint64) (interfaces.RequestState, error) {
	return nil, nil
}
func (n *svcBoostRaftNode) RequestRemoveNode(_ context.Context, _, _ uint64, _ uint64) (interfaces.RequestState, error) {
	return nil, nil
}
func (n *svcBoostRaftNode) RequestSnapshot(_ context.Context, _ uint64) (interfaces.RequestState, error) {
	return nil, nil
}
func (n *svcBoostRaftNode) StartShard(_ map[uint64]string, _ bool, _ interfaces.CreateFunc, _ interfaces.ShardConfig) error {
	return nil
}
func (n *svcBoostRaftNode) StopShard(_ uint64) error { return nil }
func (n *svcBoostRaftNode) Close() error             { return nil }

// svcBoostRoleManager - missing DeleteRole, ListRoles
func (m *svcBoostRoleManager) DeleteRole(_ string) error        { return nil }
func (m *svcBoostRoleManager) ListRoles() []interfaces.RoleInfo { return nil }

// svcBoostUserStore - missing AssignRole, RevokeRole, DeleteUser, ListUsers
func (s *svcBoostUserStore) AssignRole(_, _ string) error {
	s.assignRoleCalled = true
	return s.assignRoleErr
}
func (s *svcBoostUserStore) RevokeRole(_, _ string) error {
	s.revokeRoleCalled = true
	return s.revokeRoleErr
}
func (s *svcBoostUserStore) DeleteUser(_ string)              {}
func (s *svcBoostUserStore) ListUsers() []interfaces.UserInfo { return s.users }

// vecSDKProvider implements QRDBClientProvider
func (p *vecSDKProvider) ForShard(shardID uint64) (*qrdbsdk.Client, error) {
	p.lastShardID.Store(shardID)
	if p.transport == nil {
		return nil, fmt.Errorf("no transport")
	}
	return qrdbsdk.NewClient(p.transport)
}
func (p *vecSDKProvider) Close() error     { return nil }
func (p *vecSDKProvider) IsEmbedded() bool { return false }

// covKeyspaceError implements error
func (e *covKeyspaceError) Error() string { return e.msg }
