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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// mockMessagingService implements messaging.Service for testing.
type mockMessagingService struct {
	createChannelFunc func(ctx context.Context, name string, opts ...messaging.CreateChannelOption) (*messaging.Channel, error)
	deleteChannelFunc func(ctx context.Context, name string) error
	getChannelFunc    func(ctx context.Context, name string) (*messaging.Channel, error)
	listChannelsFunc  func(ctx context.Context) ([]*messaging.Channel, error)
	subscribeFunc     func(ctx context.Context, channel string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error)
	unsubscribeFunc   func(ctx context.Context, subscriptionID string) error
	publishFunc       func(ctx context.Context, channel string, payload interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error)
	publishDeltaFunc  func(ctx context.Context, channel string, old, new interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error)
	watchChannelFunc  func(ctx context.Context, keyPrefix string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error)
	startFunc         func(ctx context.Context) error
	stopFunc          func(ctx context.Context) error
	isRunningFunc     func() bool
	getStatsFunc      func() *messaging.Stats
}

func (m *mockMessagingService) CreateChannel(ctx context.Context, name string, opts ...messaging.CreateChannelOption) (*messaging.Channel, error) {
	if m.createChannelFunc != nil {
		return m.createChannelFunc(ctx, name, opts...)
	}
	return nil, nil
}

func (m *mockMessagingService) DeleteChannel(ctx context.Context, name string) error {
	if m.deleteChannelFunc != nil {
		return m.deleteChannelFunc(ctx, name)
	}
	return nil
}

func (m *mockMessagingService) GetChannel(ctx context.Context, name string) (*messaging.Channel, error) {
	if m.getChannelFunc != nil {
		return m.getChannelFunc(ctx, name)
	}
	return nil, nil
}

func (m *mockMessagingService) ListChannels(ctx context.Context) ([]*messaging.Channel, error) {
	if m.listChannelsFunc != nil {
		return m.listChannelsFunc(ctx)
	}
	return nil, nil
}

func (m *mockMessagingService) Subscribe(ctx context.Context, channel string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
	if m.subscribeFunc != nil {
		return m.subscribeFunc(ctx, channel, opts...)
	}
	return nil, nil
}

func (m *mockMessagingService) Unsubscribe(ctx context.Context, subscriptionID string) error {
	if m.unsubscribeFunc != nil {
		return m.unsubscribeFunc(ctx, subscriptionID)
	}
	return nil
}

func (m *mockMessagingService) Publish(ctx context.Context, channel string, payload interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
	if m.publishFunc != nil {
		return m.publishFunc(ctx, channel, payload, opts...)
	}
	return &messaging.PublishResult{MessageID: "mock-id"}, nil
}

func (m *mockMessagingService) PublishDelta(ctx context.Context, channel string, old, new interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
	if m.publishDeltaFunc != nil {
		return m.publishDeltaFunc(ctx, channel, old, new, opts...)
	}
	return &messaging.PublishResult{MessageID: "mock-id"}, nil
}

func (m *mockMessagingService) WatchChannel(ctx context.Context, keyPrefix string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
	if m.watchChannelFunc != nil {
		return m.watchChannelFunc(ctx, keyPrefix, opts...)
	}
	return nil, nil
}

func (m *mockMessagingService) Start(ctx context.Context) error {
	if m.startFunc != nil {
		return m.startFunc(ctx)
	}
	return nil
}

func (m *mockMessagingService) Stop(ctx context.Context) error {
	if m.stopFunc != nil {
		return m.stopFunc(ctx)
	}
	return nil
}

func (m *mockMessagingService) IsRunning() bool {
	if m.isRunningFunc != nil {
		return m.isRunningFunc()
	}
	return true
}

func (m *mockMessagingService) GetStats() *messaging.Stats {
	if m.getStatsFunc != nil {
		return m.getStatsFunc()
	}
	return &messaging.Stats{}
}

// newTestMessagingServer creates a MessagingServer for testing.
func newTestMessagingServer(mockSvc *mockMessagingService) *MessagingServer {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	return NewMessagingServer(mockSvc, log)
}

// =============================================================================
// CreateChannel Tests
// =============================================================================

func TestMessagingServer_CreateChannel(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.MessagingCreateChannelRequest
		mockFunc    func(ctx context.Context, name string, opts ...messaging.CreateChannelOption) (*messaging.Channel, error)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.MessagingCreateChannelResponse)
	}{
		{
			name: "successful_create_channel",
			request: &pb.MessagingCreateChannelRequest{
				Name:       "test-channel",
				Codec:      "json",
				Persistent: true,
				MaxHistory: 100,
				BufferSize: 256,
			},
			mockFunc: func(ctx context.Context, name string, opts ...messaging.CreateChannelOption) (*messaging.Channel, error) {
				return &messaging.Channel{
					Name:        name,
					Codec:       "json",
					CreatedAt:   now,
					Subscribers: 0,
					Persistent:  true,
					MaxHistory:  100,
					BufferSize:  256,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingCreateChannelResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Channel)
				assert.Equal(t, "test-channel", resp.Channel.Name)
				assert.Equal(t, "json", resp.Channel.Codec)
				assert.True(t, resp.Channel.Persistent)
				assert.Equal(t, int32(100), resp.Channel.MaxHistory)
			},
		},
		{
			name: "create_channel_minimal",
			request: &pb.MessagingCreateChannelRequest{
				Name: "minimal-channel",
			},
			mockFunc: func(ctx context.Context, name string, opts ...messaging.CreateChannelOption) (*messaging.Channel, error) {
				return &messaging.Channel{
					Name:      name,
					CreatedAt: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingCreateChannelResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Channel)
				assert.Equal(t, "minimal-channel", resp.Channel.Name)
			},
		},
		{
			name: "missing_name",
			request: &pb.MessagingCreateChannelRequest{
				Name: "",
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "channel_already_exists",
			request: &pb.MessagingCreateChannelRequest{
				Name: "existing-channel",
			},
			mockFunc: func(ctx context.Context, name string, opts ...messaging.CreateChannelOption) (*messaging.Channel, error) {
				return nil, messaging.NewChannelExistsError("CreateChannel", name)
			},
			expectError: true,
			errorCode:   codes.AlreadyExists,
		},
		{
			name: "invalid_channel_name",
			request: &pb.MessagingCreateChannelRequest{
				Name: "invalid channel name with spaces",
			},
			mockFunc: func(ctx context.Context, name string, opts ...messaging.CreateChannelOption) (*messaging.Channel, error) {
				return nil, messaging.NewInvalidChannelNameError("CreateChannel", name, "invalid characters")
			},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_not_running",
			request: &pb.MessagingCreateChannelRequest{
				Name: "test-channel",
			},
			mockFunc: func(ctx context.Context, name string, opts ...messaging.CreateChannelOption) (*messaging.Channel, error) {
				return nil, messaging.NewServiceNotRunningError("CreateChannel", "create channel")
			},
			expectError: true,
			errorCode:   codes.Unavailable,
		},
		{
			name: "with_ttl",
			request: &pb.MessagingCreateChannelRequest{
				Name:       "ttl-channel",
				TtlSeconds: 3600,
			},
			mockFunc: func(ctx context.Context, name string, opts ...messaging.CreateChannelOption) (*messaging.Channel, error) {
				return &messaging.Channel{
					Name:      name,
					CreatedAt: now,
					TTL:       time.Hour,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingCreateChannelResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Channel)
				assert.Equal(t, int64(3600), resp.Channel.TtlSeconds)
			},
		},
		{
			name: "with_max_subscribers",
			request: &pb.MessagingCreateChannelRequest{
				Name:           "limited-channel",
				MaxSubscribers: 10,
			},
			mockFunc: func(ctx context.Context, name string, opts ...messaging.CreateChannelOption) (*messaging.Channel, error) {
				return &messaging.Channel{
					Name:      name,
					CreatedAt: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingCreateChannelResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Channel)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockMessagingService{createChannelFunc: tc.mockFunc}
			server := newTestMessagingServer(mockSvc)

			resp, err := server.CreateChannel(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// DeleteChannel Tests
// =============================================================================

func TestMessagingServer_DeleteChannel(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.MessagingDeleteChannelRequest
		mockFunc    func(ctx context.Context, name string) error
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.MessagingDeleteChannelResponse)
	}{
		{
			name: "successful_delete_channel",
			request: &pb.MessagingDeleteChannelRequest{
				Name: "test-channel",
			},
			mockFunc: func(ctx context.Context, name string) error {
				return nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingDeleteChannelResponse) {
				require.NotNil(t, resp)
				assert.Equal(t, "test-channel", resp.Name)
				assert.True(t, resp.Deleted)
			},
		},
		{
			name: "missing_name",
			request: &pb.MessagingDeleteChannelRequest{
				Name: "",
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "channel_not_found",
			request: &pb.MessagingDeleteChannelRequest{
				Name: "nonexistent-channel",
			},
			mockFunc: func(ctx context.Context, name string) error {
				return messaging.NewChannelNotFoundError("DeleteChannel", name)
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
		{
			name: "service_not_running",
			request: &pb.MessagingDeleteChannelRequest{
				Name: "test-channel",
			},
			mockFunc: func(ctx context.Context, name string) error {
				return messaging.NewServiceNotRunningError("DeleteChannel", "delete channel")
			},
			expectError: true,
			errorCode:   codes.Unavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockMessagingService{deleteChannelFunc: tc.mockFunc}
			server := newTestMessagingServer(mockSvc)

			resp, err := server.DeleteChannel(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// GetChannel Tests
// =============================================================================

func TestMessagingServer_GetChannel(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.MessagingGetChannelRequest
		mockFunc    func(ctx context.Context, name string) (*messaging.Channel, error)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.MessagingGetChannelResponse)
	}{
		{
			name: "successful_get_channel",
			request: &pb.MessagingGetChannelRequest{
				Name: "test-channel",
			},
			mockFunc: func(ctx context.Context, name string) (*messaging.Channel, error) {
				return &messaging.Channel{
					Name:        name,
					Codec:       "json",
					CreatedAt:   now,
					Subscribers: 5,
					Persistent:  true,
					MaxHistory:  100,
					BufferSize:  256,
					TTL:         time.Hour,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingGetChannelResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Channel)
				assert.Equal(t, "test-channel", resp.Channel.Name)
				assert.Equal(t, "json", resp.Channel.Codec)
				assert.Equal(t, int64(5), resp.Channel.Subscribers)
				assert.True(t, resp.Channel.Persistent)
			},
		},
		{
			name: "missing_name",
			request: &pb.MessagingGetChannelRequest{
				Name: "",
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "channel_not_found",
			request: &pb.MessagingGetChannelRequest{
				Name: "nonexistent-channel",
			},
			mockFunc: func(ctx context.Context, name string) (*messaging.Channel, error) {
				return nil, messaging.NewChannelNotFoundError("GetChannel", name)
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockMessagingService{getChannelFunc: tc.mockFunc}
			server := newTestMessagingServer(mockSvc)

			resp, err := server.GetChannel(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// ListChannels Tests
// =============================================================================

func TestMessagingServer_ListChannels(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.MessagingListChannelsRequest
		mockFunc    func(ctx context.Context) ([]*messaging.Channel, error)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.MessagingListChannelsResponse)
	}{
		{
			name:    "successful_list_channels",
			request: &pb.MessagingListChannelsRequest{},
			mockFunc: func(ctx context.Context) ([]*messaging.Channel, error) {
				return []*messaging.Channel{
					{Name: "channel-1", CreatedAt: now, Subscribers: 5},
					{Name: "channel-2", CreatedAt: now, Subscribers: 10},
					{Name: "channel-3", CreatedAt: now, Subscribers: 0},
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingListChannelsResponse) {
				require.NotNil(t, resp)
				assert.Len(t, resp.Channels, 3)
				assert.Equal(t, int32(3), resp.TotalCount)
				assert.False(t, resp.HasMore)
			},
		},
		{
			name:    "empty_list",
			request: &pb.MessagingListChannelsRequest{},
			mockFunc: func(ctx context.Context) ([]*messaging.Channel, error) {
				return []*messaging.Channel{}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingListChannelsResponse) {
				require.NotNil(t, resp)
				assert.Empty(t, resp.Channels)
				assert.Equal(t, int32(0), resp.TotalCount)
			},
		},
		{
			name:    "name_prefix_filters",
			request: &pb.MessagingListChannelsRequest{NamePrefix: "events."},
			mockFunc: func(ctx context.Context) ([]*messaging.Channel, error) {
				return []*messaging.Channel{
					{Name: "events.orders"},
					{Name: "events.payments"},
					{Name: "logs.system"},
				}, nil
			},
			validate: func(t *testing.T, resp *pb.MessagingListChannelsResponse) {
				require.NotNil(t, resp)
				assert.Len(t, resp.Channels, 2)
				assert.Equal(t, int32(2), resp.TotalCount)
			},
		},
		{
			name:    "limit_truncates",
			request: &pb.MessagingListChannelsRequest{Limit: 2},
			mockFunc: func(ctx context.Context) ([]*messaging.Channel, error) {
				return []*messaging.Channel{
					{Name: "ch1"}, {Name: "ch2"}, {Name: "ch3"}, {Name: "ch4"},
				}, nil
			},
			validate: func(t *testing.T, resp *pb.MessagingListChannelsResponse) {
				require.NotNil(t, resp)
				assert.Len(t, resp.Channels, 2)
				assert.Equal(t, int32(2), resp.TotalCount)
			},
		},
		{
			name:    "prefix_then_limit",
			request: &pb.MessagingListChannelsRequest{NamePrefix: "events.", Limit: 2},
			mockFunc: func(ctx context.Context) ([]*messaging.Channel, error) {
				return []*messaging.Channel{
					{Name: "events.a"}, {Name: "events.b"}, {Name: "events.c"}, {Name: "logs.system"},
				}, nil
			},
			validate: func(t *testing.T, resp *pb.MessagingListChannelsResponse) {
				require.NotNil(t, resp)
				assert.Len(t, resp.Channels, 2)
			},
		},
		{
			name:    "nil_request_lists_all",
			request: nil,
			mockFunc: func(ctx context.Context) ([]*messaging.Channel, error) {
				return []*messaging.Channel{{Name: "ch1"}}, nil
			},
			validate: func(t *testing.T, resp *pb.MessagingListChannelsResponse) {
				require.NotNil(t, resp)
				assert.Len(t, resp.Channels, 1)
			},
		},
		{
			name:    "service_not_running",
			request: &pb.MessagingListChannelsRequest{},
			mockFunc: func(ctx context.Context) ([]*messaging.Channel, error) {
				return nil, messaging.NewServiceNotRunningError("ListChannels", "list channels")
			},
			expectError: true,
			errorCode:   codes.Unavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockMessagingService{listChannelsFunc: tc.mockFunc}
			server := newTestMessagingServer(mockSvc)

			resp, err := server.ListChannels(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// Subscribe Tests
// =============================================================================

func TestMessagingServer_Subscribe(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.MessagingSubscribeRequest
		mockFunc    func(ctx context.Context, channel string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.MessagingSubscribeResponse)
	}{
		{
			name: "successful_subscribe",
			request: &pb.MessagingSubscribeRequest{
				Channel:   "test-channel",
				SessionId: "session-123",
			},
			mockFunc: func(ctx context.Context, channel string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
				return &messaging.Subscription{
					ID:        "sub-1",
					Channel:   channel,
					ClientID:  "session-123",
					CreatedAt: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingSubscribeResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Subscription)
				assert.Equal(t, "sub-1", resp.Subscription.Id)
				assert.Equal(t, "test-channel", resp.Subscription.Channel)
				assert.Equal(t, "session-123", resp.Subscription.ClientId)
			},
		},
		{
			name: "subscribe_with_filter",
			request: &pb.MessagingSubscribeRequest{
				Channel: "test-channel",
				Filter: &pb.SubscriptionFilter{
					EventTypes: []pb.EventType{pb.EventType_EVENT_TYPE_PUT},
					KeyPrefix:  "user:",
				},
			},
			mockFunc: func(ctx context.Context, channel string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
				return &messaging.Subscription{
					ID:        "sub-2",
					Channel:   channel,
					CreatedAt: now,
					Filters: &messaging.SubscriptionFilter{
						EventTypes: []messaging.EventType{messaging.EventTypePut},
						KeyPrefix:  "user:",
					},
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingSubscribeResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Subscription)
				require.NotNil(t, resp.Subscription.Filters)
				assert.Equal(t, "user:", resp.Subscription.Filters.KeyPrefix)
			},
		},
		{
			name: "subscribe_with_options",
			request: &pb.MessagingSubscribeRequest{
				Channel:             "test-channel",
				DeltaMode:           true,
				IncludeOldValues:    true,
				DedupMetadata:       true,
				DedupSkipDuplicates: true,
				BufferSize:          512,
			},
			mockFunc: func(ctx context.Context, channel string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
				return &messaging.Subscription{
					ID:        "sub-3",
					Channel:   channel,
					CreatedAt: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingSubscribeResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Subscription)
			},
		},
		{
			name: "missing_channel",
			request: &pb.MessagingSubscribeRequest{
				Channel: "",
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "channel_not_found",
			request: &pb.MessagingSubscribeRequest{
				Channel: "nonexistent-channel",
			},
			mockFunc: func(ctx context.Context, channel string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
				return nil, messaging.NewChannelNotFoundError("Subscribe", channel)
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
		{
			name: "max_subscriptions_reached",
			request: &pb.MessagingSubscribeRequest{
				Channel: "full-channel",
			},
			mockFunc: func(ctx context.Context, channel string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
				return nil, messaging.NewMaxSubscriptionsReachedError("Subscribe", channel, 100)
			},
			expectError: true,
			errorCode:   codes.ResourceExhausted,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockMessagingService{subscribeFunc: tc.mockFunc}
			server := newTestMessagingServer(mockSvc)

			resp, err := server.Subscribe(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// Unsubscribe Tests
// =============================================================================

func TestMessagingServer_Unsubscribe(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.MessagingUnsubscribeRequest
		mockFunc    func(ctx context.Context, subscriptionID string) error
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.MessagingUnsubscribeResponse)
	}{
		{
			name: "successful_unsubscribe",
			request: &pb.MessagingUnsubscribeRequest{
				SubscriptionId: "sub-1",
			},
			mockFunc: func(ctx context.Context, subscriptionID string) error {
				return nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingUnsubscribeResponse) {
				require.NotNil(t, resp)
				assert.Equal(t, "sub-1", resp.SubscriptionId)
				assert.True(t, resp.Unsubscribed)
			},
		},
		{
			name: "missing_subscription_id",
			request: &pb.MessagingUnsubscribeRequest{
				SubscriptionId: "",
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "subscription_not_found",
			request: &pb.MessagingUnsubscribeRequest{
				SubscriptionId: "nonexistent-sub",
			},
			mockFunc: func(ctx context.Context, subscriptionID string) error {
				return messaging.NewSubscriptionNotFoundError("Unsubscribe", subscriptionID)
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockMessagingService{unsubscribeFunc: tc.mockFunc}
			server := newTestMessagingServer(mockSvc)

			resp, err := server.Unsubscribe(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// Publish Tests
// =============================================================================

func TestMessagingServer_Publish(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.MessagingPublishRequest
		mockFunc    func(ctx context.Context, channel string, payload interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.MessagingPublishResponse)
	}{
		{
			name: "successful_publish",
			request: &pb.MessagingPublishRequest{
				Channel:   "test-channel",
				Payload:   []byte(`{"key": "value"}`),
				MessageId: "msg-1",
			},
			mockFunc: func(ctx context.Context, channel string, payload interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
				return &messaging.PublishResult{MessageID: "msg-1"}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingPublishResponse) {
				require.NotNil(t, resp)
				assert.Equal(t, "msg-1", resp.MessageId)
				assert.Equal(t, "test-channel", resp.Channel)
				assert.False(t, resp.IsDuplicate)
			},
		},
		{
			name: "publish_with_metadata",
			request: &pb.MessagingPublishRequest{
				Channel: "test-channel",
				Payload: []byte("test payload"),
				Metadata: map[string]string{
					"source": "test",
					"type":   "event",
				},
			},
			mockFunc: func(ctx context.Context, channel string, payload interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
				return &messaging.PublishResult{MessageID: "mock-id"}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingPublishResponse) {
				require.NotNil(t, resp)
				assert.Equal(t, "test-channel", resp.Channel)
			},
		},
		{
			name: "publish_with_dedup_key",
			request: &pb.MessagingPublishRequest{
				Channel:  "test-channel",
				Payload:  []byte("test payload"),
				DedupKey: "dedup-123",
			},
			mockFunc: func(ctx context.Context, channel string, payload interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
				return &messaging.PublishResult{MessageID: "mock-id"}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingPublishResponse) {
				require.NotNil(t, resp)
			},
		},
		{
			name: "missing_channel",
			request: &pb.MessagingPublishRequest{
				Channel: "",
				Payload: []byte("test payload"),
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "channel_not_found",
			request: &pb.MessagingPublishRequest{
				Channel: "nonexistent-channel",
				Payload: []byte("test payload"),
			},
			mockFunc: func(ctx context.Context, channel string, payload interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
				return nil, messaging.NewChannelNotFoundError("Publish", channel)
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
		{
			name: "hub_buffer_full",
			request: &pb.MessagingPublishRequest{
				Channel: "full-channel",
				Payload: []byte("test payload"),
			},
			mockFunc: func(ctx context.Context, channel string, payload interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
				return nil, messaging.NewHubBufferFullError("Publish", channel, 1024)
			},
			expectError: true,
			errorCode:   codes.ResourceExhausted,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockMessagingService{publishFunc: tc.mockFunc}
			server := newTestMessagingServer(mockSvc)

			resp, err := server.Publish(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// PublishDelta Tests
// =============================================================================

func TestMessagingServer_PublishDelta(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.MessagingPublishDeltaRequest
		mockFunc    func(ctx context.Context, channel string, old, new interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.MessagingPublishDeltaResponse)
	}{
		{
			name: "successful_publish_delta",
			request: &pb.MessagingPublishDeltaRequest{
				Channel:   "test-channel",
				OldValue:  []byte(`{"name": "old"}`),
				NewValue:  []byte(`{"name": "new"}`),
				MessageId: "delta-msg-1",
			},
			mockFunc: func(ctx context.Context, channel string, old, new interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
				return &messaging.PublishResult{MessageID: "delta-msg-1"}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingPublishDeltaResponse) {
				require.NotNil(t, resp)
				assert.Equal(t, "delta-msg-1", resp.MessageId)
				assert.Equal(t, "test-channel", resp.Channel)
			},
		},
		{
			name: "missing_channel",
			request: &pb.MessagingPublishDeltaRequest{
				Channel:  "",
				OldValue: []byte(`{}`),
				NewValue: []byte(`{}`),
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "channel_not_found",
			request: &pb.MessagingPublishDeltaRequest{
				Channel:  "nonexistent-channel",
				OldValue: []byte(`{}`),
				NewValue: []byte(`{}`),
			},
			mockFunc: func(ctx context.Context, channel string, old, new interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
				return nil, messaging.NewChannelNotFoundError("PublishDelta", channel)
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockMessagingService{publishDeltaFunc: tc.mockFunc}
			server := newTestMessagingServer(mockSvc)

			resp, err := server.PublishDelta(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// WatchChannel Tests
// =============================================================================

func TestMessagingServer_WatchChannel(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.MessagingWatchChannelRequest
		mockFunc    func(ctx context.Context, keyPrefix string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.MessagingWatchChannelResponse)
	}{
		{
			name: "successful_watch_channel",
			request: &pb.MessagingWatchChannelRequest{
				KeyPrefix: "user:",
				SessionId: "session-123",
			},
			mockFunc: func(ctx context.Context, keyPrefix string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
				return &messaging.Subscription{
					ID:        "sub-watch-1",
					Channel:   "watch:user:",
					ClientID:  "session-123",
					CreatedAt: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingWatchChannelResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Subscription)
				assert.Equal(t, "sub-watch-1", resp.Subscription.Id)
				assert.Equal(t, "watch:user:", resp.ChannelName)
			},
		},
		{
			name: "watch_with_filter",
			request: &pb.MessagingWatchChannelRequest{
				KeyPrefix: "data:",
				Filter: &pb.SubscriptionFilter{
					EventTypes: []pb.EventType{pb.EventType_EVENT_TYPE_DELETE},
				},
				BufferSize: 128,
			},
			mockFunc: func(ctx context.Context, keyPrefix string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
				return &messaging.Subscription{
					ID:        "sub-watch-2",
					Channel:   "watch:data:",
					CreatedAt: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingWatchChannelResponse) {
				require.NotNil(t, resp)
				assert.Equal(t, "watch:data:", resp.ChannelName)
			},
		},
		{
			name: "missing_key_prefix",
			request: &pb.MessagingWatchChannelRequest{
				KeyPrefix: "",
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_not_running",
			request: &pb.MessagingWatchChannelRequest{
				KeyPrefix: "test:",
			},
			mockFunc: func(ctx context.Context, keyPrefix string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
				return nil, messaging.NewServiceNotRunningError("WatchChannel", "watch channel")
			},
			expectError: true,
			errorCode:   codes.Unavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockMessagingService{watchChannelFunc: tc.mockFunc}
			server := newTestMessagingServer(mockSvc)

			resp, err := server.WatchChannel(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// GetStats Tests
// =============================================================================

func TestMessagingServer_GetStats(t *testing.T) {
	tests := []struct {
		name        string
		mockFunc    func() *messaging.Stats
		isRunning   bool
		expectError bool
		validate    func(t *testing.T, resp *pb.MessagingGetStatsResponse)
	}{
		{
			name: "successful_get_stats",
			mockFunc: func() *messaging.Stats {
				return &messaging.Stats{
					TotalChannels:      10,
					TotalSubscriptions: 50,
					MessagesPublished:  1000,
					MessagesDelivered:  950,
					DedupHits:          25,
					DroppedMessages:    5,
					BufferUtilization:  0.75,
				}
			},
			isRunning:   true,
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingGetStatsResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Stats)
				assert.Equal(t, int64(10), resp.Stats.TotalChannels)
				assert.Equal(t, int64(50), resp.Stats.TotalSubscriptions)
				assert.Equal(t, int64(1000), resp.Stats.MessagesPublished)
				assert.Equal(t, int64(950), resp.Stats.MessagesDelivered)
				assert.True(t, resp.IsRunning)
			},
		},
		{
			name: "service_not_running",
			mockFunc: func() *messaging.Stats {
				return &messaging.Stats{}
			},
			isRunning:   false,
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingGetStatsResponse) {
				require.NotNil(t, resp)
				assert.False(t, resp.IsRunning)
			},
		},
		{
			name: "nil_stats",
			mockFunc: func() *messaging.Stats {
				return nil
			},
			isRunning:   true,
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingGetStatsResponse) {
				require.NotNil(t, resp)
				assert.Nil(t, resp.Stats)
				assert.True(t, resp.IsRunning)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockMessagingService{
				getStatsFunc:  tc.mockFunc,
				isRunningFunc: func() bool { return tc.isRunning },
			}
			server := newTestMessagingServer(mockSvc)

			resp, err := server.GetStats(context.Background(), &pb.MessagingGetStatsRequest{})

			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// GetChannelStats Tests
// =============================================================================

func TestMessagingServer_GetChannelStats(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.MessagingGetChannelStatsRequest
		mockFunc    func(ctx context.Context, name string) (*messaging.Channel, error)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.MessagingGetChannelStatsResponse)
	}{
		{
			name: "successful_get_channel_stats",
			request: &pb.MessagingGetChannelStatsRequest{
				Name: "test-channel",
			},
			mockFunc: func(ctx context.Context, name string) (*messaging.Channel, error) {
				return &messaging.Channel{
					Name:        name,
					CreatedAt:   now,
					Subscribers: 15,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.MessagingGetChannelStatsResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Stats)
				assert.Equal(t, "test-channel", resp.Stats.Name)
				assert.Equal(t, int64(15), resp.Stats.Subscribers)
			},
		},
		{
			name: "missing_name",
			request: &pb.MessagingGetChannelStatsRequest{
				Name: "",
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "channel_not_found",
			request: &pb.MessagingGetChannelStatsRequest{
				Name: "nonexistent-channel",
			},
			mockFunc: func(ctx context.Context, name string) (*messaging.Channel, error) {
				return nil, messaging.NewChannelNotFoundError("GetChannelStats", name)
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockMessagingService{getChannelFunc: tc.mockFunc}
			server := newTestMessagingServer(mockSvc)

			resp, err := server.GetChannelStats(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// Type Conversion Tests
// =============================================================================

func TestMessagingServer_TypeConversions(t *testing.T) {
	t.Run("proto_event_type_to_service", func(t *testing.T) {
		tests := []struct {
			input    pb.EventType
			expected messaging.EventType
		}{
			{pb.EventType_EVENT_TYPE_PUT, messaging.EventTypePut},
			{pb.EventType_EVENT_TYPE_DELETE, messaging.EventTypeDelete},
			{pb.EventType_EVENT_TYPE_UNSPECIFIED, messaging.EventTypePut},
		}

		for _, tc := range tests {
			result := protoEventTypeToService(tc.input)
			assert.Equal(t, tc.expected, result)
		}
	})

	t.Run("service_event_type_to_proto", func(t *testing.T) {
		tests := []struct {
			input    messaging.EventType
			expected pb.EventType
		}{
			{messaging.EventTypePut, pb.EventType_EVENT_TYPE_PUT},
			{messaging.EventTypeDelete, pb.EventType_EVENT_TYPE_DELETE},
			{messaging.EventType(999), pb.EventType_EVENT_TYPE_UNSPECIFIED},
		}

		for _, tc := range tests {
			result := serviceEventTypeToProto(tc.input)
			assert.Equal(t, tc.expected, result)
		}
	})

	t.Run("service_message_type_to_proto", func(t *testing.T) {
		tests := []struct {
			input    messaging.MessageType
			expected pb.MessageType
		}{
			{messaging.MessageTypeSubscribe, pb.MessageType_MESSAGE_TYPE_SUBSCRIBE},
			{messaging.MessageTypeUnsubscribe, pb.MessageType_MESSAGE_TYPE_UNSUBSCRIBE},
			{messaging.MessageTypePublish, pb.MessageType_MESSAGE_TYPE_PUBLISH},
			{messaging.MessageTypeNotification, pb.MessageType_MESSAGE_TYPE_NOTIFICATION},
			{messaging.MessageTypePing, pb.MessageType_MESSAGE_TYPE_PING},
			{messaging.MessageTypePong, pb.MessageType_MESSAGE_TYPE_PONG},
			{messaging.MessageTypeAck, pb.MessageType_MESSAGE_TYPE_ACK},
			{messaging.MessageTypeError, pb.MessageType_MESSAGE_TYPE_ERROR},
			{"unknown", pb.MessageType_MESSAGE_TYPE_UNSPECIFIED},
		}

		for _, tc := range tests {
			result := serviceMessageTypeToProto(tc.input)
			assert.Equal(t, tc.expected, result)
		}
	})

	t.Run("service_channel_to_proto_nil", func(t *testing.T) {
		result, err := serviceChannelToProto(nil)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("service_subscription_to_proto_nil", func(t *testing.T) {
		result := serviceSubscriptionToProto(nil)
		assert.Nil(t, result)
	})

	t.Run("service_filter_to_proto_nil", func(t *testing.T) {
		result := serviceFilterToProto(nil)
		assert.Nil(t, result)
	})

	t.Run("service_message_to_proto_nil", func(t *testing.T) {
		result := serviceMessageToProto(nil)
		assert.Nil(t, result)
	})

	t.Run("service_stats_to_proto_messaging_nil", func(t *testing.T) {
		result := serviceStatsToProtoMessaging(nil)
		assert.Nil(t, result)
	})

	t.Run("proto_filter_to_service_nil", func(t *testing.T) {
		result := protoFilterToService(nil)
		assert.Nil(t, result)
	})
}

// =============================================================================
// Error Conversion Tests
// =============================================================================

func TestMessagingServer_ConvertMessagingError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		expectedCode codes.Code
	}{
		{
			name:         "nil_error",
			err:          nil,
			expectedCode: codes.OK,
		},
		{
			name:         "channel_not_found",
			err:          messaging.NewChannelNotFoundError("test", "channel"),
			expectedCode: codes.NotFound,
		},
		{
			name:         "channel_exists",
			err:          messaging.NewChannelExistsError("test", "channel"),
			expectedCode: codes.AlreadyExists,
		},
		{
			name:         "subscription_not_found",
			err:          messaging.NewSubscriptionNotFoundError("test", "sub-id"),
			expectedCode: codes.NotFound,
		},
		{
			name:         "invalid_channel_name",
			err:          messaging.NewInvalidChannelNameError("test", "invalid", "reason"),
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "service_not_running",
			err:          messaging.NewServiceNotRunningError("test", "operation"),
			expectedCode: codes.Unavailable,
		},
		{
			name:         "max_subscriptions_reached",
			err:          messaging.NewMaxSubscriptionsReachedError("test", "channel", 100),
			expectedCode: codes.ResourceExhausted,
		},
		{
			name:         "hub_buffer_full",
			err:          messaging.NewHubBufferFullError("test", "channel", 1024),
			expectedCode: codes.ResourceExhausted,
		},
		{
			name:         "client_disconnected",
			err:          messaging.NewClientDisconnectedError("test", "client-id"),
			expectedCode: codes.Aborted,
		},
		{
			name:         "client_slow",
			err:          messaging.NewClientSlowError("test", "client-id", 10, 100),
			expectedCode: codes.ResourceExhausted,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := convertMessagingError(tc.err)
			if tc.err == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				st, ok := status.FromError(result)
				require.True(t, ok)
				assert.Equal(t, tc.expectedCode, st.Code())
			}
		})
	}
}

// =============================================================================
// NewMessagingServer Tests
// =============================================================================

func TestNewMessagingServer(t *testing.T) {
	t.Run("creates_server_with_mock_service", func(t *testing.T) {
		mockSvc := &mockMessagingService{}
		log, err := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
		require.NoError(t, err)

		server := NewMessagingServer(mockSvc, log)

		require.NotNil(t, server)
		assert.NotNil(t, server.messagingSvc)
		assert.NotNil(t, server.logger)
	})
}

// =============================================================================
// Additional Edge Case Tests
// =============================================================================

func TestMessagingServer_EdgeCases(t *testing.T) {
	now := time.Now()

	t.Run("create_channel_with_all_options", func(t *testing.T) {
		mockSvc := &mockMessagingService{
			createChannelFunc: func(ctx context.Context, name string, opts ...messaging.CreateChannelOption) (*messaging.Channel, error) {
				return &messaging.Channel{
					Name:        name,
					Codec:       "msgpack",
					CreatedAt:   now,
					Persistent:  true,
					MaxHistory:  500,
					BufferSize:  1024,
					TTL:         2 * time.Hour,
					Subscribers: 0,
				}, nil
			},
		}
		server := newTestMessagingServer(mockSvc)

		resp, err := server.CreateChannel(context.Background(), &pb.MessagingCreateChannelRequest{
			Name:           "full-options-channel",
			Codec:          "msgpack",
			Persistent:     true,
			MaxHistory:     500,
			BufferSize:     1024,
			MaxSubscribers: 100,
			TtlSeconds:     7200,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "full-options-channel", resp.Channel.Name)
	})

	t.Run("subscribe_with_multiple_event_types", func(t *testing.T) {
		mockSvc := &mockMessagingService{
			subscribeFunc: func(ctx context.Context, channel string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
				return &messaging.Subscription{
					ID:        "sub-multi-events",
					Channel:   channel,
					CreatedAt: now,
					Filters: &messaging.SubscriptionFilter{
						EventTypes: []messaging.EventType{messaging.EventTypePut, messaging.EventTypeDelete},
					},
				}, nil
			},
		}
		server := newTestMessagingServer(mockSvc)

		resp, err := server.Subscribe(context.Background(), &pb.MessagingSubscribeRequest{
			Channel: "test-channel",
			Filter: &pb.SubscriptionFilter{
				EventTypes: []pb.EventType{
					pb.EventType_EVENT_TYPE_PUT,
					pb.EventType_EVENT_TYPE_DELETE,
				},
			},
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Subscription.Filters)
		assert.Len(t, resp.Subscription.Filters.EventTypes, 2)
	})

	t.Run("service_message_conversion", func(t *testing.T) {
		msg := &messaging.Message{
			ID:        "msg-1",
			Type:      messaging.MessageTypeNotification,
			Channel:   "test-channel",
			Payload:   []byte(`{"data": "test"}`),
			Timestamp: now,
			Metadata: map[string]string{
				"source": "test",
			},
		}

		protoMsg := serviceMessageToProto(msg)

		require.NotNil(t, protoMsg)
		assert.Equal(t, "msg-1", protoMsg.Id)
		assert.Equal(t, pb.MessageType_MESSAGE_TYPE_NOTIFICATION, protoMsg.Type)
		assert.Equal(t, "test-channel", protoMsg.Channel)
		assert.Equal(t, []byte(`{"data": "test"}`), protoMsg.Payload)
		assert.Equal(t, "test", protoMsg.Metadata["source"])
	})
}

// --- Merged from server_messaging_coverage_test.go ---

// =============================================================================
// GetDedupStats Tests
// =============================================================================

// GetDedupStats and StreamNotifications need a *messaging.PushService past
// validation; with the mock service only their error paths are reachable here.
func TestMessagingServer_GetDedupStats(t *testing.T) {
	tests := []struct {
		name       string
		channel    string
		getChannel func(ctx context.Context, name string) (*messaging.Channel, error)
		errorCode  codes.Code
	}{
		{
			name:      "missing_channel",
			channel:   "",
			errorCode: codes.InvalidArgument,
		},
		{
			name:    "channel_not_found",
			channel: "nonexistent",
			getChannel: func(ctx context.Context, name string) (*messaging.Channel, error) {
				return nil, messaging.NewChannelNotFoundError("GetChannel", name)
			},
			errorCode: codes.NotFound,
		},
		{
			name:    "not_push_service",
			channel: "test-channel",
			getChannel: func(ctx context.Context, name string) (*messaging.Channel, error) {
				return &messaging.Channel{Name: name}, nil
			},
			errorCode: codes.Unimplemented,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestMessagingServer(&mockMessagingService{getChannelFunc: tc.getChannel})

			_, err := srv.GetDedupStats(context.Background(), &pb.MessagingGetDedupStatsRequest{
				Channel: tc.channel,
			})
			require.Error(t, err)
			assert.Equal(t, tc.errorCode, status.Code(err))
		})
	}
}

func TestMessagingServer_StreamNotifications(t *testing.T) {
	tests := []struct {
		name           string
		subscriptionID string
		errorCode      codes.Code
	}{
		{name: "missing_subscription_id", subscriptionID: "", errorCode: codes.InvalidArgument},
		{name: "not_push_service", subscriptionID: "sub-1", errorCode: codes.Internal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestMessagingServer(&mockMessagingService{})

			err := srv.StreamNotifications(&pb.MessagingStreamNotificationsRequest{
				SubscriptionId: tc.subscriptionID,
			}, nil)
			require.Error(t, err)
			assert.Equal(t, tc.errorCode, status.Code(err))
		})
	}
}
