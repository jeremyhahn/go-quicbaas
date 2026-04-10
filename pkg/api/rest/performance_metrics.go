//go:build rest

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

package rest

import (
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"time"

	dto "github.com/prometheus/client_model/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// PerformanceMetrics represents aggregated performance metrics.
type PerformanceMetrics struct {
	Status     string            `json:"status"`
	Timestamp  time.Time         `json:"timestamp"`
	Throughput ThroughputMetrics `json:"throughput"`
	Latency    LatencyMetrics    `json:"latency"`
	Storage    StorageMetrics    `json:"storage"`
	Pool       PoolMetrics       `json:"pool"`
}

// ThroughputMetrics contains operations per second metrics.
type ThroughputMetrics struct {
	ReadsPerSec    float64 `json:"reads_per_sec"`
	WritesPerSec   float64 `json:"writes_per_sec"`
	TotalOpsPerSec float64 `json:"total_ops_per_sec"`
}

// LatencyMetrics contains percentile latency measurements in milliseconds.
type LatencyMetrics struct {
	ReadP50Ms  float64 `json:"read_p50_ms"`
	ReadP95Ms  float64 `json:"read_p95_ms"`
	ReadP99Ms  float64 `json:"read_p99_ms"`
	WriteP50Ms float64 `json:"write_p50_ms"`
	WriteP95Ms float64 `json:"write_p95_ms"`
	WriteP99Ms float64 `json:"write_p99_ms"`
}

// StorageMetrics contains storage engine performance metrics.
type StorageMetrics struct {
	CacheHitRatePct             float64 `json:"cache_hit_rate_pct"`
	BloomFilterEffectivenessPct float64 `json:"bloom_filter_effectiveness_pct"`
	CompactionsPerMin           float64 `json:"compactions_per_min"`
	L0Files                     int     `json:"l0_files"`
	DiskUsageGB                 float64 `json:"disk_usage_gb"`
}

// PoolMetrics contains connection pool performance metrics.
type PoolMetrics struct {
	Size                   int     `json:"size"`
	Active                 int     `json:"active"`
	ConnectionReuseRatePct float64 `json:"connection_reuse_rate_pct"`
}

// HandlePerformanceMetrics returns aggregated performance metrics.
//
// @Summary Get performance metrics
// @Description Returns aggregated performance metrics including throughput, latency, storage, and pool statistics
// @Tags observability
// @Produce json
// @Success 200 {object} PerformanceMetrics
// @Failure 500 {object} ErrorResponse
// @Router /v1/metrics/performance [get].
func (s *Server) HandlePerformanceMetrics(w http.ResponseWriter, r *http.Request) {
	s.logger.Debug("Processing performance metrics request")

	metricsResp := s.collectPerformanceMetrics()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(metricsResp); err != nil {
		s.logger.WithError(err).Error("Failed to encode performance metrics")
		RespondError(w, r, http.StatusInternalServerError, err)
		return
	}
}

// collectPerformanceMetrics gathers performance metrics from the Prometheus registry.
// Throughput and latency metrics are sourced from the quicbaas_request_duration_seconds
// histogram. Storage and pool metrics require integration with PebbleDB and connection
// pool subsystems and are not yet populated (status: beta).
func (s *Server) collectPerformanceMetrics() *PerformanceMetrics {
	now := time.Now()

	pm := &PerformanceMetrics{
		Status:    "beta",
		Timestamp: now,
	}

	families := s.gatherMetricFamilies()
	if families == nil {
		s.logger.Debug("No metric families gathered, returning beta defaults")
		return pm
	}

	s.populateThroughputAndLatency(pm, families)

	// Storage and pool metrics require subsystem-specific collectors that are
	// not yet wired to the Prometheus registry. These remain zero-valued until
	// those integrations are complete.

	s.logger.WithFields(map[string]interface{}{
		"timestamp": now,
		"status":    pm.Status,
	}).Debug("Performance metrics collected from Prometheus registry")

	return pm
}

// gatherMetricFamilies collects all metric families from the Prometheus registry.
func (s *Server) gatherMetricFamilies() map[string]*dto.MetricFamily {
	registry := metrics.Registry()
	gathered, err := registry.Gather()
	if err != nil {
		s.logger.WithError(err).Warn("Failed to gather Prometheus metrics")
		return nil
	}

	families := make(map[string]*dto.MetricFamily, len(gathered))
	for _, mf := range gathered {
		if mf.Name != nil {
			families[*mf.Name] = mf
		}
	}
	return families
}

// populateThroughputAndLatency extracts throughput and latency data from the
// quicbaas_request_duration_seconds histogram. It sums sample counts per label
// to derive total operations and computes approximate percentiles from histogram
// bucket boundaries.
func (s *Server) populateThroughputAndLatency(pm *PerformanceMetrics, families map[string]*dto.MetricFamily) {
	mf, ok := families["quicbaas_request_duration_seconds"]
	if !ok {
		return
	}

	var readCount, writeCount uint64
	var readHistograms, writeHistograms []*dto.Histogram

	for _, m := range mf.GetMetric() {
		operation := labelValue(m.GetLabel(), "operation")
		h := m.GetHistogram()
		if h == nil {
			continue
		}

		switch operation {
		case "get", "read", "scan", "list", "query", "search":
			readCount += h.GetSampleCount()
			readHistograms = append(readHistograms, h)
		case "put", "write", "delete", "set", "update", "insert", "upsert", "batch_write":
			writeCount += h.GetSampleCount()
			writeHistograms = append(writeHistograms, h)
		default:
			// Count unknown operations toward total but not read/write
			readCount += h.GetSampleCount()
			readHistograms = append(readHistograms, h)
		}
	}

	// Calculate ops/sec based on server uptime
	uptimeSeconds := time.Since(s.startTime).Seconds()
	if uptimeSeconds > 0 {
		pm.Throughput.ReadsPerSec = float64(readCount) / uptimeSeconds
		pm.Throughput.WritesPerSec = float64(writeCount) / uptimeSeconds
		pm.Throughput.TotalOpsPerSec = float64(readCount+writeCount) / uptimeSeconds
	}

	// Compute latency percentiles from merged histograms
	pm.Latency.ReadP50Ms = histogramPercentile(readHistograms, 0.50) * 1000
	pm.Latency.ReadP95Ms = histogramPercentile(readHistograms, 0.95) * 1000
	pm.Latency.ReadP99Ms = histogramPercentile(readHistograms, 0.99) * 1000
	pm.Latency.WriteP50Ms = histogramPercentile(writeHistograms, 0.50) * 1000
	pm.Latency.WriteP95Ms = histogramPercentile(writeHistograms, 0.95) * 1000
	pm.Latency.WriteP99Ms = histogramPercentile(writeHistograms, 0.99) * 1000
}

// labelValue returns the value of a label with the given name from a slice of
// LabelPair, or an empty string if not found.
func labelValue(labels []*dto.LabelPair, name string) string {
	for _, lp := range labels {
		if lp.GetName() == name {
			return lp.GetValue()
		}
	}
	return ""
}

// histogramPercentile computes an approximate percentile (0..1) from one or more
// Prometheus histograms by merging their bucket counts and interpolating between
// bucket boundaries. Returns 0 if no data is available.
func histogramPercentile(histograms []*dto.Histogram, quantile float64) float64 {
	if len(histograms) == 0 {
		return 0
	}

	// Merge all histogram buckets into a unified sorted list
	type bucket struct {
		upperBound      float64
		cumulativeCount uint64
	}

	merged := make(map[float64]uint64)
	var totalCount uint64

	for _, h := range histograms {
		totalCount += h.GetSampleCount()
		for _, b := range h.GetBucket() {
			merged[b.GetUpperBound()] += b.GetCumulativeCount()
		}
	}

	if totalCount == 0 {
		return 0
	}

	buckets := make([]bucket, 0, len(merged))
	for ub, count := range merged {
		buckets = append(buckets, bucket{upperBound: ub, cumulativeCount: count})
	}
	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i].upperBound < buckets[j].upperBound
	})

	// Target count for the requested quantile
	target := quantile * float64(totalCount)

	// Linear interpolation between bucket boundaries
	var prevBound float64
	var prevCount uint64

	for _, b := range buckets {
		if math.IsInf(b.upperBound, 1) {
			continue
		}
		if float64(b.cumulativeCount) >= target {
			// Interpolate within this bucket
			bucketWidth := b.upperBound - prevBound
			bucketCount := float64(b.cumulativeCount - prevCount)
			if bucketCount == 0 {
				return b.upperBound
			}
			remaining := target - float64(prevCount)
			return prevBound + (remaining/bucketCount)*bucketWidth
		}
		prevBound = b.upperBound
		prevCount = b.cumulativeCount
	}

	// All observations exceed the last finite bucket; return the last bound
	if len(buckets) > 0 {
		return buckets[len(buckets)-1].upperBound
	}
	return 0
}
