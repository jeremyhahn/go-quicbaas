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

// Package interfaces provides interface definitions for all major QuicBaaS components.
//
// # Overview
//
// This package defines interfaces for dependency injection and comprehensive testing.
// By programming to interfaces rather than concrete implementations, QuicBaaS achieves:
//
//   - Testability: Components can be easily mocked for unit testing
//   - Flexibility: Implementations can be swapped without affecting consumers
//   - Decoupling: Reduces tight coupling between packages
//   - Documentation: Interfaces serve as contracts and documentation
//
// # Architecture Layers
//
// The interfaces are organized into several layers:
//
// Storage Layer:
//   - Storage: Key-value storage engine (e.g., PebbleDB)
//
// Consensus Layer:
//   - RaftNode: Raft consensus and replication
//   - StateMachine: Replicated state machine
//   - SnapshotManager: Snapshot creation and recovery
//
// API Layer:
//   - GRPCServer: Protocol Buffers / gRPC API
//   - RESTServer: HTTP REST API
//   - HTTPHandler: HTTP request handlers
//   - Middleware: HTTP middleware components
//
// Infrastructure Layer:
//   - Logger: Structured logging
//   - Config: Configuration management
//   - ConfigLoader: Configuration loading from multiple sources
//   - MetricsCollector: Metrics collection
//   - MetricsRegistry: Metrics registration
//   - MetricsExporter: Metrics export for monitoring
//
// # Usage Patterns
//
// Dependency Injection:
//
//	type Service struct {
//	    storage interfaces.Storage
//	    logger  interfaces.Logger
//	}
//
//	func NewService(storage interfaces.Storage, logger interfaces.Logger) *Service {
//	    return &Service{
//	        storage: storage,
//	        logger:  logger,
//	    }
//	}
//
// Testing with Mocks:
//
//	type mockStorage struct {
//	    interfaces.Storage
//	    getData map[string][]byte
//	}
//
//	func (m *mockStorage) Get(key []byte) ([]byte, error) {
//	    if val, ok := m.getData[string(key)]; ok {
//	        return val, nil
//	    }
//	    return nil, errors.ErrNotFound
//	}
//
//	func TestService(t *testing.T) {
//	    mock := &mockStorage{
//	        getData: map[string][]byte{
//	            "test": []byte("value"),
//	        },
//	    }
//	    service := NewService(mock, &mockLogger{})
//	    // Test service operations...
//	}
//
// Interface Composition:
//
//	// Extend interfaces with additional capabilities
//	type StorageWithMetrics interface {
//	    interfaces.Storage
//	    Metrics() interface{}
//	}
//
// # Design Principles
//
//   - Interface Segregation: Interfaces are small and focused
//   - Accept Interfaces, Return Structs: Functions accept interfaces but return concrete types
//   - Context Propagation: All blocking operations accept context.Context
//   - Error Handling: All operations return explicit errors
//   - Resource Management: All resources have explicit Close() methods
//
// # Testing Strategy
//
// Each interface should have:
//
//  1. Unit tests with mocked dependencies
//  2. Integration tests with real implementations
//  3. Contract tests ensuring interface compliance
//  4. Benchmark tests for performance-critical operations
//
// Example test structure:
//
//	// Unit test with mock
//	func TestServiceWithMock(t *testing.T) { ... }
//
//	// Integration test with real implementation
//	func TestServiceWithPebble(t *testing.T) { ... }
//
//	// Contract test ensuring implementation satisfies interface
//	func TestPebbleImplementsStorage(t *testing.T) {
//	    var _ interfaces.Storage = (*pebble.Storage)(nil)
//	}
//
//	// Benchmark test
//	func BenchmarkStorageGet(b *testing.B) { ... }
//
// # Thread Safety
//
// All interfaces define thread-safe contracts. Implementations must guarantee:
//
//   - Concurrent reads are safe
//   - Concurrent writes are serialized or use optimistic locking
//   - Resource lifecycle (Close) is safe with concurrent operations
//
// # Error Handling
//
// All interfaces use the QuicBaaS error package (pkg/errors) which provides:
//
//   - Error codes for categorization
//   - Error wrapping for context
//   - Retry hints for transient failures
//   - Stack traces for debugging
//
// Example error handling:
//
//	if err := storage.Get(key); err != nil {
//	    if errors.IsNotFound(err) {
//	        // Handle missing key
//	    } else if errors.IsRetryable(err) {
//	        // Retry operation
//	    } else {
//	        // Fatal error
//	    }
//	}
//
// # Performance Considerations
//
//   - Zero-allocation paths for hot paths
//   - Efficient byte slice handling (avoid copies when possible)
//   - Batch operations for reducing round trips
//   - Context-aware operations for cancellation
//   - Resource pooling where appropriate
//
// # Future Extensions
//
// New interfaces should follow these guidelines:
//
//  1. Start small and focused
//  2. Add context.Context to blocking operations
//  3. Return errors explicitly
//  4. Include comprehensive godoc comments
//  5. Provide usage examples in tests
//  6. Consider backwards compatibility
package interfaces
