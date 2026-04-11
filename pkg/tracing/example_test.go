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

package tracing_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.opentelemetry.io/otel/attribute"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tracing"
)

// Example demonstrates basic tracing setup.
func Example() {
	// Create tracer configuration
	cfg := &tracing.Config{
		Enabled:         true,
		ServiceName:     "quicbaas",
		ServiceVersion:  "1.0.0",
		ExporterType:    tracing.ExporterStdout,
		SamplingRatio:   1.0,
		Environment:     "development",
		NodeID:          1,
		ShutdownTimeout: 5 * time.Second,
	}

	// Create tracer
	tracer, err := tracing.NewTracer(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer tracer.Shutdown(context.Background())

	// Start tracing an operation
	ctx := context.Background()
	ctx, finish := tracer.TraceRequest(ctx, "example.operation",
		attribute.String("example.key", "value"),
	)
	defer finish()

	// Simulate work
	time.Sleep(10 * time.Millisecond)

	fmt.Println("Operation traced successfully")
	// Output: Operation traced successfully
}

// ExampleTracer_TraceDBOperation demonstrates tracing database operations.
func ExampleTracer_TraceDBOperation() {
	tracer, _ := tracing.NewTracer(&tracing.Config{
		Enabled:       true,
		ServiceName:   "quicbaas",
		ExporterType:  tracing.ExporterStdout,
		SamplingRatio: 1.0,
	})
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()

	// Trace a GET operation
	ctx, finish := tracer.TraceDBOperation(ctx, "get", "user:123", 1)

	// Simulate database operation
	// value, err := db.Get(ctx, key)

	// Finish span with error (or nil)
	finish(nil)

	fmt.Println("Database operation traced")
	// Output: Database operation traced
}

// ExampleTracer_TraceRaftProposal demonstrates tracing Raft proposals.
func ExampleTracer_TraceRaftProposal() {
	tracer, _ := tracing.NewTracer(&tracing.Config{
		Enabled:       true,
		ServiceName:   "quicbaas",
		ExporterType:  tracing.ExporterStdout,
		SamplingRatio: 1.0,
	})
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()
	shardID := uint64(1)
	nodeID := uint64(1)
	data := []byte("proposal data")

	// Trace Raft proposal
	ctx, finish := tracer.TraceRaftProposal(ctx, shardID, nodeID, data)

	// Simulate Raft proposal
	// err := raft.Propose(ctx, data)

	// Finish span
	finish(nil)

	fmt.Println("Raft proposal traced")
	// Output: Raft proposal traced
}

// ExampleTracer_TraceTransaction demonstrates tracing transactions.
func ExampleTracer_TraceTransaction() {
	tracer, _ := tracing.NewTracer(&tracing.Config{
		Enabled:       true,
		ServiceName:   "quicbaas",
		ExporterType:  tracing.ExporterStdout,
		SamplingRatio: 1.0,
	})
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()
	txnID := "txn-123"
	operations := []struct {
		op  string
		key string
	}{
		{"put", "key1"},
		{"put", "key2"},
		{"delete", "key3"},
	}

	// Trace transaction
	ctx, finishTxn := tracer.TraceTransaction(ctx, txnID, len(operations))

	// Trace each operation
	for i, op := range operations {
		_, finishOp := tracer.TraceTransactionOperation(ctx, op.op, op.key, i)
		// Execute operation
		finishOp(nil)
	}

	finishTxn(nil)

	fmt.Println("Transaction traced with", len(operations), "operations")
	// Output: Transaction traced with 3 operations
}

// ExampleTracer_TraceWatch demonstrates tracing watch operations.
func ExampleTracer_TraceWatch() {
	tracer, _ := tracing.NewTracer(&tracing.Config{
		Enabled:       true,
		ServiceName:   "quicbaas",
		ExporterType:  tracing.ExporterStdout,
		SamplingRatio: 1.0,
	})
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()
	watchID := "watch-456"
	keyPrefix := "/config/"

	// Start watch span
	_, finish := tracer.TraceWatch(ctx, watchID, keyPrefix)

	// Simulate receiving events
	eventCount := 0
	for i := 0; i < 3; i++ {
		tracer.TraceWatchEvent(ctx, watchID, fmt.Sprintf("%skey%d", keyPrefix, i), "put")
		eventCount++
	}

	// Finish watch
	finish(eventCount, nil)

	fmt.Println("Watch operation traced with", eventCount, "events")
	// Output: Watch operation traced with 3 events
}

// ExampleTracer_nestedSpans demonstrates creating nested spans.
func ExampleTracer_nestedSpans() {
	tracer, _ := tracing.NewTracer(&tracing.Config{
		Enabled:       true,
		ServiceName:   "quicbaas",
		ExporterType:  tracing.ExporterStdout,
		SamplingRatio: 1.0,
	})
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()

	// Parent span
	ctx, parentSpan := tracer.StartSpan(ctx, "parent.operation")
	defer parentSpan.End()

	// Child span 1
	ctx1, childSpan1 := tracer.StartSpan(ctx, "child.operation.1")
	time.Sleep(5 * time.Millisecond)
	childSpan1.End()

	// Child span 2
	ctx2, childSpan2 := tracer.StartSpan(ctx, "child.operation.2")
	time.Sleep(5 * time.Millisecond)
	childSpan2.End()

	// Both child spans share the same parent
	_, _ = ctx1, ctx2

	fmt.Println("Nested spans traced")
	// Output: Nested spans traced
}

// ExampleTracer_attributes demonstrates adding attributes and events.
func ExampleTracer_attributes() {
	tracer, _ := tracing.NewTracer(&tracing.Config{
		Enabled:       true,
		ServiceName:   "quicbaas",
		ExporterType:  tracing.ExporterStdout,
		SamplingRatio: 1.0,
	})
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()
	ctx, span := tracer.StartSpan(ctx, "operation.with.attributes")
	defer span.End()

	// Set attributes
	tracer.SetAttributes(ctx,
		tracing.AttrDBOperation.String("batch_put"),
		tracing.AttrDBKeyCount.Int(100),
		tracing.AttrDBShardID.Int64(1),
	)

	// Add events
	tracer.AddEvent(ctx, "validation.complete",
		attribute.Int("validated_count", 100),
	)

	// Simulate work
	time.Sleep(10 * time.Millisecond)

	tracer.AddEvent(ctx, "write.complete",
		attribute.Int("written_count", 100),
	)

	fmt.Println("Attributes and events added")
	// Output: Attributes and events added
}

// ExampleTracer_errorHandling demonstrates error recording.
func ExampleTracer_errorHandling() {
	tracer, _ := tracing.NewTracer(&tracing.Config{
		Enabled:       true,
		ServiceName:   "quicbaas",
		ExporterType:  tracing.ExporterStdout,
		SamplingRatio: 1.0,
	})
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()

	// Trace operation that may fail
	ctx, finish := tracer.TraceDBOperation(ctx, "get", "missing-key", 1)

	// Simulate operation that returns an error
	err := &qberrors.MessageError{Message: "key not found"}

	// Record error on span
	tracer.RecordError(ctx, err)

	// Finish with error
	finish(err)

	fmt.Println("Error recorded in span")
	// Output: Error recorded in span
}

// ExampleDBAttributes demonstrates using standard attribute helpers.
func ExampleDBAttributes() {
	attrs := tracing.DBAttributes("get", "user:123", 1)

	fmt.Println("Standard DB attributes created with", len(attrs), "fields")
	// Output: Standard DB attributes created with 4 fields
}

// ExampleRaftAttributes demonstrates Raft attribute helpers.
func ExampleRaftAttributes() {
	attrs := tracing.RaftAttributes(1, 2, 100, 500)

	fmt.Println("Raft attributes created with", len(attrs), "fields")
	// Output: Raft attributes created with 4 fields
}

// ExampleTxnAttributes demonstrates transaction attribute helpers.
func ExampleTxnAttributes() {
	attrs := tracing.TxnAttributes("txn-123", 5)

	fmt.Println("Transaction attributes created with", len(attrs), "fields")
	// Output: Transaction attributes created with 2 fields
}

// ExampleWatchAttributes demonstrates watch attribute helpers.
func ExampleWatchAttributes() {
	attrs := tracing.WatchAttributes("watch-456", "/prefix/", 10)

	fmt.Println("Watch attributes created with", len(attrs), "fields")
	// Output: Watch attributes created with 3 fields
}
