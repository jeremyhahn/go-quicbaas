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

package catalog

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-objstore/pkg/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testStorage implements common.Storage for testing.
type testStorage struct {
	data         map[string][]byte
	metadata     map[string]*common.Metadata
	listError    error
	putError     error
	getError     error
	deleteError  error
	closeError   error
	closed       bool
	mu           sync.RWMutex
	listDelay    time.Duration
	listCallback func(prefix string) ([]string, error)
}

func newTestStorage() *testStorage {
	return &testStorage{
		data:     make(map[string][]byte),
		metadata: make(map[string]*common.Metadata),
	}
}

func (s *testStorage) Configure(_ map[string]string) error { return nil }

func (s *testStorage) Put(key string, data io.Reader) error {
	if s.putError != nil {
		return s.putError
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := io.ReadAll(data)
	s.data[key] = b
	return nil
}

func (s *testStorage) PutWithContext(_ context.Context, key string, data io.Reader) error {
	return s.Put(key, data)
}

func (s *testStorage) PutWithMetadata(_ context.Context, key string, data io.Reader, meta *common.Metadata) error {
	if err := s.Put(key, data); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if meta != nil {
		s.metadata[key] = meta
	}
	return nil
}

func (s *testStorage) Get(key string) (io.ReadCloser, error) {
	if s.getError != nil {
		return nil, s.getError
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.data[key]
	if !ok {
		return nil, errors.New("key not found")
	}
	return io.NopCloser(strings.NewReader(string(data))), nil
}

func (s *testStorage) GetWithContext(_ context.Context, key string) (io.ReadCloser, error) {
	return s.Get(key)
}

func (s *testStorage) GetMetadata(_ context.Context, key string) (*common.Metadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if meta, ok := s.metadata[key]; ok {
		return meta, nil
	}
	return &common.Metadata{}, nil
}

func (s *testStorage) UpdateMetadata(_ context.Context, key string, meta *common.Metadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadata[key] = meta
	return nil
}

func (s *testStorage) Delete(key string) error {
	if s.deleteError != nil {
		return s.deleteError
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	delete(s.metadata, key)
	return nil
}

func (s *testStorage) DeleteWithContext(_ context.Context, key string) error {
	return s.Delete(key)
}

func (s *testStorage) Exists(_ context.Context, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data[key]
	return ok, nil
}

func (s *testStorage) List(prefix string) ([]string, error) {
	if s.listCallback != nil {
		return s.listCallback(prefix)
	}
	if s.listDelay > 0 {
		time.Sleep(s.listDelay)
	}
	if s.listError != nil {
		return nil, s.listError
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var keys []string
	for key := range s.data {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (s *testStorage) ListWithContext(ctx context.Context, prefix string) ([]string, error) {
	if s.listDelay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.listDelay):
		}
	}
	return s.List(prefix)
}

func (s *testStorage) ListWithOptions(_ context.Context, opts *common.ListOptions) (*common.ListResult, error) {
	prefix := ""
	if opts != nil {
		prefix = opts.Prefix
	}
	keys, err := s.List(prefix)
	if err != nil {
		return nil, err
	}
	objects := make([]*common.ObjectInfo, 0, len(keys))
	for _, key := range keys {
		objects = append(objects, &common.ObjectInfo{Key: key})
	}
	return &common.ListResult{Objects: objects}, nil
}

func (s *testStorage) Archive(key string, _ common.Archiver) error {
	return errors.New("archive not supported")
}

func (s *testStorage) AddPolicy(_ common.LifecyclePolicy) error       { return nil }
func (s *testStorage) RemovePolicy(_ string) error                    { return nil }
func (s *testStorage) GetPolicies() ([]common.LifecyclePolicy, error) { return nil, nil }

func (s *testStorage) Close() error {
	if s.closeError != nil {
		return s.closeError
	}
	s.closed = true
	return nil
}

// TestIsValidBackendTier tests the isValidBackendTier function.
func TestIsValidBackendTier(t *testing.T) {
	tests := []struct {
		tier  StorageTier
		valid bool
	}{
		{TierHot, true},
		{TierWarm, true},
		{TierCold, true},
		{StorageTier(""), true},
		{StorageTier("invalid"), false},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.valid, isValidBackendTier(tc.tier), "tier: %s", tc.tier)
	}
}

// TestBackendConfig tests the BackendConfig type.
func TestBackendConfig(t *testing.T) {
	t.Run("Clone", func(t *testing.T) {
		cfg := &BackendConfig{
			Type:          "s3",
			Enabled:       true,
			Default:       true,
			Tier:          TierHot,
			Region:        "us-east-1",
			MaxObjectSize: 1024 * 1024,
			Settings: map[string]string{
				"bucket": "test-bucket",
			},
		}

		clone := cfg.Clone()
		require.NotNil(t, clone)
		assert.Equal(t, cfg.Type, clone.Type)
		assert.Equal(t, cfg.Enabled, clone.Enabled)
		assert.Equal(t, cfg.Default, clone.Default)
		assert.Equal(t, cfg.Tier, clone.Tier)
		assert.Equal(t, cfg.Region, clone.Region)
		assert.Equal(t, cfg.MaxObjectSize, clone.MaxObjectSize)
		assert.Equal(t, cfg.Settings["bucket"], clone.Settings["bucket"])

		// Verify deep copy
		clone.Settings["bucket"] = "modified"
		assert.NotEqual(t, cfg.Settings["bucket"], clone.Settings["bucket"])
	})

	t.Run("Clone_Nil", func(t *testing.T) {
		var cfg *BackendConfig
		assert.Nil(t, cfg.Clone())
	})

	t.Run("Clone_NilSettings", func(t *testing.T) {
		cfg := &BackendConfig{Type: "memory"}
		clone := cfg.Clone()
		assert.Nil(t, clone.Settings)
	})

	t.Run("Validate", func(t *testing.T) {
		tests := []struct {
			name    string
			cfg     *BackendConfig
			wantErr error
		}{
			{
				name:    "nil config",
				cfg:     nil,
				wantErr: ErrBackendConfigNil,
			},
			{
				name:    "empty type",
				cfg:     &BackendConfig{},
				wantErr: ErrBackendTypeEmpty,
			},
			{
				name: "invalid tier",
				cfg: &BackendConfig{
					Type: "memory",
					Tier: StorageTier("invalid"),
				},
				wantErr: ErrInvalidStorageTier,
			},
			{
				name: "negative max object size",
				cfg: &BackendConfig{
					Type:          "memory",
					MaxObjectSize: -1,
				},
				wantErr: ErrInvalidMaxObjectSize,
			},
			{
				name: "valid config",
				cfg: &BackendConfig{
					Type:    "memory",
					Enabled: true,
					Tier:    TierHot,
				},
				wantErr: nil,
			},
			{
				name: "valid config with empty tier",
				cfg: &BackendConfig{
					Type:    "memory",
					Enabled: true,
					Tier:    "",
				},
				wantErr: nil,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				err := tc.cfg.Validate()
				if tc.wantErr != nil {
					assert.ErrorIs(t, err, tc.wantErr)
				} else {
					assert.NoError(t, err)
				}
			})
		}
	})
}

// TestBackendHealth tests the BackendHealth type.
func TestBackendHealth(t *testing.T) {
	t.Run("Clone", func(t *testing.T) {
		health := &BackendHealth{
			Name:      "test",
			Healthy:   true,
			LastCheck: time.Now(),
			Latency:   100 * time.Millisecond,
			Error:     "",
			Status:    "healthy",
		}

		clone := health.Clone()
		require.NotNil(t, clone)
		assert.Equal(t, health.Name, clone.Name)
		assert.Equal(t, health.Healthy, clone.Healthy)
		assert.Equal(t, health.LastCheck, clone.LastCheck)
		assert.Equal(t, health.Latency, clone.Latency)
		assert.Equal(t, health.Status, clone.Status)
	})

	t.Run("Clone_Nil", func(t *testing.T) {
		var health *BackendHealth
		assert.Nil(t, health.Clone())
	})
}

// TestBackendManagerConfig tests the BackendManagerConfig type.
func TestBackendManagerConfig(t *testing.T) {
	t.Run("DefaultBackendManagerConfig", func(t *testing.T) {
		cfg := DefaultBackendManagerConfig()
		require.NotNil(t, cfg)
		assert.Equal(t, 5*time.Second, cfg.HealthCheckTimeout)
	})

	t.Run("Validate", func(t *testing.T) {
		tests := []struct {
			name    string
			cfg     *BackendManagerConfig
			wantErr error
		}{
			{
				name:    "nil config",
				cfg:     nil,
				wantErr: ErrBackendManagerConfigNil,
			},
			{
				name:    "negative timeout",
				cfg:     &BackendManagerConfig{HealthCheckTimeout: -1},
				wantErr: ErrInvalidHealthCheckTimeout,
			},
			{
				name:    "valid config",
				cfg:     DefaultBackendManagerConfig(),
				wantErr: nil,
			},
			{
				name:    "zero timeout valid",
				cfg:     &BackendManagerConfig{HealthCheckTimeout: 0},
				wantErr: nil,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				err := tc.cfg.Validate()
				if tc.wantErr != nil {
					assert.ErrorIs(t, err, tc.wantErr)
				} else {
					assert.NoError(t, err)
				}
			})
		}
	})
}

// TestNewBackendManager tests NewBackendManager.
func TestNewBackendManager(t *testing.T) {
	t.Run("WithNilConfig", func(t *testing.T) {
		mgr, err := NewBackendManager(nil)
		require.NoError(t, err)
		require.NotNil(t, mgr)
		defer mgr.Close()
	})

	t.Run("WithValidConfig", func(t *testing.T) {
		cfg := &BackendManagerConfig{
			HealthCheckTimeout: 10 * time.Second,
			DefaultBackend:     "primary",
		}
		mgr, err := NewBackendManager(cfg)
		require.NoError(t, err)
		require.NotNil(t, mgr)
		defer mgr.Close()
	})

	t.Run("WithInvalidConfig", func(t *testing.T) {
		cfg := &BackendManagerConfig{
			HealthCheckTimeout: -1,
		}
		mgr, err := NewBackendManager(cfg)
		assert.ErrorIs(t, err, ErrInvalidHealthCheckTimeout)
		assert.Nil(t, mgr)
	})
}

// TestBackendManager_RegisterBackend tests RegisterBackend.
func TestBackendManager_RegisterBackend(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		err := mgr.RegisterBackend("test", &BackendConfig{
			Type:    "memory",
			Enabled: true,
		})
		require.NoError(t, err)

		backends := mgr.ListBackends()
		assert.Contains(t, backends, "test")
	})

	t.Run("EmptyName", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		err := mgr.RegisterBackend("", &BackendConfig{
			Type:    "memory",
			Enabled: true,
		})
		assert.ErrorIs(t, err, ErrBackendNameEmpty)
	})

	t.Run("NilConfig", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		err := mgr.RegisterBackend("test", nil)
		assert.ErrorIs(t, err, ErrBackendConfigNil)
	})

	t.Run("DuplicateBackend", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		cfg := &BackendConfig{Type: "memory", Enabled: true}
		err := mgr.RegisterBackend("test", cfg)
		require.NoError(t, err)

		err = mgr.RegisterBackend("test", cfg)
		assert.True(t, IsBackendExists(err))
	})

	t.Run("InvalidBackendType", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		err := mgr.RegisterBackend("test", &BackendConfig{
			Type:    "invalid_type_xyz",
			Enabled: true,
		})
		require.Error(t, err)
		var ce *CatalogError
		assert.True(t, errors.As(err, &ce))
		assert.Equal(t, ErrCodeBackendCreationFailed, ce.Code)
	})

	t.Run("SetDefaultBackend", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		err := mgr.RegisterBackend("first", &BackendConfig{
			Type:    "memory",
			Enabled: true,
		})
		require.NoError(t, err)

		err = mgr.RegisterBackend("second", &BackendConfig{
			Type:    "memory",
			Enabled: true,
			Default: true,
		})
		require.NoError(t, err)

		// Second should be default due to Default: true
		name, _, err := mgr.SelectBackend(nil)
		require.NoError(t, err)
		assert.Equal(t, "second", name)
	})

	t.Run("ClosedManager", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		mgr.Close()

		err := mgr.RegisterBackend("test", &BackendConfig{
			Type:    "memory",
			Enabled: true,
		})
		assert.True(t, IsBackendManagerClosed(err))
	})
}

// TestBackendManager_GetBackend tests GetBackend.
func TestBackendManager_GetBackend(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		err := mgr.RegisterBackend("test", &BackendConfig{
			Type:    "memory",
			Enabled: true,
		})
		require.NoError(t, err)

		storage, err := mgr.GetBackend("test")
		require.NoError(t, err)
		assert.NotNil(t, storage)
	})

	t.Run("EmptyName", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		_, err := mgr.GetBackend("")
		assert.ErrorIs(t, err, ErrBackendNameEmpty)
	})

	t.Run("NotFound", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		_, err := mgr.GetBackend("nonexistent")
		assert.True(t, IsBackendError(err))
	})

	t.Run("DisabledBackend", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		err := mgr.RegisterBackend("test", &BackendConfig{
			Type:    "memory",
			Enabled: false,
		})
		require.NoError(t, err)

		_, err = mgr.GetBackend("test")
		assert.True(t, IsBackendDisabled(err))
	})

	t.Run("ClosedManager", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		err := mgr.RegisterBackend("test", &BackendConfig{
			Type:    "memory",
			Enabled: true,
		})
		require.NoError(t, err)
		mgr.Close()

		_, err = mgr.GetBackend("test")
		assert.True(t, IsBackendManagerClosed(err))
	})
}

// TestBackendManager_GetBackendConfig tests GetBackendConfig.
func TestBackendManager_GetBackendConfig(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		originalCfg := &BackendConfig{
			Type:          "memory",
			Enabled:       true,
			Tier:          TierHot,
			Region:        "us-east-1",
			MaxObjectSize: 1024,
			Settings:      map[string]string{"key": "value"},
		}
		err := mgr.RegisterBackend("test", originalCfg)
		require.NoError(t, err)

		cfg, err := mgr.GetBackendConfig("test")
		require.NoError(t, err)
		assert.Equal(t, originalCfg.Type, cfg.Type)
		assert.Equal(t, originalCfg.Tier, cfg.Tier)
		assert.Equal(t, originalCfg.Region, cfg.Region)
	})

	t.Run("EmptyName", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		_, err := mgr.GetBackendConfig("")
		assert.ErrorIs(t, err, ErrBackendNameEmpty)
	})

	t.Run("NotFound", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		_, err := mgr.GetBackendConfig("nonexistent")
		assert.True(t, IsBackendError(err))
	})

	t.Run("ClosedManager", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		mgr.Close()

		_, err := mgr.GetBackendConfig("test")
		assert.True(t, IsBackendManagerClosed(err))
	})
}

// TestBackendManager_ListBackends tests ListBackends.
func TestBackendManager_ListBackends(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		backends := mgr.ListBackends()
		assert.Empty(t, backends)
	})

	t.Run("Multiple", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		mgr.RegisterBackend("alpha", &BackendConfig{Type: "memory", Enabled: true})
		mgr.RegisterBackend("beta", &BackendConfig{Type: "memory", Enabled: true})
		mgr.RegisterBackend("gamma", &BackendConfig{Type: "memory", Enabled: true})

		backends := mgr.ListBackends()
		assert.Len(t, backends, 3)
		// Should be sorted
		assert.Equal(t, []string{"alpha", "beta", "gamma"}, backends)
	})
}

// TestBackendManager_UnregisterBackend tests UnregisterBackend.
func TestBackendManager_UnregisterBackend(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		err := mgr.RegisterBackend("test", &BackendConfig{
			Type:    "memory",
			Enabled: true,
		})
		require.NoError(t, err)

		err = mgr.UnregisterBackend("test")
		require.NoError(t, err)

		backends := mgr.ListBackends()
		assert.NotContains(t, backends, "test")
	})

	t.Run("EmptyName", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		err := mgr.UnregisterBackend("")
		assert.ErrorIs(t, err, ErrBackendNameEmpty)
	})

	t.Run("NotFound", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		err := mgr.UnregisterBackend("nonexistent")
		assert.True(t, IsBackendError(err))
	})

	t.Run("UpdatesDefault", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		mgr.RegisterBackend("first", &BackendConfig{Type: "memory", Enabled: true, Default: true})
		mgr.RegisterBackend("second", &BackendConfig{Type: "memory", Enabled: true})

		err := mgr.UnregisterBackend("first")
		require.NoError(t, err)

		// Default should be updated to second
		name, _, err := mgr.SelectBackend(nil)
		require.NoError(t, err)
		assert.Equal(t, "second", name)
	})

	t.Run("ClosedManager", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		mgr.Close()

		err := mgr.UnregisterBackend("test")
		assert.True(t, IsBackendManagerClosed(err))
	})
}

// TestBackendManager_SelectBackend tests SelectBackend.
func TestBackendManager_SelectBackend(t *testing.T) {
	t.Run("NoBackends", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		_, _, err := mgr.SelectBackend(nil)
		assert.ErrorIs(t, err, ErrNoBackendsAvailable)
	})

	t.Run("PreferredBackend", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		mgr.RegisterBackend("primary", &BackendConfig{Type: "memory", Enabled: true, Default: true})
		mgr.RegisterBackend("secondary", &BackendConfig{Type: "memory", Enabled: true})

		name, storage, err := mgr.SelectBackend(&SelectionCriteria{
			PreferredBackend: "secondary",
		})
		require.NoError(t, err)
		assert.Equal(t, "secondary", name)
		assert.NotNil(t, storage)
	})

	t.Run("PreferredBackendNotFound", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		mgr.RegisterBackend("primary", &BackendConfig{Type: "memory", Enabled: true})

		_, _, err := mgr.SelectBackend(&SelectionCriteria{
			PreferredBackend: "nonexistent",
		})
		assert.True(t, IsBackendError(err))
	})

	t.Run("PreferredBackendDisabled", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		mgr.RegisterBackend("disabled", &BackendConfig{Type: "memory", Enabled: false})

		_, _, err := mgr.SelectBackend(&SelectionCriteria{
			PreferredBackend: "disabled",
		})
		assert.True(t, IsBackendDisabled(err))
	})

	t.Run("FilterByTier", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		mgr.RegisterBackend("hot-store", &BackendConfig{Type: "memory", Enabled: true, Tier: TierHot})
		mgr.RegisterBackend("cold-store", &BackendConfig{Type: "memory", Enabled: true, Tier: TierCold})

		name, _, err := mgr.SelectBackend(&SelectionCriteria{Tier: TierCold})
		require.NoError(t, err)
		assert.Equal(t, "cold-store", name)
	})

	t.Run("FilterByObjectSize", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		mgr.RegisterBackend("small", &BackendConfig{Type: "memory", Enabled: true, MaxObjectSize: 1024})
		mgr.RegisterBackend("large", &BackendConfig{Type: "memory", Enabled: true, MaxObjectSize: 1024 * 1024})

		name, _, err := mgr.SelectBackend(&SelectionCriteria{ObjectSize: 2048})
		require.NoError(t, err)
		assert.Equal(t, "large", name)
	})

	t.Run("FilterByLocality", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		mgr.RegisterBackend("remote", &BackendConfig{Type: "memory", Enabled: true, Region: "us-west-2"})
		mgr.RegisterBackend("local", &BackendConfig{Type: "memory", Enabled: true, Region: "local"})

		name, _, err := mgr.SelectBackend(&SelectionCriteria{RequireLocal: true})
		require.NoError(t, err)
		assert.Equal(t, "local", name)
	})

	t.Run("DefaultBackend", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		mgr.RegisterBackend("backup", &BackendConfig{Type: "memory", Enabled: true})
		mgr.RegisterBackend("primary", &BackendConfig{Type: "memory", Enabled: true, Default: true})

		name, _, err := mgr.SelectBackend(nil)
		require.NoError(t, err)
		assert.Equal(t, "primary", name)
	})

	t.Run("NoHealthyBackends", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		mgr.RegisterBackend("disabled1", &BackendConfig{Type: "memory", Enabled: false})
		mgr.RegisterBackend("disabled2", &BackendConfig{Type: "memory", Enabled: false})

		_, _, err := mgr.SelectBackend(nil)
		assert.ErrorIs(t, err, ErrNoHealthyBackends)
	})

	t.Run("FallbackToOriginalCandidates", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		// Only hot tier backend, but requesting cold
		mgr.RegisterBackend("hot-only", &BackendConfig{Type: "memory", Enabled: true, Tier: TierHot})

		// Should fall back to the only available backend
		name, _, err := mgr.SelectBackend(&SelectionCriteria{Tier: TierCold})
		require.NoError(t, err)
		assert.Equal(t, "hot-only", name)
	})

	t.Run("ClosedManager", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		mgr.Close()

		_, _, err := mgr.SelectBackend(nil)
		assert.True(t, IsBackendManagerClosed(err))
	})
}

// TestBackendManager_CheckHealth tests CheckHealth.
func TestBackendManager_CheckHealth(t *testing.T) {
	t.Run("HealthyBackend", func(t *testing.T) {
		mgr, _ := NewBackendManager(&BackendManagerConfig{
			HealthCheckTimeout: 5 * time.Second,
		})
		defer mgr.Close()

		mgr.RegisterBackend("test", &BackendConfig{Type: "memory", Enabled: true})

		health, err := mgr.CheckHealth(context.Background(), "test")
		require.NoError(t, err)
		assert.True(t, health.Healthy)
		assert.Equal(t, "test", health.Name)
		assert.Equal(t, "healthy", health.Status)
		assert.NotZero(t, health.LastCheck)
	})

	t.Run("EmptyName", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		_, err := mgr.CheckHealth(context.Background(), "")
		assert.ErrorIs(t, err, ErrBackendNameEmpty)
	})

	t.Run("NotFound", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		_, err := mgr.CheckHealth(context.Background(), "nonexistent")
		assert.True(t, IsBackendError(err))
	})

	t.Run("ClosedManager", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		mgr.Close()

		_, err := mgr.CheckHealth(context.Background(), "test")
		assert.True(t, IsBackendManagerClosed(err))
	})
}

// TestBackendManager_CheckAllHealth tests CheckAllHealth.
func TestBackendManager_CheckAllHealth(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		results := mgr.CheckAllHealth(context.Background())
		assert.Empty(t, results)
	})

	t.Run("Multiple", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		mgr.RegisterBackend("backend1", &BackendConfig{Type: "memory", Enabled: true})
		mgr.RegisterBackend("backend2", &BackendConfig{Type: "memory", Enabled: true})

		results := mgr.CheckAllHealth(context.Background())
		assert.Len(t, results, 2)
		assert.Contains(t, results, "backend1")
		assert.Contains(t, results, "backend2")
		assert.True(t, results["backend1"].Healthy)
		assert.True(t, results["backend2"].Healthy)
	})

	t.Run("ConcurrentExecution", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)
		defer mgr.Close()

		// Register multiple backends
		for i := 0; i < 10; i++ {
			name := string(rune('a' + i))
			mgr.RegisterBackend(name, &BackendConfig{Type: "memory", Enabled: true})
		}

		start := time.Now()
		results := mgr.CheckAllHealth(context.Background())
		elapsed := time.Since(start)

		assert.Len(t, results, 10)
		// Should complete quickly due to concurrent execution
		assert.Less(t, elapsed, 2*time.Second)
	})
}

// TestBackendManager_Close tests Close.
func TestBackendManager_Close(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)

		mgr.RegisterBackend("test", &BackendConfig{Type: "memory", Enabled: true})

		err := mgr.Close()
		assert.NoError(t, err)

		// Second close should be no-op
		err = mgr.Close()
		assert.NoError(t, err)
	})

	t.Run("ClosesAllBackends", func(t *testing.T) {
		mgr, _ := NewBackendManager(nil)

		mgr.RegisterBackend("backend1", &BackendConfig{Type: "memory", Enabled: true})
		mgr.RegisterBackend("backend2", &BackendConfig{Type: "memory", Enabled: true})

		err := mgr.Close()
		assert.NoError(t, err)

		// Operations should fail after close
		_, err = mgr.GetBackend("backend1")
		assert.True(t, IsBackendManagerClosed(err))
	})
}

// TestContainsIgnoreCase tests the containsIgnoreCase helper.
func TestContainsIgnoreCase(t *testing.T) {
	tests := []struct {
		s        string
		substr   string
		expected bool
	}{
		{"connection refused", "connection", true},
		{"CONNECTION REFUSED", "connection", true},
		{"Connection Refused", "REFUSED", true},
		{"hello world", "world", true},
		{"hello", "world", false},
		{"", "", true},
		{"hello", "", true},
		{"", "hello", false},
		{"abc", "abcd", false},
	}

	for _, tc := range tests {
		result := containsIgnoreCase(tc.s, tc.substr)
		assert.Equal(t, tc.expected, result, "s=%q, substr=%q", tc.s, tc.substr)
	}
}

// TestBytesContains tests the bytesContains helper.
func TestBytesContains(t *testing.T) {
	tests := []struct {
		b        []byte
		sub      []byte
		expected bool
	}{
		{[]byte("hello world"), []byte("world"), true},
		{[]byte("hello"), []byte("world"), false},
		{[]byte("hello"), []byte{}, true},
		{[]byte{}, []byte{}, true},
		{[]byte{}, []byte("hello"), false},
		{[]byte("abc"), []byte("abcd"), false},
	}

	for _, tc := range tests {
		result := bytesContains(tc.b, tc.sub)
		assert.Equal(t, tc.expected, result, "b=%q, sub=%q", tc.b, tc.sub)
	}
}

// TestIsConnectivityError tests the isConnectivityError function.
func TestIsConnectivityError(t *testing.T) {
	tests := []struct {
		errStr   string
		expected bool
	}{
		{"connection refused", true},
		{"Connection refused", true},
		{"dial tcp: connection reset", true},
		{"network unreachable", true},
		{"no such host", true},
		{"TLS handshake error", true},
		{"certificate invalid", true},
		{"authentication failed", true},
		{"access denied", true},
		{"forbidden", true},
		{"unauthorized", true},
		{"i/o timeout", true},
		{"key not found", false},
		{"object does not exist", false},
		{"", false},
	}

	for _, tc := range tests {
		result := isConnectivityError(tc.errStr)
		assert.Equal(t, tc.expected, result, "errStr=%q", tc.errStr)
	}
}

// TestErrorHelpers tests error helper functions.
func TestErrorHelpers(t *testing.T) {
	t.Run("IsBackendDisabled", func(t *testing.T) {
		err := NewBackendDisabledError("test")
		assert.True(t, IsBackendDisabled(err))
		assert.False(t, IsBackendDisabled(nil))
		assert.False(t, IsBackendDisabled(errors.New("other error")))
	})

	t.Run("IsBackendExists", func(t *testing.T) {
		err := NewBackendExistsError("test")
		assert.True(t, IsBackendExists(err))
		assert.False(t, IsBackendExists(nil))
		assert.False(t, IsBackendExists(errors.New("other error")))
	})

	t.Run("IsBackendManagerClosed", func(t *testing.T) {
		err := NewBackendManagerClosedError()
		assert.True(t, IsBackendManagerClosed(err))
		assert.False(t, IsBackendManagerClosed(nil))
		assert.False(t, IsBackendManagerClosed(errors.New("other error")))
	})

	t.Run("NewBackendCreationError", func(t *testing.T) {
		cause := errors.New("failed to connect")
		err := NewBackendCreationError("test", cause)
		var ce *CatalogError
		require.True(t, errors.As(err, &ce))
		assert.Equal(t, ErrCodeBackendCreationFailed, ce.Code)
		assert.ErrorIs(t, err, cause)
	})

	t.Run("NewBackendCloseError", func(t *testing.T) {
		cause := errors.New("close failed")
		err := NewBackendCloseError("test", cause)
		var ce *CatalogError
		require.True(t, errors.As(err, &ce))
		assert.Equal(t, ErrCodeBackendCloseFailed, ce.Code)
		assert.ErrorIs(t, err, cause)
	})
}

// TestBackendManager_HealthCheckWithUnhealthyBackend tests health checks with unhealthy backends.
func TestBackendManager_HealthCheckWithUnhealthyBackend(t *testing.T) {
	t.Run("UnhealthyBackendSelection", func(t *testing.T) {
		mgr, _ := NewBackendManager(&BackendManagerConfig{
			HealthCheckTimeout: 100 * time.Millisecond,
		})
		defer mgr.Close()

		// Register backend
		mgr.RegisterBackend("test", &BackendConfig{Type: "memory", Enabled: true})

		// Manually mark backend as unhealthy
		bm := mgr.(*backendManager)
		bm.mu.RLock()
		entry := bm.backends["test"]
		bm.mu.RUnlock()

		entry.health.Store(&BackendHealth{
			Name:      "test",
			Healthy:   false,
			LastCheck: time.Now(),
			Error:     "connection refused",
			Status:    "unhealthy",
		})

		// Selection should fail when preferred backend is unhealthy
		_, _, err := mgr.SelectBackend(&SelectionCriteria{
			PreferredBackend: "test",
		})
		assert.True(t, IsBackendError(err))
	})
}

// TestBackendManager_ConcurrentOperations tests concurrent operations.
func TestBackendManager_ConcurrentOperations(t *testing.T) {
	mgr, _ := NewBackendManager(nil)
	defer mgr.Close()

	// Register initial backends
	for i := 0; i < 5; i++ {
		name := string(rune('a' + i))
		mgr.RegisterBackend(name, &BackendConfig{Type: "memory", Enabled: true})
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	// Concurrent reads
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_ = mgr.ListBackends()
				_, _, err := mgr.SelectBackend(nil)
				if err != nil {
					errCh <- err
				}
			}
		}()
	}

	// Concurrent health checks
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = mgr.CheckAllHealth(context.Background())
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestBackendManager_EmptyRegionAsLocal tests that empty region is treated as local.
func TestBackendManager_EmptyRegionAsLocal(t *testing.T) {
	mgr, _ := NewBackendManager(nil)
	defer mgr.Close()

	mgr.RegisterBackend("no-region", &BackendConfig{Type: "memory", Enabled: true, Region: ""})
	mgr.RegisterBackend("remote", &BackendConfig{Type: "memory", Enabled: true, Region: "us-west-2"})

	name, _, err := mgr.SelectBackend(&SelectionCriteria{RequireLocal: true})
	require.NoError(t, err)
	assert.Equal(t, "no-region", name)
}

// TestBackendManager_ZeroMaxObjectSize tests backends with unlimited object size.
func TestBackendManager_ZeroMaxObjectSize(t *testing.T) {
	mgr, _ := NewBackendManager(nil)
	defer mgr.Close()

	// Zero means unlimited
	mgr.RegisterBackend("unlimited", &BackendConfig{Type: "memory", Enabled: true, MaxObjectSize: 0})
	mgr.RegisterBackend("limited", &BackendConfig{Type: "memory", Enabled: true, MaxObjectSize: 100})

	// Large object should use unlimited backend
	name, _, err := mgr.SelectBackend(&SelectionCriteria{ObjectSize: 1000})
	require.NoError(t, err)
	assert.Equal(t, "unlimited", name)
}

// TestBackendManager_TierMatchExact tests exact tier matching in selection.
func TestBackendManager_TierMatchExact(t *testing.T) {
	mgr, _ := NewBackendManager(nil)
	defer mgr.Close()

	mgr.RegisterBackend("hot-primary", &BackendConfig{Type: "memory", Enabled: true, Tier: TierHot, Default: true})
	mgr.RegisterBackend("warm-store", &BackendConfig{Type: "memory", Enabled: true, Tier: TierWarm})
	mgr.RegisterBackend("cold-archive", &BackendConfig{Type: "memory", Enabled: true, Tier: TierCold})

	// Should select warm backend when requesting warm tier
	name, _, err := mgr.SelectBackend(&SelectionCriteria{Tier: TierWarm})
	require.NoError(t, err)
	assert.Equal(t, "warm-store", name)
}

// TestBackendManager_MultipleFilters tests combining multiple selection criteria.
func TestBackendManager_MultipleFilters(t *testing.T) {
	mgr, _ := NewBackendManager(nil)
	defer mgr.Close()

	mgr.RegisterBackend("remote-hot-small", &BackendConfig{
		Type:          "memory",
		Enabled:       true,
		Tier:          TierHot,
		Region:        "us-west-2",
		MaxObjectSize: 100,
	})
	mgr.RegisterBackend("local-hot-large", &BackendConfig{
		Type:          "memory",
		Enabled:       true,
		Tier:          TierHot,
		Region:        "local",
		MaxObjectSize: 10000,
	})
	mgr.RegisterBackend("local-cold-large", &BackendConfig{
		Type:          "memory",
		Enabled:       true,
		Tier:          TierCold,
		Region:        "local",
		MaxObjectSize: 10000,
	})

	// Request local, hot, and large object
	name, _, err := mgr.SelectBackend(&SelectionCriteria{
		Tier:         TierHot,
		RequireLocal: true,
		ObjectSize:   500,
	})
	require.NoError(t, err)
	assert.Equal(t, "local-hot-large", name)
}
