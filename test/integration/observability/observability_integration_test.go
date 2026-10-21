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

//go:build integration

package observability

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
	"github.com/jeremyhahn/go-quicbaas/pkg/tracing"
)

func TestCompleteFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Initialize components
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	// 1. Initialize Metrics
	m := metrics.InitMetrics()
	require.NotNil(t, m)

	// 2. Initialize Tracing
	tracer, err := tracing.NewTracer(&tracing.Config{
		Enabled:         true,
		ServiceName:     "quicbaas-test",
		ServiceVersion:  "1.0.0",
		ExporterType:    tracing.ExporterStdout,
		SamplingRatio:   1.0,
		Environment:     "test",
		NodeID:          1,
		ShutdownTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	defer tracer.Shutdown(ctx)

	// 3. Initialize Cache. There is no step for minting a bearer: the caller is
	// authenticated by the integration TLS client certificate on the connection,
	// and the authz gate resolves against qrdb's policy decision point. The
	// in-process token manager this test used to build was deleted with the
	// parallel identity model (issue #905); the server never consulted it.
	cacheManager, err := cache.NewManager(cache.Config{
		L1Size:          100,
		L1TTL:           5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}, helpers.NewLoggerAdapter(log))
	require.NoError(t, err)
	defer cacheManager.Close()

	// Connect to QuicBaaS
	endpoint := helpers.GetTestEndpoint()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	conn, err := grpc.DialContext(ctx, endpoint,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithBlock(),
		grpc.WithPerRPCCredentials(helpers.TenantOperatorGRPCCredentials(t)),
	)
	require.NoError(t, err)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Start tracing span for complete operation
	ctx, endSpan := tracer.TraceRequest(ctx, "complete_flow",
		attribute.String("test", "TestCompleteFlow"),
	)
	defer endSpan()

	// Record metrics
	startTime := time.Now()
	m.IncrementConcurrentRequests()
	defer m.DecrementConcurrentRequests()

	// Test 1: Write with authentication (will be added in metadata)
	t.Run("WriteWithAuth", func(t *testing.T) {
		testKey := uint64(time.Now().UnixNano())
		testValue := []byte("test-data")

		putReq := &pb.PutRequest{
			EntityType: "test",
			Id:         testKey,
			Data:       testValue,
		}

		putResp, err := client.Put(ctx, putReq)
		require.NoError(t, err)
		assert.NotNil(t, putResp)

		// Record metrics
		duration := time.Since(startTime).Seconds()
		m.RecordRequestDuration("put", "success", duration)
		m.RecordRequestSize("put", float64(len(testValue)))

		t.Logf("Successfully wrote data with authentication: key=%d", testKey)
	})

	// Test 2: Read with cache check
	t.Run("ReadWithCache", func(t *testing.T) {
		testKey := uint64(time.Now().UnixNano())
		testValue := []byte("cached-test-data")

		// First write
		putReq := &pb.PutRequest{
			EntityType: "cache_test",
			Id:         testKey,
			Data:       testValue,
		}
		_, err := client.Put(ctx, putReq)
		require.NoError(t, err)

		// Cache the key
		cacheKey := fmt.Sprintf("cache_test/%d", testKey)
		err = cacheManager.Set(ctx, cacheKey, testValue)
		require.NoError(t, err)

		// Read from cache
		cachedValue, err := cacheManager.Get(ctx, cacheKey)
		require.NoError(t, err)
		assert.Equal(t, testValue, cachedValue)

		// Verify cache hit metrics
		stats := cacheManager.Stats()
		assert.Greater(t, stats.L1Hits, int64(0))

		t.Logf("Cache hit rate: %.2f%%", stats.L1HitRate*100)
	})

	// Test 3: Transaction with metrics
	t.Run("TransactionWithMetrics", func(t *testing.T) {
		txStart := time.Now()

		// Simulate transaction operations
		keys := make([]uint64, 5)
		for i := 0; i < 5; i++ {
			keys[i] = uint64(time.Now().UnixNano() + int64(i*1000))

			putReq := &pb.PutRequest{
				EntityType: "txn_test",
				Id:         keys[i],
				Data:       []byte(fmt.Sprintf("txn-data-%d", i)),
			}

			_, err := client.Put(ctx, putReq)
			require.NoError(t, err)
		}

		// Record transaction metrics
		txDuration := time.Since(txStart).Seconds()
		m.RecordTransactionDuration(txDuration)
		m.RecordTransactionSize(float64(len(keys)))

		t.Logf("Transaction completed: %d operations in %.3fs", len(keys), txDuration)
	})

	// Test 5: Trace propagation
	t.Run("TracePropagation", func(t *testing.T) {
		// Create nested span
		ctx, childSpan := tracer.StartSpan(ctx, "nested_operation")
		defer childSpan.End()

		tracer.SetAttributes(ctx,
			attribute.String("operation", "nested_test"),
			attribute.Int("iteration", 1),
		)

		// Simulate work
		time.Sleep(10 * time.Millisecond)

		// Record success
		span := tracer.SpanFromContext(ctx)
		span.SetStatus(codes.Ok, "Nested operation completed")

		t.Logf("Trace propagation successful")
	})

	// Verify overall metrics
	duration := time.Since(startTime).Seconds()
	m.RecordRequestDuration("complete_flow", "success", duration)

	t.Logf("Complete flow test passed in %.3fs", duration)
}

// TestMetricsCollection tests metrics collection during various operations
func TestMetricsCollection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Initialize metrics
	m := metrics.InitMetrics()
	require.NotNil(t, m)

	// Reset metrics for clean test
	metrics.Reset()
	m = metrics.InitMetrics()

	// Connect to QuicBaaS
	endpoint := helpers.GetTestEndpoint()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	conn, err := grpc.DialContext(ctx, endpoint,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithBlock(),
		grpc.WithPerRPCCredentials(helpers.TenantOperatorGRPCCredentials(t)),
	)
	require.NoError(t, err)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Perform operations and collect metrics
	operationCount := 100
	for i := 0; i < operationCount; i++ {
		m.IncrementConcurrentRequests()

		start := time.Now()

		putReq := &pb.PutRequest{
			EntityType: "metrics_test",
			Id:         uint64(time.Now().UnixNano()),
			Data:       []byte(fmt.Sprintf("metrics-data-%d", i)),
		}

		_, err := client.Put(ctx, putReq)

		duration := time.Since(start).Seconds()

		if err != nil {
			m.RecordRequestDuration("put", "error", duration)
		} else {
			m.RecordRequestDuration("put", "success", duration)
			m.RecordRequestSize("put", float64(len(putReq.Data)))
		}

		m.DecrementConcurrentRequests()
	}

	// Verify metrics were collected
	gatherer := metrics.Registry
	metricFamilies, err := gatherer.Gather()
	require.NoError(t, err)

	foundRequestDuration := false
	foundRequestSize := false

	for _, mf := range metricFamilies {
		switch *mf.Name {
		case "quicbaas_request_duration_seconds":
			foundRequestDuration = true
			assert.Greater(t, len(mf.Metric), 0, "Request duration metrics should be recorded")
		case "quicbaas_request_size_bytes":
			foundRequestSize = true
			assert.Greater(t, len(mf.Metric), 0, "Request size metrics should be recorded")
		}
	}

	assert.True(t, foundRequestDuration, "Request duration metrics should exist")
	assert.True(t, foundRequestSize, "Request size metrics should exist")

	t.Logf("Successfully collected metrics for %d operations", operationCount)
}
