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

//go:build !integration

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// mockWatchStream implements the pb.QuicBaaS_WatchServer interface for testing.
type mockWatchStream struct {
	ctx       context.Context
	sent      []*pb.WatchResponse
	sendError error
}

func newMockWatchStream(ctx context.Context) *mockWatchStream {
	return &mockWatchStream{
		ctx:  ctx,
		sent: make([]*pb.WatchResponse, 0),
	}
}

func (m *mockWatchStream) Send(resp *pb.WatchResponse) error {
	if m.sendError != nil {
		return m.sendError
	}
	m.sent = append(m.sent, resp)
	return nil
}

func (m *mockWatchStream) Context() context.Context {
	return m.ctx
}

func (m *mockWatchStream) SetHeader(md metadata.MD) error {
	return nil
}

func (m *mockWatchStream) SendHeader(md metadata.MD) error {
	return nil
}

func (m *mockWatchStream) SetTrailer(md metadata.MD) {}

func (m *mockWatchStream) SendMsg(msg interface{}) error {
	return nil
}

func (m *mockWatchStream) RecvMsg(msg interface{}) error {
	return nil
}

// createTestWatchServer creates a server with mock service for Watch tests.
func createTestWatchServer(t *testing.T) (*Server, *testutil.MockQuicBaaS) {
	t.Helper()

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "grpc-test",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()

	srv, err := NewServer(mockSvc, log, 1, "test-v1.0.0")
	require.NoError(t, err)
	return srv, mockSvc
}

// TestWatch_InvalidRequest tests Watch with invalid request parameters.
func TestWatch_InvalidRequest(t *testing.T) {
	srv, _ := createTestWatchServer(t)
	ctx := context.Background()

	t.Run("empty key", func(t *testing.T) {
		stream := newMockWatchStream(ctx)
		req := &pb.WatchRequest{
			Key:      []byte{},
			IsPrefix: false,
		}

		err := srv.Watch(req, stream)

		assert.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "key is required")
	})

	t.Run("nil key", func(t *testing.T) {
		stream := newMockWatchStream(ctx)
		req := &pb.WatchRequest{
			Key:      nil,
			IsPrefix: false,
		}

		err := srv.Watch(req, stream)

		assert.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

// TestWatch_SuccessfulWatch tests successful watch operations.
func TestWatch_SuccessfulWatch(t *testing.T) {
	srv, mockSvc := createTestWatchServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	t.Run("basic watch with events", func(t *testing.T) {
		stream := newMockWatchStream(ctx)
		req := &pb.WatchRequest{
			Key:      []byte("test-key"),
			IsPrefix: false,
		}

		// Set up mock to send events then close
		mockSvc.On("Watch", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				responseChan := args.Get(2).(chan<- *service.WatchResponse)
				// Send a test event
				responseChan <- &service.WatchResponse{
					WatchID:  1,
					Created:  true,
					Canceled: false,
					Events: []*service.WatchEvent{
						{
							Type:      service.WatchEventTypePut,
							Key:       []byte("test-key"),
							Value:     []byte("test-value"),
							Version:   1,
							Timestamp: time.Now().UnixNano(),
						},
					},
				}
				close(responseChan)
			}).
			Return(int64(1), nil).Once()

		err := srv.Watch(req, stream)

		assert.NoError(t, err)
		assert.Len(t, stream.sent, 1)
		assert.Equal(t, int64(1), stream.sent[0].WatchId)
		assert.True(t, stream.sent[0].Created)
		assert.Len(t, stream.sent[0].Events, 1)
		mockSvc.AssertExpectations(t)
	})

	t.Run("prefix watch", func(t *testing.T) {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel2()
		stream := newMockWatchStream(ctx2)
		req := &pb.WatchRequest{
			Key:      []byte("prefix/"),
			IsPrefix: true,
		}

		mockSvc.On("Watch", mock.Anything, mock.MatchedBy(func(req *service.WatchRequest) bool {
			return req.IsPrefix
		}), mock.Anything).
			Run(func(args mock.Arguments) {
				responseChan := args.Get(2).(chan<- *service.WatchResponse)
				close(responseChan)
			}).
			Return(int64(2), nil).Once()

		err := srv.Watch(req, stream)

		assert.NoError(t, err)
		mockSvc.AssertExpectations(t)
	})

	t.Run("watch with filters", func(t *testing.T) {
		ctx3, cancel3 := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel3()
		stream := newMockWatchStream(ctx3)
		req := &pb.WatchRequest{
			Key:      []byte("filtered-key"),
			IsPrefix: false,
			Filters:  []pb.WatchEventType{pb.WatchEventType_WATCH_EVENT_TYPE_PUT},
		}

		mockSvc.On("Watch", mock.Anything, mock.MatchedBy(func(req *service.WatchRequest) bool {
			return len(req.Filters) > 0
		}), mock.Anything).
			Run(func(args mock.Arguments) {
				responseChan := args.Get(2).(chan<- *service.WatchResponse)
				close(responseChan)
			}).
			Return(int64(3), nil).Once()

		err := srv.Watch(req, stream)

		assert.NoError(t, err)
		mockSvc.AssertExpectations(t)
	})
}

// TestWatch_ServiceError tests Watch handling of service errors.
func TestWatch_ServiceError(t *testing.T) {
	srv, mockSvc := createTestWatchServer(t)
	ctx := context.Background()

	t.Run("service returns error", func(t *testing.T) {
		stream := newMockWatchStream(ctx)
		req := &pb.WatchRequest{
			Key:      []byte("error-key"),
			IsPrefix: false,
		}

		mockSvc.On("Watch", mock.Anything, mock.Anything, mock.Anything).
			Return(int64(0), assert.AnError).Once()

		err := srv.Watch(req, stream)

		assert.Error(t, err)
		mockSvc.AssertExpectations(t)
	})
}

// TestWatch_SendError tests Watch handling of stream send errors.
func TestWatch_SendError(t *testing.T) {
	srv, mockSvc := createTestWatchServer(t)
	ctx := context.Background()

	t.Run("send fails", func(t *testing.T) {
		stream := newMockWatchStream(ctx)
		stream.sendError = assert.AnError
		req := &pb.WatchRequest{
			Key:      []byte("send-error-key"),
			IsPrefix: false,
		}

		mockSvc.On("Watch", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				responseChan := args.Get(2).(chan<- *service.WatchResponse)
				responseChan <- &service.WatchResponse{
					WatchID: 1,
					Events: []*service.WatchEvent{
						{
							Type: service.WatchEventTypePut,
							Key:  []byte("key"),
						},
					},
				}
				close(responseChan)
			}).
			Return(int64(1), nil).Once()

		err := srv.Watch(req, stream)

		assert.Error(t, err)
		mockSvc.AssertExpectations(t)
	})
}

// TestWatch_EventConversion tests proper conversion of watch events.
func TestWatch_EventConversion(t *testing.T) {
	srv, mockSvc := createTestWatchServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	t.Run("all event fields converted", func(t *testing.T) {
		stream := newMockWatchStream(ctx)
		req := &pb.WatchRequest{
			Key:    []byte("conversion-test"),
			PrevKv: true,
		}

		timestamp := time.Now().UnixNano()
		mockSvc.On("Watch", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				responseChan := args.Get(2).(chan<- *service.WatchResponse)
				responseChan <- &service.WatchResponse{
					WatchID:         42,
					Created:         true,
					Canceled:        false,
					CancelReason:    "",
					CompactRevision: 100,
					Revision:        200,
					Events: []*service.WatchEvent{
						{
							Type:      service.WatchEventTypePut,
							Key:       []byte("key1"),
							Value:     []byte("value1"),
							PrevValue: []byte("prev1"),
							Version:   5,
							Revision:  150,
							Timestamp: timestamp,
						},
						{
							Type:      service.WatchEventTypeDelete,
							Key:       []byte("key2"),
							Value:     nil,
							PrevValue: []byte("prev2"),
							Version:   3,
							Revision:  151,
							Timestamp: timestamp + 1,
						},
					},
				}
				close(responseChan)
			}).
			Return(int64(42), nil).Once()

		err := srv.Watch(req, stream)

		assert.NoError(t, err)
		require.Len(t, stream.sent, 1)

		resp := stream.sent[0]
		assert.Equal(t, int64(42), resp.WatchId)
		assert.True(t, resp.Created)
		assert.False(t, resp.Canceled)
		assert.Equal(t, int64(100), resp.CompactRevision)
		assert.Equal(t, int64(200), resp.Revision)
		require.Len(t, resp.Events, 2)

		// Check first event
		event1 := resp.Events[0]
		assert.Equal(t, pb.WatchEventType_WATCH_EVENT_TYPE_PUT, event1.Type)
		assert.Equal(t, []byte("key1"), event1.Key)
		assert.Equal(t, []byte("value1"), event1.Value)
		assert.Equal(t, []byte("prev1"), event1.PrevValue)
		assert.Equal(t, uint64(5), event1.Version)
		assert.Equal(t, int64(150), event1.Revision)
		assert.Equal(t, timestamp, event1.Timestamp)

		// Check second event
		event2 := resp.Events[1]
		assert.Equal(t, pb.WatchEventType_WATCH_EVENT_TYPE_DELETE, event2.Type)
		assert.Equal(t, []byte("key2"), event2.Key)

		mockSvc.AssertExpectations(t)
	})

	t.Run("canceled watch response", func(t *testing.T) {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel2()
		stream := newMockWatchStream(ctx2)
		req := &pb.WatchRequest{
			Key: []byte("cancel-test"),
		}

		mockSvc.On("Watch", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				responseChan := args.Get(2).(chan<- *service.WatchResponse)
				responseChan <- &service.WatchResponse{
					WatchID:      43,
					Canceled:     true,
					CancelReason: "test cancellation",
				}
				close(responseChan)
			}).
			Return(int64(43), nil).Once()

		err := srv.Watch(req, stream)

		assert.NoError(t, err)
		require.Len(t, stream.sent, 1)
		assert.True(t, stream.sent[0].Canceled)
		assert.Equal(t, "test cancellation", stream.sent[0].CancelReason)
		mockSvc.AssertExpectations(t)
	})
}
