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
	"context"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/routing"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// =============================================================================
// Mock gRPC Server Streams
// =============================================================================

// MockServerStreamBase provides common implementation for mock server streams.
type MockServerStreamBase struct {
	ctx      context.Context
	mu       sync.Mutex
	headers  metadata.MD
	trailers metadata.MD
	recvErr  error
	sendErr  error
}

func (m *MockServerStreamBase) SetHeader(md metadata.MD) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.headers = md
	return nil
}

func (m *MockServerStreamBase) SendHeader(md metadata.MD) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.headers = md
	return nil
}

func (m *MockServerStreamBase) SetTrailer(md metadata.MD) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trailers = md
}

func (m *MockServerStreamBase) Context() context.Context {
	return m.ctx
}

func (m *MockServerStreamBase) SendMsg(_ interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sendErr
}

func (m *MockServerStreamBase) RecvMsg(_ interface{}) error {
	return m.recvErr
}

// SetSendError sets the error to return on Send calls.
func (m *MockServerStreamBase) SetSendError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendErr = err
}

// SetRecvError sets the error to return on Recv calls.
func (m *MockServerStreamBase) SetRecvError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recvErr = err
}

// =============================================================================
// Mock List Stream
// =============================================================================

// MockListStreamHandler implements pb.QuicBaaS_ListServer for testing.
type MockListStreamHandler struct {
	MockServerStreamBase
	messages []*pb.ListResponse
}

// NewMockListStreamHandler creates a new mock list stream handler.
func NewMockListStreamHandler(ctx context.Context) *MockListStreamHandler {
	return &MockListStreamHandler{
		MockServerStreamBase: MockServerStreamBase{ctx: ctx},
		messages:             make([]*pb.ListResponse, 0),
	}
}

func (m *MockListStreamHandler) Send(resp *pb.ListResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.messages = append(m.messages, resp)
	return nil
}

// GetMessages returns all sent messages.
func (m *MockListStreamHandler) GetMessages() []*pb.ListResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages
}

// =============================================================================
// Mock Watch Stream
// =============================================================================

// MockWatchStreamHandler implements pb.QuicBaaS_WatchServer for testing.
type MockWatchStreamHandler struct {
	MockServerStreamBase
	messages []*pb.WatchResponse
}

// NewMockWatchStreamHandler creates a new mock watch stream handler.
func NewMockWatchStreamHandler(ctx context.Context) *MockWatchStreamHandler {
	return &MockWatchStreamHandler{
		MockServerStreamBase: MockServerStreamBase{ctx: ctx},
		messages:             make([]*pb.WatchResponse, 0),
	}
}

func (m *MockWatchStreamHandler) Send(resp *pb.WatchResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.messages = append(m.messages, resp)
	return nil
}

// GetMessages returns all sent messages.
func (m *MockWatchStreamHandler) GetMessages() []*pb.WatchResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages
}

// =============================================================================
// Mock Query Stream
// =============================================================================

// MockQueryStreamHandler implements pb.QuicBaaS_QueryServer for testing.
type MockQueryStreamHandler struct {
	MockServerStreamBase
	messages []*pb.QueryResponse
}

// NewMockQueryStreamHandler creates a new mock query stream handler.
func NewMockQueryStreamHandler(ctx context.Context) *MockQueryStreamHandler {
	return &MockQueryStreamHandler{
		MockServerStreamBase: MockServerStreamBase{ctx: ctx},
		messages:             make([]*pb.QueryResponse, 0),
	}
}

func (m *MockQueryStreamHandler) Send(resp *pb.QueryResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.messages = append(m.messages, resp)
	return nil
}

// GetMessages returns all sent messages.
func (m *MockQueryStreamHandler) GetMessages() []*pb.QueryResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages
}

// =============================================================================
// Mock ScanIndex Stream
// =============================================================================

// MockScanStreamHandler implements pb.QuicBaaS_ScanIndexServer for testing.
type MockScanStreamHandler struct {
	MockServerStreamBase
	messages []*pb.ScanResponse
}

// NewMockScanStreamHandler creates a new mock scan stream handler.
func NewMockScanStreamHandler(ctx context.Context) *MockScanStreamHandler {
	return &MockScanStreamHandler{
		MockServerStreamBase: MockServerStreamBase{ctx: ctx},
		messages:             make([]*pb.ScanResponse, 0),
	}
}

func (m *MockScanStreamHandler) Send(resp *pb.ScanResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.messages = append(m.messages, resp)
	return nil
}

// GetMessages returns all sent messages.
func (m *MockScanStreamHandler) GetMessages() []*pb.ScanResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages
}

// =============================================================================
// Mock TimeSeriesQuery Stream
// =============================================================================

// MockTimeSeriesStreamHandler implements pb.QuicBaaS_TimeSeriesQueryServer for testing.
type MockTimeSeriesStreamHandler struct {
	MockServerStreamBase
	messages []*pb.TimeSeriesQueryResponse
}

// NewMockTimeSeriesStreamHandler creates a new mock time series query stream handler.
func NewMockTimeSeriesStreamHandler(ctx context.Context) *MockTimeSeriesStreamHandler {
	return &MockTimeSeriesStreamHandler{
		MockServerStreamBase: MockServerStreamBase{ctx: ctx},
		messages:             make([]*pb.TimeSeriesQueryResponse, 0),
	}
}

func (m *MockTimeSeriesStreamHandler) Send(resp *pb.TimeSeriesQueryResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.messages = append(m.messages, resp)
	return nil
}

// GetMessages returns all sent messages.
func (m *MockTimeSeriesStreamHandler) GetMessages() []*pb.TimeSeriesQueryResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages
}

// =============================================================================
// Mock StreamBatchPut Bidirectional Stream
// =============================================================================

// MockStreamBatchPutHandler implements pb.QuicBaaS_StreamBatchPutServer for testing.
type MockStreamBatchPutHandler struct {
	MockServerStreamBase
	recvQueue []*pb.StreamBatchPutRequest
	recvIndex int
	sentMsgs  []*pb.StreamBatchPutResponse
}

// NewMockStreamBatchPutHandler creates a new mock stream batch put handler.
func NewMockStreamBatchPutHandler(ctx context.Context) *MockStreamBatchPutHandler {
	return &MockStreamBatchPutHandler{
		MockServerStreamBase: MockServerStreamBase{ctx: ctx},
		recvQueue:            make([]*pb.StreamBatchPutRequest, 0),
		sentMsgs:             make([]*pb.StreamBatchPutResponse, 0),
	}
}

func (m *MockStreamBatchPutHandler) Send(resp *pb.StreamBatchPutResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sentMsgs = append(m.sentMsgs, resp)
	return nil
}

func (m *MockStreamBatchPutHandler) Recv() (*pb.StreamBatchPutRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recvErr != nil {
		return nil, m.recvErr
	}
	if m.recvIndex >= len(m.recvQueue) {
		return nil, io.EOF
	}
	msg := m.recvQueue[m.recvIndex]
	m.recvIndex++
	return msg, nil
}

// AddRecvMessage adds a message to the receive queue.
func (m *MockStreamBatchPutHandler) AddRecvMessage(req *pb.StreamBatchPutRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recvQueue = append(m.recvQueue, req)
}

// GetSentMessages returns all sent messages.
func (m *MockStreamBatchPutHandler) GetSentMessages() []*pb.StreamBatchPutResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sentMsgs
}

// =============================================================================
// Test Helper Functions
// =============================================================================

// newMockTestServer creates a Server instance for testing with a mock service.
func newMockTestServer(t *testing.T) (*Server, *testutil.MockQuicBaaS) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()
	srv, err := NewServer(mockSvc, log, 1, "test-1.0.0")
	require.NoError(t, err)
	srv.initMetrics()
	srv.SetShardRouter(routing.New())
	return srv, mockSvc
}

// =============================================================================
// Streaming Handler Tests
// =============================================================================

func TestStreamBatchPut_FullFlow(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock BatchPut to return success
	mockSvc.On("BatchPut", mock.Anything, mock.Anything).
		Return(&service.BatchPutResponse{
			Responses: []*service.PutResponse{
				{ID: 1, Version: 1, Timestamp: 1234567890},
				{ID: 2, Version: 1, Timestamp: 1234567890},
			},
			Timestamp: 1234567890,
		}, nil)

	stream := NewMockStreamBatchPutHandler(context.Background())

	// Add header
	stream.AddRecvMessage(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{
			Header: &pb.StreamBatchHeader{
				ShardId:         1,
				TotalOperations: 2,
				ChunkSize:       100,
			},
		},
	})

	// Add chunk with operations
	stream.AddRecvMessage(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Chunk{
			Chunk: &pb.StreamBatchChunk{
				ChunkIndex: 0,
				Operations: []*pb.PutRequest{
					{EntityType: "users", Id: 1, Data: []byte(`{"name":"user1"}`)},
					{EntityType: "users", Id: 2, Data: []byte(`{"name":"user2"}`)},
				},
			},
		},
	})

	// Add complete message
	stream.AddRecvMessage(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Complete{
			Complete: &pb.StreamBatchComplete{
				Commit: true,
			},
		},
	})

	err := srv.StreamBatchPut(stream)
	require.NoError(t, err)

	sentMsgs := stream.GetSentMessages()
	require.GreaterOrEqual(t, len(sentMsgs), 2)

	// Last message should be the result
	lastMsg := sentMsgs[len(sentMsgs)-1]
	result := lastMsg.GetResult()
	require.NotNil(t, result)
	assert.True(t, result.Succeeded)

	mockSvc.AssertExpectations(t)
}

func TestStreamBatchPut_Abort(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	stream := NewMockStreamBatchPutHandler(context.Background())

	// Add header
	stream.AddRecvMessage(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{
			Header: &pb.StreamBatchHeader{
				ShardId:         1,
				TotalOperations: 2,
			},
		},
	})

	// Add abort complete message
	stream.AddRecvMessage(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Complete{
			Complete: &pb.StreamBatchComplete{
				Commit: false, // Abort
			},
		},
	})

	err := srv.StreamBatchPut(stream)
	require.NoError(t, err)

	sentMsgs := stream.GetSentMessages()
	lastMsg := sentMsgs[len(sentMsgs)-1]
	result := lastMsg.GetResult()
	require.NotNil(t, result)
	assert.False(t, result.Succeeded)

	mockSvc.AssertExpectations(t)
}

func TestStreamBatchPut_ValidationError_InvalidHeader(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	stream := NewMockStreamBatchPutHandler(context.Background())

	// Add invalid header (total_operations <= 0)
	stream.AddRecvMessage(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{
			Header: &pb.StreamBatchHeader{
				ShardId:         1,
				TotalOperations: 0, // Invalid
			},
		},
	})

	err := srv.StreamBatchPut(stream)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())

	mockSvc.AssertExpectations(t)
}

func TestStreamBatchPut_ChunkBeforeHeader(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	stream := NewMockStreamBatchPutHandler(context.Background())

	// Add chunk before header
	stream.AddRecvMessage(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Chunk{
			Chunk: &pb.StreamBatchChunk{
				ChunkIndex: 0,
				Operations: []*pb.PutRequest{
					{EntityType: "users", Id: 1, Data: []byte(`{}`)},
				},
			},
		},
	})

	err := srv.StreamBatchPut(stream)
	require.Error(t, err)

	mockSvc.AssertExpectations(t)
}

func TestStreamBatchPut_ContextCancellation(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	stream := NewMockStreamBatchPutHandler(ctx)

	// Cancel context before any messages
	cancel()

	// Set recv error to simulate EOF after cancel
	stream.SetRecvError(context.Canceled)

	err := srv.StreamBatchPut(stream)
	require.Error(t, err)

	mockSvc.AssertExpectations(t)
}

func TestStreamBatchPut_SendError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	stream := NewMockStreamBatchPutHandler(context.Background())
	stream.SetSendError(status.Error(codes.Unavailable, "connection lost"))

	// Add header
	stream.AddRecvMessage(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{
			Header: &pb.StreamBatchHeader{
				ShardId:         1,
				TotalOperations: 1,
			},
		},
	})

	err := srv.StreamBatchPut(stream)
	require.Error(t, err)

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Server Streaming Error Path Tests
// =============================================================================

func TestList_StreamSendError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock List to send a response
	mockSvc.On("List", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.ListResponse)
			go func() {
				defer close(stream)
				stream <- &service.ListResponse{
					Entities:   []*service.Entity{{EntityType: "users", ID: 1}},
					TotalCount: 1,
				}
			}()
		}).Return(nil)

	stream := NewMockListStreamHandler(context.Background())
	stream.SetSendError(status.Error(codes.Unavailable, "connection lost"))

	err := srv.List(&pb.ListRequest{EntityType: "users", PageSize: 100}, stream)
	require.Error(t, err)

	mockSvc.AssertExpectations(t)
}

func TestWatch_StreamSendError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock Watch to return a watch ID and send events
	mockSvc.On("Watch", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.WatchResponse)
			go func() {
				defer close(stream)
				stream <- &service.WatchResponse{
					WatchID: 42,
					Created: true,
				}
			}()
		}).Return(int64(42), nil)

	stream := NewMockWatchStreamHandler(context.Background())
	stream.SetSendError(status.Error(codes.Unavailable, "connection lost"))

	err := srv.Watch(&pb.WatchRequest{Key: []byte("key"), IsPrefix: true}, stream)
	require.Error(t, err)

	mockSvc.AssertExpectations(t)
}

func TestQuery_StreamSendError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock Query to send a response
	mockSvc.On("Query", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.QueryResponse)
			go func() {
				defer close(stream)
				stream <- &service.QueryResponse{
					Entities:   []*service.Entity{{EntityType: "users", ID: 1}},
					TotalCount: 1,
				}
			}()
		}).Return(nil)

	stream := NewMockQueryStreamHandler(context.Background())
	stream.SetSendError(status.Error(codes.Unavailable, "connection lost"))

	err := srv.Query(&pb.QueryRequest{
		EntityType: "users",
		IndexName:  "email_idx",
		Value:      []byte("test@example.com"),
	}, stream)
	require.Error(t, err)

	mockSvc.AssertExpectations(t)
}

func TestScanIndex_StreamSendError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock ScanIndex to send a response
	mockSvc.On("ScanIndex", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.ScanResponse)
			go func() {
				defer close(stream)
				stream <- &service.ScanResponse{
					Entities:   []*service.Entity{{EntityType: "users", ID: 1}},
					TotalCount: 1,
				}
			}()
		}).Return(nil)

	stream := NewMockScanStreamHandler(context.Background())
	stream.SetSendError(status.Error(codes.Unavailable, "connection lost"))

	err := srv.ScanIndex(&pb.ScanRequest{
		EntityType: "users",
		IndexName:  "created_at_idx",
	}, stream)
	require.Error(t, err)

	mockSvc.AssertExpectations(t)
}

func TestTimeSeriesQuery_StreamSendError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock TimeSeriesQuery to send a response
	mockSvc.On("TimeSeriesQuery", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.TimeSeriesQueryResponse)
			go func() {
				defer close(stream)
				stream <- &service.TimeSeriesQueryResponse{
					DataPoints: []*service.TimeSeriesPoint{{ID: 1, Timestamp: 1000, Value: 100.0}},
					TotalCount: 1,
				}
			}()
		}).Return(nil)

	stream := NewMockTimeSeriesStreamHandler(context.Background())
	stream.SetSendError(status.Error(codes.Unavailable, "connection lost"))

	err := srv.TimeSeriesQuery(&pb.TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  1000,
		EndTime:    2000,
	}, stream)
	require.Error(t, err)

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Successful Streaming Tests
// =============================================================================

func TestList_SuccessfulStream(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock List to send multiple pages
	mockSvc.On("List", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.ListResponse)
			go func() {
				defer close(stream)
				stream <- &service.ListResponse{
					Entities: []*service.Entity{
						{EntityType: "users", ID: 1, Data: []byte(`{"name":"user1"}`)},
						{EntityType: "users", ID: 2, Data: []byte(`{"name":"user2"}`)},
					},
					NextPageToken: "page2",
					TotalCount:    4,
				}
				stream <- &service.ListResponse{
					Entities: []*service.Entity{
						{EntityType: "users", ID: 3, Data: []byte(`{"name":"user3"}`)},
						{EntityType: "users", ID: 4, Data: []byte(`{"name":"user4"}`)},
					},
					TotalCount: 4,
				}
			}()
		}).Return(nil)

	stream := NewMockListStreamHandler(context.Background())

	err := srv.List(&pb.ListRequest{EntityType: "users", PageSize: 2}, stream)
	require.NoError(t, err)

	msgs := stream.GetMessages()
	assert.Len(t, msgs, 2)
	assert.Len(t, msgs[0].Entities, 2)
	assert.Len(t, msgs[1].Entities, 2)

	mockSvc.AssertExpectations(t)
}

func TestWatch_SuccessfulStream(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock Watch to send events
	mockSvc.On("Watch", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.WatchResponse)
			go func() {
				defer close(stream)
				stream <- &service.WatchResponse{
					WatchID: 42,
					Created: true,
				}
				stream <- &service.WatchResponse{
					WatchID: 42,
					Events: []*service.WatchEvent{
						{Type: service.WatchEventTypePut, Key: []byte("key1"), Value: []byte("value1")},
					},
				}
			}()
		}).Return(int64(42), nil)

	stream := NewMockWatchStreamHandler(context.Background())

	err := srv.Watch(&pb.WatchRequest{Key: []byte("key"), IsPrefix: true}, stream)
	require.NoError(t, err)

	msgs := stream.GetMessages()
	assert.GreaterOrEqual(t, len(msgs), 1)

	mockSvc.AssertExpectations(t)
}

func TestQuery_SuccessfulStream(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock Query to send results
	mockSvc.On("Query", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.QueryResponse)
			go func() {
				defer close(stream)
				stream <- &service.QueryResponse{
					Entities: []*service.Entity{
						{EntityType: "users", ID: 1, Data: []byte(`{"email":"test@example.com"}`)},
					},
					TotalCount: 1,
				}
			}()
		}).Return(nil)

	stream := NewMockQueryStreamHandler(context.Background())

	err := srv.Query(&pb.QueryRequest{
		EntityType: "users",
		IndexName:  "email_idx",
		Value:      []byte("test@example.com"),
	}, stream)
	require.NoError(t, err)

	msgs := stream.GetMessages()
	assert.Len(t, msgs, 1)
	assert.Len(t, msgs[0].Entities, 1)

	mockSvc.AssertExpectations(t)
}

func TestScanIndex_SuccessfulStream(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock ScanIndex to send results
	mockSvc.On("ScanIndex", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.ScanResponse)
			go func() {
				defer close(stream)
				stream <- &service.ScanResponse{
					Entities: []*service.Entity{
						{EntityType: "users", ID: 1},
						{EntityType: "users", ID: 2},
						{EntityType: "users", ID: 3},
					},
					TotalCount: 3,
				}
			}()
		}).Return(nil)

	stream := NewMockScanStreamHandler(context.Background())

	err := srv.ScanIndex(&pb.ScanRequest{
		EntityType: "users",
		IndexName:  "created_at_idx",
	}, stream)
	require.NoError(t, err)

	msgs := stream.GetMessages()
	assert.Len(t, msgs, 1)
	assert.Len(t, msgs[0].Entities, 3)

	mockSvc.AssertExpectations(t)
}

func TestTimeSeriesQuery_SuccessfulStream(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock TimeSeriesQuery to send results
	mockSvc.On("TimeSeriesQuery", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.TimeSeriesQueryResponse)
			go func() {
				defer close(stream)
				stream <- &service.TimeSeriesQueryResponse{
					DataPoints: []*service.TimeSeriesPoint{
						{ID: 1, Timestamp: 1000, Value: 100.0},
						{ID: 2, Timestamp: 1500, Value: 150.0},
					},
					TotalCount: 2,
				}
			}()
		}).Return(nil)

	stream := NewMockTimeSeriesStreamHandler(context.Background())

	err := srv.TimeSeriesQuery(&pb.TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  1000,
		EndTime:    2000,
	}, stream)
	require.NoError(t, err)

	msgs := stream.GetMessages()
	assert.Len(t, msgs, 1)
	assert.Len(t, msgs[0].DataPoints, 2)

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Write Mode Extraction Tests
// =============================================================================

func TestExtractWriteMode_FromMetadata(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		expected service.WriteMode
	}{
		{"sync", "sync", service.WriteModeSync},
		{"batched", "batched", service.WriteModeBatched},
		{"wal_durable", "wal_durable", service.WriteModeWALDurable},
		{"unknown defaults to sync", "unknown", service.WriteModeSync},
		{"empty defaults to sync", "", service.WriteModeSync},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ctx context.Context
			if tt.mode != "" {
				md := metadata.New(map[string]string{"write-mode": tt.mode})
				ctx = metadata.NewIncomingContext(context.Background(), md)
			} else {
				ctx = context.Background()
			}

			result := extractWriteMode(ctx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Protocol Extraction Tests
// =============================================================================

func TestExtractProtocol_FromMetadata(t *testing.T) {
	tests := []struct {
		name       string
		protocol   string
		expectJSON bool
	}{
		{"json", "json", true},
		{"binary", "binary", false},
		{"empty defaults to json", "", true},
		{"unknown defaults to json", "unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ctx context.Context
			if tt.protocol != "" {
				md := metadata.New(map[string]string{"x-quicbaas-protocol": tt.protocol})
				ctx = metadata.NewIncomingContext(context.Background(), md)
			} else {
				ctx = context.Background()
			}

			proto := extractProtocol(ctx)
			assert.Equal(t, tt.expectJSON, proto.IsJSON())
		})
	}
}

// =============================================================================
// Empty Stream Tests
// =============================================================================

func TestList_EmptyResults(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock List to send empty results
	mockSvc.On("List", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.ListResponse)
			go func() {
				defer close(stream)
				stream <- &service.ListResponse{
					Entities:   []*service.Entity{},
					TotalCount: 0,
				}
			}()
		}).Return(nil)

	stream := NewMockListStreamHandler(context.Background())

	err := srv.List(&pb.ListRequest{EntityType: "users", PageSize: 100}, stream)
	require.NoError(t, err)

	msgs := stream.GetMessages()
	assert.Len(t, msgs, 1)
	assert.Empty(t, msgs[0].Entities)

	mockSvc.AssertExpectations(t)
}

func TestQuery_EmptyResults(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock Query to send empty results
	mockSvc.On("Query", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.QueryResponse)
			go func() {
				defer close(stream)
				stream <- &service.QueryResponse{
					Entities:   []*service.Entity{},
					TotalCount: 0,
				}
			}()
		}).Return(nil)

	stream := NewMockQueryStreamHandler(context.Background())

	err := srv.Query(&pb.QueryRequest{
		EntityType: "users",
		IndexName:  "email_idx",
		Value:      []byte("nonexistent@example.com"),
	}, stream)
	require.NoError(t, err)

	msgs := stream.GetMessages()
	assert.Len(t, msgs, 1)
	assert.Empty(t, msgs[0].Entities)

	mockSvc.AssertExpectations(t)
}

func TestScanIndex_EmptyResults(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock ScanIndex to send empty results
	mockSvc.On("ScanIndex", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.ScanResponse)
			go func() {
				defer close(stream)
				stream <- &service.ScanResponse{
					Entities:   []*service.Entity{},
					TotalCount: 0,
				}
			}()
		}).Return(nil)

	stream := NewMockScanStreamHandler(context.Background())

	err := srv.ScanIndex(&pb.ScanRequest{
		EntityType: "empty_entity",
		IndexName:  "created_at_idx",
	}, stream)
	require.NoError(t, err)

	msgs := stream.GetMessages()
	assert.Len(t, msgs, 1)
	assert.Empty(t, msgs[0].Entities)

	mockSvc.AssertExpectations(t)
}

func TestTimeSeriesQuery_EmptyResults(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock TimeSeriesQuery to send empty results
	mockSvc.On("TimeSeriesQuery", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.TimeSeriesQueryResponse)
			go func() {
				defer close(stream)
				stream <- &service.TimeSeriesQueryResponse{
					DataPoints: []*service.TimeSeriesPoint{},
					TotalCount: 0,
				}
			}()
		}).Return(nil)

	stream := NewMockTimeSeriesStreamHandler(context.Background())

	err := srv.TimeSeriesQuery(&pb.TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  1000,
		EndTime:    2000,
	}, stream)
	require.NoError(t, err)

	msgs := stream.GetMessages()
	assert.Len(t, msgs, 1)
	assert.Empty(t, msgs[0].DataPoints)

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Mock Stream Base Tests
// =============================================================================

func TestMockServerStreamBase_Headers(t *testing.T) {
	base := &MockServerStreamBase{ctx: context.Background()}

	md := metadata.New(map[string]string{"key": "value"})
	err := base.SetHeader(md)
	require.NoError(t, err)
	assert.NotNil(t, base.headers)

	err = base.SendHeader(md)
	require.NoError(t, err)
}

func TestMockServerStreamBase_Trailers(t *testing.T) {
	base := &MockServerStreamBase{ctx: context.Background()}

	md := metadata.New(map[string]string{"trailer-key": "trailer-value"})
	base.SetTrailer(md)
	assert.NotNil(t, base.trailers)
}

func TestMockServerStreamBase_Context(t *testing.T) {
	type testKey string
	ctx := context.WithValue(context.Background(), testKey("key"), "value")
	base := &MockServerStreamBase{ctx: ctx}

	resultCtx := base.Context()
	assert.Equal(t, "value", resultCtx.Value(testKey("key")))
}

func TestMockServerStreamBase_SendMsg(t *testing.T) {
	base := &MockServerStreamBase{ctx: context.Background()}

	// No error initially
	err := base.SendMsg("test")
	require.NoError(t, err)

	// Set error
	base.SetSendError(status.Error(codes.Unavailable, "connection lost"))
	err = base.SendMsg("test")
	require.Error(t, err)
}

func TestMockServerStreamBase_RecvMsg(t *testing.T) {
	base := &MockServerStreamBase{ctx: context.Background()}

	// No error initially
	err := base.RecvMsg(nil)
	require.NoError(t, err)

	// Set error
	base.SetRecvError(io.EOF)
	err = base.RecvMsg(nil)
	require.Equal(t, io.EOF, err)
}
