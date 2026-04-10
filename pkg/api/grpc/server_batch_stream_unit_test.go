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

//go:build grpc

package grpc

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// TestConvertProtoToServiceConsistencyGRPC tests consistency level conversion
// with grpc build tag. Note: similar tests exist in handlers_coverage_boost2_test.go
// which runs without the grpc build tag.
func TestConvertProtoToServiceConsistencyGRPC(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.ConsistencyLevel
		expected service.ConsistencyLevel
	}{
		{
			name:     "strong consistency",
			input:    pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG,
			expected: service.ConsistencyLevelStrong,
		},
		{
			name:     "linearizable consistency",
			input:    pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
			expected: service.ConsistencyLevelLinearizable,
		},
		{
			name:     "eventual consistency",
			input:    pb.ConsistencyLevel_CONSISTENCY_LEVEL_EVENTUAL,
			expected: service.ConsistencyLevelEventual,
		},
		{
			name:     "unspecified defaults to linearizable",
			input:    pb.ConsistencyLevel_CONSISTENCY_LEVEL_UNSPECIFIED,
			expected: service.ConsistencyLevelLinearizable,
		},
		{
			name:     "unknown value defaults to linearizable",
			input:    pb.ConsistencyLevel(999),
			expected: service.ConsistencyLevelLinearizable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToServiceConsistency(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestInitStreamBatchMetricsGRPC tests metrics initialization with grpc build tag.
func TestInitStreamBatchMetricsGRPC(t *testing.T) {
	t.Run("initializes metrics", func(t *testing.T) {
		metrics := initStreamBatchMetrics()
		assert.NotNil(t, metrics)
		assert.NotNil(t, metrics.streamsTotal)
		assert.NotNil(t, metrics.streamDuration)
		assert.NotNil(t, metrics.chunksProcessed)
		assert.NotNil(t, metrics.operationsTotal)
		assert.NotNil(t, metrics.backpressure)
		assert.NotNil(t, metrics.activeStreams)
	})

	t.Run("returns same instance on multiple calls", func(t *testing.T) {
		metrics1 := initStreamBatchMetrics()
		metrics2 := initStreamBatchMetrics()
		assert.Same(t, metrics1, metrics2)
	})
}

// TestStreamBatchSessionStateGRPC tests streamBatchSession state tracking with grpc build tag.
func TestStreamBatchSessionStateGRPC(t *testing.T) {
	t.Run("initialize session", func(t *testing.T) {
		session := &streamBatchSession{
			sessionID:  "test-session",
			operations: make([]*pb.PutRequest, 0),
		}

		assert.Equal(t, "test-session", session.sessionID)
		assert.Empty(t, session.operations)
		assert.Zero(t, session.receivedChunks)
		assert.Zero(t, session.totalOperations)
	})

	t.Run("track operations", func(t *testing.T) {
		session := &streamBatchSession{
			sessionID:  "test-session",
			operations: make([]*pb.PutRequest, 0),
		}

		// Simulate adding operations
		op1 := &pb.PutRequest{EntityType: "user", Id: 1, Data: []byte("data1")}
		op2 := &pb.PutRequest{EntityType: "user", Id: 2, Data: []byte("data2")}

		session.operations = append(session.operations, op1, op2)
		session.receivedOps = 2
		session.validatedOps = 2

		assert.Len(t, session.operations, 2)
		assert.Equal(t, int64(2), session.receivedOps)
		assert.Equal(t, int64(2), session.validatedOps)
	})

	t.Run("track errors", func(t *testing.T) {
		session := &streamBatchSession{
			sessionID: "test-session",
			errors:    make([]string, 0),
		}

		session.errors = append(session.errors, "error1", "error2")
		assert.Len(t, session.errors, 2)
		assert.Contains(t, session.errors, "error1")
		assert.Contains(t, session.errors, "error2")
	})
}

// --- Merged from server_batch_stream_coverage_test.go ---

// mockBatchStream implements pb.QuicBaaS_StreamBatchPutServer for testing.
type mockBatchStream struct {
	ctx      context.Context
	sendFunc func(*pb.StreamBatchPutResponse) error
	recvFunc func() (*pb.StreamBatchPutRequest, error)
}

func (m *mockBatchStream) Send(resp *pb.StreamBatchPutResponse) error {
	if m.sendFunc != nil {
		return m.sendFunc(resp)
	}
	return nil
}

func (m *mockBatchStream) Recv() (*pb.StreamBatchPutRequest, error) {
	if m.recvFunc != nil {
		return m.recvFunc()
	}
	return nil, nil
}

func (m *mockBatchStream) SetHeader(md metadata.MD) error  { return nil }
func (m *mockBatchStream) SendHeader(md metadata.MD) error { return nil }
func (m *mockBatchStream) SetTrailer(md metadata.MD)       {}
func (m *mockBatchStream) Context() context.Context        { return m.ctx }
func (m *mockBatchStream) SendMsg(msg interface{}) error   { return nil }
func (m *mockBatchStream) RecvMsg(msg interface{}) error   { return nil }

// newTestBatchStreamServer creates a Server with a nil service for tests
// that do not invoke the service layer (header, chunk, early-exit complete tests).
func newTestBatchStreamServer(t *testing.T, svc service.QuicBaaS) *Server {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)
	return &Server{
		logger:  log,
		service: svc,
	}
}

// --- handleStreamBatchHeader tests ---

func TestBatchStream_HandleHeader_InvalidTotalOperations(t *testing.T) {
	srv := newTestBatchStreamServer(t, nil)
	stream := &mockBatchStream{ctx: context.Background()}
	session := &streamBatchSession{sessionID: "test"}
	metrics := initStreamBatchMetrics()

	err := srv.handleStreamBatchHeader(stream, session, &pb.StreamBatchHeader{
		TotalOperations: 0,
	}, metrics)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestBatchStream_HandleHeader_DefaultShardID(t *testing.T) {
	srv := newTestBatchStreamServer(t, nil)
	stream := &mockBatchStream{ctx: context.Background()}
	session := &streamBatchSession{sessionID: "test"}
	metrics := initStreamBatchMetrics()

	err := srv.handleStreamBatchHeader(stream, session, &pb.StreamBatchHeader{
		TotalOperations: 10,
		ShardId:         0, // Should default to 1
	}, metrics)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), session.header.ShardId)
	assert.Equal(t, int64(10), session.totalOperations)
}

func TestBatchStream_HandleHeader_Success(t *testing.T) {
	srv := newTestBatchStreamServer(t, nil)
	stream := &mockBatchStream{ctx: context.Background()}
	session := &streamBatchSession{sessionID: "test"}
	metrics := initStreamBatchMetrics()

	err := srv.handleStreamBatchHeader(stream, session, &pb.StreamBatchHeader{
		TotalOperations: 5,
		ShardId:         2,
		ChunkSize:       100,
	}, metrics)
	require.NoError(t, err)
	assert.NotNil(t, session.header)
	assert.Equal(t, int64(5), session.totalOperations)
	assert.Equal(t, uint64(2), session.header.ShardId)
}

func TestBatchStream_HandleHeader_SendError(t *testing.T) {
	srv := newTestBatchStreamServer(t, nil)
	stream := &mockBatchStream{
		ctx: context.Background(),
		sendFunc: func(_ *pb.StreamBatchPutResponse) error {
			return errors.New("send failed")
		},
	}
	session := &streamBatchSession{sessionID: "test"}
	metrics := initStreamBatchMetrics()

	err := srv.handleStreamBatchHeader(stream, session, &pb.StreamBatchHeader{
		TotalOperations: 5,
		ShardId:         1,
	}, metrics)
	require.Error(t, err)
}

// --- handleStreamBatchChunk tests ---

func TestBatchStream_HandleChunk_NoHeader(t *testing.T) {
	srv := newTestBatchStreamServer(t, nil)
	stream := &mockBatchStream{ctx: context.Background()}
	session := &streamBatchSession{sessionID: "test"}
	metrics := initStreamBatchMetrics()

	err := srv.handleStreamBatchChunk(stream, session, &pb.StreamBatchChunk{
		ChunkIndex: 0,
		Operations: []*pb.PutRequest{{EntityType: "user", Data: []byte("data")}},
	}, metrics)
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestBatchStream_HandleChunk_ValidOperations(t *testing.T) {
	srv := newTestBatchStreamServer(t, nil)
	stream := &mockBatchStream{ctx: context.Background()}
	session := &streamBatchSession{
		sessionID:  "test",
		header:     &pb.StreamBatchHeader{TotalOperations: 3, ShardId: 1},
		operations: make([]*pb.PutRequest, 0),
	}
	metrics := initStreamBatchMetrics()

	err := srv.handleStreamBatchChunk(stream, session, &pb.StreamBatchChunk{
		ChunkIndex: 0,
		Operations: []*pb.PutRequest{
			{EntityType: "user", Id: 1, Data: []byte("data1")},
			{EntityType: "user", Id: 2, Data: []byte("data2")},
		},
	}, metrics)
	require.NoError(t, err)
	assert.Equal(t, int32(1), session.receivedChunks)
	assert.Equal(t, int64(2), session.receivedOps)
	assert.Equal(t, int64(2), session.validatedOps)
	assert.Len(t, session.operations, 2)
}

func TestBatchStream_HandleChunk_ValidationErrors(t *testing.T) {
	srv := newTestBatchStreamServer(t, nil)
	stream := &mockBatchStream{ctx: context.Background()}
	session := &streamBatchSession{
		sessionID:  "test",
		header:     &pb.StreamBatchHeader{TotalOperations: 3, ShardId: 1},
		operations: make([]*pb.PutRequest, 0),
	}
	metrics := initStreamBatchMetrics()

	err := srv.handleStreamBatchChunk(stream, session, &pb.StreamBatchChunk{
		ChunkIndex: 0,
		Operations: []*pb.PutRequest{
			{EntityType: "", Data: []byte("data")},   // Missing entity_type
			{EntityType: "user", Data: nil},          // Missing data
			{EntityType: "user", Data: []byte("ok")}, // Valid
		},
	}, metrics)
	require.NoError(t, err)
	assert.Equal(t, int64(3), session.receivedOps)
	assert.Equal(t, int64(1), session.validatedOps) // Only 1 valid
	assert.Len(t, session.errors, 2)
	assert.Len(t, session.operations, 1)
}

func TestBatchStream_HandleChunk_SendError(t *testing.T) {
	srv := newTestBatchStreamServer(t, nil)
	stream := &mockBatchStream{
		ctx: context.Background(),
		sendFunc: func(_ *pb.StreamBatchPutResponse) error {
			return errors.New("backpressure")
		},
	}
	session := &streamBatchSession{
		sessionID:  "test",
		header:     &pb.StreamBatchHeader{TotalOperations: 1, ShardId: 1},
		operations: make([]*pb.PutRequest, 0),
	}
	metrics := initStreamBatchMetrics()

	err := srv.handleStreamBatchChunk(stream, session, &pb.StreamBatchChunk{
		ChunkIndex: 0,
		Operations: []*pb.PutRequest{
			{EntityType: "user", Data: []byte("data")},
		},
	}, metrics)
	require.Error(t, err)
}

// --- handleStreamBatchComplete tests ---

func TestBatchStream_HandleComplete_Abort(t *testing.T) {
	srv := newTestBatchStreamServer(t, nil)
	stream := &mockBatchStream{ctx: context.Background()}
	session := &streamBatchSession{sessionID: "test"}
	metrics := initStreamBatchMetrics()

	result, err := srv.handleStreamBatchComplete(context.Background(), stream, session,
		&pb.StreamBatchComplete{Commit: false}, metrics)
	require.NoError(t, err)
	assert.False(t, result.Succeeded)
	assert.Contains(t, result.Error, "aborted by client")
}

func TestBatchStream_HandleComplete_NoHeader(t *testing.T) {
	srv := newTestBatchStreamServer(t, nil)
	stream := &mockBatchStream{ctx: context.Background()}
	session := &streamBatchSession{sessionID: "test", header: nil}
	metrics := initStreamBatchMetrics()

	result, err := srv.handleStreamBatchComplete(context.Background(), stream, session,
		&pb.StreamBatchComplete{Commit: true}, metrics)
	require.NoError(t, err)
	assert.False(t, result.Succeeded)
	assert.Contains(t, result.Error, "no header received")
}

func TestBatchStream_HandleComplete_ValidationErrors(t *testing.T) {
	srv := newTestBatchStreamServer(t, nil)
	stream := &mockBatchStream{ctx: context.Background()}
	session := &streamBatchSession{
		sessionID: "test",
		header:    &pb.StreamBatchHeader{TotalOperations: 1, ShardId: 1},
		errors:    []string{"chunk 0, op 0: entity_type is required"},
	}
	metrics := initStreamBatchMetrics()

	result, err := srv.handleStreamBatchComplete(context.Background(), stream, session,
		&pb.StreamBatchComplete{Commit: true}, metrics)
	require.NoError(t, err)
	assert.False(t, result.Succeeded)
	assert.Contains(t, result.Error, "validation failed")
	assert.Len(t, result.FailedKeys, 1)
}

func TestBatchStream_HandleComplete_BatchPutError(t *testing.T) {
	mockSvc := testutil.NewMockQuicBaaS()
	mockSvc.On("BatchPut", mock.Anything, mock.Anything).
		Return((*service.BatchPutResponse)(nil), errors.New("raft unavailable"))

	srv := newTestBatchStreamServer(t, mockSvc)
	stream := &mockBatchStream{ctx: context.Background()}
	session := &streamBatchSession{
		sessionID:       "test",
		header:          &pb.StreamBatchHeader{TotalOperations: 1, ShardId: 1},
		receivedOps:     1,
		totalOperations: 1,
		operations: []*pb.PutRequest{
			{EntityType: "user", Id: 1, Data: []byte("data")},
		},
	}
	metrics := initStreamBatchMetrics()

	result, err := srv.handleStreamBatchComplete(context.Background(), stream, session,
		&pb.StreamBatchComplete{Commit: true}, metrics)
	require.NoError(t, err)
	assert.False(t, result.Succeeded)
	assert.Contains(t, result.Error, "raft unavailable")
	mockSvc.AssertExpectations(t)
}

func TestBatchStream_HandleComplete_Success(t *testing.T) {
	mockSvc := testutil.NewMockQuicBaaS()
	mockSvc.On("BatchPut", mock.Anything, mock.Anything).
		Return(&service.BatchPutResponse{
			Responses: []*service.PutResponse{
				{ID: 1},
				{ID: 2},
			},
			Timestamp: time.Now().UnixNano(),
		}, nil)

	srv := newTestBatchStreamServer(t, mockSvc)
	stream := &mockBatchStream{ctx: context.Background()}
	session := &streamBatchSession{
		sessionID:       "test",
		header:          &pb.StreamBatchHeader{TotalOperations: 2, ShardId: 1},
		receivedOps:     2,
		totalOperations: 2,
		operations: []*pb.PutRequest{
			{EntityType: "user", Id: 1, Data: []byte("data1")},
			{EntityType: "user", Id: 2, Data: []byte("data2")},
		},
	}
	metrics := initStreamBatchMetrics()

	result, err := srv.handleStreamBatchComplete(context.Background(), stream, session,
		&pb.StreamBatchComplete{Commit: true}, metrics)
	require.NoError(t, err)
	assert.True(t, result.Succeeded)
	assert.Equal(t, int64(2), result.OperationsCommitted)
	assert.Empty(t, result.Error)
	mockSvc.AssertExpectations(t)
}

func TestBatchStream_HandleComplete_PartialOps(t *testing.T) {
	mockSvc := testutil.NewMockQuicBaaS()
	mockSvc.On("BatchPut", mock.Anything, mock.Anything).
		Return(&service.BatchPutResponse{
			Responses: []*service.PutResponse{
				{ID: 1},
				{ID: 2},
			},
			Timestamp: time.Now().UnixNano(),
		}, nil)

	srv := newTestBatchStreamServer(t, mockSvc)
	stream := &mockBatchStream{ctx: context.Background()}
	// Received fewer ops than total -- should warn but proceed
	session := &streamBatchSession{
		sessionID:       "test",
		header:          &pb.StreamBatchHeader{TotalOperations: 5, ShardId: 1},
		receivedOps:     2,
		totalOperations: 5,
		operations: []*pb.PutRequest{
			{EntityType: "user", Id: 1, Data: []byte("data1")},
			{EntityType: "user", Id: 2, Data: []byte("data2")},
		},
	}
	metrics := initStreamBatchMetrics()

	result, err := srv.handleStreamBatchComplete(context.Background(), stream, session,
		&pb.StreamBatchComplete{Commit: true}, metrics)
	require.NoError(t, err)
	assert.True(t, result.Succeeded)
	assert.Equal(t, int64(2), result.OperationsCommitted)
	mockSvc.AssertExpectations(t)
}

// --- Helper function tests ---

func TestBatchStream_FormatFunctions(t *testing.T) {
	assert.Equal(t, "0", formatInt(0))
	assert.Equal(t, "42", formatInt(42))
	assert.Equal(t, "-1", formatInt(-1))
	assert.Equal(t, "0", formatInt64(0))
	assert.Equal(t, "123456789", formatInt64(123456789))
	assert.Equal(t, "-99", formatInt64(-99))
	assert.Equal(t, "0", formatUint(0))
	assert.Equal(t, "1", formatUint(1))
	assert.Equal(t, "999", formatUint(999))
}

func TestBatchStream_GenerateSessionID(t *testing.T) {
	id := generateSessionID()
	assert.NotEmpty(t, id)
	assert.Contains(t, id, "stream-")

	// Two IDs generated at different times should be different
	id2 := generateSessionID()
	// They might be equal if called too fast, but they should both start with "stream-"
	assert.Contains(t, id2, "stream-")
}

func TestBatchStream_ConvertProtoToServiceConsistency(t *testing.T) {
	assert.Equal(t, service.ConsistencyLevelStrong, convertProtoToServiceConsistency(pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG))
	assert.Equal(t, service.ConsistencyLevelLinearizable, convertProtoToServiceConsistency(pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE))
	assert.Equal(t, service.ConsistencyLevelEventual, convertProtoToServiceConsistency(pb.ConsistencyLevel_CONSISTENCY_LEVEL_EVENTUAL))
	assert.Equal(t, service.ConsistencyLevelLinearizable, convertProtoToServiceConsistency(pb.ConsistencyLevel_CONSISTENCY_LEVEL_UNSPECIFIED))
}

func TestBatchStream_InitMetrics(t *testing.T) {
	m := initStreamBatchMetrics()
	assert.NotNil(t, m)
	assert.NotNil(t, m.streamsTotal)
	assert.NotNil(t, m.streamDuration)
	assert.NotNil(t, m.chunksProcessed)
	assert.NotNil(t, m.operationsTotal)
	assert.NotNil(t, m.backpressure)
	assert.NotNil(t, m.activeStreams)

	// Calling again should return same instance
	m2 := initStreamBatchMetrics()
	assert.Same(t, m, m2)
}
func TestStreamBatchPut_RecvError(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)

	callCount := 0
	stream := &mockBatchStream{
		ctx: context.Background(),
		recvFunc: func() (*pb.StreamBatchPutRequest, error) {
			callCount++
			if callCount == 1 {
				return nil, errors.New("connection lost")
			}
			return nil, io.EOF
		},
	}

	err := srv.StreamBatchPut(stream)
	require.Error(t, err)
}

func TestStreamBatchPut_EOF(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)

	stream := &mockBatchStream{
		ctx: context.Background(),
		recvFunc: func() (*pb.StreamBatchPutRequest, error) {
			return nil, io.EOF
		},
	}

	err := srv.StreamBatchPut(stream)
	require.NoError(t, err) // EOF without complete is handled gracefully
}

func TestStreamBatchPut_ContextCanceled(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	stream := &mockBatchStream{
		ctx: ctx,
		recvFunc: func() (*pb.StreamBatchPutRequest, error) {
			return nil, io.EOF
		},
	}

	err := srv.StreamBatchPut(stream)
	require.Error(t, err)
	assert.Equal(t, codes.Canceled, status.Code(err))
}

func TestStreamBatchPut_UnknownPayload(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)

	callCount := 0
	stream := &mockBatchStream{
		ctx: context.Background(),
		recvFunc: func() (*pb.StreamBatchPutRequest, error) {
			callCount++
			if callCount == 1 {
				// Send a request with nil payload (unknown type)
				return &pb.StreamBatchPutRequest{}, nil
			}
			return nil, io.EOF
		},
	}

	err := srv.StreamBatchPut(stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestStreamBatchPut_FullFlowCoverage(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("BatchPut", mock.Anything, mock.Anything).
		Return(&service.BatchPutResponse{
			Responses: []*service.PutResponse{{ID: 1}},
			Timestamp: time.Now().UnixNano(),
		}, nil)

	callCount := 0
	stream := &mockBatchStream{
		ctx: context.Background(),
		recvFunc: func() (*pb.StreamBatchPutRequest, error) {
			callCount++
			switch callCount {
			case 1:
				return &pb.StreamBatchPutRequest{
					Payload: &pb.StreamBatchPutRequest_Header{
						Header: &pb.StreamBatchHeader{
							TotalOperations: 1,
							ShardId:         1,
						},
					},
				}, nil
			case 2:
				return &pb.StreamBatchPutRequest{
					Payload: &pb.StreamBatchPutRequest_Chunk{
						Chunk: &pb.StreamBatchChunk{
							ChunkIndex: 0,
							Operations: []*pb.PutRequest{
								{EntityType: "user", Id: 1, Data: []byte("data")},
							},
						},
					},
				}, nil
			case 3:
				return &pb.StreamBatchPutRequest{
					Payload: &pb.StreamBatchPutRequest_Complete{
						Complete: &pb.StreamBatchComplete{Commit: true},
					},
				}, nil
			default:
				return nil, io.EOF
			}
		},
	}

	err := srv.StreamBatchPut(stream)
	require.NoError(t, err)
	mockSvc.AssertExpectations(t)
}

func TestStreamBatchPut_AbortFlow(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)

	callCount := 0
	stream := &mockBatchStream{
		ctx: context.Background(),
		recvFunc: func() (*pb.StreamBatchPutRequest, error) {
			callCount++
			switch callCount {
			case 1:
				return &pb.StreamBatchPutRequest{
					Payload: &pb.StreamBatchPutRequest_Header{
						Header: &pb.StreamBatchHeader{
							TotalOperations: 1,
							ShardId:         1,
						},
					},
				}, nil
			case 2:
				return &pb.StreamBatchPutRequest{
					Payload: &pb.StreamBatchPutRequest_Complete{
						Complete: &pb.StreamBatchComplete{Commit: false},
					},
				}, nil
			default:
				return nil, io.EOF
			}
		},
	}

	err := srv.StreamBatchPut(stream)
	require.NoError(t, err)
}

func TestStreamBatchPut_FailedBatchSendResult(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("BatchPut", mock.Anything, mock.Anything).
		Return((*service.BatchPutResponse)(nil), errors.New("raft error"))

	callCount := 0
	var lastSendResp *pb.StreamBatchPutResponse
	stream := &mockBatchStream{
		ctx: context.Background(),
		recvFunc: func() (*pb.StreamBatchPutRequest, error) {
			callCount++
			switch callCount {
			case 1:
				return &pb.StreamBatchPutRequest{
					Payload: &pb.StreamBatchPutRequest_Header{
						Header: &pb.StreamBatchHeader{TotalOperations: 1, ShardId: 1},
					},
				}, nil
			case 2:
				return &pb.StreamBatchPutRequest{
					Payload: &pb.StreamBatchPutRequest_Chunk{
						Chunk: &pb.StreamBatchChunk{
							ChunkIndex: 0,
							Operations: []*pb.PutRequest{
								{EntityType: "user", Id: 1, Data: []byte("data")},
							},
						},
					},
				}, nil
			case 3:
				return &pb.StreamBatchPutRequest{
					Payload: &pb.StreamBatchPutRequest_Complete{
						Complete: &pb.StreamBatchComplete{Commit: true},
					},
				}, nil
			default:
				return nil, io.EOF
			}
		},
		sendFunc: func(resp *pb.StreamBatchPutResponse) error {
			lastSendResp = resp
			return nil
		},
	}

	err := srv.StreamBatchPut(stream)
	require.NoError(t, err) // The result is sent back, not returned as error
	// Verify the result indicates failure
	require.NotNil(t, lastSendResp)
	result := lastSendResp.GetResult()
	require.NotNil(t, result)
	assert.False(t, result.Succeeded)
	assert.Contains(t, result.Error, "raft error")
	mockSvc.AssertExpectations(t)
}
