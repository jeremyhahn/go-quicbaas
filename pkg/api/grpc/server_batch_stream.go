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
	stderrors "errors"
	"io"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// streamBatchMetrics holds metrics for streaming batch operations.
type streamBatchMetrics struct {
	streamsTotal    *metrics.CounterVec
	streamDuration  *metrics.HistogramVec
	chunksProcessed *metrics.CounterVec
	operationsTotal *metrics.CounterVec
	backpressure    *metrics.Counter
	activeStreams   *metrics.Gauge
}

// Global stream batch metrics instance.
var (
	globalStreamBatchMetrics     *streamBatchMetrics
	globalStreamBatchMetricsOnce sync.Once
)

// initStreamBatchMetrics initializes metrics for streaming batch operations.
func initStreamBatchMetrics() *streamBatchMetrics {
	globalStreamBatchMetricsOnce.Do(func() {
		globalStreamBatchMetrics = &streamBatchMetrics{
			streamsTotal: metrics.NewCounterVec(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "grpc_stream_batch",
				Name:      "streams_total",
				Help:      "Total number of streaming batch operations",
			}, []string{"status"}),

			streamDuration: metrics.NewHistogramVec(metrics.HistogramOpts{
				Namespace: "quicbaas",
				Subsystem: "grpc_stream_batch",
				Name:      "duration_seconds",
				Help:      "Duration of streaming batch operations",
				Buckets:   []float64{.1, .5, 1, 5, 10, 30, 60, 120, 300},
			}, []string{"status"}),

			chunksProcessed: metrics.NewCounterVec(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "grpc_stream_batch",
				Name:      "chunks_processed_total",
				Help:      "Total number of chunks processed",
			}, []string{"shard"}),

			operationsTotal: metrics.NewCounterVec(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "grpc_stream_batch",
				Name:      "operations_total",
				Help:      "Total number of operations in streaming batches",
			}, []string{"shard", "status"}),

			backpressure: metrics.NewCounter(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "grpc_stream_batch",
				Name:      "backpressure_events_total",
				Help:      "Total number of backpressure events",
			}),

			activeStreams: metrics.NewGauge(metrics.GaugeOpts{
				Namespace: "quicbaas",
				Subsystem: "grpc_stream_batch",
				Name:      "active_streams",
				Help:      "Number of active streaming batch operations",
			}),
		}
	})
	return globalStreamBatchMetrics
}

// StreamBatchPutHandler provides the handler interface for streaming batch operations.
// This interface allows the gRPC server to delegate to a streaming batch processor.
type StreamBatchPutHandler interface {
	// NewSession creates a new streaming batch session.
	NewSession(sessionID string, header *pb.StreamBatchHeader) error

	// AddChunk adds a chunk to an existing session.
	AddChunk(sessionID string, chunk *pb.StreamBatchChunk) (*pb.StreamBatchProgress, error)

	// Complete finalizes a streaming batch session.
	Complete(sessionID string, complete *pb.StreamBatchComplete) (*pb.StreamBatchResult, error)

	// GetProgress returns the progress channel for a session.
	GetProgress(sessionID string) (<-chan *pb.StreamBatchProgress, error)

	// Abort cancels a streaming batch session.
	Abort(sessionID string, reason string) error
}

// streamBatchSession tracks the state of a single streaming batch operation.
type streamBatchSession struct {
	sessionID       string
	header          *pb.StreamBatchHeader
	receivedChunks  int32
	totalOperations int64
	receivedOps     int64
	validatedOps    int64
	lastChunkIndex  int32
	errors          []string
	startTime       time.Time
	// operations stores validated operations for commit
	operations []*pb.PutRequest
	mu         sync.Mutex
}

// StreamBatchPut implements the bidirectional streaming RPC for batch put operations.
func (s *Server) StreamBatchPut(stream pb.QuicBaaS_StreamBatchPutServer) error {
	streamMetrics := initStreamBatchMetrics()
	streamMetrics.activeStreams.Inc()
	defer streamMetrics.activeStreams.Dec()

	start := time.Now()
	ctx := stream.Context()

	// Generate a unique session ID
	sessionID := generateSessionID()

	s.logger.WithField("session_id", sessionID).Info("Starting streaming batch put operation")

	session := &streamBatchSession{
		sessionID:  sessionID,
		startTime:  start,
		operations: make([]*pb.PutRequest, 0),
	}

	// Process incoming messages
	for {
		select {
		case <-ctx.Done():
			duration := time.Since(start).Seconds()
			streamMetrics.streamsTotal.With("canceled").Inc()
			streamMetrics.streamDuration.With("canceled").Observe(duration)
			s.logger.WithField("session_id", sessionID).Info("Streaming batch canceled by client")
			return status.Error(codes.Canceled, "stream canceled")
		default:
		}

		req, err := stream.Recv()
		if stderrors.Is(err, io.EOF) {
			// Client finished sending, but no explicit complete message
			s.logger.WithField("session_id", sessionID).Warn("Client closed stream without sending complete message")
			duration := time.Since(start).Seconds()
			streamMetrics.streamsTotal.With("incomplete").Inc()
			streamMetrics.streamDuration.With("incomplete").Observe(duration)
			return nil
		}
		if err != nil {
			duration := time.Since(start).Seconds()
			streamMetrics.streamsTotal.With("error").Inc()
			streamMetrics.streamDuration.With("error").Observe(duration)
			s.logger.WithError(err).WithField("session_id", sessionID).Error("Error receiving stream message")
			return toProtoError(&errors.QBError{
				Code: errors.ErrInternal,
				Op:   "grpc.StreamBatchPut",
				Err:  err,
			})
		}

		// Handle the message based on its type
		switch payload := req.GetPayload().(type) {
		case *pb.StreamBatchPutRequest_Header:
			if err := s.handleStreamBatchHeader(stream, session, payload.Header, streamMetrics); err != nil {
				return err
			}

		case *pb.StreamBatchPutRequest_Chunk:
			if err := s.handleStreamBatchChunk(stream, session, payload.Chunk, streamMetrics); err != nil {
				return err
			}

		case *pb.StreamBatchPutRequest_Complete:
			result, err := s.handleStreamBatchComplete(ctx, stream, session, payload.Complete, streamMetrics)
			if err != nil {
				return err
			}

			// Send the final result
			resp := &pb.StreamBatchPutResponse{
				Payload: &pb.StreamBatchPutResponse_Result{
					Result: result,
				},
			}
			if err := stream.Send(resp); err != nil {
				s.logger.WithError(err).Error("Failed to send final result")
				return toProtoError(&errors.QBError{
					Code: errors.ErrInternal,
					Op:   "grpc.StreamBatchPut",
					Err:  err,
				})
			}

			duration := time.Since(start).Seconds()
			switch {
			case result.Succeeded:
				streamMetrics.streamsTotal.With("success").Inc()
				streamMetrics.streamDuration.With("success").Observe(duration)
			case !payload.Complete.Commit:
				streamMetrics.streamsTotal.With("aborted").Inc()
				streamMetrics.streamDuration.With("aborted").Observe(duration)
			default:
				streamMetrics.streamsTotal.With("failed").Inc()
				streamMetrics.streamDuration.With("failed").Observe(duration)
			}

			s.logger.WithFields(map[string]interface{}{
				"session_id": sessionID,
				"succeeded":  result.Succeeded,
				"committed":  result.OperationsCommitted,
				"duration":   duration,
			}).Info("Streaming batch put completed")

			return nil

		default:
			s.logger.WithField("session_id", sessionID).Warn("Unknown payload type received")
			return status.Error(codes.InvalidArgument, "unknown payload type")
		}
	}
}

// handleStreamBatchHeader processes the header message that initiates a streaming batch.
func (s *Server) handleStreamBatchHeader(
	stream pb.QuicBaaS_StreamBatchPutServer,
	session *streamBatchSession,
	header *pb.StreamBatchHeader,
	streamMetrics *streamBatchMetrics,
) error {
	session.mu.Lock()
	defer session.mu.Unlock()

	// Validate header
	if header.TotalOperations <= 0 {
		return status.Error(codes.InvalidArgument, "total_operations must be positive")
	}

	if header.ShardId == 0 {
		// Default to shard 1 if not specified
		header.ShardId = 1
	}

	session.header = header
	session.totalOperations = header.TotalOperations
	session.lastChunkIndex = -1
	// Pre-allocate operations slice for better performance
	session.operations = make([]*pb.PutRequest, 0, header.TotalOperations)

	s.logger.WithFields(map[string]interface{}{
		"session_id":       session.sessionID,
		"shard_id":         header.ShardId,
		"total_operations": header.TotalOperations,
		"chunk_size":       header.ChunkSize,
		"conditions":       len(header.Conditions),
	}).Debug("Streaming batch header received")

	// Send initial progress
	progress := &pb.StreamBatchProgress{
		OperationsReceived:  0,
		OperationsValidated: 0,
		ChunkIndex:          -1,
		ValidationErrors:    nil,
	}

	resp := &pb.StreamBatchPutResponse{
		Payload: &pb.StreamBatchPutResponse_Progress{
			Progress: progress,
		},
	}

	if err := stream.Send(resp); err != nil {
		s.logger.WithError(err).Error("Failed to send initial progress")
		return toProtoError(&errors.QBError{
			Code: errors.ErrInternal,
			Op:   "grpc.StreamBatchPut.header",
			Err:  err,
		})
	}

	return nil
}

// handleStreamBatchChunk processes a chunk of operations.
func (s *Server) handleStreamBatchChunk(
	stream pb.QuicBaaS_StreamBatchPutServer,
	session *streamBatchSession,
	chunk *pb.StreamBatchChunk,
	streamMetrics *streamBatchMetrics,
) error {
	session.mu.Lock()
	defer session.mu.Unlock()

	// Ensure header was received first
	if session.header == nil {
		return status.Error(codes.FailedPrecondition, "header must be sent before chunks")
	}

	// Track chunk
	session.receivedChunks++
	opsInChunk := int64(len(chunk.Operations))
	session.receivedOps += opsInChunk

	// Update last chunk index
	if chunk.ChunkIndex > session.lastChunkIndex {
		session.lastChunkIndex = chunk.ChunkIndex
	}

	// Validate operations and store valid ones
	var validationErrors []string
	validatedCount := int64(0)

	for i, op := range chunk.Operations {
		if op.EntityType == "" {
			validationErrors = append(validationErrors,
				"chunk "+formatInt(int(chunk.ChunkIndex))+", op "+formatInt(i)+": entity_type is required")
			continue
		}
		if len(op.Data) == 0 {
			validationErrors = append(validationErrors,
				"chunk "+formatInt(int(chunk.ChunkIndex))+", op "+formatInt(i)+": data is required")
			continue
		}
		// Store valid operation for later commit
		session.operations = append(session.operations, op)
		validatedCount++
	}

	session.validatedOps += validatedCount
	session.errors = append(session.errors, validationErrors...)

	// Update metrics
	shardStr := formatUint(session.header.ShardId)
	streamMetrics.chunksProcessed.With(shardStr).Inc()
	streamMetrics.operationsTotal.With(shardStr, "received").Add(float64(opsInChunk))
	streamMetrics.operationsTotal.With(shardStr, "validated").Add(float64(validatedCount))

	s.logger.WithFields(map[string]interface{}{
		"session_id":  session.sessionID,
		"chunk_index": chunk.ChunkIndex,
		"operations":  opsInChunk,
		"validated":   validatedCount,
		"errors":      len(validationErrors),
	}).Debug("Processed streaming batch chunk")

	// Send progress update
	progress := &pb.StreamBatchProgress{
		OperationsReceived:  session.receivedOps,
		OperationsValidated: session.validatedOps,
		ChunkIndex:          chunk.ChunkIndex,
		ValidationErrors:    validationErrors,
	}

	resp := &pb.StreamBatchPutResponse{
		Payload: &pb.StreamBatchPutResponse_Progress{
			Progress: progress,
		},
	}

	if err := stream.Send(resp); err != nil {
		s.logger.WithError(err).Error("Failed to send progress")
		streamMetrics.backpressure.Inc()
		return toProtoError(&errors.QBError{
			Code: errors.ErrInternal,
			Op:   "grpc.StreamBatchPut.chunk",
			Err:  err,
		})
	}

	return nil
}

// handleStreamBatchComplete processes the complete message that finalizes a streaming batch.
func (s *Server) handleStreamBatchComplete(
	ctx interface{ Done() <-chan struct{} },
	stream pb.QuicBaaS_StreamBatchPutServer,
	session *streamBatchSession,
	complete *pb.StreamBatchComplete,
	streamMetrics *streamBatchMetrics,
) (*pb.StreamBatchResult, error) {
	session.mu.Lock()
	defer session.mu.Unlock()

	// Handle abort
	if !complete.Commit {
		s.logger.WithField("session_id", session.sessionID).Info("Client requested abort")
		return &pb.StreamBatchResult{
			Succeeded:           false,
			OperationsCommitted: 0,
			Error:               "session aborted by client",
			Timestamp:           time.Now().UnixNano(),
		}, nil
	}

	// Check if header was received
	if session.header == nil {
		return &pb.StreamBatchResult{
			Succeeded:           false,
			OperationsCommitted: 0,
			Error:               "no header received",
			Timestamp:           time.Now().UnixNano(),
		}, nil
	}

	// Check for validation errors
	if len(session.errors) > 0 {
		return &pb.StreamBatchResult{
			Succeeded:           false,
			OperationsCommitted: 0,
			FailedKeys:          session.errors,
			Error:               "validation failed",
			Timestamp:           time.Now().UnixNano(),
		}, nil
	}

	// Verify all operations were received
	if session.receivedOps != session.totalOperations {
		s.logger.WithFields(map[string]interface{}{
			"session_id": session.sessionID,
			"expected":   session.totalOperations,
			"received":   session.receivedOps,
		}).Warn("Incomplete batch, proceeding with received operations")
	}

	// Convert protobuf requests to service requests
	serviceReqs := make([]*service.PutRequest, len(session.operations))
	for i, op := range session.operations {
		serviceReqs[i] = &service.PutRequest{
			EntityType:  op.EntityType,
			ID:          op.Id,
			Data:        op.Data,
			Consistency: convertProtoToServiceConsistency(op.Consistency),
		}
	}

	// Execute the batch through the service layer
	batchReq := &service.BatchPutRequest{
		Requests: serviceReqs,
		ShardID:  session.header.ShardId,
	}

	// Use context from stream for proper cancellation
	streamCtx := stream.Context()
	batchResp, err := s.service.BatchPut(streamCtx, batchReq)
	if err != nil {
		s.logger.WithError(err).WithField("session_id", session.sessionID).Error("Failed to execute batch put")
		return &pb.StreamBatchResult{
			Succeeded:           false,
			OperationsCommitted: 0,
			Error:               err.Error(),
			Timestamp:           time.Now().UnixNano(),
		}, nil
	}

	// BatchPut commits each shard group as size-bounded atomic Raft
	// proposals; when it returns without error, every operation was
	// committed.
	committed := int64(len(batchResp.Responses))

	shardStr := formatUint(session.header.ShardId)
	streamMetrics.operationsTotal.With(shardStr, "committed").Add(float64(committed))

	return &pb.StreamBatchResult{
		Succeeded:           true,
		OperationsCommitted: committed,
		FailedKeys:          nil,
		Error:               "",
		Timestamp:           batchResp.Timestamp,
	}, nil
}

// convertProtoToServiceConsistency converts proto consistency level to service consistency.
func convertProtoToServiceConsistency(c pb.ConsistencyLevel) service.ConsistencyLevel {
	switch c {
	case pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG:
		return service.ConsistencyLevelStrong
	case pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE:
		return service.ConsistencyLevelLinearizable
	case pb.ConsistencyLevel_CONSISTENCY_LEVEL_EVENTUAL:
		return service.ConsistencyLevelEventual
	default:
		return service.ConsistencyLevelLinearizable
	}
}

// generateSessionID generates a unique session identifier.
func generateSessionID() string {
	// Use timestamp and a simple counter for uniqueness
	return "stream-" + formatInt64(time.Now().UnixNano())
}

// Helper formatting functions.
func formatInt(n int) string {
	if n < 0 {
		return "-" + formatUint(uint64(-n))
	}
	return formatUint(uint64(n))
}

func formatInt64(n int64) string {
	if n < 0 {
		return "-" + formatUint(uint64(-n))
	}
	return formatUint(uint64(n))
}

func formatUint(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
