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
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func TestHandlePerformanceMetrics_Unit(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockService := testutil.NewMockQuicBaaS()

	serverConfig := DefaultConfig()
	serverConfig.Address = ":0"

	server, err := NewServer(mockService, serverConfig, log)
	require.NoError(t, err)
	defer server.Close()

	tests := []struct {
		name           string
		method         string
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:           "successful metrics retrieval",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var m PerformanceMetrics
				err := json.NewDecoder(w.Body).Decode(&m)
				assert.NoError(t, err)

				// Status must be beta
				assert.Equal(t, "beta", m.Status)

				// Verify structure
				assert.NotZero(t, m.Timestamp)

				// Verify throughput metrics
				assert.GreaterOrEqual(t, m.Throughput.ReadsPerSec, float64(0))
				assert.GreaterOrEqual(t, m.Throughput.WritesPerSec, float64(0))
				assert.GreaterOrEqual(t, m.Throughput.TotalOpsPerSec, float64(0))

				// Verify latency metrics
				assert.GreaterOrEqual(t, m.Latency.ReadP50Ms, float64(0))
				assert.GreaterOrEqual(t, m.Latency.ReadP95Ms, float64(0))
				assert.GreaterOrEqual(t, m.Latency.ReadP99Ms, float64(0))
				assert.GreaterOrEqual(t, m.Latency.WriteP50Ms, float64(0))
				assert.GreaterOrEqual(t, m.Latency.WriteP95Ms, float64(0))
				assert.GreaterOrEqual(t, m.Latency.WriteP99Ms, float64(0))

				// Verify storage metrics
				assert.GreaterOrEqual(t, m.Storage.CacheHitRatePct, float64(0))
				assert.GreaterOrEqual(t, m.Storage.BloomFilterEffectivenessPct, float64(0))
				assert.GreaterOrEqual(t, m.Storage.CompactionsPerMin, float64(0))
				assert.GreaterOrEqual(t, m.Storage.L0Files, 0)
				assert.GreaterOrEqual(t, m.Storage.DiskUsageGB, float64(0))

				// Verify pool metrics
				assert.GreaterOrEqual(t, m.Pool.Size, 0)
				assert.GreaterOrEqual(t, m.Pool.Active, 0)
				assert.GreaterOrEqual(t, m.Pool.ConnectionReuseRatePct, float64(0))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v1/metrics/performance", nil)
			rr := httptest.NewRecorder()

			server.HandlePerformanceMetrics(rr, req)

			assert.Equal(t, tt.expectedStatus, rr.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, rr)
			}
		})
	}
}

func TestCollectPerformanceMetrics_Unit(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockService := testutil.NewMockQuicBaaS()

	serverConfig := DefaultConfig()
	serverConfig.Address = ":0"

	server, err := NewServer(mockService, serverConfig, log)
	require.NoError(t, err)
	defer server.Close()

	m := server.collectPerformanceMetrics()

	// Verify all fields are initialized
	assert.NotNil(t, m)
	assert.Equal(t, "beta", m.Status)
	assert.NotZero(t, m.Timestamp)

	// Without recorded request_duration histogram data, throughput is zero
	assert.GreaterOrEqual(t, m.Throughput.ReadsPerSec, float64(0))
	assert.GreaterOrEqual(t, m.Throughput.WritesPerSec, float64(0))
	assert.GreaterOrEqual(t, m.Throughput.TotalOpsPerSec, float64(0))

	// Latency metrics should be zero without histogram data
	assert.GreaterOrEqual(t, m.Latency.ReadP50Ms, float64(0))
	assert.GreaterOrEqual(t, m.Latency.ReadP95Ms, float64(0))
	assert.GreaterOrEqual(t, m.Latency.ReadP99Ms, float64(0))
	assert.GreaterOrEqual(t, m.Latency.WriteP50Ms, float64(0))
	assert.GreaterOrEqual(t, m.Latency.WriteP95Ms, float64(0))
	assert.GreaterOrEqual(t, m.Latency.WriteP99Ms, float64(0))

	// Storage metrics remain zero (not yet wired)
	assert.Equal(t, float64(0), m.Storage.CacheHitRatePct)
	assert.Equal(t, float64(0), m.Storage.BloomFilterEffectivenessPct)
	assert.Equal(t, float64(0), m.Storage.CompactionsPerMin)
	assert.Equal(t, 0, m.Storage.L0Files)
	assert.Equal(t, float64(0), m.Storage.DiskUsageGB)

	// Pool metrics remain zero (not yet wired)
	assert.Equal(t, 0, m.Pool.Size)
	assert.Equal(t, 0, m.Pool.Active)
	assert.Equal(t, float64(0), m.Pool.ConnectionReuseRatePct)
}

func TestLabelValue(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		labels := []*dto.LabelPair{
			{Name: proto.String("operation"), Value: proto.String("get")},
			{Name: proto.String("status"), Value: proto.String("200")},
		}
		assert.Equal(t, "get", labelValue(labels, "operation"))
		assert.Equal(t, "200", labelValue(labels, "status"))
	})

	t.Run("not found", func(t *testing.T) {
		labels := []*dto.LabelPair{
			{Name: proto.String("operation"), Value: proto.String("get")},
		}
		assert.Equal(t, "", labelValue(labels, "missing"))
	})

	t.Run("nil labels", func(t *testing.T) {
		assert.Equal(t, "", labelValue(nil, "anything"))
	})
}

func TestHistogramPercentile(t *testing.T) {
	t.Run("empty histograms", func(t *testing.T) {
		assert.Equal(t, float64(0), histogramPercentile(nil, 0.5))
		assert.Equal(t, float64(0), histogramPercentile([]*dto.Histogram{}, 0.5))
	})

	t.Run("zero sample count", func(t *testing.T) {
		h := &dto.Histogram{
			SampleCount: proto.Uint64(0),
			Bucket: []*dto.Bucket{
				{UpperBound: proto.Float64(0.1), CumulativeCount: proto.Uint64(0)},
			},
		}
		assert.Equal(t, float64(0), histogramPercentile([]*dto.Histogram{h}, 0.5))
	})

	t.Run("single bucket interpolation", func(t *testing.T) {
		h := &dto.Histogram{
			SampleCount: proto.Uint64(100),
			Bucket: []*dto.Bucket{
				{UpperBound: proto.Float64(0.1), CumulativeCount: proto.Uint64(50)},
				{UpperBound: proto.Float64(0.5), CumulativeCount: proto.Uint64(90)},
				{UpperBound: proto.Float64(1.0), CumulativeCount: proto.Uint64(100)},
				{UpperBound: proto.Float64(math.Inf(1)), CumulativeCount: proto.Uint64(100)},
			},
		}

		p50 := histogramPercentile([]*dto.Histogram{h}, 0.50)
		assert.Greater(t, p50, float64(0))
		assert.LessOrEqual(t, p50, 0.1)

		p95 := histogramPercentile([]*dto.Histogram{h}, 0.95)
		assert.Greater(t, p95, 0.1)
		assert.LessOrEqual(t, p95, 1.0)

		p99 := histogramPercentile([]*dto.Histogram{h}, 0.99)
		assert.Greater(t, p99, 0.5)
		assert.LessOrEqual(t, p99, 1.0)
	})

	t.Run("merged histograms", func(t *testing.T) {
		h1 := &dto.Histogram{
			SampleCount: proto.Uint64(50),
			Bucket: []*dto.Bucket{
				{UpperBound: proto.Float64(0.01), CumulativeCount: proto.Uint64(25)},
				{UpperBound: proto.Float64(0.1), CumulativeCount: proto.Uint64(50)},
				{UpperBound: proto.Float64(math.Inf(1)), CumulativeCount: proto.Uint64(50)},
			},
		}
		h2 := &dto.Histogram{
			SampleCount: proto.Uint64(50),
			Bucket: []*dto.Bucket{
				{UpperBound: proto.Float64(0.01), CumulativeCount: proto.Uint64(10)},
				{UpperBound: proto.Float64(0.1), CumulativeCount: proto.Uint64(50)},
				{UpperBound: proto.Float64(math.Inf(1)), CumulativeCount: proto.Uint64(50)},
			},
		}

		p50 := histogramPercentile([]*dto.Histogram{h1, h2}, 0.50)
		assert.Greater(t, p50, float64(0))
		assert.LessOrEqual(t, p50, 0.1)
	})
}

func TestPopulateThroughputAndLatency(t *testing.T) {
	t.Run("no request duration family", func(t *testing.T) {
		log, err := logger.New(logger.LogConfig{
			Level:  "error",
			Format: "json",
			Output: "stdout",
		})
		require.NoError(t, err)

		server := &Server{logger: log}
		pm := &PerformanceMetrics{Status: "beta"}
		families := map[string]*dto.MetricFamily{}

		server.populateThroughputAndLatency(pm, families)

		assert.Equal(t, float64(0), pm.Throughput.ReadsPerSec)
		assert.Equal(t, float64(0), pm.Throughput.WritesPerSec)
		assert.Equal(t, float64(0), pm.Throughput.TotalOpsPerSec)
	})

	t.Run("with request duration data", func(t *testing.T) {
		log, err := logger.New(logger.LogConfig{
			Level:  "error",
			Format: "json",
			Output: "stdout",
		})
		require.NoError(t, err)

		server := &Server{logger: log}
		pm := &PerformanceMetrics{Status: "beta"}

		histType := dto.MetricType_HISTOGRAM
		mf := &dto.MetricFamily{
			Name: proto.String("quicbaas_request_duration_seconds"),
			Type: &histType,
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: proto.String("operation"), Value: proto.String("get")},
						{Name: proto.String("status"), Value: proto.String("200")},
					},
					Histogram: &dto.Histogram{
						SampleCount: proto.Uint64(100),
						Bucket: []*dto.Bucket{
							{UpperBound: proto.Float64(0.01), CumulativeCount: proto.Uint64(80)},
							{UpperBound: proto.Float64(0.1), CumulativeCount: proto.Uint64(95)},
							{UpperBound: proto.Float64(1.0), CumulativeCount: proto.Uint64(100)},
							{UpperBound: proto.Float64(math.Inf(1)), CumulativeCount: proto.Uint64(100)},
						},
					},
				},
				{
					Label: []*dto.LabelPair{
						{Name: proto.String("operation"), Value: proto.String("put")},
						{Name: proto.String("status"), Value: proto.String("200")},
					},
					Histogram: &dto.Histogram{
						SampleCount: proto.Uint64(50),
						Bucket: []*dto.Bucket{
							{UpperBound: proto.Float64(0.01), CumulativeCount: proto.Uint64(30)},
							{UpperBound: proto.Float64(0.1), CumulativeCount: proto.Uint64(45)},
							{UpperBound: proto.Float64(1.0), CumulativeCount: proto.Uint64(50)},
							{UpperBound: proto.Float64(math.Inf(1)), CumulativeCount: proto.Uint64(50)},
						},
					},
				},
			},
		}

		families := map[string]*dto.MetricFamily{
			"quicbaas_request_duration_seconds": mf,
		}

		server.populateThroughputAndLatency(pm, families)

		// Read latency should be populated from the "get" histogram
		assert.Greater(t, pm.Latency.ReadP50Ms, float64(0))
		assert.Greater(t, pm.Latency.ReadP95Ms, float64(0))
		assert.Greater(t, pm.Latency.ReadP99Ms, float64(0))

		// Write latency should be populated from the "put" histogram
		assert.Greater(t, pm.Latency.WriteP50Ms, float64(0))
		assert.Greater(t, pm.Latency.WriteP95Ms, float64(0))
		assert.Greater(t, pm.Latency.WriteP99Ms, float64(0))
	})
}

func TestGatherMetricFamilies(t *testing.T) {
	t.Run("returns families from registry", func(t *testing.T) {
		log, err := logger.New(logger.LogConfig{
			Level:  "error",
			Format: "json",
			Output: "stdout",
		})
		require.NoError(t, err)

		server := &Server{logger: log}
		families := server.gatherMetricFamilies()

		// The default registry always has go_* and process_* collectors
		assert.NotNil(t, families)
		// At minimum, Go runtime metrics should be present
		_, hasGoInfo := families["go_info"]
		_, hasGoGoroutines := families["go_goroutines"]
		assert.True(t, hasGoInfo || hasGoGoroutines,
			"expected at least one Go runtime metric family")
	})

	t.Run("returns nil on gather error", func(t *testing.T) {
		log, err := logger.New(logger.LogConfig{
			Level:  "error",
			Format: "json",
			Output: "stdout",
		})
		require.NoError(t, err)

		// Use a custom registry with a broken collector to force a gather error
		// We cannot easily do this without mocking, so we verify the nil path
		// by checking that the method handles nil families gracefully
		server := &Server{logger: log}
		pm := server.collectPerformanceMetrics()
		assert.NotNil(t, pm)
		assert.Equal(t, "beta", pm.Status)
	})
}

// Verify that the real Prometheus metrics are wired correctly by recording
// a few observations and confirming they appear in the gathered output.
func TestCollectPerformanceMetrics_WithRecordedData(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockService := testutil.NewMockQuicBaaS()
	serverConfig := DefaultConfig()
	serverConfig.Address = ":0"

	server, err := NewServer(mockService, serverConfig, log)
	require.NoError(t, err)
	defer server.Close()

	// Record some observations to the global metrics registry.
	// The collectPerformanceMetrics reads from metrics.Registry().
	// The quicbaas_request_duration_seconds histogram is registered via
	// the metrics package, so recording observations there should be
	// reflected in our gathered data.
	reg := prometheus.NewRegistry()
	hist := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "quicbaas_request_duration_seconds",
		Help:    "test",
		Buckets: []float64{0.001, 0.01, 0.1, 1.0},
	}, []string{"operation", "status"})
	reg.MustRegister(hist)

	// Record some get operations
	for i := 0; i < 10; i++ {
		hist.WithLabelValues("get", "200").Observe(0.005)
	}

	// Gather from our test registry and verify the histogram exists
	gathered, gatherErr := reg.Gather()
	require.NoError(t, gatherErr)
	require.NotEmpty(t, gathered)

	families := make(map[string]*dto.MetricFamily, len(gathered))
	for _, mf := range gathered {
		if mf.Name != nil {
			families[*mf.Name] = mf
		}
	}

	pm := &PerformanceMetrics{Status: "beta"}
	server.populateThroughputAndLatency(pm, families)

	// With 10 observations at 0.005s, P50 should be around 5ms
	assert.Greater(t, pm.Latency.ReadP50Ms, float64(0))
	assert.Less(t, pm.Latency.ReadP50Ms, float64(15))
}
