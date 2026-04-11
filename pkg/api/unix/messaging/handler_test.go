//go:build unix

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

package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// =============================================================================
// Mock Logger
// =============================================================================

type testLogger struct {
	mu     sync.Mutex
	fields map[string]interface{}
}

func newTestLogger() *testLogger {
	return &testLogger{
		fields: make(map[string]interface{}),
	}
}

func (l *testLogger) Debug(_ string, _ ...any)                      {}
func (l *testLogger) Debugf(_ string, _ ...interface{})   {}
func (l *testLogger) Info(_ string, _ ...any)                       {}
func (l *testLogger) Infof(_ string, _ ...interface{})    {}
func (l *testLogger) Warn(_ string, _ ...any)                       {}
func (l *testLogger) Warnf(_ string, _ ...interface{})    {}
func (l *testLogger) Error(_ string, _ ...any)                      {}
func (l *testLogger) Errorf(_ string, _ ...interface{})   {}
func (l *testLogger) Fatal(_ string)                      {}
func (l *testLogger) Fatalf(_ string, _ ...interface{})   {}
func (l *testLogger) WithError(_ error) interfaces.Logger { return l }
func (l *testLogger) WithField(_ string, _ interface{}) interfaces.Logger {
	return l
}
func (l *testLogger) WithFields(_ map[string]interface{}) interfaces.Logger {
	return l
}

// =============================================================================
// Mock Messaging Service
// =============================================================================

// mockServiceError is a typed error used for mock error injection in tests.
type mockServiceError struct {
	Message string
}

// Error implements the error interface.
func (e *mockServiceError) Error() string {
	return e.Message
}

type mockMsgService struct {
	mu sync.Mutex

	// Return values
	createChannelResult *messaging.Channel
	getChannelResult    *messaging.Channel
	listChannelsResult  []*messaging.Channel
	subscribeResult     *messaging.Subscription
	statsResult         *messaging.Stats
	publishResult       *messaging.PublishResult
	publishDeltaResult  *messaging.PublishResult

	// Error injection
	createChannelErr error
	deleteChannelErr error
	getChannelErr    error
	listChannelsErr  error
	subscribeErr     error
	unsubscribeErr   error
	publishErr       error
	publishDeltaErr  error
}

func newMockMsgService() *mockMsgService {
	return &mockMsgService{
		statsResult: &messaging.Stats{
			TotalChannels:      5,
			TotalSubscriptions: 10,
		},
	}
}

func (m *mockMsgService) CreateChannel(_ context.Context, name string, _ ...messaging.CreateChannelOption) (*messaging.Channel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createChannelErr != nil {
		return nil, m.createChannelErr
	}
	if m.createChannelResult != nil {
		return m.createChannelResult, nil
	}
	return &messaging.Channel{
		Name:      name,
		CreatedAt: time.Now(),
	}, nil
}

func (m *mockMsgService) DeleteChannel(_ context.Context, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deleteChannelErr
}

func (m *mockMsgService) GetChannel(_ context.Context, name string) (*messaging.Channel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getChannelErr != nil {
		return nil, m.getChannelErr
	}
	if m.getChannelResult != nil {
		return m.getChannelResult, nil
	}
	return &messaging.Channel{Name: name, CreatedAt: time.Now()}, nil
}

func (m *mockMsgService) ListChannels(_ context.Context) ([]*messaging.Channel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listChannelsErr != nil {
		return nil, m.listChannelsErr
	}
	if m.listChannelsResult != nil {
		return m.listChannelsResult, nil
	}
	return []*messaging.Channel{}, nil
}

func (m *mockMsgService) Subscribe(_ context.Context, channel string, _ ...messaging.SubscribeOption) (*messaging.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.subscribeErr != nil {
		return nil, m.subscribeErr
	}
	if m.subscribeResult != nil {
		return m.subscribeResult, nil
	}
	return &messaging.Subscription{
		ID:        "sub-123",
		Channel:   channel,
		ClientID:  "client-1",
		CreatedAt: time.Now(),
	}, nil
}

func (m *mockMsgService) Unsubscribe(_ context.Context, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.unsubscribeErr
}

func (m *mockMsgService) Publish(_ context.Context, _ string, _ interface{}, _ ...messaging.PublishOption) (*messaging.PublishResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.publishErr != nil {
		return nil, m.publishErr
	}
	if m.publishResult != nil {
		return m.publishResult, nil
	}
	return &messaging.PublishResult{}, nil
}

func (m *mockMsgService) PublishDelta(_ context.Context, _ string, _, _ interface{}, _ ...messaging.PublishOption) (*messaging.PublishResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.publishDeltaErr != nil {
		return nil, m.publishDeltaErr
	}
	if m.publishDeltaResult != nil {
		return m.publishDeltaResult, nil
	}
	return &messaging.PublishResult{}, nil
}

func (m *mockMsgService) WatchChannel(_ context.Context, _ string, _ ...messaging.SubscribeOption) (*messaging.Subscription, error) {
	return nil, nil
}

func (m *mockMsgService) Start(_ context.Context) error { return nil }
func (m *mockMsgService) Stop(_ context.Context) error  { return nil }
func (m *mockMsgService) IsRunning() bool               { return true }

func (m *mockMsgService) GetStats() *messaging.Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statsResult
}

// =============================================================================
// NewHandler Tests
// =============================================================================

func TestNewHandler(t *testing.T) {
	t.Run("creates handler with valid inputs", func(t *testing.T) {
		svc := newMockMsgService()
		logger := newTestLogger()

		h, err := NewHandler(svc, logger)
		require.NoError(t, err)
		require.NotNil(t, h)
		assert.NotNil(t, h.handlers)
		assert.Equal(t, 10, len(h.handlers))
		assert.NoError(t, h.Close())
	})

	t.Run("returns error when service is nil", func(t *testing.T) {
		logger := newTestLogger()

		_, err := NewHandler(nil, logger)
		require.Error(t, err)

		var qbErr *dberrors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("returns error when logger is nil", func(t *testing.T) {
		svc := newMockMsgService()

		_, err := NewHandler(svc, nil)
		require.Error(t, err)

		var qbErr *dberrors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
	})
}

// =============================================================================
// Handle Tests
// =============================================================================

func TestHandler_Handle(t *testing.T) {
	t.Run("returns error for unknown opcode", func(t *testing.T) {
		h := createTestHandler(t)
		defer h.Close()

		_, err := h.Handle(context.Background(), 0xFF, nil)
		require.Error(t, err)

		var qbErr *dberrors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("returns error when handler is closed", func(t *testing.T) {
		h := createTestHandler(t)
		require.NoError(t, h.Close())

		_, err := h.Handle(context.Background(), OpPushGetStats, nil)
		require.Error(t, err)

		var qbErr *dberrors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, dberrors.ErrUnavailable, qbErr.Code)
	})
}

// =============================================================================
// HandleRequest Tests
// =============================================================================

func TestHandler_HandleRequest(t *testing.T) {
	t.Run("returns invalid request for nil request", func(t *testing.T) {
		h := createTestHandler(t)
		defer h.Close()

		resp := h.HandleRequest(context.Background(), nil)
		require.NotNil(t, resp)
		assert.Equal(t, StatusInvalidRequest, resp.Status)
	})

	t.Run("returns OK status for valid request", func(t *testing.T) {
		h := createTestHandler(t)
		defer h.Close()

		resp := h.HandleRequest(context.Background(), &Request{
			Opcode:  OpPushGetStats,
			Payload: nil,
		})
		require.NotNil(t, resp)
		assert.Equal(t, StatusOK, resp.Status)
	})

	t.Run("returns error status for failed request", func(t *testing.T) {
		h := createTestHandler(t)
		defer h.Close()

		resp := h.HandleRequest(context.Background(), &Request{
			Opcode:  0xFF, // unknown opcode
			Payload: nil,
		})
		require.NotNil(t, resp)
		assert.NotEqual(t, StatusOK, resp.Status)
	})
}

// =============================================================================
// Close Tests
// =============================================================================

func TestHandler_Close(t *testing.T) {
	t.Run("close returns nil on first call", func(t *testing.T) {
		h := createTestHandler(t)
		assert.NoError(t, h.Close())
	})

	t.Run("idempotent close returns nil", func(t *testing.T) {
		h := createTestHandler(t)
		assert.NoError(t, h.Close())
		assert.NoError(t, h.Close())
	})
}

// =============================================================================
// Service accessor Test
// =============================================================================

func TestHandler_Service(t *testing.T) {
	t.Run("returns the messaging service", func(t *testing.T) {
		svc := newMockMsgService()
		logger := newTestLogger()

		h, err := NewHandler(svc, logger)
		require.NoError(t, err)

		assert.Equal(t, svc, h.Service())
		assert.NoError(t, h.Close())
	})
}

// =============================================================================
// Channel Handler Tests
// =============================================================================

func TestHandler_CreateChannel(t *testing.T) {
	t.Run("creates channel successfully", func(t *testing.T) {
		h := createTestHandler(t)
		defer h.Close()

		reqPayload, err := EncodePayload(&CreateChannelRequest{
			Name:       "test-channel",
			Codec:      "json",
			BufferSize: 100,
			MaxSubs:    50,
			Persistent: true,
			MaxHistory: 1000,
			TTL:        time.Minute,
		})
		require.NoError(t, err)

		resp, err := h.Handle(context.Background(), OpPushCreateChannel, reqPayload)
		require.NoError(t, err)
		require.NotNil(t, resp)

		var result CreateChannelResponse
		require.NoError(t, DecodePayload(resp, &result))
		assert.Equal(t, "test-channel", result.Channel.Name)
	})

	t.Run("returns error when service fails", func(t *testing.T) {
		svc := newMockMsgService()
		svc.createChannelErr = &mockServiceError{Message: "creation failed"}
		h := createTestHandlerWithService(t, svc)
		defer h.Close()

		reqPayload, err := EncodePayload(&CreateChannelRequest{Name: "fail"})
		require.NoError(t, err)

		_, err = h.Handle(context.Background(), OpPushCreateChannel, reqPayload)
		require.Error(t, err)
	})

	t.Run("returns error for invalid payload", func(t *testing.T) {
		h := createTestHandler(t)
		defer h.Close()

		_, err := h.Handle(context.Background(), OpPushCreateChannel, []byte("invalid-json"))
		require.Error(t, err)
	})
}

func TestHandler_DeleteChannel(t *testing.T) {
	t.Run("deletes channel successfully", func(t *testing.T) {
		h := createTestHandler(t)
		defer h.Close()

		reqPayload, err := EncodePayload(&DeleteChannelRequest{Name: "ch-1"})
		require.NoError(t, err)

		resp, err := h.Handle(context.Background(), OpPushDeleteChannel, reqPayload)
		require.NoError(t, err)

		var result DeleteChannelResponse
		require.NoError(t, DecodePayload(resp, &result))
		assert.True(t, result.Success)
	})

	t.Run("returns error when service fails", func(t *testing.T) {
		svc := newMockMsgService()
		svc.deleteChannelErr = &mockServiceError{Message: "delete failed"}
		h := createTestHandlerWithService(t, svc)
		defer h.Close()

		reqPayload, err := EncodePayload(&DeleteChannelRequest{Name: "ch-1"})
		require.NoError(t, err)

		_, err = h.Handle(context.Background(), OpPushDeleteChannel, reqPayload)
		require.Error(t, err)
	})
}

func TestHandler_GetChannel(t *testing.T) {
	t.Run("gets channel successfully", func(t *testing.T) {
		h := createTestHandler(t)
		defer h.Close()

		reqPayload, err := EncodePayload(&GetChannelRequest{Name: "ch-1"})
		require.NoError(t, err)

		resp, err := h.Handle(context.Background(), OpPushGetChannel, reqPayload)
		require.NoError(t, err)

		var result GetChannelResponse
		require.NoError(t, DecodePayload(resp, &result))
		assert.Equal(t, "ch-1", result.Channel.Name)
	})

	t.Run("returns error when service fails", func(t *testing.T) {
		svc := newMockMsgService()
		svc.getChannelErr = &mockServiceError{Message: "get failed"}
		h := createTestHandlerWithService(t, svc)
		defer h.Close()

		reqPayload, err := EncodePayload(&GetChannelRequest{Name: "ch-1"})
		require.NoError(t, err)

		_, err = h.Handle(context.Background(), OpPushGetChannel, reqPayload)
		require.Error(t, err)
	})
}

func TestHandler_ListChannels(t *testing.T) {
	t.Run("lists channels successfully", func(t *testing.T) {
		svc := newMockMsgService()
		svc.listChannelsResult = []*messaging.Channel{
			{Name: "ch-1", CreatedAt: time.Now()},
			{Name: "ch-2", CreatedAt: time.Now()},
		}
		h := createTestHandlerWithService(t, svc)
		defer h.Close()

		resp, err := h.Handle(context.Background(), OpPushListChannels, nil)
		require.NoError(t, err)

		var result ListChannelsResponse
		require.NoError(t, DecodePayload(resp, &result))
		assert.Len(t, result.Channels, 2)
	})

	t.Run("returns error when service fails", func(t *testing.T) {
		svc := newMockMsgService()
		svc.listChannelsErr = &mockServiceError{Message: "list failed"}
		h := createTestHandlerWithService(t, svc)
		defer h.Close()

		_, err := h.Handle(context.Background(), OpPushListChannels, nil)
		require.Error(t, err)
	})
}

// =============================================================================
// Subscription Handler Tests
// =============================================================================

func TestHandler_Subscribe(t *testing.T) {
	t.Run("subscribes successfully with options", func(t *testing.T) {
		h := createTestHandler(t)
		defer h.Close()

		reqPayload, err := EncodePayload(&SubscribeRequest{
			Channel:    "test-ch",
			SessionID:  "session-1",
			BufferSize: 100,
			Filter: &SubscriptionFilter{
				EventTypes: []string{"PUT", "DELETE"},
				KeyPrefix:  "user:",
			},
		})
		require.NoError(t, err)

		resp, err := h.Handle(context.Background(), OpPushSubscribe, reqPayload)
		require.NoError(t, err)

		var result SubscribeResponse
		require.NoError(t, DecodePayload(resp, &result))
		assert.Equal(t, "test-ch", result.Channel)
	})

	t.Run("subscribes with nil filter", func(t *testing.T) {
		h := createTestHandler(t)
		defer h.Close()

		reqPayload, err := EncodePayload(&SubscribeRequest{
			Channel: "basic-ch",
		})
		require.NoError(t, err)

		resp, err := h.Handle(context.Background(), OpPushSubscribe, reqPayload)
		require.NoError(t, err)

		var result SubscribeResponse
		require.NoError(t, DecodePayload(resp, &result))
		assert.Equal(t, "basic-ch", result.Channel)
	})

	t.Run("returns error when service fails", func(t *testing.T) {
		svc := newMockMsgService()
		svc.subscribeErr = &mockServiceError{Message: "subscribe failed"}
		h := createTestHandlerWithService(t, svc)
		defer h.Close()

		reqPayload, err := EncodePayload(&SubscribeRequest{Channel: "ch"})
		require.NoError(t, err)

		_, err = h.Handle(context.Background(), OpPushSubscribe, reqPayload)
		require.Error(t, err)
	})
}

func TestHandler_Unsubscribe(t *testing.T) {
	t.Run("unsubscribes successfully", func(t *testing.T) {
		h := createTestHandler(t)
		defer h.Close()

		reqPayload, err := EncodePayload(&UnsubscribeRequest{
			SubscriptionID: "sub-123",
		})
		require.NoError(t, err)

		resp, err := h.Handle(context.Background(), OpPushUnsubscribe, reqPayload)
		require.NoError(t, err)

		var result UnsubscribeResponse
		require.NoError(t, DecodePayload(resp, &result))
		assert.True(t, result.Success)
	})

	t.Run("returns error when service fails", func(t *testing.T) {
		svc := newMockMsgService()
		svc.unsubscribeErr = &mockServiceError{Message: "unsubscribe failed"}
		h := createTestHandlerWithService(t, svc)
		defer h.Close()

		reqPayload, err := EncodePayload(&UnsubscribeRequest{SubscriptionID: "sub-123"})
		require.NoError(t, err)

		_, err = h.Handle(context.Background(), OpPushUnsubscribe, reqPayload)
		require.Error(t, err)
	})
}

// =============================================================================
// Publish Handler Tests
// =============================================================================

func TestHandler_Publish(t *testing.T) {
	t.Run("publishes message successfully with all options", func(t *testing.T) {
		svc := newMockMsgService()
		svc.publishResult = &messaging.PublishResult{MessageID: "msg-1"}
		h := createTestHandlerWithService(t, svc)
		defer h.Close()

		payloadData, _ := json.Marshal(map[string]string{"key": "value"})
		reqPayload, err := EncodePayload(&PublishRequest{
			Channel:   "pub-ch",
			Payload:   payloadData,
			MessageID: "msg-1",
			DedupKey:  "dedup-1",
			Metadata:  map[string]string{"source": "test"},
		})
		require.NoError(t, err)

		resp, err := h.Handle(context.Background(), OpPushPublish, reqPayload)
		require.NoError(t, err)

		var result PublishResponse
		require.NoError(t, DecodePayload(resp, &result))
		assert.True(t, result.Success)
		assert.Equal(t, "msg-1", result.MessageID)
	})

	t.Run("returns error when service fails", func(t *testing.T) {
		svc := newMockMsgService()
		svc.publishErr = &mockServiceError{Message: "publish failed"}
		h := createTestHandlerWithService(t, svc)
		defer h.Close()

		reqPayload, err := EncodePayload(&PublishRequest{
			Channel: "ch",
			Payload: json.RawMessage(`{}`),
		})
		require.NoError(t, err)

		_, err = h.Handle(context.Background(), OpPushPublish, reqPayload)
		require.Error(t, err)
	})

	t.Run("returns error for invalid payload", func(t *testing.T) {
		h := createTestHandler(t)
		defer h.Close()

		_, err := h.Handle(context.Background(), OpPushPublish, []byte("bad-json"))
		require.Error(t, err)
	})
}

func TestHandler_PublishDelta(t *testing.T) {
	t.Run("publishes delta successfully with all options", func(t *testing.T) {
		svc := newMockMsgService()
		svc.publishDeltaResult = &messaging.PublishResult{MessageID: "delta-msg-1"}
		h := createTestHandlerWithService(t, svc)
		defer h.Close()

		reqPayload, err := EncodePayload(&PublishDeltaRequest{
			Channel:   "delta-ch",
			OldValue:  json.RawMessage(`{"a":1}`),
			NewValue:  json.RawMessage(`{"a":2}`),
			MessageID: "delta-msg-1",
			DedupKey:  "delta-dedup-1",
			Metadata:  map[string]string{"type": "delta"},
		})
		require.NoError(t, err)

		resp, err := h.Handle(context.Background(), OpPushPublishDelta, reqPayload)
		require.NoError(t, err)

		var result PublishDeltaResponse
		require.NoError(t, DecodePayload(resp, &result))
		assert.True(t, result.Success)
		assert.Equal(t, "delta-msg-1", result.MessageID)
	})

	t.Run("publishes delta with empty old and new values", func(t *testing.T) {
		h := createTestHandler(t)
		defer h.Close()

		reqPayload, err := EncodePayload(&PublishDeltaRequest{
			Channel: "delta-ch",
		})
		require.NoError(t, err)

		resp, err := h.Handle(context.Background(), OpPushPublishDelta, reqPayload)
		require.NoError(t, err)

		var result PublishDeltaResponse
		require.NoError(t, DecodePayload(resp, &result))
		assert.True(t, result.Success)
	})

	t.Run("returns error when service fails", func(t *testing.T) {
		svc := newMockMsgService()
		svc.publishDeltaErr = &mockServiceError{Message: "delta failed"}
		h := createTestHandlerWithService(t, svc)
		defer h.Close()

		reqPayload, err := EncodePayload(&PublishDeltaRequest{
			Channel:  "ch",
			OldValue: json.RawMessage(`{}`),
			NewValue: json.RawMessage(`{}`),
		})
		require.NoError(t, err)

		_, err = h.Handle(context.Background(), OpPushPublishDelta, reqPayload)
		require.Error(t, err)
	})

	t.Run("returns error for invalid old value JSON", func(t *testing.T) {
		h := createTestHandler(t)
		defer h.Close()

		// Using json.RawMessage with invalid JSON will cause EncodePayload to fail
		// because json.Marshal validates RawMessage content
		_, err := EncodePayload(&PublishDeltaRequest{
			Channel:  "ch",
			OldValue: json.RawMessage(`not-json`),
			NewValue: json.RawMessage(`{}`),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to encode payload")
	})

	t.Run("returns error for invalid new value JSON", func(t *testing.T) {
		h := createTestHandler(t)
		defer h.Close()

		// Using json.RawMessage with invalid JSON will cause EncodePayload to fail
		// because json.Marshal validates RawMessage content
		_, err := EncodePayload(&PublishDeltaRequest{
			Channel:  "ch",
			OldValue: json.RawMessage(`{}`),
			NewValue: json.RawMessage(`not-json`),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to encode payload")
	})
}

// =============================================================================
// Stats Handler Tests
// =============================================================================

func TestHandler_GetStats(t *testing.T) {
	t.Run("gets stats successfully", func(t *testing.T) {
		h := createTestHandler(t)
		defer h.Close()

		resp, err := h.Handle(context.Background(), OpPushGetStats, nil)
		require.NoError(t, err)

		var result GetStatsResponse
		require.NoError(t, DecodePayload(resp, &result))
		assert.NotNil(t, result.Stats)
		assert.Equal(t, int64(5), result.Stats.TotalChannels)
		assert.Equal(t, int64(10), result.Stats.TotalSubscriptions)
	})
}

func TestHandler_DedupStats(t *testing.T) {
	t.Run("returns error when service is not PushService", func(t *testing.T) {
		h := createTestHandler(t)
		defer h.Close()

		reqPayload, err := EncodePayload(&DedupStatsRequest{Channel: "ch"})
		require.NoError(t, err)

		_, err = h.Handle(context.Background(), OpPushDedupStats, reqPayload)
		require.Error(t, err)

		var qbErr *dberrors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, dberrors.ErrInternal, qbErr.Code)
	})

	t.Run("returns error for invalid payload", func(t *testing.T) {
		h := createTestHandler(t)
		defer h.Close()

		_, err := h.Handle(context.Background(), OpPushDedupStats, []byte("bad"))
		require.Error(t, err)
	})
}

// =============================================================================
// Test Helpers
// =============================================================================

func createTestHandler(t *testing.T) *Handler {
	t.Helper()
	svc := newMockMsgService()
	logger := newTestLogger()
	h, err := NewHandler(svc, logger)
	require.NoError(t, err)
	return h
}

func createTestHandlerWithService(t *testing.T, svc messaging.Service) *Handler {
	t.Helper()
	logger := newTestLogger()
	h, err := NewHandler(svc, logger)
	require.NoError(t, err)
	return h
}
