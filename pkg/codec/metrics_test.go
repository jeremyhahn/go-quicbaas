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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMetrics_RecordEncode(t *testing.T) {
	m := NewMetrics()

	m.RecordEncode("json", 100)
	m.RecordEncode("json", 200)
	m.RecordEncode("msgpack", 150)

	assert.Equal(t, uint64(3), m.EncodeCount.Load())
	assert.Equal(t, uint64(450), m.BytesEncoded.Load())
	assert.Equal(t, uint64(2), m.GetCodecUsage("json"))
	assert.Equal(t, uint64(1), m.GetCodecUsage("msgpack"))
}

func TestMetrics_RecordDecode(t *testing.T) {
	m := NewMetrics()

	m.RecordDecode("json", 100)
	m.RecordDecode("msgpack", 200)

	assert.Equal(t, uint64(2), m.DecodeCount.Load())
	assert.Equal(t, uint64(300), m.BytesDecoded.Load())
}

func TestMetrics_RecordErrors(t *testing.T) {
	m := NewMetrics()

	m.RecordEncodeError()
	m.RecordEncodeError()
	m.RecordDecodeError()

	assert.Equal(t, uint64(2), m.EncodeErrors.Load())
	assert.Equal(t, uint64(1), m.DecodeErrors.Load())
}

func TestMetrics_GetSnapshot(t *testing.T) {
	m := NewMetrics()

	m.RecordEncode("json", 100)
	m.RecordEncode("msgpack", 200)
	m.RecordDecode("json", 150)
	m.RecordEncodeError()

	snap := m.GetSnapshot()

	assert.Equal(t, uint64(2), snap.EncodeCount)
	assert.Equal(t, uint64(1), snap.DecodeCount)
	assert.Equal(t, uint64(1), snap.EncodeErrors)
	assert.Equal(t, uint64(0), snap.DecodeErrors)
	assert.Equal(t, uint64(300), snap.BytesEncoded)
	assert.Equal(t, uint64(150), snap.BytesDecoded)
	assert.Len(t, snap.CodecUsage, 2)
}

func TestMetrics_SuccessRates(t *testing.T) {
	m := NewMetrics()

	m.RecordEncode("json", 100)
	m.RecordEncode("json", 100)
	m.RecordEncodeError()

	m.RecordDecode("json", 100)
	m.RecordDecode("json", 100)
	m.RecordDecode("json", 100)
	m.RecordDecodeError()

	snap := m.GetSnapshot()

	// 2 successes out of 3 total = 66.67%
	assert.InDelta(t, 66.67, snap.EncodeSuccessRate(), 0.01)

	// 3 successes out of 4 total = 75%
	assert.Equal(t, 75.0, snap.DecodeSuccessRate())
}

func TestMetrics_AverageBytes(t *testing.T) {
	m := NewMetrics()

	m.RecordEncode("json", 100)
	m.RecordEncode("json", 200)
	m.RecordEncode("json", 300)

	m.RecordDecode("json", 400)
	m.RecordDecode("json", 600)

	snap := m.GetSnapshot()

	// (100 + 200 + 300) / 3 = 200
	assert.Equal(t, 200.0, snap.AvgBytesEncoded())

	// (400 + 600) / 2 = 500
	assert.Equal(t, 500.0, snap.AvgBytesDecoded())
}

func TestMetrics_Reset(t *testing.T) {
	m := NewMetrics()

	m.RecordEncode("json", 100)
	m.RecordDecode("json", 200)
	m.RecordEncodeError()

	m.Reset()

	assert.Equal(t, uint64(0), m.EncodeCount.Load())
	assert.Equal(t, uint64(0), m.DecodeCount.Load())
	assert.Equal(t, uint64(0), m.EncodeErrors.Load())
	assert.Equal(t, uint64(0), m.DecodeErrors.Load())
	assert.Equal(t, uint64(0), m.BytesEncoded.Load())
	assert.Equal(t, uint64(0), m.BytesDecoded.Load())
	assert.Equal(t, uint64(0), m.GetCodecUsage("json"))
}

func TestMetrics_ConcurrentAccess(t *testing.T) {
	m := NewMetrics()

	const goroutines = 100
	const operations = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines * 2) // encode and decode goroutines

	// Concurrent encodes
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			codecName := "json"
			if id%2 == 0 {
				codecName = "msgpack"
			}

			for j := 0; j < operations; j++ {
				m.RecordEncode(codecName, 100)
			}
		}(i)
	}

	// Concurrent decodes
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			codecName := "json"
			if id%2 == 0 {
				codecName = "msgpack"
			}

			for j := 0; j < operations; j++ {
				m.RecordDecode(codecName, 100)
			}
		}(i)
	}

	wg.Wait()

	snap := m.GetSnapshot()

	expectedOps := uint64(goroutines * operations)
	assert.Equal(t, expectedOps, snap.EncodeCount)
	assert.Equal(t, expectedOps, snap.DecodeCount)

	// Should have roughly equal usage for json and msgpack
	totalUsage := m.GetCodecUsage("json") + m.GetCodecUsage("msgpack")
	assert.Equal(t, expectedOps*2, totalUsage) // *2 for encode and decode
}

func TestMetrics_ZeroValues(t *testing.T) {
	m := NewMetrics()

	snap := m.GetSnapshot()

	// Success rates should be 100% when there are no operations
	assert.Equal(t, 100.0, snap.EncodeSuccessRate())
	assert.Equal(t, 100.0, snap.DecodeSuccessRate())

	// Averages should be 0 when there are no operations
	assert.Equal(t, 0.0, snap.AvgBytesEncoded())
	assert.Equal(t, 0.0, snap.AvgBytesDecoded())
}

func TestMetrics_GetAllCodecUsage(t *testing.T) {
	m := NewMetrics()

	m.RecordEncode("json", 100)
	m.RecordEncode("msgpack", 200)
	m.RecordEncode("yaml", 300)
	m.RecordDecode("json", 100)
	m.RecordDecode("protobuf", 400)

	usage := m.GetAllCodecUsage()

	assert.Len(t, usage, 4)
	assert.Equal(t, uint64(2), usage["json"])     // 1 encode + 1 decode
	assert.Equal(t, uint64(1), usage["msgpack"])  // 1 encode
	assert.Equal(t, uint64(1), usage["yaml"])     // 1 encode
	assert.Equal(t, uint64(1), usage["protobuf"]) // 1 decode
}
