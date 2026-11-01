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

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	quicbaasErrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
)

// faultDSStore is a FAULT INJECTOR over the real memory engine, not a second
// storage implementation: every verb delegates, and only the operation a test
// has explicitly armed fails. One injector replaces the three hand-written
// storage.Storage doubles this package carried, each of which had to restate
// the whole interface to fail one method.
type faultDSStore struct {
	inner interfaces.KVStore

	// putErr, when set, decides whether a write to key fails.
	putErr func(key string) error

	getErr    error
	deleteErr error
	scanErr   error
}

// newFaultDSStore returns an injector over a fresh qrdb memory engine.
func newFaultDSStore() *faultDSStore {
	return &faultDSStore{inner: kvstore.NewMemory()}
}

// failAllWrites arms every write with err.
func (f *faultDSStore) failAllWrites(err error) *faultDSStore {
	f.putErr = func(string) error { return err }
	return f
}

// failWritesUnder arms writes to keys beginning with prefix.
func (f *faultDSStore) failWritesUnder(prefix string, err error) *faultDSStore {
	f.putErr = func(key string) error {
		if strings.HasPrefix(key, prefix) {
			return err
		}
		return nil
	}
	return f
}

var _ interfaces.KVStore = (*faultDSStore)(nil)

func (f *faultDSStore) Put(ctx context.Context, key string, value []byte) error {
	if f.putErr != nil {
		if err := f.putErr(key); err != nil {
			return err
		}
	}
	return f.inner.Put(ctx, key, value)
}

func (f *faultDSStore) Get(ctx context.Context, key string) ([]byte, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.inner.Get(ctx, key)
}

func (f *faultDSStore) Delete(ctx context.Context, key string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return f.inner.Delete(ctx, key)
}

func (f *faultDSStore) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	if f.scanErr != nil {
		return f.scanErr
	}
	return f.inner.Scan(ctx, prefix, fn)
}

func (f *faultDSStore) List(ctx context.Context, prefix string) ([]string, error) {
	return f.inner.List(ctx, prefix)
}

func (f *faultDSStore) Exists(ctx context.Context, key string) (bool, error) {
	return f.inner.Exists(ctx, key)
}

// newDSStore returns the durable store a handler under test persists through:
// qrdb's own in-memory engine via the SDK re-export (pkg/kvstore). It is the
// SAME interfaces.KVStore implementation family production uses, so a test
// exercises real scan ordering and real not-found semantics instead of a
// hand-written map that has to be kept in sync with the contract.
func newDSStore() interfaces.KVStore {
	return kvstore.NewMemory()
}

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
		return nil, &quicbaasErrors.QBError{
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
		return nil, &quicbaasErrors.QBError{Code: quicbaasErrors.ErrNotFound, Op: "mockDSLeaseManager.GetLease", Err: stderrors.New("lease not found: " + leaseID)}
	}
	return l, nil
}

func (m *mockDSLeaseManager) RevokeLease(ctx context.Context, leaseID string) error {
	if m.revokeLeaseFn != nil {
		return m.revokeLeaseFn(ctx, leaseID)
	}
	l, exists := m.leases[leaseID]
	if !exists {
		return &quicbaasErrors.QBError{Code: quicbaasErrors.ErrNotFound, Op: "mockDSLeaseManager.RevokeLease", Err: stderrors.New("lease not found: " + leaseID)}
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

func (m *mockDSLeaseManager) Validate(ctx context.Context, leaseID string) (*lease.Lease, error) {
	l, exists := m.leases[leaseID]
	if !exists {
		return nil, lease.NewLeaseNotFoundError("mockDSLeaseManager.Validate", leaseID)
	}
	if l.RevokedAt != nil {
		return nil, lease.NewLeaseRevokedError("mockDSLeaseManager.Validate", leaseID, *l.RevokedAt)
	}
	if l.State() == lease.LeaseStateExpired {
		return nil, lease.NewLeaseExpiredError("mockDSLeaseManager.Validate", leaseID, l.ExpiresAt)
	}
	return l, nil
}

func (m *mockDSLeaseManager) ReapExpired(ctx context.Context) (int, int, error) { return 0, 0, nil }

func (m *mockDSLeaseManager) StartExpirationLoop(ctx context.Context) error { return nil }
func (m *mockDSLeaseManager) Close() error                                  { return nil }

type mockDSGenerator struct {
	genType    string
	validateFn func(config map[string]string) error
	generateFn func(ctx context.Context, role *credentials.Role) (*credentials.Credential, error)

	// revokeErr, when set, is what Revoke answers with: live backing material
	// the generator could not tear down.
	revokeErr error

	// revoked records every credential Revoke was asked to destroy, so a test
	// can assert the SPI verb was actually reached rather than that the lease
	// record changed.
	revoked []string
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
	if m.revokeErr != nil {
		return m.revokeErr
	}
	m.revoked = append(m.revoked, credential.CredentialID)
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

func (l *mockDSLogger) Debug(msg string, args ...any)                              {}
func (l *mockDSLogger) Debugf(format string, args ...interface{})                  {}
func (l *mockDSLogger) Info(msg string, args ...any)                               {}
func (l *mockDSLogger) Infof(format string, args ...interface{})                   {}
func (l *mockDSLogger) Warn(msg string, args ...any)                               {}
func (l *mockDSLogger) Warnf(format string, args ...interface{})                   {}
func (l *mockDSLogger) Error(msg string, args ...any)                              {}
func (l *mockDSLogger) Errorf(format string, args ...interface{})                  {}
func (l *mockDSLogger) Fatal(msg string)                                           {}
func (l *mockDSLogger) Fatalf(format string, args ...interface{})                  {}
func (l *mockDSLogger) WithField(key string, value interface{}) interfaces.Logger  { return l }
func (l *mockDSLogger) WithFields(fields map[string]interface{}) interfaces.Logger { return l }
func (l *mockDSLogger) WithError(err error) interfaces.Logger                      { return l }

func TestNewDynamicSecretsHandler_Success(t *testing.T) {
	config := &DynamicSecretsConfig{
		Store: newDSStore(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: credentials.NewGeneratorRegistry(), Logger: newMockDSLogger(),
	}

	svc, err := NewDynamicSecretsHandler(context.Background(), config)

	require.NoError(t, err)
	require.NotNil(t, svc)
	assert.NotNil(t, svc.metrics)
}

func TestNewDynamicSecretsHandler_RejectsMissingDependency(t *testing.T) {
	valid := func() *DynamicSecretsConfig {
		return &DynamicSecretsConfig{
			Store: newDSStore(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: credentials.NewGeneratorRegistry(), Logger: newMockDSLogger(),
		}
	}

	tests := []struct {
		name    string
		config  func() *DynamicSecretsConfig
		wantMsg string
	}{
		{"nil config", func() *DynamicSecretsConfig { return nil }, "config cannot be nil"},
		{"nil store", func() *DynamicSecretsConfig { c := valid(); c.Store = nil; return c }, "store cannot be nil"},
		{"nil lease manager", func() *DynamicSecretsConfig { c := valid(); c.LeaseManager = nil; return c }, "lease manager cannot be nil"},
		{"nil registry", func() *DynamicSecretsConfig { c := valid(); c.GeneratorRegistry = nil; return c }, "generator registry cannot be nil"},
		{"nil logger", func() *DynamicSecretsConfig { c := valid(); c.Logger = nil; return c }, "logger cannot be nil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := NewDynamicSecretsHandler(context.Background(), tt.config())

			assert.Nil(t, svc)
			var qbErr *quicbaasErrors.QBError
			require.ErrorAs(t, err, &qbErr)
			assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestDynamicSecretsService_CreateRole_Success(t *testing.T) {
	store := newDSStore()
	leaseMgr := newMockDSLeaseManager()
	registry := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	config := &DynamicSecretsConfig{Store: store, LeaseManager: leaseMgr, GeneratorRegistry: registry, Logger: logger}
	svc, err := NewDynamicSecretsHandler(context.Background(), config)
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

// TestDynamicSecretsService_RejectsBadInput checks every method's guard: a
// nil or empty request is invalid input, and a canceled context is a timeout
// reported before any work is done.
func TestDynamicSecretsService_RejectsBadInput(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	live := context.Background()

	tests := []struct {
		name     string
		ctx      context.Context
		call     func(ctx context.Context, svc *dynamicSecretsHandler) error
		wantCode quicbaasErrors.ErrorCode
	}{
		{"CreateRole nil request", live, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.CreateRole(ctx, nil)
			return err
		}, quicbaasErrors.ErrInvalidInput},
		{"CreateRole empty name", live, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.CreateRole(ctx, &CreateRoleRequest{GeneratorType: "mock", DefaultTTL: time.Hour, MaxTTL: 2 * time.Hour})
			return err
		}, quicbaasErrors.ErrInvalidInput},
		{"CreateRole canceled", canceled, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.CreateRole(ctx, &CreateRoleRequest{Name: "r", GeneratorType: "mock", DefaultTTL: time.Hour, MaxTTL: 2 * time.Hour})
			return err
		}, quicbaasErrors.ErrTimeout},
		{"GetRole nil request", live, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.GetRole(ctx, nil)
			return err
		}, quicbaasErrors.ErrInvalidInput},
		{"GetRole empty name", live, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.GetRole(ctx, &GetRoleRequest{})
			return err
		}, quicbaasErrors.ErrInvalidInput},
		{"GetRole canceled", canceled, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.GetRole(ctx, &GetRoleRequest{Name: "r"})
			return err
		}, quicbaasErrors.ErrTimeout},
		{"ListRoles canceled", canceled, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.ListRoles(ctx, &ListRolesRequest{})
			return err
		}, quicbaasErrors.ErrTimeout},
		{"DeleteRole nil request", live, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.DeleteRole(ctx, nil)
			return err
		}, quicbaasErrors.ErrInvalidInput},
		{"DeleteRole empty name", live, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.DeleteRole(ctx, &DeleteRoleRequest{})
			return err
		}, quicbaasErrors.ErrInvalidInput},
		{"DeleteRole canceled", canceled, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.DeleteRole(ctx, &DeleteRoleRequest{Name: "r"})
			return err
		}, quicbaasErrors.ErrTimeout},
		{"GenerateCredential nil request", live, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.GenerateCredential(ctx, nil)
			return err
		}, quicbaasErrors.ErrInvalidInput},
		{"GenerateCredential empty role name", live, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.GenerateCredential(ctx, &GenerateCredentialRequest{})
			return err
		}, quicbaasErrors.ErrInvalidInput},
		{"GenerateCredential canceled", canceled, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.GenerateCredential(ctx, &GenerateCredentialRequest{RoleName: "r"})
			return err
		}, quicbaasErrors.ErrTimeout},
		{"RenewLease nil request", live, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.RenewLease(ctx, nil)
			return err
		}, quicbaasErrors.ErrInvalidInput},
		{"RenewLease empty lease ID", live, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.RenewLease(ctx, &RenewLeaseRequest{})
			return err
		}, quicbaasErrors.ErrInvalidInput},
		{"RenewLease canceled", canceled, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.RenewLease(ctx, &RenewLeaseRequest{LeaseID: "l", Increment: time.Hour})
			return err
		}, quicbaasErrors.ErrTimeout},
		{"RevokeLease nil request", live, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.RevokeLease(ctx, nil)
			return err
		}, quicbaasErrors.ErrInvalidInput},
		{"RevokeLease empty lease ID", live, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.RevokeLease(ctx, &RevokeLeaseRequest{})
			return err
		}, quicbaasErrors.ErrInvalidInput},
		{"RevokeLease canceled", canceled, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.RevokeLease(ctx, &RevokeLeaseRequest{LeaseID: "l"})
			return err
		}, quicbaasErrors.ErrTimeout},
		{"ListLeases canceled", canceled, func(ctx context.Context, svc *dynamicSecretsHandler) error {
			_, err := svc.ListLeases(ctx, &ListLeasesRequest{})
			return err
		}, quicbaasErrors.ErrTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
				Store: newDSStore(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: credentials.NewGeneratorRegistry(), Logger: newMockDSLogger(),
			})
			require.NoError(t, err)

			err = tt.call(tt.ctx, svc)

			var qbErr *quicbaasErrors.QBError
			require.ErrorAs(t, err, &qbErr)
			assert.Equal(t, tt.wantCode, qbErr.Code)
		})
	}
}

// TestDynamicSecretsService_CreateRole_RejectsInvalidRole checks each CreateRole
// validation in the order the service applies them.
func TestDynamicSecretsService_CreateRole_RejectsInvalidRole(t *testing.T) {
	isType := func(target any) func(error) bool {
		return func(err error) bool { return stderrors.As(err, target) }
	}

	tests := []struct {
		name     string
		req      *CreateRoleRequest
		wantCode quicbaasErrors.ErrorCode
		wantErr  func(error) bool
	}{
		{
			name:     "empty generator type",
			req:      &CreateRoleRequest{Name: "r", DefaultTTL: time.Hour, MaxTTL: 2 * time.Hour},
			wantCode: quicbaasErrors.ErrInvalidInput,
			wantErr:  func(err error) bool { return strings.Contains(err.Error(), "generator type cannot be empty") },
		},
		{
			name:     "default TTL below minimum",
			req:      &CreateRoleRequest{Name: "r", GeneratorType: "postgres", DefaultTTL: 30 * time.Second, MaxTTL: 24 * time.Hour},
			wantCode: quicbaasErrors.ErrInvalidInput,
			wantErr:  isType(new(*api.MinDurationValueError)),
		},
		{
			name:     "max TTL above limit",
			req:      &CreateRoleRequest{Name: "r", GeneratorType: "postgres", DefaultTTL: time.Hour, MaxTTL: MaxRoleTTL + time.Hour},
			wantCode: quicbaasErrors.ErrInvalidInput,
			wantErr:  isType(new(*api.MaxDurationValueError)),
		},
		{
			name:     "default TTL above max TTL",
			req:      &CreateRoleRequest{Name: "r", GeneratorType: "postgres", DefaultTTL: 3 * time.Hour, MaxTTL: 2 * time.Hour},
			wantCode: quicbaasErrors.ErrInvalidInput,
			wantErr:  isType(new(*api.DefaultTTLExceedsMaxError)),
		},
		{
			name:     "unregistered generator",
			req:      &CreateRoleRequest{Name: "r", GeneratorType: "nonexistent", DefaultTTL: time.Hour, MaxTTL: 2 * time.Hour},
			wantCode: quicbaasErrors.ErrNotFound,
			wantErr:  isType(new(*api.GeneratorNotFoundError)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := credentials.NewGeneratorRegistry()
			require.NoError(t, registry.Register(newMockDSGenerator("postgres")))
			svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
				Store: newDSStore(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: registry, Logger: newMockDSLogger(),
			})
			require.NoError(t, err)

			resp, err := svc.CreateRole(context.Background(), tt.req)

			assert.Nil(t, resp)
			var qbErr *quicbaasErrors.QBError
			require.ErrorAs(t, err, &qbErr)
			assert.Equal(t, tt.wantCode, qbErr.Code)
			assert.True(t, tt.wantErr(err), "unexpected error: %v", err)
		})
	}
}

func TestDynamicSecretsService_CreateRole_DuplicateRole(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	svc, _ := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
		Store: newDSStore(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: registry, Logger: newMockDSLogger(),
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

	svc, _ := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
		Store: newDSStore(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: registry, Logger: newMockDSLogger(),
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
	svc, _ := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
		Store: newDSStore(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: credentials.NewGeneratorRegistry(), Logger: newMockDSLogger(),
	})

	resp, err := svc.GetRole(context.Background(), &GetRoleRequest{Name: "nonexistent"})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "role not found")
}

func TestDynamicSecretsService_DeleteRole_Success(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	svc, _ := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
		Store: newDSStore(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: registry, Logger: newMockDSLogger(),
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

	svc, _ := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
		Store: newDSStore(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: registry, Logger: newMockDSLogger(),
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
	svc, _ := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
		Store: newDSStore(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: credentials.NewGeneratorRegistry(), Logger: newMockDSLogger(),
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

	svc, _ := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
		Store: newDSStore(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: registry, Logger: newMockDSLogger(),
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

	svc, _ := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
		Store: newDSStore(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: registry, Logger: newMockDSLogger(),
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

	svc, _ := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
		Store: newDSStore(), LeaseManager: newMockDSLeaseManager(), GeneratorRegistry: registry, Logger: newMockDSLogger(),
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

// The lease manager keeps a revoked lease's record as a tombstone for the
// retention window, and an expired lease stays stored as it was until the
// reaper's next pass. ListLeases is documented, on every surface that reaches
// it, as the ACTIVE leases — so neither may be listed (go-quicbaas#852).
func TestDynamicSecretsService_ListLeases_ExcludesExpiredAndRevoked(t *testing.T) {
	now := time.Now()
	revokedAt := now.Add(-time.Minute)
	active := &lease.Lease{LeaseID: "active", SecretPath: "db/a", IssuedAt: now, ExpiresAt: now.Add(time.Hour), TTL: time.Hour}
	expired := &lease.Lease{LeaseID: "expired", SecretPath: "db/b", IssuedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour), TTL: time.Hour}
	revoked := &lease.Lease{LeaseID: "revoked", SecretPath: "db/c", IssuedAt: now, ExpiresAt: now.Add(time.Hour), TTL: time.Hour, RevokedAt: &revokedAt}

	leaseManager := newMockDSLeaseManager()
	leaseManager.listLeasesFn = func(ctx context.Context, prefix string) ([]*lease.Lease, error) {
		return []*lease.Lease{expired, active, revoked}, nil
	}

	svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
		Store: newDSStore(), LeaseManager: leaseManager, GeneratorRegistry: credentials.NewGeneratorRegistry(), Logger: newMockDSLogger(),
	})
	require.NoError(t, err)

	resp, err := svc.ListLeases(context.Background(), &ListLeasesRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Leases, 1)
	assert.Equal(t, "active", resp.Leases[0].LeaseID)
	assert.Equal(t, 1, resp.Count)
}

func TestDynamicSecretsService_GenerateCredential_StoreCredentialError(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	store := newFaultDSStore().failWritesUnder(CredentialPrefix, &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   "faultDSStore.Put",
		Err:  stderrors.New("credential storage write failure"),
	})

	svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
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

	svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
		Store:             newDSStore(),
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

func TestDynamicSecretsService_GenerateCredential_TTLExceedsMax(t *testing.T) {
	store := newDSStore()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	generatorReg.Register(newMockDSGenerator("mock"))
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
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
	resp, err := svc.GenerateCredential(context.Background(), &GenerateCredentialRequest{
		RoleName: "test-role",
		TTL:      3 * time.Hour,
	})
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds max TTL")
	qbErr, ok := err.(*quicbaasErrors.QBError)
	require.True(t, ok)
	assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
}

func TestDynamicSecretsService_GenerateCredential_GeneratorFailure(t *testing.T) {
	store := newDSStore()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()

	// Create a generator that fails
	failGen := newMockDSGenerator("mock")
	failGen.generateFn = func(ctx context.Context, role *credentials.Role) (*credentials.Credential, error) {
		return nil, stderrors.New("generator error")
	}
	generatorReg.Register(failGen)

	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
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
	qbErr, ok := err.(*quicbaasErrors.QBError)
	require.True(t, ok)
	assert.Equal(t, quicbaasErrors.ErrInternal, qbErr.Code)
}

func TestDynamicSecretsService_GenerateCredential_LeaseFailure(t *testing.T) {
	store := newDSStore()
	leaseManager := newMockDSLeaseManager()
	leaseManager.createLeaseFn = func(ctx context.Context, path string, ttl time.Duration, opts *lease.LeaseOptions) (*lease.Lease, error) {
		return nil, stderrors.New("lease creation failed")
	}

	generatorReg := credentials.NewGeneratorRegistry()
	generatorReg.Register(newMockDSGenerator("mock"))
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
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
	qbErr, ok := err.(*quicbaasErrors.QBError)
	require.True(t, ok)
	assert.Equal(t, quicbaasErrors.ErrInternal, qbErr.Code)
}

func TestDynamicSecretsService_RenewLease_Failure(t *testing.T) {
	store := newDSStore()
	leaseManager := newMockDSLeaseManager()
	leaseManager.renewLeaseFn = func(ctx context.Context, leaseID string, increment time.Duration) (*lease.Lease, error) {
		return nil, stderrors.New("renew failed")
	}

	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
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
	store := newDSStore()
	leaseManager := newMockDSLeaseManager()
	leaseManager.revokeLeaseFn = func(ctx context.Context, leaseID string) error {
		return stderrors.New("revoke failed")
	}

	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
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

func TestDynamicSecretsService_ListLeases_Failure(t *testing.T) {
	store := newDSStore()
	leaseManager := newMockDSLeaseManager()
	leaseManager.listLeasesFn = func(ctx context.Context, prefix string) ([]*lease.Lease, error) {
		return nil, stderrors.New("list failed")
	}

	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
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
	store := newDSStore()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	generatorReg.Register(newMockDSGenerator("mock"))
	logger := newMockDSLogger()

	// Pre-populate with role data
	roleJSON := `{"name":"preloaded","generator_type":"mock","default_ttl":3600000000000,"max_ttl":7200000000000}`
	err := store.Put(context.Background(), RolePrefix+"preloaded", []byte(roleJSON))
	require.NoError(t, err)

	svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
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
	store := newDSStore()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	// Pre-populate with invalid JSON
	err := store.Put(context.Background(), RolePrefix+"invalid", []byte("not json"))
	require.NoError(t, err)

	svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
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

// TestDynamicSecretsService_DeleteRole_NotFound tests deleting non-existent role.
func TestDynamicSecretsService_DeleteRole_NotFound(t *testing.T) {
	store := newDSStore()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
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
	qbErr, ok := err.(*quicbaasErrors.QBError)
	require.True(t, ok)
	assert.Equal(t, quicbaasErrors.ErrNotFound, qbErr.Code)
}

// TestDynamicSecretsService_ListLeases_NilRequest tests nil request with default behavior.
func TestDynamicSecretsService_ListLeases_NilRequest(t *testing.T) {
	store := newDSStore()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
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

// TestDynamicSecretsService_GenerateCredential_GetGeneratorError tests when getting generator fails.
func TestDynamicSecretsService_GenerateCredential_GetGeneratorError(t *testing.T) {
	store := newDSStore()
	leaseManager := newMockDSLeaseManager()
	generatorReg := credentials.NewGeneratorRegistry()
	logger := newMockDSLogger()

	svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
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
	qbErr, ok := err.(*quicbaasErrors.QBError)
	require.True(t, ok)
	assert.Equal(t, quicbaasErrors.ErrInternal, qbErr.Code)
}

func TestDynamicSecretsService_ListRoles_WithPrefix(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
		Store:             newDSStore(),
		LeaseManager:      newMockDSLeaseManager(),
		GeneratorRegistry: registry,
		Logger:            newMockDSLogger(),
	})
	require.NoError(t, err)

	empty, err := svc.ListRoles(context.Background(), &ListRolesRequest{})
	require.NoError(t, err)
	assert.Equal(t, 0, empty.Count)

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

	all, err := svc.ListRoles(context.Background(), &ListRolesRequest{})
	require.NoError(t, err)
	assert.Equal(t, 3, all.Count)
}

func TestDynamicSecretsService_CreateRole_StorageError(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	store := newFaultDSStore().failAllWrites(stderrors.New("storage write error"))

	svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
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
	assert.Contains(t, err.Error(), "store role failed")
}

func TestDynamicSecretsService_GenerateCredential_WithCustomTTL(t *testing.T) {
	registry := credentials.NewGeneratorRegistry()
	gen := newMockDSGenerator("postgres")
	require.NoError(t, registry.Register(gen))

	svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
		Store:             newDSStore(),
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

func TestDynamicSecretsService_storeCredential_SetError(t *testing.T) {
	store := newFaultDSStore().failAllWrites(stderrors.New("storage write error"))
	svc := &dynamicSecretsHandler{
		store: store,
	}

	cred := &credentials.Credential{
		CredentialID: "test-cred-id",
		Type:         "database",
	}

	err := svc.storeCredential(context.Background(), cred)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store credential failed")
}

// =============================================================================
// REVOCATION REACHES THE THING THAT WAS LEASED
//
// RevokeLease used to mark the lease and stop. The database user, cloud role or
// whatever else the generator had created stayed live and usable forever, and
// credentials.CredentialGenerator.Revoke -- the SPI verb that exists precisely to
// undo a Generate -- had no caller anywhere in the product. These tests pin the
// property that makes revocation a control: the backing material goes too.
// =============================================================================

// newRealLeaseDynamicSecrets builds the handler over the PRODUCTION lease manager
// so revocation is tested against tombstones rather than against a double that
// deletes.
func newRealLeaseDynamicSecrets(t *testing.T) (*dynamicSecretsHandler, *mockDSGenerator, lease.LeaseManager) {
	t.Helper()
	store := newDSStore()
	log := newMockDSLogger()
	leaseMgr, err := lease.NewManager(&lease.ManagerConfig{Store: store, Logger: log})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, leaseMgr.Close()) })

	generator := newMockDSGenerator("postgres")
	registry := credentials.NewGeneratorRegistry()
	require.NoError(t, registry.Register(generator))

	svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
		Store: store, LeaseManager: leaseMgr, GeneratorRegistry: registry, Logger: log,
	})
	require.NoError(t, err)
	return svc, generator, leaseMgr
}

// seedLeasedCredential creates a role and generates one credential from it,
// returning the lease and the credential ID.
func seedLeasedCredential(t *testing.T, svc *dynamicSecretsHandler) (string, string) {
	t.Helper()
	ctx := context.Background()
	_, err := svc.CreateRole(ctx, &CreateCredentialRoleRequest{
		Name:          "app-db",
		GeneratorType: "postgres",
		Config:        map[string]string{"connection_url": "postgres://localhost/app"},
		DefaultTTL:    time.Hour,
		MaxTTL:        24 * time.Hour,
	})
	require.NoError(t, err)

	generated, err := svc.GenerateCredential(ctx, &GenerateCredentialRequest{RoleName: "app-db"})
	require.NoError(t, err)
	require.NotEmpty(t, generated.Credential.LeaseID)
	return generated.Credential.LeaseID, generated.Credential.CredentialID
}

// TestDynamicSecrets_RevokeLease_DestroysTheBackingCredential is the property the
// issue asks for, at the generated-credential layer: after revocation the login
// the generator created no longer exists, and neither does its record.
func TestDynamicSecrets_RevokeLease_DestroysTheBackingCredential(t *testing.T) {
	svc, generator, leaseMgr := newRealLeaseDynamicSecrets(t)
	ctx := context.Background()
	leaseID, credentialID := seedLeasedCredential(t, svc)

	// The record exists before revocation, so its absence afterwards is this
	// call's doing.
	held, err := svc.loadCredential(ctx, credentialID)
	require.NoError(t, err)
	require.NotNil(t, held)

	_, err = svc.RevokeLease(ctx, &RevokeLeaseRequest{LeaseID: leaseID})
	require.NoError(t, err)

	assert.Equal(t, []string{credentialID}, generator.revoked,
		"the generator was never asked to destroy the credential it created")

	gone, err := svc.loadCredential(ctx, credentialID)
	require.NoError(t, err)
	assert.Nil(t, gone, "the credential record outlived the login it describes")

	// And the lease is a tombstone, so an operator can still see what became of
	// it while Validate refuses it.
	_, err = leaseMgr.Validate(ctx, leaseID)
	assert.True(t, lease.IsLeaseRevoked(err), "Validate() error = %v, want a revoked refusal", err)
}

// TestDynamicSecrets_RevokeLease_KeepsTheLeaseWhenTheGeneratorRefuses is the
// ordering proof. Marking the lease revoked first would leave a live database
// login behind a record saying it is gone, and a revoked lease is terminal --
// nothing would ever revisit it. So a generator failure leaves the lease ACTIVE
// and the caller sees the refusal.
func TestDynamicSecrets_RevokeLease_KeepsTheLeaseWhenTheGeneratorRefuses(t *testing.T) {
	svc, generator, leaseMgr := newRealLeaseDynamicSecrets(t)
	ctx := context.Background()
	leaseID, credentialID := seedLeasedCredential(t, svc)

	generator.revokeErr = stderrors.New("database refused the DROP ROLE")

	_, err := svc.RevokeLease(ctx, &RevokeLeaseRequest{LeaseID: leaseID})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DROP ROLE")

	// Still live, in both places, so the retry below has something to converge
	// on.
	_, validateErr := leaseMgr.Validate(ctx, leaseID)
	require.NoError(t, validateErr, "the lease was revoked while its login was still standing")
	stillThere, err := svc.loadCredential(ctx, credentialID)
	require.NoError(t, err)
	require.NotNil(t, stillThere)

	// And the retry converges once the backend recovers.
	generator.revokeErr = nil
	_, err = svc.RevokeLease(ctx, &RevokeLeaseRequest{LeaseID: leaseID})
	require.NoError(t, err)
	_, validateErr = leaseMgr.Validate(ctx, leaseID)
	assert.True(t, lease.IsLeaseRevoked(validateErr))
}

// TestDynamicSecrets_RevokeLease_ToleratesEveryAbsence covers the leases that
// have no backing material, each for its own reason. None of them may fail a
// revocation: an account delivery grant has nothing to destroy, a credential
// already torn down is the desired end state, and a deployment that has dropped
// a generator plugin must still be able to revoke the leases it left behind.
func TestDynamicSecrets_RevokeLease_ToleratesEveryAbsence(t *testing.T) {
	ctx := context.Background()

	t.Run("lease carries no credential metadata", func(t *testing.T) {
		svc, generator, leaseMgr := newRealLeaseDynamicSecrets(t)
		// The shape of an account credential DELIVERY grant: a real lease with
		// identifiers only and no generated material behind it.
		held, err := leaseMgr.CreateLease(ctx, "accounts/delivery/t1/_none/_none/acct-1", time.Hour,
			&lease.LeaseOptions{Renewable: true, Metadata: map[string]string{"kind": "delivery"}})
		require.NoError(t, err)

		_, err = svc.RevokeLease(ctx, &RevokeLeaseRequest{LeaseID: held.LeaseID})
		require.NoError(t, err)
		assert.Empty(t, generator.revoked)
	})

	t.Run("credential record already gone", func(t *testing.T) {
		svc, generator, _ := newRealLeaseDynamicSecrets(t)
		leaseID, credentialID := seedLeasedCredential(t, svc)
		require.NoError(t, svc.store.Delete(ctx, CredentialPrefix+credentialID))

		_, err := svc.RevokeLease(ctx, &RevokeLeaseRequest{LeaseID: leaseID})
		require.NoError(t, err)
		assert.Empty(t, generator.revoked, "a credential that is gone was asked to be destroyed again")
	})

	t.Run("generator no longer registered", func(t *testing.T) {
		svc, _, leaseMgr := newRealLeaseDynamicSecrets(t)
		leaseID, credentialID := seedLeasedCredential(t, svc)
		require.NoError(t, svc.generatorReg.Unregister("postgres"))

		_, err := svc.RevokeLease(ctx, &RevokeLeaseRequest{LeaseID: leaseID})
		require.NoError(t, err, "a lease became unrevocable because a plugin was dropped")
		_, validateErr := leaseMgr.Validate(ctx, leaseID)
		assert.True(t, lease.IsLeaseRevoked(validateErr))
		// The record is deliberately LEFT: nothing destroyed the login, so
		// deleting the only description of it would strand it invisibly.
		orphan, err := svc.loadCredential(ctx, credentialID)
		require.NoError(t, err)
		assert.NotNil(t, orphan)
	})

	t.Run("lease does not exist", func(t *testing.T) {
		svc, _, _ := newRealLeaseDynamicSecrets(t)
		// The lookup finds nothing; RevokeLease reports its own answer for an
		// absent lease rather than failing inside the credential teardown.
		require.NoError(t, svc.destroyLeasedCredential(ctx, "lease_never_existed"))
	})
}

// TestDynamicSecrets_LoadCredential_BadInput proves the reader distinguishes
// absent from unreadable: an absent record is nil with no error, an undecodable
// one is a refusal, because silently treating corruption as absence would skip
// the generator teardown and strand a live login.
func TestDynamicSecrets_LoadCredential_BadInput(t *testing.T) {
	svc, _, _ := newRealLeaseDynamicSecrets(t)
	ctx := context.Background()

	missing, err := svc.loadCredential(ctx, "cred-nope")
	require.NoError(t, err)
	assert.Nil(t, missing)

	require.NoError(t, svc.store.Put(ctx, CredentialPrefix+"cred-bad", []byte("{not json")))
	_, err = svc.loadCredential(ctx, "cred-bad")
	require.Error(t, err)

	require.NoError(t, svc.store.Put(ctx, CredentialPrefix+"cred-empty", []byte{}))
	empty, err := svc.loadCredential(ctx, "cred-empty")
	require.NoError(t, err)
	assert.Nil(t, empty)
}

// TestDynamicSecrets_RevokeLease_StoreFaults proves the two store failures the
// teardown can hit are REFUSALS rather than silent skips. A read that cannot
// answer must not be mistaken for "no such credential" — that would skip the
// generator teardown and strand a live login — and a record that cannot be
// deleted leaves an inventory the operator must be told about.
func TestDynamicSecrets_RevokeLease_StoreFaults(t *testing.T) {
	ctx := context.Background()

	newOnFaultStore := func(t *testing.T) (*dynamicSecretsHandler, *mockDSGenerator, *faultDSStore) {
		t.Helper()
		store := newFaultDSStore()
		log := newMockDSLogger()
		leaseMgr, err := lease.NewManager(&lease.ManagerConfig{Store: store, Logger: log})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, leaseMgr.Close()) })

		generator := newMockDSGenerator("postgres")
		registry := credentials.NewGeneratorRegistry()
		require.NoError(t, registry.Register(generator))

		svc, err := NewDynamicSecretsHandler(ctx, &DynamicSecretsConfig{
			Store: store, LeaseManager: leaseMgr, GeneratorRegistry: registry, Logger: log,
		})
		require.NoError(t, err)
		return svc, generator, store
	}

	t.Run("the credential record cannot be read", func(t *testing.T) {
		svc, generator, store := newOnFaultStore(t)
		leaseID, _ := seedLeasedCredential(t, svc)

		// Armed only now: the lease lookup and the credential read share this
		// store, so the refusal lands on whichever the teardown reaches first.
		store.getErr = stderrors.New("kv read unavailable")

		_, err := svc.RevokeLease(ctx, &RevokeLeaseRequest{LeaseID: leaseID})
		require.Error(t, err)
		assert.Empty(t, generator.revoked)
	})

	t.Run("the credential record cannot be deleted", func(t *testing.T) {
		svc, generator, store := newOnFaultStore(t)
		leaseID, credentialID := seedLeasedCredential(t, svc)
		store.deleteErr = stderrors.New("kv delete unavailable")

		_, err := svc.RevokeLease(ctx, &RevokeLeaseRequest{LeaseID: leaseID})
		require.Error(t, err)
		// The login IS gone — the generator succeeded — so the retry has only
		// the record left to clean up, and the lease is still active for it to
		// find.
		assert.Equal(t, []string{credentialID}, generator.revoked)
	})
}
