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

//go:build integration

package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

func TestStartStorage(t *testing.T) {
	tests := []struct {
		name        string
		setupConfig func(*testing.T) *config.Config
		wantErr     bool
		errContains string
	}{
		{
			name: "ValidStorage",
			setupConfig: func(t *testing.T) *config.Config {
				return createTestConfig(t)
			},
			wantErr: false,
		},
		{
			name: "InvalidDataDir_NonExistentParent",
			setupConfig: func(t *testing.T) *config.Config {
				cfg := createTestConfig(t)
				// Use invalid path with non-existent parent
				cfg.Storage.DataDir = "/nonexistent/path/that/should/not/exist/data"
				return cfg
			},
			wantErr:     true,
			errContains: "failed to create storage engine",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.setupConfig(t)
			if !tt.wantErr {
				defer cleanupTestData(t, cfg)
			}

			srv, err := NewServer(cfg)
			require.NoError(t, err)

			err = srv.startStorage()

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					if err != nil {
						assert.Contains(t, err.Error(), tt.errContains)
					}
				}
				assert.Nil(t, srv.storage)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, srv.storage)
			}
		})
	}
}

// TestStartRaftNode moved to server_integration_test.go (lines 17-119)
// Reason: Uses actual Raft cluster setup with real node creation and leader election

func TestStartIndexes(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(*testing.T) *Server
		wantErr     bool
		errContains string
	}{
		{
			name: "ValidIndexManager",
			setupServer: func(t *testing.T) *Server {
				cfg := createTestConfig(t)
				srv, err := NewServer(cfg)
				require.NoError(t, err)

				err = srv.startStorage()
				require.NoError(t, err)

				return srv
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			defer func() {
				if srv.indexManager != nil {
					srv.indexManager.Close()
				}
				if srv.storage != nil {
					srv.storage.Close()
				}
				cleanupTestData(t, srv.config)
			}()

			err := srv.startIndexes()

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					if err != nil {
						assert.Contains(t, err.Error(), tt.errContains)
					}
				}
				assert.Nil(t, srv.indexManager)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, srv.indexManager)
			}
		})
	}
}

// TestStartGRPCServer and TestStartRESTServer removed - redundant with server_additional_unit_test.go
// Those tests are properly implemented with dependency injection/seams in server_additional_unit_test.go:
// - TestStartRESTServer: lines 68-98
// - TestStartGRPCServer: lines 100-138

func TestStopRESTServer(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(*testing.T) *Server
		wantErr     bool
	}{
		{
			name: "StopNilServer",
			setupServer: func(t *testing.T) *Server {
				cfg := createTestConfig(t)
				srv, err := NewServer(cfg)
				require.NoError(t, err)
				// Don't start REST server
				return srv
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			defer func() {
				if srv.indexManager != nil {
					srv.indexManager.Close()
				}
				if srv.storage != nil {
					srv.storage.Close()
				}
				cleanupTestData(t, srv.config)
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err := srv.stopRESTServer(ctx)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestStopGRPCServer(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(*testing.T) *Server
	}{
		{
			name: "StopNilServer",
			setupServer: func(t *testing.T) *Server {
				cfg := createTestConfig(t)
				srv, err := NewServer(cfg)
				require.NoError(t, err)
				// Don't start gRPC server
				return srv
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			defer func() {
				if srv.indexManager != nil {
					srv.indexManager.Close()
				}
				if srv.storage != nil {
					srv.storage.Close()
				}
				cleanupTestData(t, srv.config)
			}()

			// Should not panic
			srv.stopGRPCServer()
		})
	}
}

func TestStopIndexes(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(*testing.T) *Server
		wantErr     bool
	}{
		{
			name: "StopValidIndexManager",
			setupServer: func(t *testing.T) *Server {
				cfg := createTestConfig(t)
				srv, err := NewServer(cfg)
				require.NoError(t, err)

				err = srv.startStorage()
				require.NoError(t, err)
				err = srv.startIndexes()
				require.NoError(t, err)

				return srv
			},
			wantErr: false,
		},
		{
			name: "StopNilIndexManager",
			setupServer: func(t *testing.T) *Server {
				cfg := createTestConfig(t)
				srv, err := NewServer(cfg)
				require.NoError(t, err)
				// Don't start indexes
				return srv
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			defer func() {
				if srv.storage != nil {
					srv.storage.Close()
				}
				cleanupTestData(t, srv.config)
			}()

			err := srv.stopIndexes()

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestStopRaftNode(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(*testing.T) *Server
		wantErr     bool
	}{
		{
			name: "StopNilRaftNode",
			setupServer: func(t *testing.T) *Server {
				cfg := createTestConfig(t)
				srv, err := NewServer(cfg)
				require.NoError(t, err)
				// Don't start Raft
				return srv
			},
			wantErr: false,
		},
		// Note: Testing actual Raft node stop requires full Raft setup
		// which is complex and time-consuming for unit tests
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			defer cleanupTestData(t, srv.config)

			err := srv.stopRaftNode()

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
