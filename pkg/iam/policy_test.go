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

package iam

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// mockPolicyStorage implements PolicyStorage for testing.
type mockPolicyStorage struct {
	mu          sync.RWMutex
	policies    map[string]map[string]*Policy // tenantID -> policyName -> Policy
	createError error
	getError    error
	updateError error
	deleteError error
	listError   error
}

func newMockPolicyStorage() *mockPolicyStorage {
	return &mockPolicyStorage{
		policies: make(map[string]map[string]*Policy),
	}
}

// newTestPolicyCache creates a PolicyCache for testing.
func newTestPolicyCache() *PolicyCache {
	return NewPolicyCache(&CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   10000,
		BloomFilterHashes: 3,
	})
}

func (m *mockPolicyStorage) CreatePolicy(ctx context.Context, tenantID string, policy *Policy) error {
	if m.createError != nil {
		return m.createError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.policies[tenantID] == nil {
		m.policies[tenantID] = make(map[string]*Policy)
	}

	// Deep copy to avoid mutation
	p := *policy
	m.policies[tenantID][policy.Metadata.Name] = &p

	return nil
}

func (m *mockPolicyStorage) GetPolicy(ctx context.Context, tenantID, name string) (*Policy, error) {
	if m.getError != nil {
		return nil, m.getError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	tenantPolicies, exists := m.policies[tenantID]
	if !exists {
		return nil, nil
	}

	policy, exists := tenantPolicies[name]
	if !exists {
		return nil, nil
	}

	// Return a copy
	p := *policy
	return &p, nil
}

func (m *mockPolicyStorage) UpdatePolicy(ctx context.Context, tenantID string, policy *Policy) error {
	if m.updateError != nil {
		return m.updateError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.policies[tenantID] == nil {
		return &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "iam.policy",
			Err:  ErrPolicyNotFound.Err,
		}
	}

	// Deep copy
	p := *policy
	m.policies[tenantID][policy.Metadata.Name] = &p

	return nil
}

func (m *mockPolicyStorage) DeletePolicy(ctx context.Context, tenantID, name string) error {
	if m.deleteError != nil {
		return m.deleteError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.policies[tenantID] == nil {
		return nil
	}

	delete(m.policies[tenantID], name)
	return nil
}

func (m *mockPolicyStorage) ListPolicies(ctx context.Context, tenantID string, opts ListOptions) ([]*Policy, error) {
	if m.listError != nil {
		return nil, m.listError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	tenantPolicies, exists := m.policies[tenantID]
	if !exists {
		return []*Policy{}, nil
	}

	policies := make([]*Policy, 0, len(tenantPolicies))
	for _, policy := range tenantPolicies {
		p := *policy
		policies = append(policies, &p)
	}

	return policies, nil
}

func createTestPolicy(tenantID, name string) *Policy {
	return &Policy{
		APIVersion: "iam.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata: PolicyMetadata{
			Name:        name,
			Tenant:      tenantID,
			Description: "Test policy",
			Priority:    100,
		},
		Spec: PolicySpec{
			Subjects: SubjectSelector{
				Authenticated: true,
			},
			Rules: []Rule{
				{
					Name:      "allow-read",
					Effect:    EffectAllow,
					Priority:  100,
					Actions:   []string{"read"},
					Resources: []string{"/test/*"},
				},
			},
		},
	}
}

func TestNewPolicyEngine(t *testing.T) {
	tests := []struct {
		name string
		opts PolicyEngineOptions
	}{
		{
			name: "with all options",
			opts: PolicyEngineOptions{
				Storage:      newMockPolicyStorage(),
				Cache:        newTestPolicyCache(),
				CacheTTL:     5 * time.Minute,
				MaxCacheSize: 1000,
			},
		},
		{
			name: "minimal options",
			opts: PolicyEngineOptions{
				Storage: newMockPolicyStorage(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewPolicyEngine(tt.opts)

			if engine == nil {
				t.Fatal("NewPolicyEngine returned nil")
			}
			if engine.storage == nil {
				t.Error("engine.storage is nil")
			}
		})
	}
}

func TestPolicyEngine_CreatePolicy(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  string
		policy    *Policy
		setup     func(*mockPolicyStorage)
		wantError bool
		errorType interface{}
	}{
		{
			name:      "success",
			tenantID:  "tenant1",
			policy:    createTestPolicy("tenant1", "policy1"),
			setup:     func(s *mockPolicyStorage) {},
			wantError: false,
		},
		{
			name:      "nil policy",
			tenantID:  "tenant1",
			policy:    nil,
			setup:     func(s *mockPolicyStorage) {},
			wantError: true,
			errorType: &InvalidPolicyError{},
		},
		{
			name:      "policy tenant mismatch",
			tenantID:  "tenant1",
			policy:    createTestPolicy("tenant2", "policy1"),
			setup:     func(s *mockPolicyStorage) {},
			wantError: true,
			errorType: &InvalidPolicyError{},
		},
		{
			name:     "policy already exists",
			tenantID: "tenant1",
			policy:   createTestPolicy("tenant1", "policy1"),
			setup: func(s *mockPolicyStorage) {
				ctx := context.Background()
				_ = s.CreatePolicy(ctx, "tenant1", createTestPolicy("tenant1", "policy1"))
			},
			wantError: true,
			errorType: &PolicyExistsError{},
		},
		{
			name:     "storage error",
			tenantID: "tenant1",
			policy:   createTestPolicy("tenant1", "policy1"),
			setup: func(s *mockPolicyStorage) {
				s.createError = &dberrors.MessageError{Message: "storage error"}
			},
			wantError: true,
		},
		{
			name:     "invalid policy - no apiVersion",
			tenantID: "tenant1",
			policy: &Policy{
				Kind: "Policy",
				Metadata: PolicyMetadata{
					Name:   "policy1",
					Tenant: "tenant1",
				},
				Spec: PolicySpec{
					Rules: []Rule{
						{
							Name:      "rule1",
							Effect:    EffectAllow,
							Actions:   []string{"read"},
							Resources: []string{"/test/*"},
						},
					},
				},
			},
			setup:     func(s *mockPolicyStorage) {},
			wantError: true,
		},
		{
			name:     "policy with empty tenant - sets from parameter",
			tenantID: "tenant1",
			policy: &Policy{
				APIVersion: "iam.quicbaas.io/v1",
				Kind:       "Policy",
				Metadata: PolicyMetadata{
					Name:   "policy1",
					Tenant: "", // Empty tenant
				},
				Spec: PolicySpec{
					Subjects: SubjectSelector{
						Authenticated: true,
					},
					Rules: []Rule{
						{
							Name:      "allow-read",
							Effect:    EffectAllow,
							Actions:   []string{"read"},
							Resources: []string{"/test/*"},
						},
					},
				},
			},
			setup:     func(s *mockPolicyStorage) {},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := newMockPolicyStorage()
			tt.setup(storage)

			cache := newTestPolicyCache()
			engine := NewPolicyEngine(PolicyEngineOptions{
				Storage: storage,
				Cache:   cache,
			})

			ctx := context.Background()
			err := engine.CreatePolicy(ctx, tt.tenantID, tt.policy)

			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				if tt.errorType != nil {
					switch tt.errorType.(type) {
					case *InvalidPolicyError:
						if _, ok := err.(*InvalidPolicyError); !ok {
							t.Errorf("expected ErrInvalidPolicy, got %T: %v", err, err)
						}
					case *PolicyExistsError:
						if _, ok := err.(*PolicyExistsError); !ok {
							t.Errorf("expected ErrPolicyExists, got %T: %v", err, err)
						}
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}

				// Verify policy was created
				if tt.policy != nil {
					created, _ := storage.GetPolicy(ctx, tt.tenantID, tt.policy.Metadata.Name)
					if created == nil {
						t.Error("policy was not created in storage")
					}

					// If tenant was empty, verify it was set
					if tt.policy.Metadata.Tenant == "" {
						if created.Metadata.Tenant != tt.tenantID {
							t.Errorf("expected tenant to be set to %s, got %s", tt.tenantID, created.Metadata.Tenant)
						}
					}
				}
			}
		})
	}
}

func TestPolicyEngine_GetPolicy(t *testing.T) {
	tests := []struct {
		name       string
		tenantID   string
		policyName string
		setup      func(*mockPolicyStorage)
		wantError  bool
		errorType  interface{}
	}{
		{
			name:       "success",
			tenantID:   "tenant1",
			policyName: "policy1",
			setup: func(s *mockPolicyStorage) {
				ctx := context.Background()
				_ = s.CreatePolicy(ctx, "tenant1", createTestPolicy("tenant1", "policy1"))
			},
			wantError: false,
		},
		{
			name:       "not found",
			tenantID:   "tenant1",
			policyName: "nonexistent",
			setup:      func(s *mockPolicyStorage) {},
			wantError:  true,
			errorType:  &PolicyNotFoundTypeError{},
		},
		{
			name:       "storage error",
			tenantID:   "tenant1",
			policyName: "policy1",
			setup: func(s *mockPolicyStorage) {
				s.getError = &dberrors.MessageError{Message: "storage error"}
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := newMockPolicyStorage()
			tt.setup(storage)

			engine := NewPolicyEngine(PolicyEngineOptions{
				Storage: storage,
			})

			ctx := context.Background()
			policy, err := engine.GetPolicy(ctx, tt.tenantID, tt.policyName)

			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				if tt.errorType != nil {
					if _, ok := tt.errorType.(*PolicyNotFoundTypeError); ok {
						if _, ok := err.(*PolicyNotFoundTypeError); !ok {
							t.Errorf("expected ErrPolicyNotFoundType, got %T: %v", err, err)
						}
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if policy == nil {
					t.Error("expected policy, got nil")
				}
				if policy.Metadata.Name != tt.policyName {
					t.Errorf("expected policy name %s, got %s", tt.policyName, policy.Metadata.Name)
				}
			}
		})
	}
}

func TestPolicyEngine_UpdatePolicy(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  string
		policy    *Policy
		setup     func(*mockPolicyStorage)
		wantError bool
		errorType interface{}
	}{
		{
			name:     "success",
			tenantID: "tenant1",
			policy:   createTestPolicy("tenant1", "policy1"),
			setup: func(s *mockPolicyStorage) {
				ctx := context.Background()
				_ = s.CreatePolicy(ctx, "tenant1", createTestPolicy("tenant1", "policy1"))
			},
			wantError: false,
		},
		{
			name:      "nil policy",
			tenantID:  "tenant1",
			policy:    nil,
			setup:     func(s *mockPolicyStorage) {},
			wantError: true,
			errorType: &InvalidPolicyError{},
		},
		{
			name:      "tenant mismatch",
			tenantID:  "tenant1",
			policy:    createTestPolicy("tenant2", "policy1"),
			setup:     func(s *mockPolicyStorage) {},
			wantError: true,
			errorType: &InvalidPolicyError{},
		},
		{
			name:      "not found",
			tenantID:  "tenant1",
			policy:    createTestPolicy("tenant1", "nonexistent"),
			setup:     func(s *mockPolicyStorage) {},
			wantError: true,
			errorType: &PolicyNotFoundTypeError{},
		},
		{
			name:     "storage error",
			tenantID: "tenant1",
			policy:   createTestPolicy("tenant1", "policy1"),
			setup: func(s *mockPolicyStorage) {
				ctx := context.Background()
				_ = s.CreatePolicy(ctx, "tenant1", createTestPolicy("tenant1", "policy1"))
				s.updateError = &dberrors.MessageError{Message: "storage error"}
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := newMockPolicyStorage()
			tt.setup(storage)

			cache := newTestPolicyCache()
			engine := NewPolicyEngine(PolicyEngineOptions{
				Storage: storage,
				Cache:   cache,
			})

			ctx := context.Background()
			err := engine.UpdatePolicy(ctx, tt.tenantID, tt.policy)

			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				if tt.errorType != nil {
					switch tt.errorType.(type) {
					case *InvalidPolicyError:
						if _, ok := err.(*InvalidPolicyError); !ok {
							t.Errorf("expected ErrInvalidPolicy, got %T: %v", err, err)
						}
					case *PolicyNotFoundTypeError:
						if _, ok := err.(*PolicyNotFoundTypeError); !ok {
							t.Errorf("expected ErrPolicyNotFoundType, got %T: %v", err, err)
						}
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestPolicyEngine_DeletePolicy(t *testing.T) {
	tests := []struct {
		name       string
		tenantID   string
		policyName string
		setup      func(*mockPolicyStorage)
		wantError  bool
		errorType  interface{}
	}{
		{
			name:       "success",
			tenantID:   "tenant1",
			policyName: "policy1",
			setup: func(s *mockPolicyStorage) {
				ctx := context.Background()
				_ = s.CreatePolicy(ctx, "tenant1", createTestPolicy("tenant1", "policy1"))
			},
			wantError: false,
		},
		{
			name:       "not found",
			tenantID:   "tenant1",
			policyName: "nonexistent",
			setup:      func(s *mockPolicyStorage) {},
			wantError:  true,
			errorType:  &PolicyNotFoundTypeError{},
		},
		{
			name:       "storage error",
			tenantID:   "tenant1",
			policyName: "policy1",
			setup: func(s *mockPolicyStorage) {
				ctx := context.Background()
				_ = s.CreatePolicy(ctx, "tenant1", createTestPolicy("tenant1", "policy1"))
				s.deleteError = &dberrors.MessageError{Message: "storage error"}
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := newMockPolicyStorage()
			tt.setup(storage)

			cache := newTestPolicyCache()
			engine := NewPolicyEngine(PolicyEngineOptions{
				Storage: storage,
				Cache:   cache,
			})

			ctx := context.Background()
			err := engine.DeletePolicy(ctx, tt.tenantID, tt.policyName)

			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				if tt.errorType != nil {
					if _, ok := tt.errorType.(*PolicyNotFoundTypeError); ok {
						if _, ok := err.(*PolicyNotFoundTypeError); !ok {
							t.Errorf("expected ErrPolicyNotFoundType, got %T: %v", err, err)
						}
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}

				// Verify policy was deleted
				policy, _ := storage.GetPolicy(ctx, tt.tenantID, tt.policyName)
				if policy != nil {
					t.Error("policy was not deleted from storage")
				}
			}
		})
	}
}

// snapshotPolicyStorage is a mockPolicyStorage that implements
// PolicyWriteSnapshotter and counts its snapshots and restores.
type snapshotPolicyStorage struct {
	*mockPolicyStorage
	snapshotErr error
	snapshots   int
	restores    int
}

func (s *snapshotPolicyStorage) SnapshotPolicy(_ context.Context, _, _ string) (func(context.Context) error, error) {
	if s.snapshotErr != nil {
		return nil, s.snapshotErr
	}
	s.snapshots++
	return func(context.Context) error {
		s.restores++
		return nil
	}, nil
}

// snapshotTestABACSync is an ABACSync that returns err.
type snapshotTestABACSync struct{ err error }

func (s snapshotTestABACSync) Sync(context.Context) error { return s.err }

func TestPolicyEngine_WriteSnapshotter(t *testing.T) {
	ctx := context.Background()
	syncErr := errors.New("sync failed")
	writeErr := errors.New("write failed")
	snapErr := errors.New("snapshot failed")

	setup := func(t *testing.T, syncResult error) (*snapshotPolicyStorage, *PolicyEngine) {
		t.Helper()
		storage := &snapshotPolicyStorage{mockPolicyStorage: newMockPolicyStorage()}
		require.NoError(t, storage.CreatePolicy(ctx, "tenant1", createTestPolicy("tenant1", "p")))
		engine := NewPolicyEngine(PolicyEngineOptions{Storage: storage, ABACSync: snapshotTestABACSync{err: syncResult}})
		return storage, engine
	}
	update := func(e *PolicyEngine) error {
		return e.UpdatePolicy(ctx, "tenant1", createTestPolicy("tenant1", "p"))
	}
	del := func(e *PolicyEngine) error { return e.DeletePolicy(ctx, "tenant1", "p") }

	for name, write := range map[string]func(*PolicyEngine) error{"update": update, "delete": del} {
		t.Run(name+" sync failure restores once", func(t *testing.T) {
			storage, engine := setup(t, syncErr)
			err := write(engine)
			assert.ErrorIs(t, err, syncErr)
			assert.Equal(t, 1, storage.snapshots)
			assert.Equal(t, 1, storage.restores)
		})
		t.Run(name+" storage write error restores once", func(t *testing.T) {
			storage, engine := setup(t, nil)
			storage.updateError = writeErr
			storage.deleteError = writeErr
			err := write(engine)
			assert.ErrorIs(t, err, writeErr)
			assert.Equal(t, 1, storage.restores)
		})
		t.Run(name+" snapshot error writes nothing", func(t *testing.T) {
			storage, engine := setup(t, nil)
			storage.snapshotErr = snapErr
			err := write(engine)
			assert.ErrorIs(t, err, snapErr)
			assert.Equal(t, 0, storage.restores)
			got, err := storage.GetPolicy(ctx, "tenant1", "p")
			require.NoError(t, err)
			assert.NotNil(t, got)
		})
	}
}

// writeCountingPolicyStorage counts the creates and deletes that reach storage.
type writeCountingPolicyStorage struct {
	*snapshotPolicyStorage
	creates int
	deletes int
}

func (s *writeCountingPolicyStorage) CreatePolicy(ctx context.Context, tenantID string, policy *Policy) error {
	s.creates++
	return s.mockPolicyStorage.CreatePolicy(ctx, tenantID, policy)
}

func (s *writeCountingPolicyStorage) DeletePolicy(ctx context.Context, tenantID, name string) error {
	s.deletes++
	return s.mockPolicyStorage.DeletePolicy(ctx, tenantID, name)
}

func TestPolicyEngine_CreateRefusesUndecodableExisting(t *testing.T) {
	storage := &writeCountingPolicyStorage{snapshotPolicyStorage: &snapshotPolicyStorage{mockPolicyStorage: newMockPolicyStorage()}}
	storage.getError = &StoredPolicyDecodeError{TenantID: "tenant1", Name: "p", Reason: "bad json"}
	engine := NewPolicyEngine(PolicyEngineOptions{Storage: storage})

	err := engine.CreatePolicy(context.Background(), "tenant1", createTestPolicy("tenant1", "p"))
	var exists *PolicyExistsError
	require.ErrorAs(t, err, &exists)
	assert.Equal(t, 0, storage.creates)
	assert.Equal(t, 0, storage.snapshots)
}

func TestPolicyEngine_CreatePropagatesLookupError(t *testing.T) {
	lookupErr := errors.New("catalog read failed")
	storage := &writeCountingPolicyStorage{snapshotPolicyStorage: &snapshotPolicyStorage{mockPolicyStorage: newMockPolicyStorage()}}
	storage.getError = lookupErr
	engine := NewPolicyEngine(PolicyEngineOptions{Storage: storage})

	err := engine.CreatePolicy(context.Background(), "tenant1", createTestPolicy("tenant1", "p"))
	assert.ErrorIs(t, err, lookupErr)
	assert.Equal(t, 0, storage.creates)
	assert.Equal(t, 0, storage.deletes)
	assert.Equal(t, 0, storage.snapshots)
}

func TestPolicyEngine_RejectedCreateRestoresSnapshot(t *testing.T) {
	syncErr := errors.New("sync failed")
	storage := &writeCountingPolicyStorage{snapshotPolicyStorage: &snapshotPolicyStorage{mockPolicyStorage: newMockPolicyStorage()}}
	engine := NewPolicyEngine(PolicyEngineOptions{Storage: storage, ABACSync: snapshotTestABACSync{err: syncErr}})

	err := engine.CreatePolicy(context.Background(), "tenant1", createTestPolicy("tenant1", "p"))
	assert.ErrorIs(t, err, syncErr)
	assert.Equal(t, 1, storage.creates)
	assert.Equal(t, 1, storage.snapshots)
	assert.Equal(t, 1, storage.restores)
	assert.Equal(t, 0, storage.deletes, "a rejected create restores the snapshot, never deletes")
}

func TestPolicyEngine_ListPolicies(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  string
		opts      ListOptions
		setup     func(*mockPolicyStorage)
		wantCount int
		wantError bool
	}{
		{
			name:     "success with policies",
			tenantID: "tenant1",
			opts:     ListOptions{},
			setup: func(s *mockPolicyStorage) {
				ctx := context.Background()
				_ = s.CreatePolicy(ctx, "tenant1", createTestPolicy("tenant1", "policy1"))
				_ = s.CreatePolicy(ctx, "tenant1", createTestPolicy("tenant1", "policy2"))
			},
			wantCount: 2,
			wantError: false,
		},
		{
			name:      "empty list",
			tenantID:  "tenant1",
			opts:      ListOptions{},
			setup:     func(s *mockPolicyStorage) {},
			wantCount: 0,
			wantError: false,
		},
		{
			name:     "storage error",
			tenantID: "tenant1",
			opts:     ListOptions{},
			setup: func(s *mockPolicyStorage) {
				s.listError = &dberrors.MessageError{Message: "storage error"}
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := newMockPolicyStorage()
			tt.setup(storage)

			engine := NewPolicyEngine(PolicyEngineOptions{
				Storage: storage,
			})

			ctx := context.Background()
			policies, err := engine.ListPolicies(ctx, tt.tenantID, tt.opts)

			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if len(policies) != tt.wantCount {
					t.Errorf("expected %d policies, got %d", tt.wantCount, len(policies))
				}
			}
		})
	}
}

func TestPolicyEngine_ValidatePolicy(t *testing.T) {
	tests := []struct {
		name      string
		policy    *Policy
		wantError bool
	}{
		{
			name:      "nil policy",
			policy:    nil,
			wantError: true,
		},
		{
			name:      "valid policy",
			policy:    createTestPolicy("tenant1", "policy1"),
			wantError: false,
		},
		{
			name: "invalid policy - no rules",
			policy: &Policy{
				APIVersion: "iam.quicbaas.io/v1",
				Kind:       "Policy",
				Metadata: PolicyMetadata{
					Name:   "policy1",
					Tenant: "tenant1",
				},
				Spec: PolicySpec{
					Rules: []Rule{},
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewPolicyEngine(PolicyEngineOptions{
				Storage: newMockPolicyStorage(),
			})

			err := engine.ValidatePolicy(tt.policy)

			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestPolicyEngine_TestPolicy(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  string
		req       *CheckRequest
		setup     func(*PolicyCache)
		wantError bool
	}{
		{
			name:     "success with policies",
			tenantID: "tenant1",
			req: &CheckRequest{
				Subject: Subject{
					ID:            "user1",
					TenantID:      "tenant1",
					Authenticated: true,
				},
				Action:   "read",
				Resource: "/test/resource",
			},
			setup: func(c *PolicyCache) {
				c.Set("tenant1", createTestPolicy("tenant1", "policy1"))
			},
			wantError: false,
		},
		{
			name:     "success without policies",
			tenantID: "tenant1",
			req: &CheckRequest{
				Subject: Subject{
					ID:            "user1",
					TenantID:      "tenant1",
					Authenticated: true,
				},
				Action:   "read",
				Resource: "/test/resource",
			},
			setup:     func(c *PolicyCache) {},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewPolicyCache(&CacheConfig{
				TTL:               5 * time.Minute,
				MaxSize:           1000,
				BloomFilterSize:   1000000,
				BloomFilterHashes: 7,
			})
			tt.setup(cache)

			engine := NewPolicyEngine(PolicyEngineOptions{
				Storage: newMockPolicyStorage(),
				Cache:   cache,
			})

			ctx := context.Background()
			resp, err := engine.TestPolicy(ctx, tt.tenantID, tt.req)

			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if resp == nil {
					t.Error("expected response, got nil")
				}
			}
		})
	}
}

func TestPolicyEngine_Evaluate(t *testing.T) {
	tests := []struct {
		name      string
		req       *CheckRequest
		setup     func(*PolicyCache)
		wantError bool
	}{
		{
			name: "success",
			req: &CheckRequest{
				Subject: Subject{
					ID:            "user1",
					TenantID:      "tenant1",
					Authenticated: true,
				},
				Action:   "read",
				Resource: "/test/resource",
			},
			setup: func(c *PolicyCache) {
				c.Set("tenant1", createTestPolicy("tenant1", "policy1"))
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewPolicyCache(&CacheConfig{
				TTL:               5 * time.Minute,
				MaxSize:           1000,
				BloomFilterSize:   1000000,
				BloomFilterHashes: 7,
			})
			tt.setup(cache)

			engine := NewPolicyEngine(PolicyEngineOptions{
				Storage: newMockPolicyStorage(),
				Cache:   cache,
			})

			ctx := context.Background()
			resp, err := engine.Evaluate(ctx, tt.req)

			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if resp == nil {
					t.Error("expected response, got nil")
				}
			}
		})
	}
}

func TestPolicyEngine_ClearCache(t *testing.T) {
	cache := NewPolicyCache(&CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   1000000,
		BloomFilterHashes: 7,
	})

	// Add a policy to cache
	cache.Set("tenant1", createTestPolicy("tenant1", "policy1"))

	engine := NewPolicyEngine(PolicyEngineOptions{
		Storage: newMockPolicyStorage(),
		Cache:   cache,
	})

	ctx := context.Background()
	err := engine.ClearCache(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify cache was cleared
	policies := cache.GetAll("tenant1")
	if len(policies) != 0 {
		t.Errorf("expected 0 cached policies, got %d", len(policies))
	}
}

func TestMemoryPolicyCache_NewMemoryPolicyCache(t *testing.T) {
	tests := []struct {
		name string
		ttl  time.Duration
	}{
		{
			name: "with TTL",
			ttl:  5 * time.Minute,
		},
		{
			name: "zero TTL",
			ttl:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewMemoryPolicyCache(tt.ttl)

			if cache == nil {
				t.Fatal("NewMemoryPolicyCache returned nil")
			}
			if cache.cache == nil {
				t.Error("cache.cache is nil")
			}
			if cache.expires == nil {
				t.Error("cache.expires is nil")
			}
			if cache.ttl != tt.ttl {
				t.Errorf("expected TTL %v, got %v", tt.ttl, cache.ttl)
			}
		})
	}
}

func TestMemoryPolicyCache_Get(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*MemoryPolicyCache)
		tenantID  string
		wantFound bool
		wantCount int
	}{
		{
			name: "not found",
			setup: func(c *MemoryPolicyCache) {
			},
			tenantID:  "tenant1",
			wantFound: false,
			wantCount: 0,
		},
		{
			name: "found",
			setup: func(c *MemoryPolicyCache) {
				ctx := context.Background()
				compiled := createTestPolicy("tenant1", "policy1")
				_ = c.Set(ctx, "tenant1", []*Policy{compiled})
			},
			tenantID:  "tenant1",
			wantFound: true,
			wantCount: 1,
		},
		{
			name: "expired",
			setup: func(c *MemoryPolicyCache) {
				ctx := context.Background()
				compiled := createTestPolicy("tenant1", "policy1")
				_ = c.Set(ctx, "tenant1", []*Policy{compiled})
				// Set expiration to past
				c.expires["tenant1"] = time.Now().Add(-1 * time.Minute)
			},
			tenantID:  "tenant1",
			wantFound: false,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewMemoryPolicyCache(5 * time.Minute)
			tt.setup(cache)

			ctx := context.Background()
			policies, err := cache.Get(ctx, tt.tenantID)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if tt.wantFound {
				if policies == nil {
					t.Error("expected policies, got nil")
				}
				if len(policies) != tt.wantCount {
					t.Errorf("expected %d policies, got %d", tt.wantCount, len(policies))
				}
			} else {
				if policies != nil && len(policies) > 0 {
					t.Errorf("expected no policies, got %d", len(policies))
				}
			}
		})
	}
}

func TestMemoryPolicyCache_Set(t *testing.T) {
	tests := []struct {
		name      string
		ttl       time.Duration
		tenantID  string
		policies  []*Policy
		wantError bool
	}{
		{
			name:     "store with TTL",
			ttl:      5 * time.Minute,
			tenantID: "tenant1",
			policies: func() []*Policy {
				compiled := createTestPolicy("tenant1", "policy1")
				return []*Policy{compiled}
			}(),
			wantError: false,
		},
		{
			name:     "store without TTL",
			ttl:      0,
			tenantID: "tenant1",
			policies: func() []*Policy {
				compiled := createTestPolicy("tenant1", "policy1")
				return []*Policy{compiled}
			}(),
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewMemoryPolicyCache(tt.ttl)

			ctx := context.Background()
			err := cache.Set(ctx, tt.tenantID, tt.policies)

			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}

				// Verify policies were stored
				policies, _ := cache.Get(ctx, tt.tenantID)
				if len(policies) != len(tt.policies) {
					t.Errorf("expected %d policies, got %d", len(tt.policies), len(policies))
				}

				// Verify expiration was set if TTL > 0
				if tt.ttl > 0 {
					if _, ok := cache.expires[tt.tenantID]; !ok {
						t.Error("expiration was not set")
					}
				}
			}
		})
	}
}

func TestMemoryPolicyCache_Invalidate(t *testing.T) {
	cache := NewMemoryPolicyCache(5 * time.Minute)
	ctx := context.Background()

	// Add a policy
	compiled := createTestPolicy("tenant1", "policy1")
	_ = cache.Set(ctx, "tenant1", []*Policy{compiled})

	// Verify it was added
	policies, _ := cache.Get(ctx, "tenant1")
	if len(policies) != 1 {
		t.Fatalf("expected 1 policy before invalidation, got %d", len(policies))
	}

	// Invalidate
	err := cache.Invalidate(ctx, "tenant1")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify it was removed
	policies, _ = cache.Get(ctx, "tenant1")
	if policies != nil && len(policies) > 0 {
		t.Errorf("expected no policies after invalidation, got %d", len(policies))
	}

	// Verify expiration was removed
	if _, ok := cache.expires["tenant1"]; ok {
		t.Error("expiration was not removed")
	}
}

func TestMemoryPolicyCache_Clear(t *testing.T) {
	cache := NewMemoryPolicyCache(5 * time.Minute)
	ctx := context.Background()

	// Add policies for multiple tenants
	compiled1 := createTestPolicy("tenant1", "policy1")
	_ = cache.Set(ctx, "tenant1", []*Policy{compiled1})

	compiled2 := createTestPolicy("tenant2", "policy2")
	_ = cache.Set(ctx, "tenant2", []*Policy{compiled2})

	// Verify they were added
	policies1, _ := cache.Get(ctx, "tenant1")
	if len(policies1) != 1 {
		t.Fatalf("expected 1 policy for tenant1, got %d", len(policies1))
	}
	policies2, _ := cache.Get(ctx, "tenant2")
	if len(policies2) != 1 {
		t.Fatalf("expected 1 policy for tenant2, got %d", len(policies2))
	}

	// Clear all
	err := cache.Clear(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify everything was cleared
	policies1, _ = cache.Get(ctx, "tenant1")
	if policies1 != nil && len(policies1) > 0 {
		t.Errorf("expected no policies for tenant1 after clear, got %d", len(policies1))
	}
	policies2, _ = cache.Get(ctx, "tenant2")
	if policies2 != nil && len(policies2) > 0 {
		t.Errorf("expected no policies for tenant2 after clear, got %d", len(policies2))
	}

	// Verify maps were recreated
	if cache.cache == nil {
		t.Error("cache.cache is nil after clear")
	}
	if cache.expires == nil {
		t.Error("cache.expires is nil after clear")
	}
}

func TestMemoryPolicyCache_Concurrent(t *testing.T) {
	cache := NewMemoryPolicyCache(5 * time.Minute)
	ctx := context.Background()

	compiled := createTestPolicy("tenant1", "policy1")

	// Run concurrent operations
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(3)

		// Concurrent writes
		go func() {
			defer wg.Done()
			_ = cache.Set(ctx, "tenant1", []*Policy{compiled})
		}()

		// Concurrent reads
		go func() {
			defer wg.Done()
			_, _ = cache.Get(ctx, "tenant1")
		}()

		// Concurrent invalidates
		go func() {
			defer wg.Done()
			_ = cache.Invalidate(ctx, "tenant1")
		}()
	}

	wg.Wait()

	// No assertion needed - just verify no race conditions
}

func TestPolicyEngine_CacheInvalidation(t *testing.T) {
	storage := newMockPolicyStorage()
	cache := newTestPolicyCache()
	engine := NewPolicyEngine(PolicyEngineOptions{
		Storage: storage,
		Cache:   cache,
	})

	ctx := context.Background()
	policy := createTestPolicy("tenant1", "policy1")

	// Create policy
	err := engine.CreatePolicy(ctx, "tenant1", policy)
	if err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}

	// Populate cache directly (CompileAndCache was removed along with the
	// deleted local ABAC engine; the cache is no longer decision-affecting,
	// see PolicyCache's doc comment).
	cache.Set("tenant1", policy)

	// Verify cache is populated
	policies := cache.GetAll("tenant1")
	if len(policies) != 1 {
		t.Fatalf("expected 1 cached policy, got %d", len(policies))
	}

	// Update policy - should invalidate cache
	err = engine.UpdatePolicy(ctx, "tenant1", policy)
	if err != nil {
		t.Fatalf("failed to update policy: %v", err)
	}

	// Cache should be cleared for this tenant
	policies = cache.GetAll("tenant1")
	if len(policies) > 0 {
		t.Errorf("expected cache to be invalidated after update, got %d policies", len(policies))
	}

	// Repopulate cache directly.
	cache.Set("tenant1", policy)

	// Delete policy - should invalidate cache
	err = engine.DeletePolicy(ctx, "tenant1", "policy1")
	if err != nil {
		t.Fatalf("failed to delete policy: %v", err)
	}

	// Cache should be cleared for this tenant
	policies = cache.GetAll("tenant1")
	if len(policies) > 0 {
		t.Errorf("expected cache to be invalidated after delete, got %d policies", len(policies))
	}
}
