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

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// TraceRaftProposal traces a Raft proposal operation.
// Returns a new context with the span and a function to end the span.
func (t *Tracer) TraceRaftProposal(ctx context.Context, shardID, nodeID uint64, proposalData []byte) (context.Context, func(error)) {
	if !t.IsEnabled() {
		return ctx, func(error) {}
	}

	spanCtx, span := t.tracer.Start(ctx, "raft.propose",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			AttrDBShardID.Int64(conv.ClampUint64ToInt64(shardID)),
			AttrDBNodeID.Int64(conv.ClampUint64ToInt64(nodeID)),
			attribute.Int("raft.proposal_size", len(proposalData)),
		),
	)

	return spanCtx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// TraceRaftApply traces a Raft apply operation.
func (t *Tracer) TraceRaftApply(ctx context.Context, shardID, nodeID, term, index uint64) (context.Context, func(error)) {
	if !t.IsEnabled() {
		return ctx, func(error) {}
	}

	spanCtx, span := t.tracer.Start(ctx, "raft.apply",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			AttrDBShardID.Int64(conv.ClampUint64ToInt64(shardID)),
			AttrDBNodeID.Int64(conv.ClampUint64ToInt64(nodeID)),
			AttrRaftTerm.Int64(conv.ClampUint64ToInt64(term)),
			AttrRaftLogIndex.Int64(conv.ClampUint64ToInt64(index)),
		),
	)

	return spanCtx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// TraceRaftSnapshot traces a Raft snapshot operation.
func (t *Tracer) TraceRaftSnapshot(ctx context.Context, shardID, nodeID, index uint64) (context.Context, func(error)) {
	if !t.IsEnabled() {
		return ctx, func(error) {}
	}

	spanCtx, span := t.tracer.Start(ctx, "raft.snapshot",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			AttrDBShardID.Int64(conv.ClampUint64ToInt64(shardID)),
			AttrDBNodeID.Int64(conv.ClampUint64ToInt64(nodeID)),
			AttrRaftLogIndex.Int64(conv.ClampUint64ToInt64(index)),
		),
	)

	return spanCtx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// TraceDBOperation traces a database operation (Get, Put, Delete, etc.)
func (t *Tracer) TraceDBOperation(ctx context.Context, operation, key string, shardID uint64) (context.Context, func(error)) {
	if !t.IsEnabled() {
		return ctx, func(error) {}
	}

	spanCtx, span := t.tracer.Start(ctx, "db."+operation,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(DBAttributes(operation, key, shardID)...),
	)

	return spanCtx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// TraceTransaction traces a transaction operation.
func (t *Tracer) TraceTransaction(ctx context.Context, txnID string, opCount int) (context.Context, func(error)) {
	if !t.IsEnabled() {
		return ctx, func(error) {}
	}

	spanCtx, span := t.tracer.Start(ctx, "txn.execute",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(TxnAttributes(txnID, opCount)...),
	)

	return spanCtx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// TraceTransactionOperation traces an individual operation within a transaction.
func (t *Tracer) TraceTransactionOperation(ctx context.Context, operation, key string, index int) (context.Context, func(error)) {
	if !t.IsEnabled() {
		return ctx, func(error) {}
	}

	spanCtx, span := t.tracer.Start(ctx, "txn.op."+operation,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			AttrDBOperation.String(operation),
			AttrDBKey.String(key),
			attribute.Int("txn.operation_index", index),
		),
	)

	return spanCtx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// TraceWatch traces a watch operation.
func (t *Tracer) TraceWatch(ctx context.Context, watchID, keyPrefix string) (context.Context, func(int, error)) {
	if !t.IsEnabled() {
		return ctx, func(int, error) {}
	}

	spanCtx, span := t.tracer.Start(ctx, "watch.stream",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			AttrWatchID.String(watchID),
			AttrWatchKeyPrefix.String(keyPrefix),
		),
	)

	return spanCtx, func(eventCount int, err error) {
		span.SetAttributes(AttrWatchEventCount.Int(eventCount))
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// TraceWatchEvent traces a watch event being sent.
func (t *Tracer) TraceWatchEvent(ctx context.Context, watchID, key string, eventType string) {
	if !t.IsEnabled() {
		return
	}

	span := trace.SpanFromContext(ctx)
	span.AddEvent("watch.event",
		trace.WithAttributes(
			AttrWatchID.String(watchID),
			AttrDBKey.String(key),
			attribute.String("watch.event_type", eventType),
		),
	)
}

// TraceStorageOperation traces a storage engine operation.
func (t *Tracer) TraceStorageOperation(ctx context.Context, operation string, batchSize int) (context.Context, func(error)) {
	if !t.IsEnabled() {
		return ctx, func(error) {}
	}

	attrs := []attribute.KeyValue{
		attribute.String("storage.operation", operation),
	}
	if batchSize > 0 {
		attrs = append(attrs, attribute.Int("storage.batch_size", batchSize))
	}

	spanCtx, span := t.tracer.Start(ctx, "storage."+operation,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)

	return spanCtx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// TraceReplication traces a replication operation.
func (t *Tracer) TraceReplication(ctx context.Context, sourceNode, targetNode uint64, entryCount int) (context.Context, func(error)) {
	if !t.IsEnabled() {
		return ctx, func(error) {}
	}

	spanCtx, span := t.tracer.Start(ctx, "replication.sync",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.Int64("replication.source_node", conv.ClampUint64ToInt64(sourceNode)),
			attribute.Int64("replication.target_node", conv.ClampUint64ToInt64(targetNode)),
			attribute.Int("replication.entry_count", entryCount),
		),
	)

	return spanCtx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// TraceBatchOperation traces a batch database operation.
func (t *Tracer) TraceBatchOperation(ctx context.Context, operation string, keyCount int, shardID uint64) (context.Context, func(error)) {
	if !t.IsEnabled() {
		return ctx, func(error) {}
	}

	spanCtx, span := t.tracer.Start(ctx, "db.batch."+operation,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			AttrDBOperation.String(operation),
			AttrDBKeyCount.Int(keyCount),
			AttrDBShardID.Int64(conv.ClampUint64ToInt64(shardID)),
		),
	)

	return spanCtx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// TraceDiscovery traces a peer discovery operation.
func (t *Tracer) TraceDiscovery(ctx context.Context, method string) (context.Context, func(int, error)) {
	if !t.IsEnabled() {
		return ctx, func(int, error) {}
	}

	spanCtx, span := t.tracer.Start(ctx, "discovery."+method,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("discovery.method", method),
		),
	)

	return spanCtx, func(peerCount int, err error) {
		span.SetAttributes(attribute.Int("discovery.peer_count", peerCount))
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// TraceMembership traces a cluster membership operation.
func (t *Tracer) TraceMembership(ctx context.Context, operation string, nodeID uint64) (context.Context, func(error)) {
	if !t.IsEnabled() {
		return ctx, func(error) {}
	}

	spanCtx, span := t.tracer.Start(ctx, "membership."+operation,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("membership.operation", operation),
			AttrDBNodeID.Int64(conv.ClampUint64ToInt64(nodeID)),
		),
	)

	return spanCtx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// =============================================================================
// Orchestration Tracing Operations
// =============================================================================

// Orchestration-specific attribute keys.
var (
	AttrOrchJobID     = attribute.Key("orch.job_id")
	AttrOrchAgentID   = attribute.Key("orch.agent_id")
	AttrOrchProjectID = attribute.Key("orch.project_id")
	AttrOrchState     = attribute.Key("orch.state")
	AttrOrchOperation = attribute.Key("orch.operation")
	AttrOrchFromAgent = attribute.Key("orch.from_agent")
	AttrOrchToAgent   = attribute.Key("orch.to_agent")
	AttrOrchAction    = attribute.Key("orch.action")
	AttrOrchFilePath  = attribute.Key("orch.file_path")
)

// TraceJobLifecycle creates a span for a job state transition.
func (t *Tracer) TraceJobLifecycle(ctx context.Context, jobID, state string) (context.Context, func(error)) {
	if !t.IsEnabled() {
		return ctx, func(error) {}
	}

	spanCtx, span := t.tracer.Start(ctx, "orch.job.lifecycle",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			AttrOrchJobID.String(jobID),
			AttrOrchState.String(state),
		),
	)

	return spanCtx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// TraceAgentOperation creates a span for an agent operation (spawn, prompt, shutdown).
func (t *Tracer) TraceAgentOperation(ctx context.Context, agentID, operation string) (context.Context, func(error)) {
	if !t.IsEnabled() {
		return ctx, func(error) {}
	}

	spanCtx, span := t.tracer.Start(ctx, "orch.agent."+operation,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			AttrOrchAgentID.String(agentID),
			AttrOrchOperation.String(operation),
		),
	)

	return spanCtx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// TracePeerMessage creates a span for peer message send/deliver/acknowledge.
func (t *Tracer) TracePeerMessage(ctx context.Context, from, to, action string) (context.Context, func(error)) {
	if !t.IsEnabled() {
		return ctx, func(error) {}
	}

	spanCtx, span := t.tracer.Start(ctx, "orch.peer."+action,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			AttrOrchFromAgent.String(from),
			AttrOrchToAgent.String(to),
			AttrOrchAction.String(action),
		),
	)

	return spanCtx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// TraceFileLock creates a span for file lock acquire/renew/release.
func (t *Tracer) TraceFileLock(ctx context.Context, projectID, filePath, operation string) (context.Context, func(error)) {
	if !t.IsEnabled() {
		return ctx, func(error) {}
	}

	spanCtx, span := t.tracer.Start(ctx, "orch.filelock."+operation,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			AttrOrchProjectID.String(projectID),
			AttrOrchFilePath.String(filePath),
			AttrOrchOperation.String(operation),
		),
	)

	return spanCtx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// TraceIndexing creates a span for file indexing operations.
func (t *Tracer) TraceIndexing(ctx context.Context, projectID, filePath, operation string) (context.Context, func(error)) {
	if !t.IsEnabled() {
		return ctx, func(error) {}
	}

	spanCtx, span := t.tracer.Start(ctx, "orch.index."+operation,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			AttrOrchProjectID.String(projectID),
			AttrOrchFilePath.String(filePath),
			AttrOrchOperation.String(operation),
		),
	)

	return spanCtx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// OrchJobAttributes creates standard orchestration job attributes.
func OrchJobAttributes(jobID, state string) []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrOrchJobID.String(jobID),
		AttrOrchState.String(state),
	}
}

// OrchAgentAttributes creates standard orchestration agent attributes.
func OrchAgentAttributes(agentID, operation string) []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrOrchAgentID.String(agentID),
		AttrOrchOperation.String(operation),
	}
}

// OrchPeerAttributes creates standard orchestration peer message attributes.
func OrchPeerAttributes(from, to, action string) []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrOrchFromAgent.String(from),
		AttrOrchToAgent.String(to),
		AttrOrchAction.String(action),
	}
}
