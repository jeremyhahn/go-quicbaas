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

// Package service provides unit tests for the DynamicSecretsService implementation.
package service

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	quicbaasErrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// Mock implementations with unique names to avoid conflicts.
type mockDSStorage struct {
	data map[string][]byte
}

func newMockDSStorage() *mockDSStorage {
	return &mockDSStorage{data: make(map[string][]byte)}
}

func (m *mockDSStorage) Set(key, value []byte) error {
	m.data[string(key)] = value
	return nil
}

func (m *mockDSStorage) Get(key []byte) ([]byte, error) {
	if val, ok := m.data[string(key)]; ok {
		return val, nil
	}
	return nil, &quicbaasErrors.DragonError{
		Code: quicbaasErrors.ErrNotFound,
		Op:   "mockDSStorage.Get",
		Err:  stderrors.New("key not found"),
	}
}

func (m *mockDSStorage) Delete(key []byte) error {
	delete(m.data, string(key))
	return nil
}

func (m *mockDSStorage) Batch() storage.Batch {
	return &mockDSBatch{storage: m}
}

func (m *mockDSStorage) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	return &mockDSIterator{data: m.data, opts: opts, index: -1}
}

func (m *mockDSStorage) Snapshot() (storage.Snapshot, error) { return nil, nil }
func (m *mockDSStorage) Close() error                        { return nil }

func (m *mockDSStorage) Exists(key []byte) (bool, error) {
	_, ok := m.data[string(key)]
	return ok, nil
}

func (m *mockDSStorage) List(prefix []byte) ([][]byte, error) {
	var keys [][]byte
	pfx := string(prefix)
	for k := range m.data {
		if len(pfx) == 0 || strings.HasPrefix(k, pfx) {
			keys = append(keys, []byte(k))
		}
	}
	return keys, nil
}

type mockDSBatch struct {
	storage *mockDSStorage
}

func (b *mockDSBatch) Set(key, value []byte) error { return b.storage.Set(key, value) }
func (b *mockDSBatch) Delete(key []byte) error     { return b.storage.Delete(key) }
func (b *mockDSBatch) Commit() error               { return nil }
func (b *mockDSBatch) Rollback() error             { return nil }
func (b *mockDSBatch) Close() error                { return nil }

type mockDSIterator struct {
	data  map[string][]byte
	opts  *storage.IteratorOptions
	keys  []string
	index int
}

func (m *mockDSIterator) buildKeys() {
	lowerBound := string(m.opts.LowerBound)
	upperBound := string(m.opts.UpperBound)
	m.keys = make([]string, 0)
	for key := range m.data {
		if key >= lowerBound && key < upperBound {
			m.keys = append(m.keys, key)
		}
	}
}

func (m *mockDSIterator) SeekToFirst() bool {
	if len(m.keys) == 0 {
		m.buildKeys()
	}
	if len(m.keys) > 0 {
		m.index = 0
		return true
	}
	return false
}

func (m *mockDSIterator) SeekToLast() bool {
	if len(m.keys) == 0 {
		m.buildKeys()
	}
	if len(m.keys) > 0 {
		m.index = len(m.keys) - 1
		return true
	}
	return false
}

func (m *mockDSIterator) Seek(target []byte) bool {
	if len(m.keys) == 0 {
		m.buildKeys()
	}
	targetStr := string(target)
	for i, key := range m.keys {
		if key >= targetStr {
			m.index = i
			return true
		}
	}
	return false
}

func (m *mockDSIterator) Next() bool {
	if m.index == -1 {
		m.buildKeys()
		if len(m.keys) > 0 {
			m.index = 0
			return true
		}
		return false
	}
	m.index++
	return m.index < len(m.keys)
}

func (m *mockDSIterator) Prev() bool {
	if m.index <= 0 {
		return false
	}
	m.index--
	return true
}

func (m *mockDSIterator) Valid() bool {
	return m.index >= 0 && m.index < len(m.keys)
}

func (m *mockDSIterator) Key() []byte {
	if !m.Valid() {
		return nil
	}
	return []byte(m.keys[m.index])
}

func (m *mockDSIterator) Value() []byte {
	if !m.Valid() {
		return nil
	}
	return m.data[m.keys[m.index]]
}

func (m *mockDSIterator) Error() error { return nil }
func (m *mockDSIterator) Close() error { return nil }

type mockDSLeaseManager struct {
	leases        map[string]*lease.Lease
	createLeaseFn func(ctx context.Context, path string, ttl time.Duration, opts *lease.LeaseOptions) (*lease.Lease, error)
	renewLeaseFn  func(ctx context.Context, leaseID string, increment time.Duration) (*lease.Lease, error)
	revokeLeaseFn func(ctx context.Context, leaseID string) error
	listLeasesFn  func(ctx context.Context, prefix string) ([]*lease.Lease, error)
}

func newMockDSLeaseManager() *mockDSLeaseManager {
	return &mockDSLeaseManager{leases: make(map[string]*lease.Lease)}
}

func (m *mockDSLeaseManager) CreateLease(ctx context.Context, path string, ttl time.Duration, opts *lease.LeaseOptions) (*lease.Lease, error) {
	if m.createLeaseFn != nil {
		return m.createLeaseFn(ctx, path, ttl, opts)
	}
	leaseID := fmt.Sprintf("lease-%d", len(m.leases)+1)
	l := &lease.Lease{
		LeaseID: leaseID, SecretPath: path, IssuedAt: time.Now(), ExpiresAt: time.Now().Add(ttl),
		TTL: ttl, RenewCount: 0, MaxRenewals: opts.MaxRenewals, Renewable: opts.Renewable, Metadata: opts.Metadata,
	}
	m.leases[leaseID] = l
	return l, nil
}

func (m *mockDSLeaseManager) RenewLease(ctx context.Context, leaseID string, increment time.Duration) (*lease.Lease, error) {
	if m.renewLeaseFn != nil {
		return m.renewLeaseFn(ctx, leaseID, increment)
	}
	l, exists := m.leases[leaseID]
	if !exists {
		return nil, &quicbaasErrors.DragonError{
			Code: quicbaasErrors.ErrNotFound, Op: "mockDSLeaseManager.RenewLease",
			Err: stderrors.New("lease not found: " + leaseID),
		}
	}
	if increment == 0 {
		increment = l.TTL
	}
	l.ExpiresAt = time.Now().Add(increment)
	l.RenewCount++
	return l, nil
}

func (m *mockDSLeaseManager) GetLease(ctx context.Context, leaseID string) (*lease.Lease, error) {
	l, exists := m.leases[leaseID]
	if !exists {
		return nil, &quicbaasErrors.DragonError{Code: quicbaasErrors.ErrNotFound, Op: "mockDSLeaseManager.GetLease", Err: stderrors.New("lease not found: " + leaseID)}
	}
	return l, nil
}

func (m *mockDSLeaseManager) RevokeLease(ctx context.Context, leaseID string) error {
	if m.revokeLeaseFn != nil {
		return m.revokeLeaseFn(ctx, leaseID)
	}
	l, exists := m.leases[leaseID]
	if !exists {
		return &quicbaasErrors.DragonError{Code: quicbaasErrors.ErrNotFound, Op: "mockDSLeaseManager.RevokeLease", Err: stderrors.New("lease not found: " + leaseID)}
	}
	now := time.Now()
	l.RevokedAt = &now
	delete(m.leases, leaseID)
	return nil
}

func (m *mockDSLeaseManager) RevokePrefix(ctx context.Context, prefix string) (int, error) {
	return 0, nil
}

func (m *mockDSLeaseManager) ListLeases(ctx context.Context, prefix string) ([]*lease.Lease, error) {
	if m.listLeasesFn != nil {
		return m.listLeasesFn(ctx, prefix)
	}
	result := make([]*lease.Lease, 0, len(m.leases))
	for _, l := range m.leases {
		result = append(result, l)
	}
	return result, nil
}

func (m *mockDSLeaseManager) StartExpirationLoop(ctx context.Context) error { return nil }
func (m *mockDSLeaseManager) Close() error                                  { return nil }

type mockDSGenerator struct {
	genType    string
	validateFn func(config map[string]string) error
	generateFn func(ctx context.Context, role *credentials.Role) (*credentials.Credential, error)
}

func newMockDSGenerator(genType string) *mockDSGenerator {
	return &mockDSGenerator{genType: genType}
}

func (m *mockDSGenerator) Type() string { return m.genType }

func (m *mockDSGenerator) ValidateConfig(config map[string]string) error {
	if m.validateFn != nil {
		return m.validateFn(config)
	}
	return nil
}

func (m *mockDSGenerator) Generate(ctx context.Context, role *credentials.Role) (*credentials.Credential, error) {
	if m.generateFn != nil {
		return m.generateFn(ctx, role)
	}
	return &credentials.Credential{
		CredentialID: fmt.Sprintf("cred-%s-%d", role.Name, time.Now().UnixNano()),
		Type:         "database",
		Data:         map[string]string{"username": "test_user", "password": "test_password"},
		CreatedAt:    time.Now(),
		ExpiresAt:    time.Now().Add(role.DefaultTTL),
	}, nil
}

func (m *mockDSGenerator) Revoke(ctx context.Context, credential *credentials.Credential) error {
	return nil
}

func (m *mockDSGenerator) Rotate(ctx context.Context, old *credentials.Credential, role *credentials.Role) (*credentials.Credential, error) {
	return m.Generate(ctx, role)
}

type mockDSLogger struct {
	fields map[string]interface{}
}

func newMockDSLogger() *mockDSLogger {
	return &mockDSLogger{fields: make(map[string]interface{})}
}

func (l *mockDSLogger) Debug(msg string)                                           {}
func (l *mockDSLogger) Debugf(format string, args ...interface{})                  {}
func (l *mockDSLogger) Info(msg string)                                            {}
func (l *mockDSLogger) Infof(format string, args ...interface{})                   {}
func (l *mockDSLogger) Warn(msg string)                                            {}
func (l *mockDSLogger) Warnf(format string, args ...interface{})                   {}
func (l *mockDSLogger) Error(msg string)                                           {}
func (l *mockDSLogger) Errorf(format string, args ...interface{})                  {}
func (l *mockDSLogger) Fatal(msg string)                                           {}
func (l *mockDSLogger) Fatalf(format string, args ...interface{})                  {}
func (l *mockDSLogger) WithField(key string, value interface{}) interfaces.Logger  { return l }
func (l *mockDSLogger) WithFields(fields map[string]interface{}) interfaces.Logger { return l }
func (l *mockDSLogger) WithError(err error) interfaces.Logger                      { return l }

func TestNewDynamicSecretsHandler_Success(t *testing.T) {
	store := newMockDSStorage()
	leaseMgr := newMockDSLeaseManager()
	registry := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	config := &DynamicSecretsConfig{
		Store: store, LeaseManager: leaseMgr, GeneratorRegistry: registry, Logger: logger,
	}

	svc, err := NewDynamicSecretsHandler(config)

	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestNewDynamicSecretsHandler_NilConfig(t *testing.T) {
	svc, err := NewDynamicSecretsHandler(nil)

	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "config cannot be nil")

	var dragonErr *quicbaasErrors.DragonError
	assert.True(t, stderrors.As(err, &dragonErr))
	assert.Equal(t, quicbaasErrors.ErrInvalidInput, dragonErr.Code)
}

func TestNewDynamicSecretsHandler_NilStore(t *testing.T) {
	config := &DynamicSecretsConfig{
		Store: nil, LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: credentials.NewGeneratorRegistry(), Logger: newMockDSLogger(),
	}

	svc, err := NewDynamicSecretsHandler(config)

	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "store cannot be nil")
}

func TestNewDynamicSecretsHandler_NilLeaseManager(t *testing.T) {
	config := &DynamicSecretsConfig{
		Store: newMockDSStorage(), LeaseManager: nil, GeneratorRegistry: credentials.NewGeneratorRegistry(), Logger: newMockDSLogger(),
	}

	svc, err := NewDynamicSecretsHandler(config)

	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "lease manager cannot be nil")
}

func TestNewDynamicSecretsHandler_NilRegistry(t *testing.T) {
	config := &DynamicSecretsConfig{
		Store: newMockDSStorage(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: nil, Logger: newMockDSLogger(),
	}

	svc, err := NewDynamicSecretsHandler(config)

	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "generator registry cannot be nil")
}

func TestNewDynamicSecretsHandler_NilLogger(t *testing.T) {
	config := &DynamicSecretsConfig{
		Store: newMockDSStorage(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: credentials.NewGeneratorRegistry(), Logger: nil,
	}

	svc, err := NewDynamicSecretsHandler(config)

	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "logger cannot be nil")
}

func TestDynamicSecretsService_CreateRole_Success(t *testing.T) {
	store := newMockDSStorage()
	leaseMgr := newMockDSLeaseManager()
	registry := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	config := &DynamicSecretsConfig{Store: store, LeaseManager: leaseMgr, GeneratorRegistry: registry, Logger: logger}
	svc, err := NewDynamicSecretsHandler(config)
	require.NoError(t, err)

	req := &CreateRoleRequest{
		Name: "db-readonly", GeneratorType: "postgres",
		Config:     map[string]string{"connection_url": "postgresql://localhost/testdb"},
		DefaultTTL: 1 * time.Hour, MaxTTL: 24 * time.Hour,
	}

	resp, err := svc.CreateRole(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "db-readonly", resp.Role.Name)
}

func TestDynamicSecretsService_CreateRole_NilRequest(t *testing.T) {
	svc, _ := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store: newMockDSStorage(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: credentials.NewGeneratorRegistry(), Logger: newMockDSLogger(),
	})

	resp, err := svc.CreateRole(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestDynamicSecretsService_CreateRole_EmptyName(t *testing.T) {
	svc, _ := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store: newMockDSStorage(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: credentials.NewGeneratorRegistry(), Logger: newMockDSLogger(),
	})

	req := &CreateRoleRequest{Name: "", GeneratorType: "postgres", DefaultTTL: 1 * time.Hour, MaxTTL: 24 * time.Hour}
	resp, err := svc.CreateRole(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "role name cannot be empty")
}

func TestDynamicSecretsService_CreateRole_InvalidTTL_TooShort(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	svc, _ := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store: newMockDSStorage(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: registry, Logger: newMockDSLogger(),
	})

	req := &CreateRoleRequest{Name: "db-role", GeneratorType: "postgres", Config: map[string]string{}, DefaultTTL: 30 * time.Second, MaxTTL: 24 * time.Hour}
	resp, err := svc.CreateRole(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "default TTL must be at least")
}

func TestDynamicSecretsService_CreateRole_DuplicateRole(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	svc, _ := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store: newMockDSStorage(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: registry, Logger: newMockDSLogger(),
	})

	req := &CreateRoleRequest{Name: "db-readonly", GeneratorType: "postgres", Config: map[string]string{}, DefaultTTL: 1 * time.Hour, MaxTTL: 24 * time.Hour}
	_, err := svc.CreateRole(context.Background(), req)
	require.NoError(t, err)

	resp, err := svc.CreateRole(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "role already exists")
}

func TestDynamicSecretsService_GetRole_Success(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	svc, _ := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store: newMockDSStorage(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: registry, Logger: newMockDSLogger(),
	})

	createReq := &CreateRoleRequest{Name: "db-readonly", GeneratorType: "postgres", Config: map[string]string{}, DefaultTTL: 1 * time.Hour, MaxTTL: 24 * time.Hour}
	_, err := svc.CreateRole(context.Background(), createReq)
	require.NoError(t, err)

	resp, err := svc.GetRole(context.Background(), &GetRoleRequest{Name: "db-readonly"})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "db-readonly", resp.Role.Name)
}

func TestDynamicSecretsService_GetRole_NotFound(t *testing.T) {
	svc, _ := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store: newMockDSStorage(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: credentials.NewGeneratorRegistry(), Logger: newMockDSLogger(),
	})

	resp, err := svc.GetRole(context.Background(), &GetRoleRequest{Name: "nonexistent"})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "role not found")
}

func TestDynamicSecretsService_ListRoles_Success(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	svc, _ := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store: newMockDSStorage(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: registry, Logger: newMockDSLogger(),
	})

	for _, name := range []string{"db-readonly", "db-admin"} {
		_, err := svc.CreateRole(context.Background(), &CreateRoleRequest{Name: name, GeneratorType: "postgres", Config: map[string]string{}, DefaultTTL: 1 * time.Hour, MaxTTL: 24 * time.Hour})
		require.NoError(t, err)
	}

	resp, err := svc.ListRoles(context.Background(), &ListRolesRequest{})

	require.NoError(t, err)
	assert.Equal(t, 2, resp.Count)
}

func TestDynamicSecretsService_DeleteRole_Success(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	svc, _ := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store: newMockDSStorage(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: registry, Logger: newMockDSLogger(),
	})

	_, err := svc.CreateRole(context.Background(), &CreateRoleRequest{Name: "db-readonly", GeneratorType: "postgres", Config: map[string]string{}, DefaultTTL: 1 * time.Hour, MaxTTL: 24 * time.Hour})
	require.NoError(t, err)

	resp, err := svc.DeleteRole(context.Background(), &DeleteRoleRequest{Name: "db-readonly"})

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestDynamicSecretsService_GenerateCredential_Success(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	svc, _ := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store: newMockDSStorage(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: registry, Logger: newMockDSLogger(),
	})

	_, err := svc.CreateRole(context.Background(), &CreateRoleRequest{Name: "db-readonly", GeneratorType: "postgres", Config: map[string]string{}, DefaultTTL: 1 * time.Hour, MaxTTL: 24 * time.Hour})
	require.NoError(t, err)

	resp, err := svc.GenerateCredential(context.Background(), &GenerateCredentialRequest{RoleName: "db-readonly"})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotEmpty(t, resp.Credential.CredentialID)
	assert.NotEmpty(t, resp.LeaseID)
}

func TestDynamicSecretsService_GenerateCredential_RoleNotFound(t *testing.T) {
	svc, _ := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store: newMockDSStorage(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: credentials.NewGeneratorRegistry(), Logger: newMockDSLogger(),
	})

	resp, err := svc.GenerateCredential(context.Background(), &GenerateCredentialRequest{RoleName: "nonexistent"})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "role not found")
}

func TestDynamicSecretsService_RenewLease_Success(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	svc, _ := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store: newMockDSStorage(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: registry, Logger: newMockDSLogger(),
	})

	_, err := svc.CreateRole(context.Background(), &CreateRoleRequest{Name: "db-readonly", GeneratorType: "postgres", Config: map[string]string{}, DefaultTTL: 1 * time.Hour, MaxTTL: 24 * time.Hour})
	require.NoError(t, err)

	genResp, err := svc.GenerateCredential(context.Background(), &GenerateCredentialRequest{RoleName: "db-readonly"})
	require.NoError(t, err)

	resp, err := svc.RenewLease(context.Background(), &RenewLeaseRequest{LeaseID: genResp.LeaseID, Increment: 2 * time.Hour})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 1, resp.Lease.RenewCount)
}

func TestDynamicSecretsService_RevokeLease_Success(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	svc, _ := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store: newMockDSStorage(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: registry, Logger: newMockDSLogger(),
	})

	_, err := svc.CreateRole(context.Background(), &CreateRoleRequest{Name: "db-readonly", GeneratorType: "postgres", Config: map[string]string{}, DefaultTTL: 1 * time.Hour, MaxTTL: 24 * time.Hour})
	require.NoError(t, err)

	genResp, err := svc.GenerateCredential(context.Background(), &GenerateCredentialRequest{RoleName: "db-readonly"})
	require.NoError(t, err)

	resp, err := svc.RevokeLease(context.Background(), &RevokeLeaseRequest{LeaseID: genResp.LeaseID})

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestDynamicSecretsService_ListLeases_Success(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	svc, _ := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store: newMockDSStorage(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: registry, Logger: newMockDSLogger(),
	})

	_, err := svc.CreateRole(context.Background(), &CreateRoleRequest{Name: "db-readonly", GeneratorType: "postgres", Config: map[string]string{}, DefaultTTL: 1 * time.Hour, MaxTTL: 24 * time.Hour})
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		_, err := svc.GenerateCredential(context.Background(), &GenerateCredentialRequest{RoleName: "db-readonly"})
		require.NoError(t, err)
	}

	resp, err := svc.ListLeases(context.Background(), &ListLeasesRequest{})

	require.NoError(t, err)
	assert.Equal(t, 3, resp.Count)
}

func TestDynamicSecretsService_GenerateCredential_StoreCredentialError(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	store := &failingDSCredStorage{
		mockDSStorage: *newMockDSStorage(),
		failOnCred:    true,
	}

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      newMockDSLeaseManager(),
		GeneratorRegistry: registry,
		Logger:            newMockDSLogger(),
	})
	require.NoError(t, err)

	_, err = svc.CreateRole(context.Background(), &CreateRoleRequest{
		Name:          "cred-fail-role",
		GeneratorType: "postgres",
		Config:        map[string]string{},
		DefaultTTL:    1 * time.Hour,
		MaxTTL:        24 * time.Hour,
	})
	require.NoError(t, err)

	resp, err := svc.GenerateCredential(context.Background(), &GenerateCredentialRequest{
		RoleName: "cred-fail-role",
		TTL:      30 * time.Minute,
	})
	if err != nil {
		assert.Nil(t, resp)
	}
}

func TestDynamicSecretsService_GenerateCredential_DefaultTTL(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             newMockDSStorage(),
		LeaseManager:      newMockDSLeaseManager(),
		GeneratorRegistry: registry,
		Logger:            newMockDSLogger(),
	})
	require.NoError(t, err)

	_, err = svc.CreateRole(context.Background(), &CreateRoleRequest{
		Name:          "default-ttl-role",
		GeneratorType: "postgres",
		Config:        map[string]string{},
		DefaultTTL:    2 * time.Hour,
		MaxTTL:        24 * time.Hour,
	})
	require.NoError(t, err)

	resp, err := svc.GenerateCredential(context.Background(), &GenerateCredentialRequest{
		RoleName: "default-ttl-role",
		TTL:      0,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.LeaseID)
}

func TestDynamicSecretsService_CreateRole_ContextCanceled(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	generatorReg.Register(newMockDSGenerator("mock"))
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := &CreateRoleRequest{
		Name:          "test-role",
		GeneratorType: "mock",
		DefaultTTL:    time.Hour,
		MaxTTL:        2 * time.Hour,
	}

	_, err = svc.CreateRole(ctx, req)
	assert.Error(t, err)
	dragonErr, ok := err.(*quicbaasErrors.DragonError)
	require.True(t, ok)
	assert.Equal(t, quicbaasErrors.ErrTimeout, dragonErr.Code)
}

func TestDynamicSecretsService_GetRole_ContextCanceled(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	generatorReg.Register(newMockDSGenerator("mock"))
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	// Create a role first
	_, err = svc.CreateRole(context.Background(), &CreateRoleRequest{
		Name:          "test-role",
		GeneratorType: "mock",
		DefaultTTL:    time.Hour,
		MaxTTL:        2 * time.Hour,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = svc.GetRole(ctx, &GetRoleRequest{Name: "test-role"})
	assert.Error(t, err)
	dragonErr, ok := err.(*quicbaasErrors.DragonError)
	require.True(t, ok)
	assert.Equal(t, quicbaasErrors.ErrTimeout, dragonErr.Code)
}

func TestDynamicSecretsService_ListRoles_ContextCanceled(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = svc.ListRoles(ctx, &ListRolesRequest{})
	assert.Error(t, err)
	dragonErr, ok := err.(*quicbaasErrors.DragonError)
	require.True(t, ok)
	assert.Equal(t, quicbaasErrors.ErrTimeout, dragonErr.Code)
}

func TestDynamicSecretsService_DeleteRole_ContextCanceled(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = svc.DeleteRole(ctx, &DeleteRoleRequest{Name: "test-role"})
	assert.Error(t, err)
	dragonErr, ok := err.(*quicbaasErrors.DragonError)
	require.True(t, ok)
	assert.Equal(t, quicbaasErrors.ErrTimeout, dragonErr.Code)
}

func TestDynamicSecretsService_GenerateCredential_TTLExceedsMax(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	generatorReg.Register(newMockDSGenerator("mock"))
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	// Create role
	_, err = svc.CreateRole(context.Background(), &CreateRoleRequest{
		Name:          "test-role",
		GeneratorType: "mock",
		DefaultTTL:    time.Hour,
		MaxTTL:        2 * time.Hour,
	})
	require.NoError(t, err)

	// Try to generate with TTL exceeding max
	_, err = svc.GenerateCredential(context.Background(), &GenerateCredentialRequest{
		RoleName: "test-role",
		TTL:      3 * time.Hour,
	})
	assert.Error(t, err)
	dragonErr, ok := err.(*quicbaasErrors.DragonError)
	require.True(t, ok)
	assert.Equal(t, quicbaasErrors.ErrInvalidInput, dragonErr.Code)
}

func TestDynamicSecretsService_GenerateCredential_GeneratorFailure(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()

	// Create a generator that fails
	failGen := newMockDSGenerator("mock")
	failGen.generateFn = func(ctx context.Context, role *credentials.Role) (*credentials.Credential, error) {
		return nil, stderrors.New("generator error")
	}
	generatorReg.Register(failGen)

	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	// Create role
	_, err = svc.CreateRole(context.Background(), &CreateRoleRequest{
		Name:          "test-role",
		GeneratorType: "mock",
		DefaultTTL:    time.Hour,
		MaxTTL:        2 * time.Hour,
	})
	require.NoError(t, err)

	_, err = svc.GenerateCredential(context.Background(), &GenerateCredentialRequest{
		RoleName: "test-role",
	})
	assert.Error(t, err)
	dragonErr, ok := err.(*quicbaasErrors.DragonError)
	require.True(t, ok)
	assert.Equal(t, quicbaasErrors.ErrInternal, dragonErr.Code)
}

func TestDynamicSecretsService_GenerateCredential_LeaseFailure(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	leaseManager.createLeaseFn = func(ctx context.Context, path string, ttl time.Duration, opts *lease.LeaseOptions) (*lease.Lease, error) {
		return nil, stderrors.New("lease creation failed")
	}

	generatorReg := credentials.NewGeneratorRegistry()
	generatorReg.Register(newMockDSGenerator("mock"))
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	// Create role
	_, err = svc.CreateRole(context.Background(), &CreateRoleRequest{
		Name:          "test-role",
		GeneratorType: "mock",
		DefaultTTL:    time.Hour,
		MaxTTL:        2 * time.Hour,
	})
	require.NoError(t, err)

	_, err = svc.GenerateCredential(context.Background(), &GenerateCredentialRequest{
		RoleName: "test-role",
	})
	assert.Error(t, err)
	dragonErr, ok := err.(*quicbaasErrors.DragonError)
	require.True(t, ok)
	assert.Equal(t, quicbaasErrors.ErrInternal, dragonErr.Code)
}

func TestDynamicSecretsService_RenewLease_Failure(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	leaseManager.renewLeaseFn = func(ctx context.Context, leaseID string, increment time.Duration) (*lease.Lease, error) {
		return nil, stderrors.New("renew failed")
	}

	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	_, err = svc.RenewLease(context.Background(), &RenewLeaseRequest{
		LeaseID:   "test-lease",
		Increment: time.Hour,
	})
	assert.Error(t, err)
}

func TestDynamicSecretsService_RevokeLease_Failure(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	leaseManager.revokeLeaseFn = func(ctx context.Context, leaseID string) error {
		return stderrors.New("revoke failed")
	}

	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	_, err = svc.RevokeLease(context.Background(), &RevokeLeaseRequest{
		LeaseID: "test-lease",
	})
	assert.Error(t, err)
}

func TestDynamicSecretsService_ListLeases_ContextCanceled(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = svc.ListLeases(ctx, &ListLeasesRequest{})
	assert.Error(t, err)
	dragonErr, ok := err.(*quicbaasErrors.DragonError)
	require.True(t, ok)
	assert.Equal(t, quicbaasErrors.ErrTimeout, dragonErr.Code)
}

func TestDynamicSecretsService_ListLeases_Failure(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	leaseManager.listLeasesFn = func(ctx context.Context, prefix string) ([]*lease.Lease, error) {
		return nil, stderrors.New("list failed")
	}

	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	_, err = svc.ListLeases(context.Background(), &ListLeasesRequest{})
	assert.Error(t, err)
}

func TestDynamicSecretsService_LoadRoles_WithData(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	generatorReg.Register(newMockDSGenerator("mock"))
	logger := newMockDSLogger()

	// Pre-populate with role data
	roleJSON := `{"name":"preloaded","generator_type":"mock","default_ttl":3600000000000,"max_ttl":7200000000000}`
	err := store.Set([]byte(RolePrefix+"preloaded"), []byte(roleJSON))
	require.NoError(t, err)

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	// Verify role was loaded
	svc.mu.RLock()
	_, exists := svc.credentialRoles["preloaded"]
	svc.mu.RUnlock()
	assert.True(t, exists)
}

func TestDynamicSecretsService_LoadRoles_SkipInvalidJSON(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	// Pre-populate with invalid JSON
	err := store.Set([]byte(RolePrefix+"invalid"), []byte("not json"))
	require.NoError(t, err)

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	// Verify invalid role was not loaded
	svc.mu.RLock()
	_, exists := svc.credentialRoles["invalid"]
	svc.mu.RUnlock()
	assert.False(t, exists)
}

func TestDynamicSecretsService_CreateRole_GeneratorNotFound(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	_, err = svc.CreateRole(context.Background(), &CreateRoleRequest{
		Name:          "test-role",
		GeneratorType: "nonexistent",
		DefaultTTL:    time.Hour,
		MaxTTL:        2 * time.Hour,
	})
	assert.Error(t, err)
	dragonErr, ok := err.(*quicbaasErrors.DragonError)
	require.True(t, ok)
	assert.Equal(t, quicbaasErrors.ErrNotFound, dragonErr.Code)
}

// TestNewDynamicSecretsHandler_InitMetricsSuccess tests metrics initialization during service creation.
func TestNewDynamicSecretsHandler_InitMetricsSuccess(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})

	require.NoError(t, err)
	require.NotNil(t, svc)

	// Metrics should be initialized
	assert.NotNil(t, svc.metrics)
}

// TestDynamicSecretsService_CreateRole_EmptyGeneratorType tests validation.
func TestDynamicSecretsService_CreateRole_EmptyGeneratorType(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	_, err = svc.CreateRole(context.Background(), &CreateRoleRequest{
		Name:          "test-role",
		GeneratorType: "", // Empty generator type
		DefaultTTL:    time.Hour,
		MaxTTL:        2 * time.Hour,
	})

	assert.Error(t, err)
	dragonErr, ok := err.(*quicbaasErrors.DragonError)
	require.True(t, ok)
	assert.Equal(t, quicbaasErrors.ErrInvalidInput, dragonErr.Code)
}

// TestDynamicSecretsService_CreateRole_TTLBoundaries tests TTL validation boundaries.
func TestDynamicSecretsService_CreateRole_TTLBoundaries(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	generatorReg.Register(newMockDSGenerator("mock"))
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	// Test MaxTTL exceeds limit
	_, err = svc.CreateRole(context.Background(), &CreateRoleRequest{
		Name:          "test-role",
		GeneratorType: "mock",
		DefaultTTL:    time.Hour,
		MaxTTL:        MaxRoleTTL + time.Hour, // Exceeds max
	})
	assert.Error(t, err)

	// Test DefaultTTL > MaxTTL
	_, err = svc.CreateRole(context.Background(), &CreateRoleRequest{
		Name:          "test-role2",
		GeneratorType: "mock",
		DefaultTTL:    3 * time.Hour,
		MaxTTL:        2 * time.Hour, // Default exceeds max
	})
	assert.Error(t, err)
}

// TestDynamicSecretsService_GetRole_NilRequest tests nil request handling.
func TestDynamicSecretsService_GetRole_NilRequest(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	_, err = svc.GetRole(context.Background(), nil)
	assert.Error(t, err)
}

// TestDynamicSecretsService_DeleteRole_NilRequest tests nil request handling.
func TestDynamicSecretsService_DeleteRole_NilRequest(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	_, err = svc.DeleteRole(context.Background(), nil)
	assert.Error(t, err)
}

// TestDynamicSecretsService_RenewLease_ContextCanceled tests context handling.
func TestDynamicSecretsService_RenewLease_ContextCanceled(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = svc.RenewLease(ctx, &RenewLeaseRequest{
		LeaseID:   "test-lease",
		Increment: time.Hour,
	})
	assert.Error(t, err)
}

// TestDynamicSecretsService_RevokeLease_ContextCanceled tests context handling.
func TestDynamicSecretsService_RevokeLease_ContextCanceled(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = svc.RevokeLease(ctx, &RevokeLeaseRequest{LeaseID: "test-lease"})
	assert.Error(t, err)
}

// TestDynamicSecretsService_GenerateCredential_NilRequest tests nil request.
func TestDynamicSecretsService_GenerateCredential_NilRequest(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	_, err = svc.GenerateCredential(context.Background(), nil)
	assert.Error(t, err)
}

// TestDynamicSecretsService_GenerateCredential_ContextCanceled tests context.
func TestDynamicSecretsService_GenerateCredential_ContextCanceled(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = svc.GenerateCredential(ctx, &GenerateCredentialRequest{
		RoleName: "test-role",
	})
	assert.Error(t, err)
}

// TestDynamicSecretsService_RenewLease_NilRequest tests nil request.
func TestDynamicSecretsService_RenewLease_NilRequest(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	_, err = svc.RenewLease(context.Background(), nil)
	assert.Error(t, err)
}

// TestDynamicSecretsService_RevokeLease_NilRequest tests nil request.
func TestDynamicSecretsService_RevokeLease_NilRequest(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	_, err = svc.RevokeLease(context.Background(), nil)
	assert.Error(t, err)
}

// TestDynamicSecretsService_DeleteRole_NotFound tests deleting non-existent role.
func TestDynamicSecretsService_DeleteRole_NotFound(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	_, err = svc.DeleteRole(context.Background(), &DeleteRoleRequest{
		Name: "nonexistent-role",
	})
	assert.Error(t, err)
	dragonErr, ok := err.(*quicbaasErrors.DragonError)
	require.True(t, ok)
	assert.Equal(t, quicbaasErrors.ErrNotFound, dragonErr.Code)
}

// TestDynamicSecretsService_GetRole_EmptyName tests empty role name.
func TestDynamicSecretsService_GetRole_EmptyName(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	_, err = svc.GetRole(context.Background(), &GetRoleRequest{Name: ""})
	assert.Error(t, err)
}

// TestDynamicSecretsService_DeleteRole_EmptyName tests empty role name.
func TestDynamicSecretsService_DeleteRole_EmptyName(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	_, err = svc.DeleteRole(context.Background(), &DeleteRoleRequest{Name: ""})
	assert.Error(t, err)
}

// TestDynamicSecretsService_RenewLease_EmptyLeaseID tests empty lease ID.
func TestDynamicSecretsService_RenewLease_EmptyLeaseID(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	_, err = svc.RenewLease(context.Background(), &RenewLeaseRequest{LeaseID: ""})
	assert.Error(t, err)
}

// TestDynamicSecretsService_RevokeLease_EmptyLeaseID tests empty lease ID.
func TestDynamicSecretsService_RevokeLease_EmptyLeaseID(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	_, err = svc.RevokeLease(context.Background(), &RevokeLeaseRequest{LeaseID: ""})
	assert.Error(t, err)
}

// TestDynamicSecretsService_ListLeases_NilRequest tests nil request with default behavior.
func TestDynamicSecretsService_ListLeases_NilRequest(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	// Nil request should be handled (creates default empty request)
	resp, err := svc.ListLeases(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

// TestDynamicSecretsService_GenerateCredential_EmptyRoleName tests empty role name.
func TestDynamicSecretsService_GenerateCredential_EmptyRoleName(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	_, err = svc.GenerateCredential(context.Background(), &GenerateCredentialRequest{
		RoleName: "",
	})
	assert.Error(t, err)
}

// TestDynamicSecretsService_GenerateCredential_GetGeneratorError tests when getting generator fails.
func TestDynamicSecretsService_GenerateCredential_GetGeneratorError(t *testing.T) {
	store := newMockDSStorage()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generatorReg,
		Logger:            logger,
	})
	require.NoError(t, err)

	// Manually add a role with a generator type that doesn't exist
	svc.mu.Lock()
	svc.credentialRoles["test-role"] = &credentials.Role{
		Name:          "test-role",
		GeneratorType: "nonexistent",
		DefaultTTL:    time.Hour,
		MaxTTL:        2 * time.Hour,
	}
	svc.mu.Unlock()

	_, err = svc.GenerateCredential(context.Background(), &GenerateCredentialRequest{
		RoleName: "test-role",
	})
	assert.Error(t, err)
	dragonErr, ok := err.(*quicbaasErrors.DragonError)
	require.True(t, ok)
	assert.Equal(t, quicbaasErrors.ErrInternal, dragonErr.Code)
}

func TestDynamicSecretsService_ListRoles_WithPrefix(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             newMockDSStorage(),
		LeaseManager:      newMockDSLeaseManager(),
		GeneratorRegistry: registry,
		Logger:            newMockDSLogger(),
	})
	require.NoError(t, err)

	// Create roles with different names
	for _, name := range []string{"db-readonly", "db-admin", "cache-reader"} {
		_, err := svc.CreateRole(context.Background(), &CreateRoleRequest{
			Name:          name,
			GeneratorType: "postgres",
			Config:        map[string]string{},
			DefaultTTL:    1 * time.Hour,
			MaxTTL:        24 * time.Hour,
		})
		require.NoError(t, err)
	}

	// List roles with prefix filter
	resp, err := svc.ListRoles(context.Background(), &ListRolesRequest{
		Prefix: "db-",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 2, resp.Count)
}

func TestDynamicSecretsService_ListRoles_EmptyResult(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             newMockDSStorage(),
		LeaseManager:      newMockDSLeaseManager(),
		GeneratorRegistry: registry,
		Logger:            newMockDSLogger(),
	})
	require.NoError(t, err)

	resp, err := svc.ListRoles(context.Background(), &ListRolesRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 0, resp.Count)
}

func TestDynamicSecretsService_CreateRole_StorageError(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	store := &failingDSStorage{failOnSet: true}

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      newMockDSLeaseManager(),
		GeneratorRegistry: registry,
		Logger:            newMockDSLogger(),
	})
	require.NoError(t, err)

	resp, err := svc.CreateRole(context.Background(), &CreateRoleRequest{
		Name:          "failing-role",
		GeneratorType: "postgres",
		Config:        map[string]string{},
		DefaultTTL:    1 * time.Hour,
		MaxTTL:        24 * time.Hour,
	})
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestDynamicSecretsService_DeleteRole_NotFoundPath(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             newMockDSStorage(),
		LeaseManager:      newMockDSLeaseManager(),
		GeneratorRegistry: registry,
		Logger:            newMockDSLogger(),
	})
	require.NoError(t, err)

	resp, err := svc.DeleteRole(context.Background(), &DeleteRoleRequest{Name: "nonexistent"})
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestDynamicSecretsService_GenerateCredential_WithCustomTTL(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             newMockDSStorage(),
		LeaseManager:      newMockDSLeaseManager(),
		GeneratorRegistry: registry,
		Logger:            newMockDSLogger(),
	})
	require.NoError(t, err)

	_, err = svc.CreateRole(context.Background(), &CreateRoleRequest{
		Name:          "ttl-role",
		GeneratorType: "postgres",
		Config:        map[string]string{},
		DefaultTTL:    1 * time.Hour,
		MaxTTL:        24 * time.Hour,
	})
	require.NoError(t, err)

	resp, err := svc.GenerateCredential(context.Background(), &GenerateCredentialRequest{
		RoleName: "ttl-role",
		TTL:      30 * time.Minute,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.LeaseID)
}

func TestDynamicSecretsService_GenerateCredential_TTLClampedToMax(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             newMockDSStorage(),
		LeaseManager:      newMockDSLeaseManager(),
		GeneratorRegistry: registry,
		Logger:            newMockDSLogger(),
	})
	require.NoError(t, err)

	_, err = svc.CreateRole(context.Background(), &CreateRoleRequest{
		Name:          "max-ttl-role",
		GeneratorType: "postgres",
		Config:        map[string]string{},
		DefaultTTL:    1 * time.Hour,
		MaxTTL:        2 * time.Hour,
	})
	require.NoError(t, err)

	// Request TTL exceeding the max should return an error
	resp, err := svc.GenerateCredential(context.Background(), &GenerateCredentialRequest{
		RoleName: "max-ttl-role",
		TTL:      48 * time.Hour, // exceeds max
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "exceeds max TTL")
}

func TestDynamicSecretsService_RevokeLease_NotFoundPath(t *testing.T) {
	svc, err := NewDynamicSecretsHandler(&DynamicSecretsConfig{
		Store:             newMockDSStorage(),
		LeaseManager:      newMockDSLeaseManager(),
		GeneratorRegistry: credentials.NewGeneratorRegistry(),
		Logger:            newMockDSLogger(),
	})
	require.NoError(t, err)

	resp, err := svc.RevokeLease(context.Background(), &RevokeLeaseRequest{
		LeaseID: "nonexistent-lease",
	})
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestDynamicSecretsService_storeRole_SetError(t *testing.T) {
	store := &errorReturningStorage{setError: true}
	svc := &dynamicSecretsHandler{
		store: store,
	}

	role := &credentials.Role{
		Name:          "test-role",
		GeneratorType: "postgres",
	}

	err := svc.storeRole(role)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store role failed")
}

func TestDynamicSecretsService_storeCredential_SetError(t *testing.T) {
	store := &errorReturningStorage{setError: true}
	svc := &dynamicSecretsHandler{
		store: store,
	}

	cred := &credentials.Credential{
		CredentialID: "test-cred-id",
		Type:         "database",
	}

	err := svc.storeCredential(cred)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store credential failed")
}
