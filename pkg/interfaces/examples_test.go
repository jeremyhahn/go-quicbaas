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

package interfaces_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// Example mock implementations demonstrating interface usage

// mockStorage implements interfaces.Storage for testing.
type mockStorage struct {
	data    map[string][]byte
	batches int
	closed  bool
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		data: make(map[string][]byte),
	}
}

func (m *mockStorage) Get(key []byte) ([]byte, error) {
	if m.closed {
		return nil, errors.Newf(errors.ErrStorage, "mockStorage.Get", "storage is closed")
	}
	if val, ok := m.data[string(key)]; ok {
		return val, nil
	}
	return nil, errors.Newf(errors.ErrNotFound, "mockStorage.Get", "key not found")
}

func (m *mockStorage) Set(key, value []byte) error {
	if m.closed {
		return errors.Newf(errors.ErrStorage, "mockStorage.Set", "storage is closed")
	}
	m.data[string(key)] = value
	return nil
}

func (m *mockStorage) Delete(key []byte) error {
	if m.closed {
		return errors.Newf(errors.ErrStorage, "mockStorage.Delete", "storage is closed")
	}
	delete(m.data, string(key))
	return nil
}

func (m *mockStorage) GetWithContext(ctx context.Context, key []byte) ([]byte, error) {
	return m.Get(key)
}

func (m *mockStorage) SetWithContext(ctx context.Context, key, value []byte) error {
	return m.Set(key, value)
}

func (m *mockStorage) Batch() storage.Batch {
	m.batches++
	return &mockBatch{storage: m}
}

func (m *mockStorage) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	return &mockIterator{data: m.data}
}

func (m *mockStorage) Snapshot() (storage.Snapshot, error) {
	return &mockSnapshot{data: m.data}, nil
}

func (m *mockStorage) Compact(start, end []byte) error {
	return nil
}

func (m *mockStorage) Flush() error {
	return nil
}

func (m *mockStorage) Close() error {
	m.closed = true
	return nil
}

// mockBatch implements storage.Batch for testing.
type mockBatch struct {
	storage *mockStorage
	ops     []func() error
	closed  bool
}

func (m *mockBatch) Set(key, value []byte) error {
	m.ops = append(m.ops, func() error {
		return m.storage.Set(key, value)
	})
	return nil
}

func (m *mockBatch) Delete(key []byte) error {
	m.ops = append(m.ops, func() error {
		return m.storage.Delete(key)
	})
	return nil
}

func (m *mockBatch) Commit() error {
	for _, op := range m.ops {
		if err := op(); err != nil {
			return err
		}
	}
	m.closed = true
	return nil
}

func (m *mockBatch) Rollback() error {
	m.ops = nil
	m.closed = true
	return nil
}

func (m *mockBatch) Close() error {
	if !m.closed {
		return m.Rollback()
	}
	return nil
}

// mockIterator implements storage.Iterator for testing.
type mockIterator struct {
	data    map[string][]byte
	keys    []string
	current int
}

func (m *mockIterator) SeekToFirst() bool {
	m.current = 0
	return len(m.keys) > 0
}

func (m *mockIterator) SeekToLast() bool {
	m.current = len(m.keys) - 1
	return len(m.keys) > 0
}

func (m *mockIterator) Seek(target []byte) bool {
	return m.SeekToFirst()
}

func (m *mockIterator) Next() bool {
	m.current++
	return m.current < len(m.keys)
}

func (m *mockIterator) Prev() bool {
	m.current--
	return m.current >= 0
}

func (m *mockIterator) Valid() bool {
	return m.current >= 0 && m.current < len(m.keys)
}

func (m *mockIterator) Key() []byte {
	if !m.Valid() {
		return nil
	}
	return []byte(m.keys[m.current])
}

func (m *mockIterator) Value() []byte {
	if !m.Valid() {
		return nil
	}
	return m.data[m.keys[m.current]]
}

func (m *mockIterator) Error() error {
	return nil
}

func (m *mockIterator) Close() error {
	return nil
}

// mockSnapshot implements storage.Snapshot for testing.
type mockSnapshot struct {
	data map[string][]byte
}

func (m *mockSnapshot) Get(key []byte) ([]byte, error) {
	if val, ok := m.data[string(key)]; ok {
		return val, nil
	}
	return nil, errors.Newf(errors.ErrNotFound, "mockSnapshot.Get", "key not found")
}

func (m *mockSnapshot) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	return &mockIterator{data: m.data}
}

func (m *mockSnapshot) Close() error {
	return nil
}

// mockLogger implements interfaces.Logger for testing.
type mockLogger struct {
	logs   []string
	fields map[string]interface{}
}

func newMockLogger() *mockLogger {
	return &mockLogger{
		logs:   make([]string, 0),
		fields: make(map[string]interface{}),
	}
}

func (m *mockLogger) Debug(msg string, args ...any)                          { m.logs = append(m.logs, msg) }
func (m *mockLogger) Debugf(format string, args ...interface{}) {}
func (m *mockLogger) Info(msg string, args ...any)                           { m.logs = append(m.logs, msg) }
func (m *mockLogger) Infof(format string, args ...interface{})  {}
func (m *mockLogger) Warn(msg string, args ...any)                           { m.logs = append(m.logs, msg) }
func (m *mockLogger) Warnf(format string, args ...interface{})  {}
func (m *mockLogger) Error(msg string, args ...any)                          { m.logs = append(m.logs, msg) }
func (m *mockLogger) Errorf(format string, args ...interface{}) {}
func (m *mockLogger) Fatal(msg string)                          { m.logs = append(m.logs, msg) }
func (m *mockLogger) Fatalf(format string, args ...interface{}) {}

func (m *mockLogger) WithField(key string, value interface{}) interfaces.Logger {
	newLogger := &mockLogger{
		logs:   m.logs,
		fields: make(map[string]interface{}),
	}
	for k, v := range m.fields {
		newLogger.fields[k] = v
	}
	newLogger.fields[key] = value
	return newLogger
}

func (m *mockLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	newLogger := &mockLogger{
		logs:   m.logs,
		fields: make(map[string]interface{}),
	}
	for k, v := range m.fields {
		newLogger.fields[k] = v
	}
	for k, v := range fields {
		newLogger.fields[k] = v
	}
	return newLogger
}

func (m *mockLogger) WithError(err error) interfaces.Logger {
	return m.WithField("error", err)
}

// Example tests demonstrating interface usage

func TestStorageInterface(t *testing.T) {
	// Create mock storage
	storage := newMockStorage()
	defer storage.Close()

	// Test basic operations
	key := []byte("test-key")
	value := []byte("test-value")

	// Set a value
	if err := storage.Set(key, value); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Get the value
	result, err := storage.Get(key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if string(result) != string(value) {
		t.Errorf("Expected %s, got %s", value, result)
	}

	// Delete the value
	if err := storage.Delete(key); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify deletion
	_, err = storage.Get(key)
	if err == nil {
		t.Error("Expected ErrNotFound after deletion")
	}
}

func TestBatchInterface(t *testing.T) {
	storage := newMockStorage()
	defer storage.Close()

	// Create a batch
	batch := storage.Batch()
	defer batch.Close()

	// Add operations to batch
	batch.Set([]byte("key1"), []byte("value1"))
	batch.Set([]byte("key2"), []byte("value2"))
	batch.Delete([]byte("key3"))

	// Commit batch
	if err := batch.Commit(); err != nil {
		t.Fatalf("Batch commit failed: %v", err)
	}

	// Verify batch operations
	val, err := storage.Get([]byte("key1"))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(val) != "value1" {
		t.Errorf("Expected value1, got %s", val)
	}
}

func TestLoggerInterface(t *testing.T) {
	logger := newMockLogger()

	// Test basic logging
	logger.Info("test message")
	if len(logger.logs) != 1 {
		t.Errorf("Expected 1 log entry, got %d", len(logger.logs))
	}

	// Test field logging
	logger2 := logger.WithField("key", "value")
	logger2.Debug("debug message")

	// Original logger should be unchanged
	if len(logger.fields) != 0 {
		t.Error("Original logger should not have fields")
	}

	// New logger should have field
	logger2Mock := logger2.(*mockLogger)
	if logger2Mock.fields["key"] != "value" {
		t.Error("New logger should have field")
	}
}

// ExampleStorage demonstrates using the Storage interface.
func ExampleStorage() {
	// Create storage instance (mock in this example)
	storage := newMockStorage()
	defer storage.Close()

	// Store a key-value pair
	storage.Set([]byte("user:1"), []byte(`{"name":"Alice"}`))

	// Retrieve the value
	value, _ := storage.Get([]byte("user:1"))
	fmt.Println(string(value))

	// Output: {"name":"Alice"}
}

// Contract tests ensuring implementations satisfy interfaces.
func TestStorageContract(t *testing.T) {
	var _ interfaces.Storage = (*mockStorage)(nil)
}

func TestLoggerContract(t *testing.T) {
	var _ interfaces.Logger = (*mockLogger)(nil)
}

// TestLogLevel_String tests the LogLevel.String() method.
func TestLogLevel_String(t *testing.T) {
	tests := []struct {
		level    interfaces.LogLevel
		expected string
	}{
		{interfaces.LogLevelDebug, "debug"},
		{interfaces.LogLevelInfo, "info"},
		{interfaces.LogLevelWarn, "warn"},
		{interfaces.LogLevelError, "error"},
		{interfaces.LogLevelFatal, "fatal"},
		{interfaces.LogLevel(100), "unknown"}, // Invalid level
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.level.String()
			if result != tt.expected {
				t.Errorf("LogLevel.String() = %v, want %v", result, tt.expected)
			}
		})
	}
}
