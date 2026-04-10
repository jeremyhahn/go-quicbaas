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
	"sync/atomic"
	"time"
)

// QPSTracker tracks queries per second using a lock-free sliding window algorithm.
// It maintains atomic counters for the current and previous second to calculate
// an accurate QPS value without locking overhead.
type QPSTracker struct {
	// currentCount holds the request count for the current second
	currentCount atomic.Int64
	// previousCount holds the request count for the previous second
	previousCount atomic.Int64
	// currentSecond holds the Unix timestamp of the current tracking window
	currentSecond atomic.Int64
	// totalCount holds the total request count since tracker creation
	totalCount atomic.Int64
}

// NewQPSTracker creates a new QPS tracker.
func NewQPSTracker() *QPSTracker {
	tracker := &QPSTracker{}
	tracker.currentSecond.Store(time.Now().Unix())
	return tracker
}

// Record increments the request counter for QPS tracking.
// This method is lock-free and safe for concurrent use.
func (q *QPSTracker) Record() {
	now := time.Now().Unix()
	currentSec := q.currentSecond.Load()

	// Check if we've moved to a new second
	if now != currentSec {
		// Try to advance the window
		if q.currentSecond.CompareAndSwap(currentSec, now) {
			// Successfully advanced: move current to previous
			currentVal := q.currentCount.Swap(0)
			q.previousCount.Store(currentVal)
		}
	}

	// Increment current second counter
	q.currentCount.Add(1)
	q.totalCount.Add(1)
}

// QPS returns the current queries per second rate.
// It uses a weighted average of the current and previous second
// to provide a smooth QPS value.
func (q *QPSTracker) QPS() float64 {
	now := time.Now()
	currentSec := q.currentSecond.Load()
	nowSec := now.Unix()

	// If we're in the same second, use weighted average
	if nowSec == currentSec {
		// Weight based on how far into the current second we are
		fraction := float64(now.Nanosecond()) / 1e9
		current := float64(q.currentCount.Load())
		previous := float64(q.previousCount.Load())

		// Weighted average: extrapolate current, blend with previous
		if fraction > 0.1 {
			extrapolated := current / fraction
			return extrapolated*fraction + previous*(1-fraction)
		}
		// Early in the second, rely more on previous
		return previous
	}

	// We're in a new second, the counter might not have rolled over yet
	if nowSec == currentSec+1 {
		return float64(q.currentCount.Load())
	}

	// More than 1 second gap means no recent activity
	return 0
}

// Total returns the total number of requests recorded since tracker creation.
func (q *QPSTracker) Total() int64 {
	return q.totalCount.Load()
}

// Reset resets all counters. This is primarily useful for testing.
func (q *QPSTracker) Reset() {
	q.currentCount.Store(0)
	q.previousCount.Store(0)
	q.currentSecond.Store(time.Now().Unix())
	q.totalCount.Store(0)
}
