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

// Package mocks provides mock implementations for server interfaces.
// These mocks enable comprehensive unit testing of the Server package
// without requiring real infrastructure.
package mocks

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/server"
)

// =============================================================================
// MockStorage - Mocks StorageInterface
// =============================================================================

// MockStorage provides a mock implementation of StorageInterface.
type MockStorage struct {
	mu sync.RWMutex

	// Configurable behavior
	MetricsFunc func() interface{}
	CloseFunc   func() error

	// Call tracking
	MetricsCalled bool
	CloseCalled   bool
}

// NewMockStorage creates a new MockStorage with sensible defaults.
func NewMockStorage() *MockStorage {
	return &MockStorage{
		MetricsFunc: func() interface{} {
			return map[string]int64{"reads": 0, "writes": 0}
		},
		CloseFunc: func() error {
			return nil
		},
	}
}

// Metrics returns storage metrics.
func (m *MockStorage) Metrics() interface{} {
	m.mu.Lock()
	m.MetricsCalled = true
	m.mu.Unlock()

	if m.MetricsFunc != nil {
		return m.MetricsFunc()
	}
	return nil
}

// Close shuts down the mock storage.
func (m *MockStorage) Close() error {
	m.mu.Lock()
	m.CloseCalled = true
	m.mu.Unlock()

	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// =============================================================================
// MockIndexManager - Mocks IndexManagerInterface
// =============================================================================

// MockIndexManager provides a mock implementation of IndexManagerInterface.
type MockIndexManager struct {
	mu sync.RWMutex

	// Configurable behavior
	CloseFunc func() error

	// Call tracking
	CloseCalled bool
}

// NewMockIndexManager creates a new MockIndexManager with sensible defaults.
func NewMockIndexManager() *MockIndexManager {
	return &MockIndexManager{
		CloseFunc: func() error {
			return nil
		},
	}
}

// Close shuts down the mock index manager.
func (m *MockIndexManager) Close() error {
	m.mu.Lock()
	m.CloseCalled = true
	m.mu.Unlock()

	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// =============================================================================
// MockDiscoveryManager - Mocks DiscoveryManagerInterface
// =============================================================================

// MockDiscoveryManager provides a mock implementation of DiscoveryManagerInterface.
type MockDiscoveryManager struct {
	mu sync.RWMutex

	// Configurable behavior
	GetPeersFunc func() ([]string, error)
	CloseFunc    func() error

	// Call tracking
	GetPeersCalled bool
	CloseCalled    bool
}

// NewMockDiscoveryManager creates a new MockDiscoveryManager with sensible defaults.
func NewMockDiscoveryManager() *MockDiscoveryManager {
	return &MockDiscoveryManager{
		GetPeersFunc: func() ([]string, error) {
			return []string{"peer1:8080", "peer2:8080"}, nil
		},
		CloseFunc: func() error {
			return nil
		},
	}
}

// GetPeers returns discovered peers.
func (m *MockDiscoveryManager) GetPeers() ([]string, error) {
	m.mu.Lock()
	m.GetPeersCalled = true
	m.mu.Unlock()

	if m.GetPeersFunc != nil {
		return m.GetPeersFunc()
	}
	return nil, nil
}

// Close shuts down the mock discovery manager.
func (m *MockDiscoveryManager) Close() error {
	m.mu.Lock()
	m.CloseCalled = true
	m.mu.Unlock()

	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// =============================================================================
// MockBootstrapper - Mocks BootstrapperInterface
// =============================================================================

// MockBootstrapper provides a mock implementation of BootstrapperInterface.
type MockBootstrapper struct {
	mu sync.RWMutex

	// Configurable behavior
	BootstrapFunc func(ctx context.Context) error

	// Call tracking
	BootstrapCalled bool
}

// NewMockBootstrapper creates a new MockBootstrapper with sensible defaults.
func NewMockBootstrapper() *MockBootstrapper {
	return &MockBootstrapper{
		BootstrapFunc: func(ctx context.Context) error {
			return nil
		},
	}
}

// Bootstrap performs cluster initialization.
func (m *MockBootstrapper) Bootstrap(ctx context.Context) error {
	m.mu.Lock()
	m.BootstrapCalled = true
	m.mu.Unlock()

	if m.BootstrapFunc != nil {
		return m.BootstrapFunc(ctx)
	}
	return nil
}

// =============================================================================
// MockGRPCServer - Mocks GRPCServerInterface
// =============================================================================

// MockGRPCServer provides a mock implementation of GRPCServerInterface.
type MockGRPCServer struct {
	mu sync.RWMutex

	// Configurable behavior
	ServeFunc        func(lis net.Listener) error
	GracefulStopFunc func()
	StopFunc         func()

	// Call tracking
	ServeCalled        bool
	GracefulStopCalled bool
	StopCalled         bool
}

// NewMockGRPCServer creates a new MockGRPCServer with sensible defaults.
func NewMockGRPCServer() *MockGRPCServer {
	return &MockGRPCServer{
		ServeFunc: func(lis net.Listener) error {
			return nil
		},
		GracefulStopFunc: func() {},
		StopFunc:         func() {},
	}
}

// Serve starts serving gRPC requests.
func (m *MockGRPCServer) Serve(lis net.Listener) error {
	m.mu.Lock()
	m.ServeCalled = true
	m.mu.Unlock()

	if m.ServeFunc != nil {
		return m.ServeFunc(lis)
	}
	return nil
}

// GracefulStop gracefully stops the server.
func (m *MockGRPCServer) GracefulStop() {
	m.mu.Lock()
	m.GracefulStopCalled = true
	m.mu.Unlock()

	if m.GracefulStopFunc != nil {
		m.GracefulStopFunc()
	}
}

// Stop immediately stops the server.
func (m *MockGRPCServer) Stop() {
	m.mu.Lock()
	m.StopCalled = true
	m.mu.Unlock()

	if m.StopFunc != nil {
		m.StopFunc()
	}
}

// =============================================================================
// MockGRPCAPI - Mocks GRPCAPIInterface
// =============================================================================

// MockGRPCAPI provides a mock implementation of GRPCAPIInterface.
type MockGRPCAPI struct {
	mu sync.RWMutex

	// Configurable behavior
	SetShardRouterFunc func(router server.ShardRouterInterface)

	// Call tracking
	SetShardRouterCalled bool
	LastRouter           server.ShardRouterInterface
}

// NewMockGRPCAPI creates a new MockGRPCAPI with sensible defaults.
func NewMockGRPCAPI() *MockGRPCAPI {
	return &MockGRPCAPI{
		SetShardRouterFunc: func(router server.ShardRouterInterface) {},
	}
}

// SetShardRouter configures the shard router.
func (m *MockGRPCAPI) SetShardRouter(router server.ShardRouterInterface) {
	m.mu.Lock()
	m.SetShardRouterCalled = true
	m.LastRouter = router
	m.mu.Unlock()

	if m.SetShardRouterFunc != nil {
		m.SetShardRouterFunc(router)
	}
}

// =============================================================================
// MockRESTServer - Mocks RESTServerInterface
// =============================================================================

// MockRESTServer provides a mock implementation of RESTServerInterface.
type MockRESTServer struct {
	mu sync.RWMutex

	// Configurable behavior
	StartFunc          func(address string) error
	ShutdownFunc       func(ctx context.Context) error
	IsStartedFunc      func() bool
	IsClosedFunc       func() bool
	SetShardRouterFunc func(router server.ShardRouterInterface)

	// State tracking
	started bool
	closed  bool

	// Call tracking
	StartCalled          bool
	ShutdownCalled       bool
	SetShardRouterCalled bool
}

// NewMockRESTServer creates a new MockRESTServer with sensible defaults.
func NewMockRESTServer() *MockRESTServer {
	m := &MockRESTServer{}
	m.StartFunc = func(address string) error {
		m.started = true
		return nil
	}
	m.ShutdownFunc = func(ctx context.Context) error {
		m.closed = true
		return nil
	}
	m.IsStartedFunc = func() bool {
		return m.started
	}
	m.IsClosedFunc = func() bool {
		return m.closed
	}
	m.SetShardRouterFunc = func(router server.ShardRouterInterface) {}
	return m
}

// Start starts the REST server.
func (m *MockRESTServer) Start(address string) error {
	m.mu.Lock()
	m.StartCalled = true
	m.mu.Unlock()

	if m.StartFunc != nil {
		return m.StartFunc(address)
	}
	return nil
}

// Shutdown gracefully shuts down the server.
func (m *MockRESTServer) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.ShutdownCalled = true
	m.mu.Unlock()

	if m.ShutdownFunc != nil {
		return m.ShutdownFunc(ctx)
	}
	return nil
}

// IsStarted returns true if the server is running.
func (m *MockRESTServer) IsStarted() bool {
	if m.IsStartedFunc != nil {
		return m.IsStartedFunc()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}

// IsClosed returns true if the server has been shut down.
func (m *MockRESTServer) IsClosed() bool {
	if m.IsClosedFunc != nil {
		return m.IsClosedFunc()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.closed
}

// SetShardRouter configures the shard router.
func (m *MockRESTServer) SetShardRouter(router server.ShardRouterInterface) {
	m.mu.Lock()
	m.SetShardRouterCalled = true
	m.mu.Unlock()

	if m.SetShardRouterFunc != nil {
		m.SetShardRouterFunc(router)
	}
}

// =============================================================================
// MockHealthChecker - Mocks HealthCheckerInterface
// =============================================================================

// MockHealthChecker provides a mock implementation of HealthCheckerInterface.
type MockHealthChecker struct {

	// Configurable behavior
	CheckGRPCFunc        func() server.ComponentHealthStatus
	CheckRESTFunc        func() server.ComponentHealthStatus
	GetOverallHealthFunc func() *server.OverallHealthStatus
	IsHealthyFunc        func() bool
	IsReadyFunc          func() bool
}

// NewMockHealthChecker creates a new MockHealthChecker with sensible defaults.
func NewMockHealthChecker() *MockHealthChecker {
	return &MockHealthChecker{
		CheckGRPCFunc: func() server.ComponentHealthStatus {
			return server.ComponentHealthStatus{
				Name:      "grpc",
				Status:    server.HealthStatusTypeHealthy,
				Message:   "gRPC is healthy",
				LastCheck: time.Now().Unix(),
			}
		},
		CheckRESTFunc: func() server.ComponentHealthStatus {
			return server.ComponentHealthStatus{
				Name:      "rest",
				Status:    server.HealthStatusTypeHealthy,
				Message:   "REST is healthy",
				LastCheck: time.Now().Unix(),
			}
		},
		GetOverallHealthFunc: func() *server.OverallHealthStatus {
			return &server.OverallHealthStatus{
				Status:     server.HealthStatusTypeHealthy,
				Timestamp:  time.Now().Unix(),
				Uptime:     0,
				Components: make(map[string]server.ComponentHealthStatus),
			}
		},
		IsHealthyFunc: func() bool {
			return true
		},
		IsReadyFunc: func() bool {
			return true
		},
	}
}

// CheckGRPC returns gRPC health status.
func (m *MockHealthChecker) CheckGRPC() server.ComponentHealthStatus {
	if m.CheckGRPCFunc != nil {
		return m.CheckGRPCFunc()
	}
	return server.ComponentHealthStatus{}
}

// CheckREST returns REST health status.
func (m *MockHealthChecker) CheckREST() server.ComponentHealthStatus {
	if m.CheckRESTFunc != nil {
		return m.CheckRESTFunc()
	}
	return server.ComponentHealthStatus{}
}

// GetOverallHealth returns overall health status.
func (m *MockHealthChecker) GetOverallHealth() *server.OverallHealthStatus {
	if m.GetOverallHealthFunc != nil {
		return m.GetOverallHealthFunc()
	}
	return nil
}

// IsHealthy returns true if all components are healthy.
func (m *MockHealthChecker) IsHealthy() bool {
	if m.IsHealthyFunc != nil {
		return m.IsHealthyFunc()
	}
	return false
}

// IsReady returns true if the server is ready to serve.
func (m *MockHealthChecker) IsReady() bool {
	if m.IsReadyFunc != nil {
		return m.IsReadyFunc()
	}
	return false
}

// =============================================================================
// MockShardRouter - Mocks ShardRouterInterface
// =============================================================================

// MockShardRouter provides a mock implementation of ShardRouterInterface.
type MockShardRouter struct {
	mu sync.RWMutex

	// Configurable behavior
	SetEntityShardFunc    func(entityType string, shardID uint64)
	GetShardForKeyFunc    func(key []byte) uint64
	GetShardForEntityFunc func(entityType string) uint64
	ListShardsFunc        func() []uint64

	// State tracking
	EntityShards map[string]uint64
	Shards       []uint64
}

// NewMockShardRouter creates a new MockShardRouter with sensible defaults.
func NewMockShardRouter() *MockShardRouter {
	return &MockShardRouter{
		EntityShards:          make(map[string]uint64),
		Shards:                []uint64{1},
		SetEntityShardFunc:    func(entityType string, shardID uint64) {},
		GetShardForKeyFunc:    func(key []byte) uint64 { return 1 },
		GetShardForEntityFunc: func(entityType string) uint64 { return 1 },
		ListShardsFunc:        func() []uint64 { return []uint64{1} },
	}
}

// SetEntityShard pins an entity type to a specific shard.
func (m *MockShardRouter) SetEntityShard(entityType string, shardID uint64) {
	m.mu.Lock()
	m.EntityShards[entityType] = shardID
	m.mu.Unlock()

	if m.SetEntityShardFunc != nil {
		m.SetEntityShardFunc(entityType, shardID)
	}
}

// GetShardForKey returns the shard ID for the given key.
func (m *MockShardRouter) GetShardForKey(key []byte) uint64 {
	if m.GetShardForKeyFunc != nil {
		return m.GetShardForKeyFunc(key)
	}
	return 1
}

// GetShardForEntity returns the shard ID for the given entity type.
func (m *MockShardRouter) GetShardForEntity(entityType string) uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if shardID, ok := m.EntityShards[entityType]; ok {
		return shardID
	}

	if m.GetShardForEntityFunc != nil {
		return m.GetShardForEntityFunc(entityType)
	}
	return 1
}

// ListShards returns all shard IDs.
func (m *MockShardRouter) ListShards() []uint64 {
	if m.ListShardsFunc != nil {
		return m.ListShardsFunc()
	}
	return m.Shards
}

// =============================================================================
// MockLogger - Mocks LoggerInterface
// =============================================================================

// MockLogger provides a mock implementation of LoggerInterface.
type MockLogger struct {
	mu sync.RWMutex

	// Log storage
	DebugLogs []string
	InfoLogs  []string
	WarnLogs  []string
	ErrorLogs []string
	Fields    map[string]interface{}
	Err       error
}

// NewMockLogger creates a new MockLogger.
func NewMockLogger() *MockLogger {
	return &MockLogger{
		DebugLogs: make([]string, 0),
		InfoLogs:  make([]string, 0),
		WarnLogs:  make([]string, 0),
		ErrorLogs: make([]string, 0),
		Fields:    make(map[string]interface{}),
	}
}

// Debug logs a debug message.
func (m *MockLogger) Debug(msg string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DebugLogs = append(m.DebugLogs, msg)
}

// Debugf logs a formatted debug message.
func (m *MockLogger) Debugf(format string, args ...interface{}) {
	m.Debug(format)
}

// Info logs an info message.
func (m *MockLogger) Info(msg string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.InfoLogs = append(m.InfoLogs, msg)
}

// Infof logs a formatted info message.
func (m *MockLogger) Infof(format string, args ...interface{}) {
	m.Info(format)
}

// Warn logs a warning message.
func (m *MockLogger) Warn(msg string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WarnLogs = append(m.WarnLogs, msg)
}

// Warnf logs a formatted warning message.
func (m *MockLogger) Warnf(format string, args ...interface{}) {
	m.Warn(format)
}

// Error logs an error message.
func (m *MockLogger) Error(msg string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ErrorLogs = append(m.ErrorLogs, msg)
}

// Errorf logs a formatted error message.
func (m *MockLogger) Errorf(format string, args ...interface{}) {
	m.Error(format)
}

// WithError returns a logger with the error attached.
func (m *MockLogger) WithError(err error) server.LoggerInterface {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Err = err
	return m
}

// WithField returns a logger with the field attached.
func (m *MockLogger) WithField(key string, value interface{}) server.LoggerInterface {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Fields[key] = value
	return m
}

// =============================================================================
// MockListener - Mocks net.Listener
// =============================================================================

// MockListener provides a mock implementation of net.Listener.
type MockListener struct {
	mu sync.RWMutex

	// Configurable behavior
	AcceptFunc func() (net.Conn, error)
	CloseFunc  func() error
	AddrFunc   func() net.Addr

	// State tracking
	AcceptCalled bool
	CloseCalled  bool
	Closed       bool
}

// NewMockListener creates a new MockListener with sensible defaults.
func NewMockListener() *MockListener {
	return &MockListener{
		AcceptFunc: func() (net.Conn, error) {
			// Block forever or until closed
			select {}
		},
		CloseFunc: func() error {
			return nil
		},
		AddrFunc: func() net.Addr {
			return &mockAddr{network: "tcp", addr: "127.0.0.1:8080"}
		},
	}
}

// Accept accepts a connection.
func (m *MockListener) Accept() (net.Conn, error) {
	m.mu.Lock()
	m.AcceptCalled = true
	m.mu.Unlock()

	if m.AcceptFunc != nil {
		return m.AcceptFunc()
	}
	return nil, nil
}

// Close closes the listener.
func (m *MockListener) Close() error {
	m.mu.Lock()
	m.CloseCalled = true
	m.Closed = true
	m.mu.Unlock()

	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// Addr returns the listener's address.
func (m *MockListener) Addr() net.Addr {
	if m.AddrFunc != nil {
		return m.AddrFunc()
	}
	return &mockAddr{network: "tcp", addr: "127.0.0.1:8080"}
}

type mockAddr struct {
	network string
	addr    string
}

func (a *mockAddr) Network() string { return a.network }
func (a *mockAddr) String() string  { return a.addr }
