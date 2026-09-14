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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// ServiceBuilder Comprehensive Tests
// =============================================================================

func TestServiceBuilder_ChainedCalls(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockStore := &mockStorage{}
	mockBackup := &mockBackupManager{}
	log := createTestLogger(t)

	cacheConfig := cache.Config{
		L1Size: 100,
		L1TTL:  5 * time.Minute,
	}
	cacheMgr, err := cache.NewManager(cacheConfig, log)
	require.NoError(t, err)

	config := &ServiceConfig{
		NodeID:                1,
		Version:               "1.0.0",
		EnableAuth:            true,
		EnableMetrics:         true,
		EnableTracing:         true,
		RequestTimeout:        10 * time.Second,
		MaxConcurrentRequests: 5000,
	}

	builder := NewServiceBuilder().
		WithRaftNode(mockRaft).
		WithStorage(mockStore).
		WithCacheManager(cacheMgr).
		WithBackupManager(mockBackup).
		WithLogger(log).
		WithConfig(config)

	sb, ok := builder.(*serviceBuilder)
	require.True(t, ok)

	assert.Equal(t, mockRaft, sb.raftNode)
	assert.Equal(t, mockStore, sb.storage)
	assert.Equal(t, cacheMgr, sb.cacheManager)
	assert.Equal(t, mockBackup, sb.backupManager)
	assert.Equal(t, log, sb.logger)
	assert.Equal(t, config, sb.config)
}

func TestServiceBuilder_Build_MissingDependencies(t *testing.T) {
	tests := []struct {
		name        string
		setupFn     func() *serviceBuilder
		wantErrCode dberrors.ErrorCode
		wantErrMsg  string
	}{
		{
			name: "missing raft node",
			setupFn: func() *serviceBuilder {
				return &serviceBuilder{
					config: &ServiceConfig{NodeID: 1},
					logger: createTestLogger(t),
				}
			},
			wantErrCode: dberrors.ErrInvalidInput,
			wantErrMsg:  "raft node is required",
		},
		{
			name: "missing config",
			setupFn: func() *serviceBuilder {
				return &serviceBuilder{
					raftNode: &mockRaftNode{},
					logger:   createTestLogger(t),
				}
			},
			wantErrCode: dberrors.ErrInvalidInput,
			wantErrMsg:  "config is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := tt.setupFn()
			svc, err := newQuicBaaS(builder)

			require.Error(t, err)
			assert.Nil(t, svc)

			var dErr *dberrors.QBError
			require.True(t, dberrors.As(err, &dErr))
			assert.Equal(t, tt.wantErrCode, dErr.Code)
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}

func TestServiceBuilder_Build_InvalidTypes(t *testing.T) {
	tests := []struct {
		name       string
		setupFn    func() *serviceBuilder
		wantErrMsg string
	}{
		{
			name: "invalid raft node type",
			setupFn: func() *serviceBuilder {
				return &serviceBuilder{
					raftNode: "invalid type", // Wrong type
					config:   &ServiceConfig{NodeID: 1},
					logger:   createTestLogger(t),
				}
			},
			wantErrMsg: "raft node must be interfaces.RaftNode",
		},
		{
			name: "invalid storage type",
			setupFn: func() *serviceBuilder {
				return &serviceBuilder{
					raftNode: &mockRaftNode{},
					storage:  "invalid type", // Wrong type
					config:   &ServiceConfig{NodeID: 1},
					logger:   createTestLogger(t),
				}
			},
			wantErrMsg: "storage must be interfaces.Storage",
		},
		{
			name: "invalid cache manager type",
			setupFn: func() *serviceBuilder {
				return &serviceBuilder{
					raftNode:     &mockRaftNode{},
					cacheManager: "invalid type", // Wrong type
					config:       &ServiceConfig{NodeID: 1},
					logger:       createTestLogger(t),
				}
			},
			wantErrMsg: "cache manager must be interfaces.CacheManager",
		},
		{
			name: "invalid backup manager type",
			setupFn: func() *serviceBuilder {
				return &serviceBuilder{
					raftNode:      &mockRaftNode{},
					backupManager: "invalid type", // Wrong type
					config:        &ServiceConfig{NodeID: 1},
					logger:        createTestLogger(t),
				}
			},
			wantErrMsg: "backup manager must be interfaces.BackupManager",
		},
		{
			name: "invalid logger type",
			setupFn: func() *serviceBuilder {
				return &serviceBuilder{
					raftNode: &mockRaftNode{},
					logger:   "invalid type", // Wrong type
					config:   &ServiceConfig{NodeID: 1},
				}
			},
			wantErrMsg: "logger must be interfaces.Logger",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := tt.setupFn()
			svc, err := newQuicBaaS(builder)

			require.Error(t, err)
			assert.Nil(t, svc)
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}

func TestServiceBuilder_Build_WithMetrics(t *testing.T) {
	config := &ServiceConfig{
		NodeID:        1,
		Version:       "1.0.0",
		EnableMetrics: true,
	}

	svc, err := NewServiceBuilder().
		WithRaftNode(&mockRaftNode{}).
		WithConfig(config).
		WithLogger(createTestLogger(t)).
		Build()

	require.NoError(t, err)
	require.NotNil(t, svc)
	defer svc.Close()

	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)

	assert.NotNil(t, impl.metrics)
	assert.NotNil(t, impl.metrics.requestsTotal)
	assert.NotNil(t, impl.metrics.requestDuration)
	assert.NotNil(t, impl.metrics.activeRequests)
	assert.NotNil(t, impl.metrics.consistencyLevel)
	assert.NotNil(t, impl.metrics.errorsByType)
}

func TestServiceBuilder_Build_WithoutMetrics(t *testing.T) {
	config := &ServiceConfig{
		NodeID:        1,
		Version:       "1.0.0",
		EnableMetrics: false,
	}

	svc, err := NewServiceBuilder().
		WithRaftNode(&mockRaftNode{}).
		WithConfig(config).
		WithLogger(createTestLogger(t)).
		Build()

	require.NoError(t, err)
	require.NotNil(t, svc)
	defer svc.Close()

	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)

	assert.Nil(t, impl.metrics)
}

func TestServiceBuilder_Build_AllComponents(t *testing.T) {
	mockRaft := &mockRaftNode{}
	log := createTestLogger(t)

	cacheConfig := cache.Config{
		L1Size: 100,
		L1TTL:  5 * time.Minute,
	}
	cacheMgr, err := cache.NewManager(cacheConfig, log)
	require.NoError(t, err)

	config := &ServiceConfig{
		NodeID:                1,
		Version:               "1.0.0",
		EnableAuth:            false,
		EnableMetrics:         true,
		EnableTracing:         true,
		LogLevel:              "info",
		RequestTimeout:        30 * time.Second,
		MaxConcurrentRequests: 10000,
	}

	svc, err := NewServiceBuilder().
		WithRaftNode(mockRaft).
		WithCacheManager(cacheMgr).
		WithLogger(log).
		WithConfig(config).
		Build()

	require.NoError(t, err)
	require.NotNil(t, svc)
	defer svc.Close()

	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)

	// Verify all sub-services are initialized
	assert.NotNil(t, impl.kvService)
	assert.NotNil(t, impl.batchService)
	assert.NotNil(t, impl.transactionService)
	assert.NotNil(t, impl.queryService)
	assert.NotNil(t, impl.clusterService)
	assert.NotNil(t, impl.backupService)
	assert.NotNil(t, impl.authService)
	assert.NotNil(t, impl.rbacService)
	assert.NotNil(t, impl.cacheService)
	assert.NotNil(t, impl.healthService)
	assert.NotNil(t, impl.timeSeriesService)
	assert.NotNil(t, impl.operationsService)

	// Verify components are set
	assert.Equal(t, mockRaft, impl.raftNode)
	assert.Equal(t, cacheMgr, impl.cacheManager)
	assert.Equal(t, log, impl.logger)
	assert.Equal(t, config, impl.config)

	// Verify service is not closed
	assert.False(t, impl.closed)

	// Verify start time is set
	assert.False(t, impl.startTime.IsZero())
}

func TestServiceBuilder_Build_MinimalConfig(t *testing.T) {
	// Test building with only required components
	svc, err := NewServiceBuilder().
		WithRaftNode(&mockRaftNode{}).
		WithConfig(&ServiceConfig{
			NodeID:  1,
			Version: "1.0.0",
		}).
		Build()

	require.NoError(t, err)
	require.NotNil(t, svc)
	defer svc.Close()

	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)

	// Verify optional components can be nil
	assert.Nil(t, impl.storage)
	assert.Nil(t, impl.cacheManager)
	assert.Nil(t, impl.backupMgr)
	assert.Nil(t, impl.logger)

	// But sub-services should still be initialized
	assert.NotNil(t, impl.kvService)
	assert.NotNil(t, impl.batchService)
	assert.NotNil(t, impl.clusterService)
}

func TestServiceBuilder_DefaultConfig(t *testing.T) {
	builder := NewServiceBuilder()

	sb, ok := builder.(*serviceBuilder)
	require.True(t, ok)
	require.NotNil(t, sb.config)

	// Verify default config values
	assert.True(t, sb.config.EnableAuth)
	assert.True(t, sb.config.EnableMetrics)
	assert.False(t, sb.config.EnableTracing)
	assert.Equal(t, "info", sb.config.LogLevel)
	assert.Equal(t, 30*time.Second, sb.config.RequestTimeout)
	assert.Equal(t, 10000, sb.config.MaxConcurrentRequests)
}

func TestServiceBuilder_OverrideDefaultConfig(t *testing.T) {
	customConfig := &ServiceConfig{
		NodeID:                42,
		Version:               "2.0.0",
		EnableAuth:            false,
		EnableMetrics:         false,
		EnableTracing:         true,
		LogLevel:              "debug",
		RequestTimeout:        60 * time.Second,
		MaxConcurrentRequests: 5000,
	}

	builder := NewServiceBuilder().WithConfig(customConfig)

	sb, ok := builder.(*serviceBuilder)
	require.True(t, ok)

	assert.Equal(t, uint64(42), sb.config.NodeID)
	assert.Equal(t, "2.0.0", sb.config.Version)
	assert.False(t, sb.config.EnableAuth)
	assert.False(t, sb.config.EnableMetrics)
	assert.True(t, sb.config.EnableTracing)
	assert.Equal(t, "debug", sb.config.LogLevel)
	assert.Equal(t, 60*time.Second, sb.config.RequestTimeout)
	assert.Equal(t, 5000, sb.config.MaxConcurrentRequests)
}

func TestServiceBuilder_BuilderReturnsItself(t *testing.T) {
	// Verify builder methods return the builder for chaining
	builder1 := NewServiceBuilder()
	builder2 := builder1.WithRaftNode(&mockRaftNode{})
	builder3 := builder2.WithStorage(&mockStorage{})

	// All should be the same builder instance
	assert.Same(t, builder1, builder2)
	assert.Same(t, builder2, builder3)
}
