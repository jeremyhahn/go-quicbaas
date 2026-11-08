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

package mocks

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMockStorage_Metrics(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		m := NewMockStorage()
		metrics := m.Metrics()
		assert.NotNil(t, metrics)
		assert.True(t, m.MetricsCalled)
	})

	t.Run("nil function", func(t *testing.T) {
		m := &MockStorage{}
		metrics := m.Metrics()
		assert.Nil(t, metrics)
		assert.True(t, m.MetricsCalled)
	})
}

func TestMockStorage_Close(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		m := NewMockStorage()
		err := m.Close()
		assert.NoError(t, err)
		assert.True(t, m.CloseCalled)
	})

	t.Run("nil function", func(t *testing.T) {
		m := &MockStorage{}
		err := m.Close()
		assert.NoError(t, err)
		assert.True(t, m.CloseCalled)
	})
}

func TestMockIndexManager_Close(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		m := NewMockIndexManager()
		err := m.Close()
		assert.NoError(t, err)
		assert.True(t, m.CloseCalled)
	})

	t.Run("nil function", func(t *testing.T) {
		m := &MockIndexManager{}
		err := m.Close()
		assert.NoError(t, err)
		assert.True(t, m.CloseCalled)
	})
}

func TestMockDiscoveryManager_GetPeers(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		m := NewMockDiscoveryManager()
		peers, err := m.GetPeers()
		assert.NoError(t, err)
		assert.Len(t, peers, 2)
		assert.True(t, m.GetPeersCalled)
	})

	t.Run("nil function", func(t *testing.T) {
		m := &MockDiscoveryManager{}
		peers, err := m.GetPeers()
		assert.NoError(t, err)
		assert.Nil(t, peers)
		assert.True(t, m.GetPeersCalled)
	})
}

func TestMockDiscoveryManager_Close(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		m := NewMockDiscoveryManager()
		err := m.Close()
		assert.NoError(t, err)
		assert.True(t, m.CloseCalled)
	})

	t.Run("nil function", func(t *testing.T) {
		m := &MockDiscoveryManager{}
		err := m.Close()
		assert.NoError(t, err)
		assert.True(t, m.CloseCalled)
	})
}

func TestMockBootstrapper_Bootstrap(t *testing.T) {
	ctx := context.Background()

	t.Run("default function", func(t *testing.T) {
		m := NewMockBootstrapper()
		err := m.Bootstrap(ctx)
		assert.NoError(t, err)
		assert.True(t, m.BootstrapCalled)
	})

	t.Run("nil function", func(t *testing.T) {
		m := &MockBootstrapper{}
		err := m.Bootstrap(ctx)
		assert.NoError(t, err)
		assert.True(t, m.BootstrapCalled)
	})
}

func TestMockGRPCServer_Serve(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		m := NewMockGRPCServer()
		err := m.Serve(nil)
		assert.NoError(t, err)
		assert.True(t, m.ServeCalled)
	})

	t.Run("nil function", func(t *testing.T) {
		m := &MockGRPCServer{}
		err := m.Serve(nil)
		assert.NoError(t, err)
		assert.True(t, m.ServeCalled)
	})
}

func TestMockGRPCServer_GracefulStop(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		m := NewMockGRPCServer()
		m.GracefulStop()
		assert.True(t, m.GracefulStopCalled)
	})

	t.Run("nil function", func(t *testing.T) {
		m := &MockGRPCServer{}
		m.GracefulStop()
		assert.True(t, m.GracefulStopCalled)
	})
}

func TestMockGRPCServer_Stop(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		m := NewMockGRPCServer()
		m.Stop()
		assert.True(t, m.StopCalled)
	})

	t.Run("nil function", func(t *testing.T) {
		m := &MockGRPCServer{}
		m.Stop()
		assert.True(t, m.StopCalled)
	})
}

func TestMockGRPCAPI_SetShardRouter(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		m := NewMockGRPCAPI()
		m.SetShardRouter(nil)
		assert.True(t, m.SetShardRouterCalled)
	})

	t.Run("nil function", func(t *testing.T) {
		m := &MockGRPCAPI{}
		m.SetShardRouter(nil)
		assert.True(t, m.SetShardRouterCalled)
	})
}

func TestMockRESTServer_Start(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		m := NewMockRESTServer()
		err := m.Start(":8080")
		assert.NoError(t, err)
		assert.True(t, m.StartCalled)
		assert.True(t, m.IsStarted())
	})

	t.Run("nil function", func(t *testing.T) {
		m := &MockRESTServer{}
		err := m.Start(":8080")
		assert.NoError(t, err)
		assert.True(t, m.StartCalled)
	})
}

func TestMockRESTServer_Shutdown(t *testing.T) {
	ctx := context.Background()

	t.Run("default function", func(t *testing.T) {
		m := NewMockRESTServer()
		err := m.Shutdown(ctx)
		assert.NoError(t, err)
		assert.True(t, m.ShutdownCalled)
		assert.True(t, m.IsClosed())
	})

	t.Run("nil function", func(t *testing.T) {
		m := &MockRESTServer{}
		err := m.Shutdown(ctx)
		assert.NoError(t, err)
		assert.True(t, m.ShutdownCalled)
	})
}

func TestMockRESTServer_IsStarted(t *testing.T) {
	t.Run("default not started", func(t *testing.T) {
		m := NewMockRESTServer()
		assert.False(t, m.IsStarted())
	})

	t.Run("nil function fallback", func(t *testing.T) {
		m := &MockRESTServer{started: true}
		assert.True(t, m.IsStarted())
	})
}

func TestMockRESTServer_IsClosed(t *testing.T) {
	t.Run("default not closed", func(t *testing.T) {
		m := NewMockRESTServer()
		assert.False(t, m.IsClosed())
	})

	t.Run("nil function fallback", func(t *testing.T) {
		m := &MockRESTServer{closed: true}
		assert.True(t, m.IsClosed())
	})
}

func TestMockRESTServer_SetShardRouter(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		m := NewMockRESTServer()
		m.SetShardRouter(nil)
		assert.True(t, m.SetShardRouterCalled)
	})

	t.Run("nil function", func(t *testing.T) {
		m := &MockRESTServer{}
		m.SetShardRouter(nil)
		assert.True(t, m.SetShardRouterCalled)
	})
}

func TestMockHealthChecker_CheckGRPC(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		m := NewMockHealthChecker()
		status := m.CheckGRPC()
		assert.Equal(t, "grpc", status.Name)
	})

	t.Run("nil function", func(t *testing.T) {
		m := &MockHealthChecker{}
		status := m.CheckGRPC()
		assert.Empty(t, status.Name)
	})
}

func TestMockHealthChecker_CheckREST(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		m := NewMockHealthChecker()
		status := m.CheckREST()
		assert.Equal(t, "rest", status.Name)
	})

	t.Run("nil function", func(t *testing.T) {
		m := &MockHealthChecker{}
		status := m.CheckREST()
		assert.Empty(t, status.Name)
	})
}

func TestMockHealthChecker_GetOverallHealth(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		m := NewMockHealthChecker()
		health := m.GetOverallHealth()
		assert.NotNil(t, health)
	})

	t.Run("nil function", func(t *testing.T) {
		m := &MockHealthChecker{}
		health := m.GetOverallHealth()
		assert.Nil(t, health)
	})
}

func TestMockHealthChecker_IsHealthy(t *testing.T) {
	t.Run("default function returns true", func(t *testing.T) {
		m := NewMockHealthChecker()
		assert.True(t, m.IsHealthy())
	})

	t.Run("nil function returns false", func(t *testing.T) {
		m := &MockHealthChecker{}
		assert.False(t, m.IsHealthy())
	})
}

func TestMockHealthChecker_IsReady(t *testing.T) {
	t.Run("default function returns true", func(t *testing.T) {
		m := NewMockHealthChecker()
		assert.True(t, m.IsReady())
	})

	t.Run("nil function returns false", func(t *testing.T) {
		m := &MockHealthChecker{}
		assert.False(t, m.IsReady())
	})
}

func TestMockShardRouter_SetEntityShard(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		m := NewMockShardRouter()
		m.SetEntityShard("users", 2)
		assert.Equal(t, uint64(2), m.EntityShards["users"])
	})

	t.Run("nil function", func(t *testing.T) {
		m := &MockShardRouter{EntityShards: make(map[string]uint64)}
		m.SetEntityShard("users", 2)
		assert.Equal(t, uint64(2), m.EntityShards["users"])
	})
}

func TestMockShardRouter_GetShardForKey(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		m := NewMockShardRouter()
		shard := m.GetShardForKey([]byte("test"))
		assert.Equal(t, uint64(1), shard)
	})

	t.Run("nil function", func(t *testing.T) {
		m := &MockShardRouter{}
		shard := m.GetShardForKey([]byte("test"))
		assert.Equal(t, uint64(1), shard)
	})
}

func TestMockShardRouter_GetShardForEntity(t *testing.T) {
	t.Run("from entity shards map", func(t *testing.T) {
		m := NewMockShardRouter()
		m.EntityShards["users"] = 5
		shard := m.GetShardForEntity("users")
		assert.Equal(t, uint64(5), shard)
	})

	t.Run("default function fallback", func(t *testing.T) {
		m := NewMockShardRouter()
		shard := m.GetShardForEntity("unknown")
		assert.Equal(t, uint64(1), shard)
	})

	t.Run("nil function fallback", func(t *testing.T) {
		m := &MockShardRouter{EntityShards: make(map[string]uint64)}
		shard := m.GetShardForEntity("unknown")
		assert.Equal(t, uint64(1), shard)
	})
}

func TestMockShardRouter_ListShards(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		m := NewMockShardRouter()
		shards := m.ListShards()
		assert.Equal(t, []uint64{1}, shards)
	})

	t.Run("nil function fallback", func(t *testing.T) {
		m := &MockShardRouter{Shards: []uint64{1, 2, 3}}
		shards := m.ListShards()
		assert.Equal(t, []uint64{1, 2, 3}, shards)
	})
}

func TestMockLogger_RecordsEachLevel(t *testing.T) {
	tests := []struct {
		name string
		log  func(m *MockLogger)
		logs func(m *MockLogger) []string
	}{
		{
			name: "debug",
			log:  func(m *MockLogger) { m.Debug("plain"); m.Debugf("format %s", "arg") },
			logs: func(m *MockLogger) []string { return m.DebugLogs },
		},
		{
			name: "info",
			log:  func(m *MockLogger) { m.Info("plain"); m.Infof("format %s", "arg") },
			logs: func(m *MockLogger) []string { return m.InfoLogs },
		},
		{
			name: "warn",
			log:  func(m *MockLogger) { m.Warn("plain"); m.Warnf("format %s", "arg") },
			logs: func(m *MockLogger) []string { return m.WarnLogs },
		},
		{
			name: "error",
			log:  func(m *MockLogger) { m.Error("plain"); m.Errorf("format %s", "arg") },
			logs: func(m *MockLogger) []string { return m.ErrorLogs },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMockLogger()
			tt.log(m)
			assert.Equal(t, []string{"plain", "format %s"}, tt.logs(m))
		})
	}
}

func TestMockLogger_WithError(t *testing.T) {
	m := NewMockLogger()
	err := errors.New("test error")
	result := m.WithError(err)
	assert.Equal(t, m, result)
	assert.Equal(t, err, m.Err)
}

func TestMockLogger_WithField(t *testing.T) {
	m := NewMockLogger()
	result := m.WithField("key", "value")
	assert.Equal(t, m, result)
	assert.Equal(t, "value", m.Fields["key"])
}

func TestMockListener_Close(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		m := NewMockListener()
		err := m.Close()
		assert.NoError(t, err)
		assert.True(t, m.CloseCalled)
		assert.True(t, m.Closed)
	})

	t.Run("nil function", func(t *testing.T) {
		m := &MockListener{}
		err := m.Close()
		assert.NoError(t, err)
		assert.True(t, m.CloseCalled)
	})
}

func TestMockListener_Addr(t *testing.T) {
	t.Run("default function", func(t *testing.T) {
		m := NewMockListener()
		addr := m.Addr()
		assert.NotNil(t, addr)
		assert.Equal(t, "tcp", addr.Network())
		assert.Equal(t, "127.0.0.1:8080", addr.String())
	})

	t.Run("nil function fallback", func(t *testing.T) {
		m := &MockListener{}
		addr := m.Addr()
		assert.NotNil(t, addr)
		assert.Equal(t, "tcp", addr.Network())
	})
}

func TestMockListener_Accept(t *testing.T) {
	t.Run("nil function returns nil", func(t *testing.T) {
		m := &MockListener{}
		conn, err := m.Accept()
		assert.NoError(t, err)
		assert.Nil(t, conn)
		assert.True(t, m.AcceptCalled)
	})

	t.Run("custom function with error", func(t *testing.T) {
		testErr := errors.New("accept error")
		m := &MockListener{
			AcceptFunc: func() (net.Conn, error) {
				return nil, testErr
			},
		}
		conn, err := m.Accept()
		assert.Equal(t, testErr, err)
		assert.Nil(t, conn)
	})
}
