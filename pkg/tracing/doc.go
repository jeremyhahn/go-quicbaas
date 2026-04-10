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

// Package tracing provides OpenTelemetry distributed tracing for QuicBaaS.
//
// The tracing package enables end-to-end distributed tracing across all
// QuicBaaS components including gRPC services, REST APIs, Raft consensus,
// transactions, and watch operations. It supports multiple exporters including
// Jaeger, Zipkin, and OTLP.
//
// # Overview
//
// Distributed tracing helps understand request flow through the system,
// identify performance bottlenecks, and debug complex distributed operations.
// Each operation creates spans that are linked together to form a complete
// trace tree.
//
// # Configuration
//
// Create a tracer with appropriate configuration:
//
//	cfg := &tracing.Config{
//		Enabled:          true,
//		ServiceName:      "quicbaas",
//		ServiceVersion:   "1.0.0",
//		ExporterType:     tracing.ExporterJaeger,
//		ExporterEndpoint: "localhost:4317",
//		SamplingRatio:    1.0,  // Sample all traces
//		Environment:      "production",
//		NodeID:           1,
//		ShutdownTimeout:  5 * time.Second,
//	}
//
//	tracer, err := tracing.NewTracer(cfg)
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer tracer.Shutdown(context.Background())
//
// # Exporter Types
//
// The package supports multiple exporter types:
//   - ExporterJaeger: Export to Jaeger via OTLP
//   - ExporterZipkin: Export to Zipkin via OTLP
//   - ExporterOTLP: Export to any OTLP-compatible backend
//   - ExporterStdout: Print traces to stdout (for testing)
//   - ExporterNoop: Disable tracing (zero overhead)
//
// # gRPC Integration
//
// Add tracing interceptors to gRPC servers:
//
//	server := grpc.NewServer(
//		grpc.UnaryInterceptor(tracer.UnaryServerInterceptor()),
//		grpc.StreamInterceptor(tracer.StreamServerInterceptor()),
//	)
//
// For gRPC clients:
//
//	conn, err := grpc.Dial(addr,
//		grpc.WithUnaryInterceptor(tracer.UnaryClientInterceptor()),
//		grpc.WithStreamInterceptor(tracer.StreamClientInterceptor()),
//	)
//
// # REST Integration
//
// Add tracing middleware to HTTP servers:
//
//	router := chi.NewRouter()
//	router.Use(tracer.HTTPMiddleware())
//
// # Manual Instrumentation
//
// For custom operations, use TraceRequest or StartSpan:
//
//	ctx, finish := tracer.TraceRequest(ctx, "operation.name",
//		tracing.AttrDBKey.String("key"),
//		tracing.AttrDBShardID.Int64(1),
//	)
//	defer finish()
//
// Or with StartSpan for more control:
//
//	ctx, span := tracer.StartSpan(ctx, "operation.name")
//	defer span.End()
//
//	// Add attributes
//	span.SetAttributes(attribute.String("key", "value"))
//
//	// Add events
//	span.AddEvent("checkpoint", trace.WithAttributes(
//		attribute.Int("items", 10),
//	))
//
//	// Record errors
//	if err != nil {
//		span.RecordError(err)
//		span.SetStatus(codes.Error, err.Error())
//	}
//
// # Raft Operations
//
// Trace Raft proposals and apply operations:
//
//	ctx, finish := tracer.TraceRaftProposal(ctx, shardID, nodeID, data)
//	defer finish(err)
//
//	ctx, finish := tracer.TraceRaftApply(ctx, shardID, nodeID, term, index)
//	defer finish(err)
//
// # Transactions
//
// Trace transaction execution:
//
//	ctx, finish := tracer.TraceTransaction(ctx, txnID, len(operations))
//	defer finish(err)
//
//	// Trace individual operations
//	for i, op := range operations {
//		ctx, finishOp := tracer.TraceTransactionOperation(ctx, op.Type, op.Key, i)
//		// ... execute operation
//		finishOp(err)
//	}
//
// # Watch Operations
//
// Trace watch streams:
//
//	ctx, finish := tracer.TraceWatch(ctx, watchID, keyPrefix)
//	defer finish(eventCount, err)
//
//	// Trace individual events
//	tracer.TraceWatchEvent(ctx, watchID, key, eventType)
//
// # Context Propagation
//
// Trace context is automatically propagated through:
//   - gRPC metadata (via interceptors)
//   - HTTP headers (via middleware)
//   - Go context.Context (within process)
//
// This enables distributed traces across service boundaries.
//
// # Sampling
//
// Control which traces are recorded using sampling:
//   - SamplingRatio = 1.0: Sample all traces (100%)
//   - SamplingRatio = 0.1: Sample 10% of traces
//   - SamplingRatio = 0.0: Sample no traces (0%)
//
// Sampling is applied at trace creation time. All spans within a
// sampled trace are recorded.
//
// # Standard Attributes
//
// The package provides standard attribute keys for consistency:
//
// Database operations:
//   - db.system: "quicbaas"
//   - db.operation: get, put, delete, etc.
//   - db.key: The key being operated on
//   - db.shard_id: Shard identifier
//   - db.node_id: Node identifier
//
// Raft operations:
//   - raft.proposal_id: Proposal identifier
//   - raft.log_index: Raft log index
//   - raft.term: Raft term
//   - raft.leader_id: Current leader
//
// Transactions:
//   - txn.id: Transaction identifier
//   - txn.operation_count: Number of operations
//   - txn.isolation: Isolation level
//
// Watch operations:
//   - watch.id: Watch identifier
//   - watch.event_count: Number of events
//   - watch.key_prefix: Prefix being watched
//
// # Performance
//
// When tracing is disabled (Enabled = false), all operations become
// no-ops with minimal overhead. When enabled with sampling < 1.0,
// only sampled traces incur overhead.
//
// Typical overhead with full sampling:
//   - ~100-200ns per span creation
//   - ~50-100ns per attribute
//   - Async batched export (no blocking)
//
// # Best Practices
//
// 1. Use descriptive span names that indicate the operation
// 2. Add relevant attributes for filtering and analysis
// 3. Record errors on spans when they occur
// 4. Use appropriate sampling in production
// 5. Always call span.End() or finish() functions
// 6. Propagate context through function calls
// 7. Use child spans for sub-operations
// 8. Add events for important milestones
//
// # Example
//
// Complete example with nested spans:
//
//	func (s *Server) HandleGet(ctx context.Context, key string) ([]byte, error) {
//		// Create span for the entire operation
//		ctx, span := s.tracer.StartSpan(ctx, "server.get",
//			trace.WithAttributes(
//				tracing.AttrDBOperation.String("get"),
//				tracing.AttrDBKey.String(key),
//			),
//		)
//		defer span.End()
//
//		// Route to shard (child span)
//		ctx, routeSpan := s.tracer.StartSpan(ctx, "routing.resolve")
//		shardID := s.router.GetShard(key)
//		routeSpan.SetAttributes(tracing.AttrDBShardID.Int64(int64(shardID)))
//		routeSpan.End()
//
//		// Propose to Raft (child span)
//		ctx, finishPropose := s.tracer.TraceRaftProposal(ctx, shardID, s.nodeID, data)
//		err := s.raft.Propose(ctx, data)
//		finishPropose(err)
//
//		if err != nil {
//			span.RecordError(err)
//			span.SetStatus(codes.Error, err.Error())
//			return nil, err
//		}
//
//		// Success
//		span.SetStatus(codes.Ok, "")
//		return value, nil
//	}
package tracing
