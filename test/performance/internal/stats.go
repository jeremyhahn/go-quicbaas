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

// Package internal provides shared utilities for performance testing.
package internal

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Stats tracks performance test statistics with thread-safe operations.
type Stats struct {
	totalRequests   atomic.Int64
	successRequests atomic.Int64
	failedRequests  atomic.Int64
	totalLatency    atomic.Int64 // in microseconds

	mu        sync.Mutex
	latencies []int64 // for percentile calculation
	startTime time.Time
	endTime   time.Time
}

// NewStats creates a new Stats instance.
func NewStats() *Stats {
	return &Stats{
		latencies: make([]int64, 0, 1000000), // Pre-allocate for 1M requests
		startTime: time.Now(),
	}
}

// RecordSuccess records a successful request with its latency.
func (s *Stats) RecordSuccess(latency time.Duration) {
	s.successRequests.Add(1)
	s.totalRequests.Add(1)
	latencyUs := latency.Microseconds()
	s.totalLatency.Add(latencyUs)

	s.mu.Lock()
	s.latencies = append(s.latencies, latencyUs)
	s.mu.Unlock()
}

// RecordFailure records a failed request.
func (s *Stats) RecordFailure() {
	s.failedRequests.Add(1)
	s.totalRequests.Add(1)
}

// Snapshot returns a thread-safe snapshot of current statistics.
func (s *Stats) Snapshot() Snapshot {
	total := s.totalRequests.Load()
	success := s.successRequests.Load()
	failed := s.failedRequests.Load()
	totalLatencyUs := s.totalLatency.Load()

	var avgLatencyMs float64
	if success > 0 {
		avgLatencyMs = float64(totalLatencyUs) / float64(success) / 1000.0
	}

	return Snapshot{
		TotalRequests:   total,
		SuccessRequests: success,
		FailedRequests:  failed,
		AvgLatencyMs:    avgLatencyMs,
	}
}

// Snapshot represents a point-in-time snapshot of statistics.
type Snapshot struct {
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
	AvgLatencyMs    float64
}

// Throughput calculates requests per second.
func (s Snapshot) Throughput(duration time.Duration) float64 {
	if duration.Seconds() == 0 {
		return 0
	}
	return float64(s.SuccessRequests) / duration.Seconds()
}

// SuccessRate calculates the success rate as a percentage.
func (s Snapshot) SuccessRate() float64 {
	if s.TotalRequests == 0 {
		return 0
	}
	return float64(s.SuccessRequests) / float64(s.TotalRequests) * 100.0
}

// Stop marks the end time for duration calculations.
func (s *Stats) Stop() {
	s.endTime = time.Now()
}

// Duration returns the total test duration.
func (s *Stats) Duration() time.Duration {
	if s.endTime.IsZero() {
		return time.Since(s.startTime)
	}
	return s.endTime.Sub(s.startTime)
}

// Percentiles calculates latency percentiles (p50, p95, p99).
type Percentiles struct {
	P50 float64
	P95 float64
	P99 float64
	Min float64
	Max float64
}

// CalculatePercentiles computes latency percentiles in milliseconds.
func (s *Stats) CalculatePercentiles() Percentiles {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.latencies) == 0 {
		return Percentiles{}
	}

	// Copy and sort latencies
	sorted := make([]int64, len(s.latencies))
	copy(sorted, s.latencies)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	percentile := func(p float64) float64 {
		idx := int(float64(len(sorted)) * p)
		if idx >= len(sorted) {
			idx = len(sorted) - 1
		}
		return float64(sorted[idx]) / 1000.0 // Convert to ms
	}

	return Percentiles{
		P50: percentile(0.50),
		P95: percentile(0.95),
		P99: percentile(0.99),
		Min: float64(sorted[0]) / 1000.0,
		Max: float64(sorted[len(sorted)-1]) / 1000.0,
	}
}

// PrintReport prints a formatted performance report.
func (s *Stats) PrintReport(testName string) {
	snapshot := s.Snapshot()
	duration := s.Duration()
	percentiles := s.CalculatePercentiles()

	fmt.Printf("\n═══════════════════════════════════════════════════════════════\n")
	fmt.Printf("📊 Performance Report: %s\n", testName)
	fmt.Printf("═══════════════════════════════════════════════════════════════\n")
	fmt.Printf("Duration:          %.2f seconds\n", duration.Seconds())
	fmt.Printf("Total Requests:    %d\n", snapshot.TotalRequests)
	fmt.Printf("Successful:        %d (%.2f%%)\n", snapshot.SuccessRequests, snapshot.SuccessRate())
	fmt.Printf("Failed:            %d (%.2f%%)\n", snapshot.FailedRequests, 100.0-snapshot.SuccessRate())
	fmt.Printf("\n")
	fmt.Printf("Throughput:        %.2f req/s\n", snapshot.Throughput(duration))
	fmt.Printf("Avg Latency:       %.2f ms\n", snapshot.AvgLatencyMs)
	fmt.Printf("\n")
	fmt.Printf("📈 Latency Percentiles:\n")
	fmt.Printf("Min:               %.2f ms\n", percentiles.Min)
	fmt.Printf("p50:               %.2f ms\n", percentiles.P50)
	fmt.Printf("p95:               %.2f ms\n", percentiles.P95)
	fmt.Printf("p99:               %.2f ms\n", percentiles.P99)
	fmt.Printf("Max:               %.2f ms\n", percentiles.Max)
	fmt.Printf("═══════════════════════════════════════════════════════════════\n\n")
}
