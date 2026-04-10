//go:build rag

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

package indexing

import (
	"context"
	"errors"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/watcher"
)

// --- In-memory KVStore for testing ---

// memoryKVStore is a simple in-memory KVStore for unit tests.
type memoryKVStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newMemoryKVStore() *memoryKVStore {
	return &memoryKVStore{data: make(map[string][]byte)}
}

func (s *memoryKVStore) Put(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(value))
	copy(cp, value)
	s.data[key] = cp
	return nil
}

func (s *memoryKVStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	if !ok {
		return nil, nil
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (s *memoryKVStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func (s *memoryKVStore) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for k, v := range s.data {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			if err := fn(k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *memoryKVStore) List(_ context.Context, prefix string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var keys []string
	for k := range s.data {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (s *memoryKVStore) Exists(_ context.Context, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data[key]
	return ok, nil
}

// --- Failing KVStore for error path testing ---

// failingKVStore returns errors on all operations.
type failingKVStore struct {
	err error
}

func newFailingKVStore(err error) *failingKVStore {
	return &failingKVStore{err: err}
}

func (s *failingKVStore) Put(_ context.Context, _ string, _ []byte) error {
	return s.err
}

func (s *failingKVStore) Get(_ context.Context, _ string) ([]byte, error) {
	return nil, s.err
}

func (s *failingKVStore) Delete(_ context.Context, _ string) error {
	return s.err
}

func (s *failingKVStore) Scan(_ context.Context, _ string, _ func(key string, value []byte) error) error {
	return s.err
}

func (s *failingKVStore) List(_ context.Context, _ string) ([]string, error) {
	return nil, s.err
}

func (s *failingKVStore) Exists(_ context.Context, _ string) (bool, error) {
	return false, s.err
}

// --- Test embedding provider ---

// testEmbeddingProvider generates deterministic embeddings for testing.
type testEmbeddingProvider struct {
	dimensions int
}

func newTestEmbeddingProvider(dimensions int) *testEmbeddingProvider {
	return &testEmbeddingProvider{dimensions: dimensions}
}

func (p *testEmbeddingProvider) Embed(_ context.Context, text string) ([]float32, error) {
	vec := make([]float32, p.dimensions)
	for i := range vec {
		// Deterministic based on text content length and position.
		vec[i] = float32(len(text)%100) / 100.0 * float32(i+1) / float32(p.dimensions)
	}
	return vec, nil
}

func (p *testEmbeddingProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		vec := make([]float32, p.dimensions)
		for j := range vec {
			vec[j] = float32(len(text)%100) / 100.0 * float32(j+1) / float32(p.dimensions)
		}
		results[i] = vec
	}
	return results, nil
}

func (p *testEmbeddingProvider) Dimensions() int { return p.dimensions }
func (p *testEmbeddingProvider) MaxTokens() int  { return 8192 }
func (p *testEmbeddingProvider) Name() string    { return "test" }
func (p *testEmbeddingProvider) Close() error    { return nil }

// --- Failing embedding provider ---

// failingEmbeddingProvider returns errors on all embedding operations.
type failingEmbeddingProvider struct {
	dimensions int
	err        error
}

func newFailingEmbeddingProvider(dimensions int, err error) *failingEmbeddingProvider {
	return &failingEmbeddingProvider{dimensions: dimensions, err: err}
}

func (p *failingEmbeddingProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, p.err
}

func (p *failingEmbeddingProvider) EmbedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return nil, p.err
}

func (p *failingEmbeddingProvider) Dimensions() int { return p.dimensions }
func (p *failingEmbeddingProvider) MaxTokens() int  { return 8192 }
func (p *failingEmbeddingProvider) Name() string    { return "failing-test" }
func (p *failingEmbeddingProvider) Close() error    { return nil }

// Compile-time interface checks.
var _ embedding.Provider = (*testEmbeddingProvider)(nil)
var _ embedding.Provider = (*failingEmbeddingProvider)(nil)
var _ interfaces.KVStore = (*memoryKVStore)(nil)
var _ interfaces.KVStore = (*failingKVStore)(nil)

// --- Test logger ---

// testLogger implements interfaces.Logger for testing.
type testLogger struct {
	mu   sync.Mutex
	logs []string
}

func newTestLogger() *testLogger {
	return &testLogger{logs: make([]string, 0)}
}

func (l *testLogger) Debug(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, msg)
}

func (l *testLogger) Debugf(_ string, _ ...interface{}) {}
func (l *testLogger) Info(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, msg)
}

func (l *testLogger) Infof(_ string, _ ...interface{}) {}
func (l *testLogger) Warn(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, msg)
}

func (l *testLogger) Warnf(_ string, _ ...interface{}) {}
func (l *testLogger) Error(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, msg)
}

func (l *testLogger) Errorf(_ string, _ ...interface{}) {}
func (l *testLogger) Fatal(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, msg)
}

func (l *testLogger) Fatalf(_ string, _ ...interface{}) {}

func (l *testLogger) WithField(_ string, _ interface{}) interfaces.Logger   { return l }
func (l *testLogger) WithFields(_ map[string]interface{}) interfaces.Logger { return l }
func (l *testLogger) WithError(_ error) interfaces.Logger                   { return l }

var _ interfaces.Logger = (*testLogger)(nil)

// --- Test watcher ---

// testWatcher implements watcher.Watcher for unit tests with controllable events.
type testWatcher struct {
	events  chan watcher.Event
	started bool
	stopped bool
	mu      sync.Mutex
}

func newTestWatcher() *testWatcher {
	return &testWatcher{
		events: make(chan watcher.Event, 100),
	}
}

func (w *testWatcher) Start(_ context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started {
		return errors.New("already started")
	}
	w.started = true
	return nil
}

func (w *testWatcher) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.started {
		return errors.New("not started")
	}
	w.stopped = true
	close(w.events)
	return nil
}

func (w *testWatcher) Events() <-chan watcher.Event {
	return w.events
}

func (w *testWatcher) AddPath(_ string) error    { return nil }
func (w *testWatcher) RemovePath(_ string) error { return nil }

func (w *testWatcher) SendEvent(evt watcher.Event) {
	w.events <- evt
}

var _ watcher.Watcher = (*testWatcher)(nil)
