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

package tracing

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTraceRaftProposal(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		shardID   uint64
		nodeID    uint64
		data      []byte
		err       error
		wantSpans int
	}{
		{
			name:      "successful proposal",
			enabled:   true,
			shardID:   1,
			nodeID:    100,
			data:      []byte("proposal data"),
			err:       nil,
			wantSpans: 1,
		},
		{
			name:      "proposal with error",
			enabled:   true,
			shardID:   2,
			nodeID:    200,
			data:      []byte("proposal"),
			err:       errors.New("proposal failed"),
			wantSpans: 1,
		},
		{
			name:      "tracing disabled",
			enabled:   false,
			shardID:   1,
			nodeID:    100,
			data:      []byte("proposal"),
			err:       nil,
			wantSpans: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			ctx, finish := tracer.TraceRaftProposal(context.Background(), tt.shardID, tt.nodeID, tt.data)
			assert.NotNil(t, ctx)
			assert.NotNil(t, finish)

			finish(tt.err)

			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpans)

			if tt.wantSpans > 0 {
				span := spans[0]
				assert.Equal(t, "raft.propose", span.Name())

				attrs := span.Attributes()
				hasAttr := func(key string) bool {
					for _, attr := range attrs {
						if string(attr.Key) == key {
							return true
						}
					}
					return false
				}
				assert.True(t, hasAttr("db.shard_id"))
				assert.True(t, hasAttr("db.node_id"))
			}
		})
	}
}

func TestTraceRaftApply(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		shardID   uint64
		nodeID    uint64
		term      uint64
		index     uint64
		err       error
		wantSpans int
	}{
		{
			name:      "successful apply",
			enabled:   true,
			shardID:   1,
			nodeID:    100,
			term:      5,
			index:     1000,
			err:       nil,
			wantSpans: 1,
		},
		{
			name:      "apply with error",
			enabled:   true,
			shardID:   2,
			nodeID:    200,
			term:      10,
			index:     2000,
			err:       errors.New("apply failed"),
			wantSpans: 1,
		},
		{
			name:      "tracing disabled",
			enabled:   false,
			shardID:   1,
			nodeID:    100,
			term:      5,
			index:     1000,
			err:       nil,
			wantSpans: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			ctx, finish := tracer.TraceRaftApply(context.Background(), tt.shardID, tt.nodeID, tt.term, tt.index)
			assert.NotNil(t, ctx)
			assert.NotNil(t, finish)

			finish(tt.err)

			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpans)

			if tt.wantSpans > 0 {
				span := spans[0]
				assert.Equal(t, "raft.apply", span.Name())

				attrs := span.Attributes()
				hasAttr := func(key string) bool {
					for _, attr := range attrs {
						if string(attr.Key) == key {
							return true
						}
					}
					return false
				}
				assert.True(t, hasAttr("raft.term"))
				assert.True(t, hasAttr("raft.log_index"))
			}
		})
	}
}

func TestTraceRaftSnapshot(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		shardID   uint64
		nodeID    uint64
		index     uint64
		err       error
		wantSpans int
	}{
		{
			name:      "successful snapshot",
			enabled:   true,
			shardID:   1,
			nodeID:    100,
			index:     5000,
			err:       nil,
			wantSpans: 1,
		},
		{
			name:      "snapshot with error",
			enabled:   true,
			shardID:   2,
			nodeID:    200,
			index:     10000,
			err:       errors.New("snapshot failed"),
			wantSpans: 1,
		},
		{
			name:      "tracing disabled",
			enabled:   false,
			shardID:   1,
			nodeID:    100,
			index:     5000,
			err:       nil,
			wantSpans: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			ctx, finish := tracer.TraceRaftSnapshot(context.Background(), tt.shardID, tt.nodeID, tt.index)
			assert.NotNil(t, ctx)
			assert.NotNil(t, finish)

			finish(tt.err)

			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpans)

			if tt.wantSpans > 0 {
				span := spans[0]
				assert.Equal(t, "raft.snapshot", span.Name())
			}
		})
	}
}

func TestTraceDBOperation(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		operation string
		key       string
		shardID   uint64
		err       error
		wantSpans int
	}{
		{
			name:      "successful get",
			enabled:   true,
			operation: "get",
			key:       "user:123",
			shardID:   1,
			err:       nil,
			wantSpans: 1,
		},
		{
			name:      "failed put",
			enabled:   true,
			operation: "put",
			key:       "user:456",
			shardID:   2,
			err:       errors.New("put failed"),
			wantSpans: 1,
		},
		{
			name:      "tracing disabled",
			enabled:   false,
			operation: "delete",
			key:       "user:789",
			shardID:   1,
			err:       nil,
			wantSpans: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			ctx, finish := tracer.TraceDBOperation(context.Background(), tt.operation, tt.key, tt.shardID)
			assert.NotNil(t, ctx)
			assert.NotNil(t, finish)

			finish(tt.err)

			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpans)

			if tt.wantSpans > 0 {
				span := spans[0]
				assert.Equal(t, "db."+tt.operation, span.Name())
			}
		})
	}
}

func TestTraceTransaction(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		txnID     string
		opCount   int
		err       error
		wantSpans int
	}{
		{
			name:      "successful transaction",
			enabled:   true,
			txnID:     "txn-123",
			opCount:   5,
			err:       nil,
			wantSpans: 1,
		},
		{
			name:      "failed transaction",
			enabled:   true,
			txnID:     "txn-456",
			opCount:   3,
			err:       errors.New("transaction failed"),
			wantSpans: 1,
		},
		{
			name:      "tracing disabled",
			enabled:   false,
			txnID:     "txn-789",
			opCount:   2,
			err:       nil,
			wantSpans: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			ctx, finish := tracer.TraceTransaction(context.Background(), tt.txnID, tt.opCount)
			assert.NotNil(t, ctx)
			assert.NotNil(t, finish)

			finish(tt.err)

			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpans)

			if tt.wantSpans > 0 {
				span := spans[0]
				assert.Equal(t, "txn.execute", span.Name())
			}
		})
	}
}

func TestTraceTransactionOperation(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		operation string
		key       string
		index     int
		err       error
		wantSpans int
	}{
		{
			name:      "successful operation",
			enabled:   true,
			operation: "put",
			key:       "key1",
			index:     0,
			err:       nil,
			wantSpans: 1,
		},
		{
			name:      "failed operation",
			enabled:   true,
			operation: "delete",
			key:       "key2",
			index:     1,
			err:       errors.New("operation failed"),
			wantSpans: 1,
		},
		{
			name:      "tracing disabled",
			enabled:   false,
			operation: "get",
			key:       "key3",
			index:     2,
			err:       nil,
			wantSpans: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			ctx, finish := tracer.TraceTransactionOperation(context.Background(), tt.operation, tt.key, tt.index)
			assert.NotNil(t, ctx)
			assert.NotNil(t, finish)

			finish(tt.err)

			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpans)

			if tt.wantSpans > 0 {
				span := spans[0]
				assert.Equal(t, "txn.op."+tt.operation, span.Name())
			}
		})
	}
}

func TestTraceWatch(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		watchID    string
		keyPrefix  string
		eventCount int
		err        error
		wantSpans  int
	}{
		{
			name:       "successful watch",
			enabled:    true,
			watchID:    "watch-123",
			keyPrefix:  "/prefix/",
			eventCount: 10,
			err:        nil,
			wantSpans:  1,
		},
		{
			name:       "watch with error",
			enabled:    true,
			watchID:    "watch-456",
			keyPrefix:  "/test/",
			eventCount: 5,
			err:        errors.New("watch failed"),
			wantSpans:  1,
		},
		{
			name:       "tracing disabled",
			enabled:    false,
			watchID:    "watch-789",
			keyPrefix:  "/keys/",
			eventCount: 0,
			err:        nil,
			wantSpans:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			ctx, finish := tracer.TraceWatch(context.Background(), tt.watchID, tt.keyPrefix)
			assert.NotNil(t, ctx)
			assert.NotNil(t, finish)

			finish(tt.eventCount, tt.err)

			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpans)

			if tt.wantSpans > 0 {
				span := spans[0]
				assert.Equal(t, "watch.stream", span.Name())
			}
		})
	}
}

func TestTraceWatchEvent(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		watchID   string
		key       string
		eventType string
	}{
		{
			name:      "put event",
			enabled:   true,
			watchID:   "watch-123",
			key:       "key1",
			eventType: "put",
		},
		{
			name:      "delete event",
			enabled:   true,
			watchID:   "watch-456",
			key:       "key2",
			eventType: "delete",
		},
		{
			name:      "tracing disabled",
			enabled:   false,
			watchID:   "watch-789",
			key:       "key3",
			eventType: "put",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			// Create a parent span
			ctx, span := tracer.StartSpan(context.Background(), "parent")

			// Add watch event
			tracer.TraceWatchEvent(ctx, tt.watchID, tt.key, tt.eventType)

			span.End()

			spans := sr.Ended()
			if tt.enabled {
				require.Len(t, spans, 1)
				events := spans[0].Events()
				assert.GreaterOrEqual(t, len(events), 1)
			} else {
				assert.Len(t, spans, 1)
			}
		})
	}
}

func TestTraceStorageOperation(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		operation string
		batchSize int
		err       error
		wantSpans int
	}{
		{
			name:      "single operation",
			enabled:   true,
			operation: "compact",
			batchSize: 0,
			err:       nil,
			wantSpans: 1,
		},
		{
			name:      "batch operation",
			enabled:   true,
			operation: "flush",
			batchSize: 100,
			err:       nil,
			wantSpans: 1,
		},
		{
			name:      "operation with error",
			enabled:   true,
			operation: "sync",
			batchSize: 50,
			err:       errors.New("sync failed"),
			wantSpans: 1,
		},
		{
			name:      "tracing disabled",
			enabled:   false,
			operation: "compact",
			batchSize: 10,
			err:       nil,
			wantSpans: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			ctx, finish := tracer.TraceStorageOperation(context.Background(), tt.operation, tt.batchSize)
			assert.NotNil(t, ctx)
			assert.NotNil(t, finish)

			finish(tt.err)

			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpans)

			if tt.wantSpans > 0 {
				span := spans[0]
				assert.Equal(t, "storage."+tt.operation, span.Name())
			}
		})
	}
}

func TestTraceReplication(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		sourceNode uint64
		targetNode uint64
		entryCount int
		err        error
		wantSpans  int
	}{
		{
			name:       "successful replication",
			enabled:    true,
			sourceNode: 1,
			targetNode: 2,
			entryCount: 100,
			err:        nil,
			wantSpans:  1,
		},
		{
			name:       "replication with error",
			enabled:    true,
			sourceNode: 2,
			targetNode: 3,
			entryCount: 50,
			err:        errors.New("replication failed"),
			wantSpans:  1,
		},
		{
			name:       "tracing disabled",
			enabled:    false,
			sourceNode: 1,
			targetNode: 2,
			entryCount: 10,
			err:        nil,
			wantSpans:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			ctx, finish := tracer.TraceReplication(context.Background(), tt.sourceNode, tt.targetNode, tt.entryCount)
			assert.NotNil(t, ctx)
			assert.NotNil(t, finish)

			finish(tt.err)

			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpans)

			if tt.wantSpans > 0 {
				span := spans[0]
				assert.Equal(t, "replication.sync", span.Name())
			}
		})
	}
}

func TestTraceBatchOperation(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		operation string
		keyCount  int
		shardID   uint64
		err       error
		wantSpans int
	}{
		{
			name:      "successful batch",
			enabled:   true,
			operation: "put",
			keyCount:  10,
			shardID:   1,
			err:       nil,
			wantSpans: 1,
		},
		{
			name:      "batch with error",
			enabled:   true,
			operation: "delete",
			keyCount:  5,
			shardID:   2,
			err:       errors.New("batch failed"),
			wantSpans: 1,
		},
		{
			name:      "tracing disabled",
			enabled:   false,
			operation: "get",
			keyCount:  3,
			shardID:   1,
			err:       nil,
			wantSpans: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			ctx, finish := tracer.TraceBatchOperation(context.Background(), tt.operation, tt.keyCount, tt.shardID)
			assert.NotNil(t, ctx)
			assert.NotNil(t, finish)

			finish(tt.err)

			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpans)

			if tt.wantSpans > 0 {
				span := spans[0]
				assert.Equal(t, "db.batch."+tt.operation, span.Name())
			}
		})
	}
}

func TestTraceDiscovery(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		method    string
		peerCount int
		err       error
		wantSpans int
	}{
		{
			name:      "successful discovery",
			enabled:   true,
			method:    "dns",
			peerCount: 5,
			err:       nil,
			wantSpans: 1,
		},
		{
			name:      "discovery with error",
			enabled:   true,
			method:    "k8s",
			peerCount: 0,
			err:       errors.New("discovery failed"),
			wantSpans: 1,
		},
		{
			name:      "tracing disabled",
			enabled:   false,
			method:    "static",
			peerCount: 3,
			err:       nil,
			wantSpans: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			ctx, finish := tracer.TraceDiscovery(context.Background(), tt.method)
			assert.NotNil(t, ctx)
			assert.NotNil(t, finish)

			finish(tt.peerCount, tt.err)

			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpans)

			if tt.wantSpans > 0 {
				span := spans[0]
				assert.Equal(t, "discovery."+tt.method, span.Name())
			}
		})
	}
}

func TestTraceMembership(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		operation string
		nodeID    uint64
		err       error
		wantSpans int
	}{
		{
			name:      "add member",
			enabled:   true,
			operation: "add",
			nodeID:    100,
			err:       nil,
			wantSpans: 1,
		},
		{
			name:      "remove member",
			enabled:   true,
			operation: "remove",
			nodeID:    200,
			err:       nil,
			wantSpans: 1,
		},
		{
			name:      "membership with error",
			enabled:   true,
			operation: "update",
			nodeID:    300,
			err:       errors.New("membership failed"),
			wantSpans: 1,
		},
		{
			name:      "tracing disabled",
			enabled:   false,
			operation: "add",
			nodeID:    100,
			err:       nil,
			wantSpans: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			ctx, finish := tracer.TraceMembership(context.Background(), tt.operation, tt.nodeID)
			assert.NotNil(t, ctx)
			assert.NotNil(t, finish)

			finish(tt.err)

			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpans)

			if tt.wantSpans > 0 {
				span := spans[0]
				assert.Equal(t, "membership."+tt.operation, span.Name())
			}
		})
	}
}
