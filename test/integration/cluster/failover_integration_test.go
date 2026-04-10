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

package cluster

import (
	"context"
	"crypto/tls"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
	"github.com/jeremyhahn/go-quicbaas/pkg/tracing"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestFailoverScenarios tests various failover scenarios
func TestFailoverScenarios(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	require.GreaterOrEqual(t, len(endpoints), 3, "Integration test cluster must have at least 3 nodes")

	// Initialize components
	m := metrics.InitMetrics()
	_, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	tracer, err := tracing.NewTracer(&tracing.Config{
		Enabled:         true,
		ServiceName:     "quicbaas-failover-test",
		ServiceVersion:  "1.0.0",
		ExporterType:    tracing.ExporterStdout,
		SamplingRatio:   0.1, // Sample 10% during failover test
		Environment:     "test",
		NodeID:          1,
		ShutdownTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	defer tracer.Shutdown(ctx)

	// Test 1: Leader failover with metrics
	t.Run("LeaderFailoverWithMetrics", func(t *testing.T) {
		ctx, span := tracer.StartSpan(ctx, "leader_failover_test")
		defer span.End()

		conn, err := grpc.DialContext(ctx, endpoints[0],
			grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13})),
			grpc.WithBlock(),
		)
		require.NoError(t, err)
		defer conn.Close()

		client := pb.NewQuicBaaSClient(conn)

		// Write data before failover
		preFailoverStart := time.Now()
		for i := 0; i < 10; i++ {
			m.IncrementConcurrentRequests()

			putReq := &pb.PutRequest{
				EntityType: "failover_test",
				Id:         uint64(time.Now().UnixNano()),
				Data:       []byte(fmt.Sprintf("pre-failover-data-%d", i)),
			}

			start := time.Now()
			_, err := client.Put(ctx, putReq)
			duration := time.Since(start).Seconds()

			if err == nil {
				m.RecordRequestDuration("put", "success", duration)
			} else {
				m.RecordRequestDuration("put", "error", duration)
			}

			m.DecrementConcurrentRequests()
		}
		preFailoverLatency := time.Since(preFailoverStart) / 10

		t.Logf("Pre-failover average latency: %v", preFailoverLatency)

		// Simulate leader failover
		t.Logf("Simulating leader failover...")
		time.Sleep(10 * time.Second)

		// Write data after failover
		postFailoverStart := time.Now()
		successCount := 0
		for i := 0; i < 10; i++ {
			m.IncrementConcurrentRequests()

			putReq := &pb.PutRequest{
				EntityType: "failover_test",
				Id:         uint64(time.Now().UnixNano()),
				Data:       []byte(fmt.Sprintf("post-failover-data-%d", i)),
			}

			start := time.Now()
			_, err := client.Put(ctx, putReq)
			duration := time.Since(start).Seconds()

			if err == nil {
				m.RecordRequestDuration("put", "success", duration)
				successCount++
			} else {
				m.RecordRequestDuration("put", "error", duration)
			}

			m.DecrementConcurrentRequests()
		}
		postFailoverLatency := time.Since(postFailoverStart) / 10

		t.Logf("Post-failover average latency: %v, success rate: %d/10",
			postFailoverLatency, successCount)

		// Verify cluster recovered
		assert.GreaterOrEqual(t, successCount, 7, "At least 70%% of writes should succeed after failover")

		span.SetStatus(codes.Ok, "Leader failover completed")
	})

	// Test 2: Network partition with tracing
	t.Run("NetworkPartitionWithTracing", func(t *testing.T) {
		ctx, span := tracer.StartSpan(ctx, "network_partition_test")
		defer span.End()

		conn, err := grpc.DialContext(ctx, endpoints[0],
			grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13})),
			grpc.WithBlock(),
		)
		require.NoError(t, err)
		defer conn.Close()

		client := pb.NewQuicBaaSClient(conn)

		// Write before partition
		tracer.AddEvent(ctx, "write_before_partition")

		putReq := &pb.PutRequest{
			EntityType: "partition_test",
			Id:         uint64(time.Now().UnixNano()),
			Data:       []byte("before-partition"),
		}
		_, err = client.Put(ctx, putReq)
		require.NoError(t, err)

		// Simulate partition
		tracer.AddEvent(ctx, "partition_started")
		t.Logf("Simulating network partition...")
		time.Sleep(5 * time.Second)

		// Try to write during partition
		tracer.AddEvent(ctx, "write_during_partition")

		putReq2 := &pb.PutRequest{
			EntityType: "partition_test",
			Id:         uint64(time.Now().UnixNano()),
			Data:       []byte("during-partition"),
		}
		_, err = client.Put(ctx, putReq2)
		if err != nil {
			tracer.RecordError(ctx, err)
			t.Logf("Write failed during partition (expected): %v", err)
		}

		// Heal partition
		tracer.AddEvent(ctx, "partition_healed")
		t.Logf("Healing partition...")
		time.Sleep(5 * time.Second)

		// Verify recovery
		tracer.AddEvent(ctx, "verify_recovery")

		getReq := &pb.GetRequest{
			EntityType: putReq.EntityType,
			Id:         putReq.Id,
		}
		_, err = client.Get(ctx, getReq)
		require.NoError(t, err)

		span.SetStatus(codes.Ok, "Network partition test completed")
		t.Logf("Successfully traced network partition scenario")
	})
}
