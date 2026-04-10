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

package cache

import (
	"sync"
	"sync/atomic"
)

// Metrics tracks cache performance metrics.
type Metrics struct {
	l1Hits            atomic.Int64
	l1Misses          atomic.Int64
	l1Sets            atomic.Int64
	l1Deletes         atomic.Int64
	l1Expired         atomic.Int64
	l2Hits            atomic.Int64
	l2Misses          atomic.Int64
	l2Sets            atomic.Int64
	l2Deletes         atomic.Int64
	l2Errors          atomic.Int64
	cacheMisses       atomic.Int64
	cacheClears       atomic.Int64
	writebackSuccess  atomic.Int64
	writebackFailures atomic.Int64
}

// metricRegistry returns the map of metric name to atomic field pointer for the given Metrics instance.
// This is called per-operation rather than stored, since the Metrics struct contains value-type atomics.
func (m *Metrics) metricRegistry() map[string]*atomic.Int64 {
	return map[string]*atomic.Int64{
		"l1_hits":            &m.l1Hits,
		"l1_misses":          &m.l1Misses,
		"l1_sets":            &m.l1Sets,
		"l1_deletes":         &m.l1Deletes,
		"l1_expired":         &m.l1Expired,
		"l2_hits":            &m.l2Hits,
		"l2_misses":          &m.l2Misses,
		"l2_sets":            &m.l2Sets,
		"l2_deletes":         &m.l2Deletes,
		"l2_errors":          &m.l2Errors,
		"cache_misses":       &m.cacheMisses,
		"cache_clears":       &m.cacheClears,
		"writeback_success":  &m.writebackSuccess,
		"writeback_failures": &m.writebackFailures,
	}
}

// NewMetrics creates a new metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{}
}

// Increment atomically increments a metric by 1.
func (m *Metrics) Increment(name string) {
	m.Add(name, 1)
}

// Add atomically adds a value to a metric.
func (m *Metrics) Add(name string, value int64) {
	registry := m.metricRegistry()
	if field, ok := registry[name]; ok {
		field.Add(value)
	}
}

// Get returns the current value of a metric.
func (m *Metrics) Get(name string) int64 {
	registry := m.metricRegistry()
	if field, ok := registry[name]; ok {
		return field.Load()
	}
	return 0
}

// Reset resets all metrics to zero.
func (m *Metrics) Reset() {
	for _, field := range m.metricRegistry() {
		field.Store(0)
	}
}

// Copy creates a snapshot of the current metrics.
func (m *Metrics) Copy() *Metrics {
	cpy := &Metrics{}
	srcRegistry := m.metricRegistry()
	dstRegistry := cpy.metricRegistry()
	for name, srcField := range srcRegistry {
		if dstField, ok := dstRegistry[name]; ok {
			dstField.Store(srcField.Load())
		}
	}
	return cpy
}

// ToMap returns metrics as a map for easy serialization.
func (m *Metrics) ToMap() map[string]int64 {
	registry := m.metricRegistry()
	result := make(map[string]int64, len(registry))
	for name, field := range registry {
		result[name] = field.Load()
	}
	return result
}

// L1Hits returns the number of L1 cache hits.
func (m *Metrics) L1Hits() int64 {
	return m.l1Hits.Load()
}

// L1Misses returns the number of L1 cache misses.
func (m *Metrics) L1Misses() int64 {
	return m.l1Misses.Load()
}

// L2Hits returns the number of L2 cache hits.
func (m *Metrics) L2Hits() int64 {
	return m.l2Hits.Load()
}

// L2Misses returns the number of L2 cache misses.
func (m *Metrics) L2Misses() int64 {
	return m.l2Misses.Load()
}

// MetricsSnapshot provides a thread-safe snapshot of metrics at a point in time.
type MetricsSnapshot struct {
	mu      sync.RWMutex
	metrics map[string]int64
}

// NewMetricsSnapshot creates a new metrics snapshot from current metrics.
func NewMetricsSnapshot(m *Metrics) *MetricsSnapshot {
	return &MetricsSnapshot{
		metrics: m.ToMap(),
	}
}

// Get returns a metric value from the snapshot.
func (s *MetricsSnapshot) Get(name string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.metrics[name]
}

// All returns all metrics from the snapshot.
func (s *MetricsSnapshot) All() map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a copy to prevent external modification
	copy := make(map[string]int64, len(s.metrics))
	for k, v := range s.metrics {
		copy[k] = v
	}
	return copy
}
