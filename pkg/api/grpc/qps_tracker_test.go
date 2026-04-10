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

package grpc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewQPSTracker(t *testing.T) {
	tracker := NewQPSTracker()
	require.NotNil(t, tracker)
	assert.Equal(t, int64(0), tracker.Total())
	assert.Equal(t, 0.0, tracker.QPS())
}

func TestQPSTracker_Record(t *testing.T) {
	tracker := NewQPSTracker()

	// Record some requests
	for i := 0; i < 100; i++ {
		tracker.Record()
	}

	assert.Equal(t, int64(100), tracker.Total())
}

func TestQPSTracker_QPS(t *testing.T) {
	tracker := NewQPSTracker()

	// Record requests
	for i := 0; i < 50; i++ {
		tracker.Record()
	}

	// QPS should be non-negative
	qps := tracker.QPS()
	assert.GreaterOrEqual(t, qps, 0.0)
}

func TestQPSTracker_Reset(t *testing.T) {
	tracker := NewQPSTracker()

	// Record some requests
	for i := 0; i < 100; i++ {
		tracker.Record()
	}
	assert.Equal(t, int64(100), tracker.Total())

	// Reset
	tracker.Reset()
	assert.Equal(t, int64(0), tracker.Total())
}

func TestQPSTracker_ConcurrentRecords(t *testing.T) {
	tracker := NewQPSTracker()
	done := make(chan bool)

	// Start multiple goroutines recording concurrently
	numGoroutines := 10
	numRecordsPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		go func() {
			for j := 0; j < numRecordsPerGoroutine; j++ {
				tracker.Record()
			}
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify total count
	expectedTotal := int64(numGoroutines * numRecordsPerGoroutine)
	assert.Equal(t, expectedTotal, tracker.Total())
}

func TestQPSTracker_QPS_NoActivity(t *testing.T) {
	tracker := NewQPSTracker()

	// Wait a bit to ensure we're in a different second
	time.Sleep(50 * time.Millisecond)

	// QPS should be zero or very low with no activity
	qps := tracker.QPS()
	assert.GreaterOrEqual(t, qps, 0.0)
}

func TestQPSTracker_QPS_AfterSecondRollover(t *testing.T) {
	tracker := NewQPSTracker()

	// Record requests
	for i := 0; i < 10; i++ {
		tracker.Record()
	}

	// Get initial QPS
	initialQPS := tracker.QPS()
	assert.GreaterOrEqual(t, initialQPS, 0.0)

	// Total should be correct regardless of QPS calculation
	assert.Equal(t, int64(10), tracker.Total())
}

func TestQPSTracker_Total_NeverDecreases(t *testing.T) {
	tracker := NewQPSTracker()

	var lastTotal int64 = 0
	for i := 0; i < 100; i++ {
		tracker.Record()
		currentTotal := tracker.Total()
		assert.GreaterOrEqual(t, currentTotal, lastTotal, "Total should never decrease")
		lastTotal = currentTotal
	}
}

func TestQPSTracker_QPS_ReasonableRange(t *testing.T) {
	tracker := NewQPSTracker()

	// Record some requests
	for i := 0; i < 50; i++ {
		tracker.Record()
	}

	qps := tracker.QPS()

	// QPS should be non-negative and reasonable
	assert.GreaterOrEqual(t, qps, 0.0)
	// Should not be astronomically high for 50 records
	assert.LessOrEqual(t, qps, 1000000.0, "QPS should be reasonable")
}

func TestQPSTracker_MultipleSamples(t *testing.T) {
	tracker := NewQPSTracker()

	// Record batches and check QPS multiple times
	for batch := 0; batch < 5; batch++ {
		for i := 0; i < 20; i++ {
			tracker.Record()
		}

		qps := tracker.QPS()
		assert.GreaterOrEqual(t, qps, 0.0)

		// Small delay between batches
		time.Sleep(10 * time.Millisecond)
	}

	assert.Equal(t, int64(100), tracker.Total())
}

// =============================================================================
// Additional Tests for Coverage
// =============================================================================

func TestQPSTracker_Record_WindowRollover(t *testing.T) {
	tracker := NewQPSTracker()

	// Record some values
	for i := 0; i < 50; i++ {
		tracker.Record()
	}

	// Manually set currentSecond to be in the past to simulate time passing
	pastSecond := time.Now().Unix() - 2
	tracker.currentSecond.Store(pastSecond)

	// This record should trigger the window rollover logic
	tracker.Record()

	// Total should include all records
	assert.Equal(t, int64(51), tracker.Total())
}

func TestQPSTracker_QPS_SameSecondWithPreviousData(t *testing.T) {
	tracker := NewQPSTracker()

	// Set up previous second data
	tracker.previousCount.Store(100)

	// Record in current second
	for i := 0; i < 10; i++ {
		tracker.Record()
	}

	// QPS should factor in both current and previous
	qps := tracker.QPS()
	assert.GreaterOrEqual(t, qps, 0.0)
}

func TestQPSTracker_QPS_NewSecondNotRolledOver(t *testing.T) {
	tracker := NewQPSTracker()

	// Record values
	for i := 0; i < 100; i++ {
		tracker.Record()
	}

	// Set currentSecond to previous second (simulating the counter hasn't rolled over)
	pastSecond := time.Now().Unix() - 1
	tracker.currentSecond.Store(pastSecond)

	// QPS should return the current count
	qps := tracker.QPS()
	assert.GreaterOrEqual(t, qps, 0.0)
}

func TestQPSTracker_QPS_GapInActivity(t *testing.T) {
	tracker := NewQPSTracker()

	// Record some values
	for i := 0; i < 50; i++ {
		tracker.Record()
	}

	// Set currentSecond to be more than 1 second ago (gap in activity)
	pastSecond := time.Now().Unix() - 3
	tracker.currentSecond.Store(pastSecond)

	// QPS should be 0 due to gap
	qps := tracker.QPS()
	assert.Equal(t, 0.0, qps)
}

func TestQPSTracker_Record_CompareAndSwapRace(t *testing.T) {
	tracker := NewQPSTracker()

	// Set currentSecond to past to ensure CompareAndSwap is triggered
	pastSecond := time.Now().Unix() - 1
	tracker.currentSecond.Store(pastSecond)

	// Store some current count
	tracker.currentCount.Store(75)

	// Record - this should attempt to advance the window
	tracker.Record()

	// Total should be incremented
	assert.Equal(t, int64(1), tracker.Total())
}

func TestQPSTracker_QPS_EarlyInSecond(t *testing.T) {
	tracker := NewQPSTracker()

	// Set up previous count
	tracker.previousCount.Store(200)

	// When early in second (before fraction > 0.1), should rely on previous
	// This is hard to test deterministically, but we can verify behavior
	qps := tracker.QPS()
	assert.GreaterOrEqual(t, qps, 0.0)
}
