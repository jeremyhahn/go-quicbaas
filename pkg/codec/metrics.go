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

package codec

import (
	"sync"
	"sync/atomic"
)

// Metrics tracks codec performance and usage statistics.
// All fields use atomic operations for thread-safe updates.
type Metrics struct {
	// Operation counts
	EncodeCount atomic.Uint64
	DecodeCount atomic.Uint64

	// Error counts
	EncodeErrors atomic.Uint64
	DecodeErrors atomic.Uint64

	// Bytes processed
	BytesEncoded atomic.Uint64
	BytesDecoded atomic.Uint64

	// Codec usage distribution (codec name -> count)
	codecUsageMu sync.RWMutex
	codecUsage   map[string]*atomic.Uint64
}

// NewMetrics creates a new Metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{
		codecUsage: make(map[string]*atomic.Uint64),
	}
}

// RecordEncode records a successful encode operation.
func (m *Metrics) RecordEncode(codecName string, bytesEncoded uint64) {
	m.EncodeCount.Add(1)
	m.BytesEncoded.Add(bytesEncoded)
	m.recordCodecUsage(codecName)
}

// RecordDecode records a successful decode operation.
func (m *Metrics) RecordDecode(codecName string, bytesDecoded uint64) {
	m.DecodeCount.Add(1)
	m.BytesDecoded.Add(bytesDecoded)
	m.recordCodecUsage(codecName)
}

// RecordEncodeError records an encode error.
func (m *Metrics) RecordEncodeError() {
	m.EncodeErrors.Add(1)
}

// RecordDecodeError records a decode error.
func (m *Metrics) RecordDecodeError() {
	m.DecodeErrors.Add(1)
}

// recordCodecUsage increments the usage counter for a specific codec.
func (m *Metrics) recordCodecUsage(codecName string) {
	m.codecUsageMu.RLock()
	counter, exists := m.codecUsage[codecName]
	m.codecUsageMu.RUnlock()

	if exists {
		counter.Add(1)
		return
	}

	// Create new counter for this codec
	m.codecUsageMu.Lock()
	// Double-check after acquiring write lock
	counter, exists = m.codecUsage[codecName]
	if !exists {
		counter = &atomic.Uint64{}
		m.codecUsage[codecName] = counter
	}
	m.codecUsageMu.Unlock()

	counter.Add(1)
}

// GetCodecUsage returns the usage count for a specific codec.
func (m *Metrics) GetCodecUsage(codecName string) uint64 {
	m.codecUsageMu.RLock()
	defer m.codecUsageMu.RUnlock()

	counter, exists := m.codecUsage[codecName]
	if !exists {
		return 0
	}
	return counter.Load()
}

// GetAllCodecUsage returns a snapshot of all codec usage counts.
func (m *Metrics) GetAllCodecUsage() map[string]uint64 {
	m.codecUsageMu.RLock()
	defer m.codecUsageMu.RUnlock()

	result := make(map[string]uint64, len(m.codecUsage))
	for name, counter := range m.codecUsage {
		result[name] = counter.Load()
	}
	return result
}

// Snapshot returns a point-in-time snapshot of all metrics.
type Snapshot struct {
	EncodeCount  uint64
	DecodeCount  uint64
	EncodeErrors uint64
	DecodeErrors uint64
	BytesEncoded uint64
	BytesDecoded uint64
	CodecUsage   map[string]uint64
}

// GetSnapshot returns a point-in-time snapshot of all metrics.
func (m *Metrics) GetSnapshot() Snapshot {
	return Snapshot{
		EncodeCount:  m.EncodeCount.Load(),
		DecodeCount:  m.DecodeCount.Load(),
		EncodeErrors: m.EncodeErrors.Load(),
		DecodeErrors: m.DecodeErrors.Load(),
		BytesEncoded: m.BytesEncoded.Load(),
		BytesDecoded: m.BytesDecoded.Load(),
		CodecUsage:   m.GetAllCodecUsage(),
	}
}

// Reset resets all metrics to zero.
func (m *Metrics) Reset() {
	m.EncodeCount.Store(0)
	m.DecodeCount.Store(0)
	m.EncodeErrors.Store(0)
	m.DecodeErrors.Store(0)
	m.BytesEncoded.Store(0)
	m.BytesDecoded.Store(0)

	m.codecUsageMu.Lock()
	m.codecUsage = make(map[string]*atomic.Uint64)
	m.codecUsageMu.Unlock()
}

// EncodeSuccessRate returns the encode success rate as a percentage (0-100).
func (s Snapshot) EncodeSuccessRate() float64 {
	total := s.EncodeCount + s.EncodeErrors
	if total == 0 {
		return 100.0
	}
	return (float64(s.EncodeCount) / float64(total)) * 100.0
}

// DecodeSuccessRate returns the decode success rate as a percentage (0-100).
func (s Snapshot) DecodeSuccessRate() float64 {
	total := s.DecodeCount + s.DecodeErrors
	if total == 0 {
		return 100.0
	}
	return (float64(s.DecodeCount) / float64(total)) * 100.0
}

// AvgBytesEncoded returns the average bytes per encode operation.
func (s Snapshot) AvgBytesEncoded() float64 {
	if s.EncodeCount == 0 {
		return 0
	}
	return float64(s.BytesEncoded) / float64(s.EncodeCount)
}

// AvgBytesDecoded returns the average bytes per decode operation.
func (s Snapshot) AvgBytesDecoded() float64 {
	if s.DecodeCount == 0 {
		return 0
	}
	return float64(s.BytesDecoded) / float64(s.DecodeCount)
}
