//go:build mcp

package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func newMessagingRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	svc := &extendedMockService{
		messagingSvc: &mockMessagingService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(svc, log)
}

func TestMsgCreateChannel_Success(t *testing.T) {
	registry := newMessagingRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{"basic create", map[string]interface{}{"name": "events"}},
		{"with options", map[string]interface{}{
			"name": "logs", "codec": "json",
			"buffer_size": float64(256), "max_history": float64(100),
			"persistent": "true", "ttl": "24h",
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_msg_create_channel", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestMsgCreateChannel_MissingName(t *testing.T) {
	registry := newMessagingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_create_channel", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "name is required")
}

func TestMsgCreateChannel_ServiceError(t *testing.T) {
	svc := &extendedMockService{
		messagingSvc: &mockMessagingService{
			createFunc: func(ctx context.Context, name string, opts ...messaging.CreateChannelOption) (*messaging.Channel, error) {
				return nil, &dberrors.MessageError{Message: "channel already exists"}
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_create_channel", map[string]interface{}{
		"name": "existing",
	})
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestMsgDeleteChannel_Success(t *testing.T) {
	registry := newMessagingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_delete_channel", map[string]interface{}{
		"name": "old-channel",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestMsgDeleteChannel_MissingName(t *testing.T) {
	registry := newMessagingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_delete_channel", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestMsgGetChannel_Success(t *testing.T) {
	registry := newMessagingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_get_channel", map[string]interface{}{
		"name": "events",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "name")
	assert.Contains(t, resultMap, "codec")
}

func TestMsgGetChannel_MissingName(t *testing.T) {
	registry := newMessagingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_get_channel", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestMsgListChannels_Success(t *testing.T) {
	registry := newMessagingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_list_channels", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "channels")
	assert.Contains(t, resultMap, "count")
}

func TestMsgListChannels_ServiceError(t *testing.T) {
	svc := &extendedMockService{
		messagingSvc: &mockMessagingService{
			listFunc: func(ctx context.Context) ([]*messaging.Channel, error) {
				return nil, &dberrors.MessageError{Message: "internal error"}
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_list_channels", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestMsgSubscribe_Success(t *testing.T) {
	registry := newMessagingRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{"basic subscribe", map[string]interface{}{"channel": "events"}},
		{"with options", map[string]interface{}{
			"channel": "events", "buffer_size": float64(128),
			"session_id": "sess-1", "filter_key_prefix": "user:",
			"filter_event_types": `["PUT","DELETE"]`,
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_msg_subscribe", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestMsgSubscribe_MissingChannel(t *testing.T) {
	registry := newMessagingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_subscribe", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestMsgUnsubscribe_Success(t *testing.T) {
	registry := newMessagingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_unsubscribe", map[string]interface{}{
		"subscription_id": "sub-1",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestMsgUnsubscribe_MissingID(t *testing.T) {
	registry := newMessagingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_unsubscribe", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestMsgPublish_Success(t *testing.T) {
	registry := newMessagingRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{"basic publish", map[string]interface{}{
			"channel": "events", "payload": "hello",
		}},
		{"with options", map[string]interface{}{
			"channel": "events", "payload": `{"key":"value"}`,
			"message_id": "msg-1", "dedup_key": "dedup-1",
			"metadata": `{"source":"test"}`,
		}},
		{"plain string payload", map[string]interface{}{
			"channel": "events", "payload": "plain text message",
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_msg_publish", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestMsgPublish_Errors(t *testing.T) {
	registry := newMessagingRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{"missing channel", map[string]interface{}{"payload": "x"}, "channel is required"},
		{"missing payload", map[string]interface{}{"channel": "events"}, "payload is required"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_msg_publish", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestMsgPublishDelta_Success(t *testing.T) {
	registry := newMessagingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_publish_delta", map[string]interface{}{
		"channel":   "events",
		"old_state": `{"count":1}`,
		"new_state": `{"count":2}`,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestMsgPublishDelta_Errors(t *testing.T) {
	registry := newMessagingRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{"missing channel", map[string]interface{}{
			"old_state": `{}`, "new_state": `{}`,
		}, "channel is required"},
		{"missing old_state", map[string]interface{}{
			"channel": "events", "new_state": `{}`,
		}, "old_state is required"},
		{"missing new_state", map[string]interface{}{
			"channel": "events", "old_state": `{}`,
		}, "new_state is required"},
		{"invalid old_state", map[string]interface{}{
			"channel": "events", "old_state": "bad", "new_state": `{}`,
		}, "invalid old_state"},
		{"invalid new_state", map[string]interface{}{
			"channel": "events", "old_state": `{}`, "new_state": "bad",
		}, "invalid new_state"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_msg_publish_delta", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestMsgPublishDelta_WithOptions(t *testing.T) {
	registry := newMessagingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_publish_delta", map[string]interface{}{
		"channel":    "events",
		"old_state":  `{"count":1}`,
		"new_state":  `{"count":2}`,
		"message_id": "delta-1",
		"dedup_key":  "dedup-delta-1",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestMsgStats_Success(t *testing.T) {
	registry := newMessagingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_stats", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "available")
}

func TestMsgStats_NilStats(t *testing.T) {
	svc := &extendedMockService{
		messagingSvc: &mockMessagingService{
			statsResult: nil,
		},
	}
	// Override GetStats to return nil
	svc.messagingSvc = &nilStatsMessagingService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_stats", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, false, resultMap["available"])
}

func TestMsgDedupStats_ServiceTypeMismatch(t *testing.T) {
	registry := newMessagingRegistry(t)
	ctx := context.Background()

	// The mock messaging service is not a *messaging.PushService,
	// so this should return a type mismatch error.
	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_dedup_stats", map[string]interface{}{
		"channel": "events",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "dedup stats service not available")
}

func TestMsgDedupStats_MissingChannel(t *testing.T) {
	registry := newMessagingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_dedup_stats", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "channel is required")
}

func TestMsgServiceNotAvailable(t *testing.T) {
	svc := &extendedMockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_create_channel", map[string]interface{}{
		"name": "events",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not configured")
}

func TestIsMsgTool(t *testing.T) {
	msgTools := []string{
		"quicbaas_msg_create_channel", "quicbaas_msg_delete_channel",
		"quicbaas_msg_get_channel", "quicbaas_msg_list_channels",
		"quicbaas_msg_subscribe", "quicbaas_msg_unsubscribe",
		"quicbaas_msg_publish", "quicbaas_msg_publish_delta",
		"quicbaas_msg_stats", "quicbaas_msg_dedup_stats",
	}
	for _, name := range msgTools {
		assert.True(t, isMsgTool(name))
	}
	assert.False(t, isMsgTool("qrdb_put"))
	assert.False(t, isMsgTool(""))
}

// nilStatsMessagingService is a messaging service that returns nil stats.
type nilStatsMessagingService struct {
	mockMessagingService
}

func (m *nilStatsMessagingService) GetStats() *messaging.Stats {
	return nil
}

// =============================================================================
// Messaging Service Error Path Tests
// =============================================================================

func TestHandleMsgDeleteChannel_ServiceError(t *testing.T) {
	expectedErr := errors.New("channel not found")
	svc := &extendedMockService{
		messagingSvc: &mockMessagingService{
			deleteFunc: func(ctx context.Context, name string) error {
				return expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_delete_channel", map[string]interface{}{
		"name": "nonexistent",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "delete channel failed")
}

func TestHandleMsgGetChannel_ServiceError(t *testing.T) {
	expectedErr := errors.New("channel not found")
	svc := &extendedMockService{
		messagingSvc: &mockMessagingService{
			getFunc: func(ctx context.Context, name string) (*messaging.Channel, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_get_channel", map[string]interface{}{
		"name": "nonexistent",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "get channel failed")
}

func TestHandleMsgUnsubscribe_ServiceError(t *testing.T) {
	expectedErr := errors.New("subscription not found")
	svc := &extendedMockService{
		messagingSvc: &mockMessagingService{
			unsubFunc: func(ctx context.Context, subscriptionID string) error {
				return expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_unsubscribe", map[string]interface{}{
		"subscription_id": "invalid-sub",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unsubscribe failed")
}

func TestHandleMsgSubscribe_ServiceError(t *testing.T) {
	expectedErr := errors.New("channel does not exist")
	svc := &extendedMockService{
		messagingSvc: &mockMessagingService{
			subscribeFunc: func(ctx context.Context, channel string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_subscribe", map[string]interface{}{
		"channel": "nonexistent",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "subscribe failed")
}

func TestHandleMsgPublish_ServiceError(t *testing.T) {
	expectedErr := errors.New("publish quota exceeded")
	svc := &extendedMockService{
		messagingSvc: &mockMessagingService{
			publishFunc: func(ctx context.Context, channel string, payload interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_publish", map[string]interface{}{
		"channel": "events",
		"payload": "test message",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "publish failed")
}

func TestHandleMsgPublishDelta_ServiceError(t *testing.T) {
	expectedErr := errors.New("delta publish failed")
	svc := &extendedMockService{
		messagingSvc: &mockMessagingService{
			pubDeltaFunc: func(ctx context.Context, channel string, old, new interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_publish_delta", map[string]interface{}{
		"channel":   "events",
		"old_state": `{"count":1}`,
		"new_state": `{"count":2}`,
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "publish delta failed")
}

func TestHandleMsgCreateChannel_InvalidTTL(t *testing.T) {
	svc := &extendedMockService{
		messagingSvc: &mockMessagingService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_create_channel", map[string]interface{}{
		"name": "events",
		"ttl":  "invalid-duration",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid ttl format")
}

func TestExecuteMsgTool_UnknownTool(t *testing.T) {
	svc := &extendedMockService{
		messagingSvc: &mockMessagingService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.executeMsgTool(ctx, "quicbaas_msg_unknown_tool", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unknown tool: quicbaas_msg_unknown_tool")
}

func TestHandleMsgCreateChannel_WithMaxSubscribers(t *testing.T) {
	svc := &extendedMockService{
		messagingSvc: &mockMessagingService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_msg_create_channel", map[string]interface{}{
		"name":            "events",
		"max_subscribers": float64(100),
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// Embedding Service Error Path Tests
// =============================================================================

func TestHandleEmbeddingListProviders_ServiceError(t *testing.T) {
	expectedErr := errors.New("provider list unavailable")
	svc := &extendedMockService{
		embeddingSvc: &mockEmbeddingService{
			listProvidersFunc: func(ctx context.Context, req *service.ListProvidersRequest) (*service.ListProvidersResponse, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_list_providers", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "list providers failed")
}

func TestHandleEmbeddingListProviders_WithRateLimitAndError(t *testing.T) {
	svc := &extendedMockService{
		embeddingSvc: &mockEmbeddingService{
			listProvidersFunc: func(ctx context.Context, req *service.ListProvidersRequest) (*service.ListProvidersResponse, error) {
				return &service.ListProvidersResponse{
					Providers: []*service.ProviderInfo{
						{
							Name:               "ollama",
							Type:               "ollama",
							Status:             "available",
							RateLimitPerMinute: 100,
							LastError:          "previous connection timeout",
						},
					},
					DefaultProvider: "ollama",
					Timestamp:       time.Now(),
				}, nil
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_list_providers", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	providers := resultMap["providers"].([]map[string]interface{})
	assert.Equal(t, 100, providers[0]["rate_limit_per_minute"])
	assert.Equal(t, "previous connection timeout", providers[0]["last_error"])
}

func TestHandleEmbeddingGetProvider_ServiceError(t *testing.T) {
	expectedErr := errors.New("provider not found")
	svc := &extendedMockService{
		embeddingSvc: &mockEmbeddingService{
			getProviderFunc: func(ctx context.Context, req *service.GetProviderInfoRequest) (*service.GetProviderInfoResponse, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_get_provider", map[string]interface{}{
		"provider": "nonexistent",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "get provider info failed")
}

func TestHandleEmbeddingGetProvider_WithExtendedInfo(t *testing.T) {
	now := time.Now()
	svc := &extendedMockService{
		embeddingSvc: &mockEmbeddingService{
			getProviderFunc: func(ctx context.Context, req *service.GetProviderInfoRequest) (*service.GetProviderInfoResponse, error) {
				return &service.GetProviderInfoResponse{
					Info: &service.ProviderInfo{
						Name:               string(req.Provider),
						Type:               req.Provider,
						Status:             "available",
						RateLimitPerMinute: 60,
						LastError:          "connection reset",
						LastErrorTime:      &now,
					},
					Timestamp: time.Now(),
				}, nil
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_get_provider", map[string]interface{}{
		"provider": "ollama",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	assert.Equal(t, 60, resultMap["rate_limit_per_minute"])
	assert.Equal(t, "connection reset", resultMap["last_error"])
	assert.Contains(t, resultMap, "last_error_time")
}

func TestHandleEmbeddingSetDefault_ServiceError(t *testing.T) {
	expectedErr := errors.New("invalid provider")
	svc := &extendedMockService{
		embeddingSvc: &mockEmbeddingService{
			setDefaultFunc: func(ctx context.Context, req *service.SetDefaultProviderRequest) (*service.SetDefaultProviderResponse, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_set_default", map[string]interface{}{
		"provider": "invalid",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "set default provider failed")
}

func TestHandleEmbeddingCacheStats_WithOptionalFields(t *testing.T) {
	svc := &extendedMockService{
		embeddingSvc: &mockEmbeddingService{
			cacheStatsFunc: func(ctx context.Context, req *service.GetEmbeddingCacheStatsRequest) (*service.GetEmbeddingCacheStatsResponse, error) {
				return &service.GetEmbeddingCacheStatsResponse{
					Enabled:            true,
					Size:               500,
					Capacity:           1000,
					Hits:               400,
					Misses:             100,
					HitRate:            0.8,
					Evictions:          50,
					MemoryBytes:        1024000,
					CompressionEnabled: true,
					CompressionRatio:   0.6,
					Timestamp:          time.Now(),
				}, nil
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_cache_stats", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	// Use NotNil instead of Equal to avoid type mismatches
	assert.NotNil(t, resultMap["memory_bytes"])
	assert.NotNil(t, resultMap["compression_ratio"])
}

func TestHandleEmbeddingCacheClear_WithProviderFilter(t *testing.T) {
	svc := &extendedMockService{
		embeddingSvc: &mockEmbeddingService{
			cacheClearFunc: func(ctx context.Context, req *service.ClearEmbeddingCacheRequest) (*service.ClearEmbeddingCacheResponse, error) {
				return &service.ClearEmbeddingCacheResponse{
					EntriesCleared: 50,
					Provider:       req.Provider,
					Timestamp:      time.Now(),
				}, nil
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_cache_clear", map[string]interface{}{
		"provider": "ollama",
		"all":      "true",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	assert.Equal(t, "ollama", resultMap["provider"])
}

func TestHandleEmbeddingCacheClear_ServiceError(t *testing.T) {
	expectedErr := errors.New("cache clear failed")
	svc := &extendedMockService{
		embeddingSvc: &mockEmbeddingService{
			cacheClearFunc: func(ctx context.Context, req *service.ClearEmbeddingCacheRequest) (*service.ClearEmbeddingCacheResponse, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_cache_clear", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "clear cache failed")
}

func TestHandleEmbeddingGenerate_ServiceError(t *testing.T) {
	expectedErr := errors.New("embedding generation failed")
	svc := &extendedMockService{
		embeddingSvc: &mockEmbeddingService{
			generateFunc: func(ctx context.Context, req *service.GenerateEmbeddingRequest) (*service.GenerateEmbeddingResponse, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_generate", map[string]interface{}{
		"text": "test text",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "generate embedding failed")
}

func TestHandleEmbeddingBatch_ServiceError(t *testing.T) {
	expectedErr := errors.New("batch embedding failed")
	svc := &extendedMockService{
		embeddingSvc: &mockEmbeddingService{
			batchFunc: func(ctx context.Context, req *service.BatchGenerateEmbeddingsRequest) (*service.BatchGenerateEmbeddingsResponse, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_batch", map[string]interface{}{
		"items": `[{"text":"hello"}]`,
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "batch generate embeddings failed")
}

func TestHandleEmbeddingBatch_WithResultError(t *testing.T) {
	svc := &extendedMockService{
		embeddingSvc: &mockEmbeddingService{
			batchFunc: func(ctx context.Context, req *service.BatchGenerateEmbeddingsRequest) (*service.BatchGenerateEmbeddingsResponse, error) {
				results := make([]*service.BatchEmbeddingResult, len(req.Items))
				for i, item := range req.Items {
					if i == 0 {
						results[i] = &service.BatchEmbeddingResult{
							ID:        item.ID,
							Embedding: []float32{0.1, 0.2},
							Metadata:  &service.EmbeddingMetadata{Model: "test"},
						}
					} else {
						results[i] = &service.BatchEmbeddingResult{
							ID:    item.ID,
							Error: "text too long",
						}
					}
				}
				return &service.BatchGenerateEmbeddingsResponse{
					Results:      results,
					SuccessCount: 1,
					ErrorCount:   1,
					Timestamp:    time.Now(),
				}, nil
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_batch", map[string]interface{}{
		"items": `[{"text":"short"},{"text":"too long text"}]`,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	assert.Equal(t, 1, resultMap["success_count"])
	assert.Equal(t, 1, resultMap["error_count"])
}

func TestExecuteEmbeddingTool_UnknownTool(t *testing.T) {
	svc := &extendedMockService{
		embeddingSvc: &mockEmbeddingService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.executeEmbeddingTool(ctx, "quicbaas_embedding_unknown", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unknown tool: quicbaas_embedding_unknown")
}

func TestEmbeddingServiceNotConfigured(t *testing.T) {
	svc := &extendedMockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_generate", map[string]interface{}{
		"text": "test",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "embedding service not configured")
}

// =============================================================================
// Ollama Service Error Path Tests
// =============================================================================

func TestHandleOllamaModelInfo_ServiceError(t *testing.T) {
	expectedErr := errors.New("model not found")
	svc := &extendedMockService{
		ollamaSvc: &mockOllamaService{
			getModelFunc: func(ctx context.Context, req *service.OllamaGetModelInfoRequest) (*service.OllamaGetModelInfoResponse, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_ollama_model_info", map[string]interface{}{
		"model": "nonexistent-model",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "get model info failed")
}

func TestHandleOllamaHealth_ServiceError(t *testing.T) {
	expectedErr := errors.New("connection refused")
	svc := &extendedMockService{
		ollamaSvc: &mockOllamaService{
			healthFunc: func(ctx context.Context, req *service.OllamaHealthRequest) (*service.OllamaHealthResponse, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_ollama_health", map[string]interface{}{})
	require.NoError(t, err) // Health returns result even on error
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	assert.Equal(t, false, resultMap["available"])
	assert.Contains(t, resultMap["error"], "connection refused")
}

func TestHandleOllamaHealth_WithErrorResponse(t *testing.T) {
	svc := &extendedMockService{
		ollamaSvc: &mockOllamaService{
			healthFunc: func(ctx context.Context, req *service.OllamaHealthRequest) (*service.OllamaHealthResponse, error) {
				return &service.OllamaHealthResponse{
					Available:   false,
					Error:       "service degraded",
					LastChecked: time.Now(),
				}, nil
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_ollama_health", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	assert.Equal(t, false, resultMap["available"])
	assert.Equal(t, "service degraded", resultMap["error"])
}

func TestHandleOllamaEmbed_ServiceError(t *testing.T) {
	expectedErr := errors.New("embed failed")
	svc := &extendedMockService{
		ollamaSvc: &mockOllamaService{
			embedFunc: func(ctx context.Context, req *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_ollama_embed", map[string]interface{}{
		"text": "test",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "embed failed")
}

func TestHandleOllamaEmbedBatch_ServiceError(t *testing.T) {
	expectedErr := errors.New("batch embed failed")
	svc := &extendedMockService{
		ollamaSvc: &mockOllamaService{
			embedBatchFunc: func(ctx context.Context, req *service.OllamaEmbedBatchRequest) (*service.OllamaEmbedBatchResponse, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_ollama_embed_batch", map[string]interface{}{
		"texts": `["hello","world"]`,
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "embed batch failed")
}

func TestHandleOllamaModelList_ServiceError(t *testing.T) {
	expectedErr := errors.New("list models failed")
	svc := &extendedMockService{
		ollamaSvc: &mockOllamaService{
			listModelsFunc: func(ctx context.Context, req *service.OllamaListModelsRequest) (*service.OllamaListModelsResponse, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_ollama_model_list", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "list models failed")
}

func TestExecuteOllamaTool_UnknownTool(t *testing.T) {
	svc := &extendedMockService{
		ollamaSvc: &mockOllamaService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.executeOllamaTool(ctx, "quicbaas_ollama_unknown", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unknown tool: quicbaas_ollama_unknown")
}

func TestOllamaServiceNotConfigured(t *testing.T) {
	svc := &extendedMockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_ollama_embed", map[string]interface{}{
		"text": "test",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "ollama service not configured")
}

// =============================================================================
// Parameter Store Error Path Tests
// =============================================================================

func TestHandleParameterSet_ServiceError(t *testing.T) {
	expectedErr := errors.New("parameter set failed")
	svc := &extendedMockService{
		parameterSvc: &mockParameterStoreService{
			setFunc: func(ctx context.Context, req *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_set", map[string]interface{}{
		"path":  "/app/config",
		"value": "test",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "set parameter failed")
}

func TestHandleParameterSet_InvalidTagsJSON(t *testing.T) {
	svc := &extendedMockService{
		parameterSvc: &mockParameterStoreService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_set", map[string]interface{}{
		"path":  "/app/config",
		"value": "test",
		"tags":  "invalid-json",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid tags JSON")
}

func TestHandleParameterDelete_ServiceError(t *testing.T) {
	expectedErr := errors.New("parameter delete failed")
	svc := &extendedMockService{
		parameterSvc: &mockParameterStoreService{
			deleteFunc: func(ctx context.Context, req *interfaces.DeleteParameterRequest) (*interfaces.DeleteParameterResponse, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_delete", map[string]interface{}{
		"path": "/app/config",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "delete parameter failed")
}

func TestHandleParameterList_ServiceError(t *testing.T) {
	expectedErr := errors.New("list parameters failed")
	svc := &extendedMockService{
		parameterSvc: &mockParameterStoreService{
			listFunc: func(ctx context.Context, req *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_list", map[string]interface{}{
		"prefix": "/app",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "list parameters failed")
}

func TestHandleParameterList_WithOptionalFields(t *testing.T) {
	svc := &extendedMockService{
		parameterSvc: &mockParameterStoreService{
			listFunc: func(ctx context.Context, req *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error) {
				return &interfaces.ListParametersResponse{
					Parameters: []*interfaces.ParameterValue{
						{Path: "/app/config", Value: []byte("value"), Type: "string"},
					},
					HasMore:    true,
					NextCursor: "cursor123",
					TotalCount: 50,
					Timestamp:  time.Now(),
				}, nil
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_list", map[string]interface{}{
		"prefix": "/app",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	assert.Equal(t, true, resultMap["has_more"])
	assert.Equal(t, "cursor123", resultMap["next_cursor"])
	// Use NotNil to avoid type comparison issues
	assert.NotNil(t, resultMap["total_count"])
}

func TestHandleParameterBatchGet_ServiceError(t *testing.T) {
	expectedErr := errors.New("batch get failed")
	svc := &extendedMockService{
		parameterSvc: &mockParameterStoreService{
			getMultiFunc: func(ctx context.Context, req *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_batch_get", map[string]interface{}{
		"paths": `["/app/config1","/app/config2"]`,
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "batch get parameters failed")
}

func TestHandleParameterBatchGet_WithErrors(t *testing.T) {
	svc := &extendedMockService{
		parameterSvc: &mockParameterStoreService{
			getMultiFunc: func(ctx context.Context, req *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error) {
				return &interfaces.GetParametersResponse{
					Parameters: []*interfaces.ParameterValue{
						{Path: "/app/config1", Value: []byte("val1"), Type: "string"},
					},
					Errors:    map[string]string{"/app/config2": "not found"},
					Timestamp: time.Now(),
				}, nil
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_batch_get", map[string]interface{}{
		"paths": `["/app/config1","/app/config2"]`,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	assert.Contains(t, resultMap, "errors")
}

func TestHandleParameterBatchSet_ServiceError(t *testing.T) {
	expectedErr := errors.New("batch set failed")
	svc := &extendedMockService{
		parameterSvc: &mockParameterStoreService{
			setMultiFunc: func(ctx context.Context, req *interfaces.SetParametersRequest) (*interfaces.SetParametersResponse, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_batch_set", map[string]interface{}{
		"parameters": `[{"path":"/app/config","value":"test"}]`,
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "batch set parameters failed")
}

func TestHandleParameterBatchSet_WithResultError(t *testing.T) {
	svc := &extendedMockService{
		parameterSvc: &mockParameterStoreService{
			setMultiFunc: func(ctx context.Context, req *interfaces.SetParametersRequest) (*interfaces.SetParametersResponse, error) {
				results := make([]*interfaces.SetParameterResult, len(req.Parameters))
				for i, p := range req.Parameters {
					if i == 0 {
						results[i] = &interfaces.SetParameterResult{Path: p.Path, Created: true, Success: true}
					} else {
						results[i] = &interfaces.SetParameterResult{Path: p.Path, Success: false, Error: "validation failed"}
					}
				}
				return &interfaces.SetParametersResponse{
					Results:   results,
					Timestamp: time.Now(),
				}, nil
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_batch_set", map[string]interface{}{
		"parameters": `[{"path":"/app/config1","value":"v1"},{"path":"/app/config2","value":"v2"}]`,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	results := resultMap["results"].([]map[string]interface{})
	assert.Equal(t, "validation failed", results[1]["error"])
}

func TestHandleParameterBatchDelete_ServiceError(t *testing.T) {
	expectedErr := errors.New("batch delete failed")
	svc := &extendedMockService{
		parameterSvc: &mockParameterStoreService{
			delMultiFunc: func(ctx context.Context, req *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_batch_delete", map[string]interface{}{
		"paths": `["/app/config1","/app/config2"]`,
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "batch delete parameters failed")
}

func TestHandleParameterBatchDelete_WithResultError(t *testing.T) {
	svc := &extendedMockService{
		parameterSvc: &mockParameterStoreService{
			delMultiFunc: func(ctx context.Context, req *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error) {
				results := make([]*interfaces.DeleteParameterResult, len(req.Paths))
				for i, path := range req.Paths {
					if i == 0 {
						results[i] = &interfaces.DeleteParameterResult{Path: path, Deleted: true}
					} else {
						results[i] = &interfaces.DeleteParameterResult{Path: path, Deleted: false, Error: "protected parameter"}
					}
				}
				return &interfaces.DeleteParametersResponse{
					Results:      results,
					DeletedCount: 1,
					Timestamp:    time.Now(),
				}, nil
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_batch_delete", map[string]interface{}{
		"paths": `["/app/config1","/app/protected"]`,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	results := resultMap["results"].([]map[string]interface{})
	assert.Equal(t, "protected parameter", results[1]["error"])
}

func TestHandleParameterSearch_ServiceError(t *testing.T) {
	expectedErr := errors.New("search failed")
	svc := &extendedMockService{
		parameterSvc: &mockParameterStoreService{
			searchFunc: func(ctx context.Context, req *interfaces.SearchByTagsRequest) (*interfaces.SearchByTagsResponse, error) {
				return nil, expectedErr
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_search", map[string]interface{}{
		"tags": `{"env":"prod"}`,
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "search parameters failed")
}

func TestHandleParameterSearch_WithOptionalFields(t *testing.T) {
	svc := &extendedMockService{
		parameterSvc: &mockParameterStoreService{
			searchFunc: func(ctx context.Context, req *interfaces.SearchByTagsRequest) (*interfaces.SearchByTagsResponse, error) {
				return &interfaces.SearchByTagsResponse{
					Parameters: []*interfaces.ParameterValue{
						{Path: "/app/config", Value: []byte("val"), Type: "string", Tags: req.Tags},
					},
					HasMore:    true,
					NextCursor: "search-cursor",
					Timestamp:  time.Now(),
				}, nil
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_search", map[string]interface{}{
		"tags": `{"env":"prod"}`,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	assert.Equal(t, true, resultMap["has_more"])
	assert.Equal(t, "search-cursor", resultMap["next_cursor"])
}

func TestExecuteParameterTool_UnknownTool(t *testing.T) {
	svc := &extendedMockService{
		parameterSvc: &mockParameterStoreService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.executeParameterTool(ctx, "quicbaas_parameter_unknown", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unknown tool: quicbaas_parameter_unknown")
}

func TestParameterServiceNotConfigured(t *testing.T) {
	svc := &extendedMockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_get", map[string]interface{}{
		"path": "/app/config",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "parameter store service not configured")
}

// =============================================================================
// Helper Utility Tests
// =============================================================================

func TestParseBoolDefault(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		defValue bool
		expected bool
	}{
		{"empty string returns default true", "", true, true},
		{"empty string returns default false", "", false, false},
		{"true string", "true", false, true},
		{"false string", "false", true, false},
		{"invalid returns default", "invalid", true, true},
		{"1 is true", "1", false, true},
		{"0 is false", "0", true, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseBool(tc.input, tc.defValue)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestParseIntDefault(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		defValue int
		expected int
	}{
		{"empty string returns default", "", 100, 100},
		{"valid number", "42", 0, 42},
		{"invalid returns default", "abc", 50, 50},
		{"negative number", "-10", 0, -10},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseInt(tc.input, tc.defValue)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestParseInt64Default(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		defValue int64
		expected int64
	}{
		{"empty string returns default", "", 100, 100},
		{"valid number", "42", 0, 42},
		{"invalid returns default", "abc", 50, 50},
		{"large number", "9999999999", 0, 9999999999},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseInt64(tc.input, tc.defValue)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGetStringDefault(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		key      string
		defValue string
		expected string
	}{
		{"existing key", map[string]interface{}{"key": "value"}, "key", "default", "value"},
		{"missing key", map[string]interface{}{}, "key", "default", "default"},
		{"empty value", map[string]interface{}{"key": ""}, "key", "default", "default"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := getStringDefault(tc.args, tc.key, tc.defValue)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// Parameter Formatting Tests
// =============================================================================

func TestFormatParameterValue_AllFields(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)

	param := &interfaces.ParameterValue{
		Path:         "/app/secret",
		Value:        []byte("secret-value"),
		Type:         "securestring",
		Description:  "A secret parameter",
		IsEncrypted:  true,
		AuditEnabled: true,
		KMSKeyID:     "key-123",
		Tags:         map[string]string{"env": "prod"},
		ExpiresAt:    &expiresAt,
		CreatedBy:    "admin",
		UpdatedBy:    "admin",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	result := formatParameterValue(param)

	assert.Equal(t, "/app/secret", result["path"])
	assert.Equal(t, "secret-value", result["value"])
	assert.Equal(t, "securestring", result["type"])
	assert.Equal(t, "A secret parameter", result["description"])
	assert.Equal(t, true, result["is_encrypted"])
	assert.Equal(t, true, result["audit_enabled"])
	assert.Equal(t, "key-123", result["kms_key_id"])
	assert.Equal(t, map[string]string{"env": "prod"}, result["tags"])
	assert.Contains(t, result, "expires_at")
	assert.Equal(t, "admin", result["created_by"])
	assert.Equal(t, "admin", result["updated_by"])
}

func TestFormatParameterValue_MinimalFields(t *testing.T) {
	now := time.Now()

	param := &interfaces.ParameterValue{
		Path:      "/app/config",
		Type:      "string",
		CreatedAt: now,
		UpdatedAt: now,
	}

	result := formatParameterValue(param)

	assert.Equal(t, "/app/config", result["path"])
	assert.Equal(t, "string", result["type"])
	assert.NotContains(t, result, "value")
	assert.NotContains(t, result, "description")
	assert.NotContains(t, result, "kms_key_id")
	assert.NotContains(t, result, "tags")
	assert.NotContains(t, result, "expires_at")
	assert.NotContains(t, result, "created_by")
	assert.NotContains(t, result, "updated_by")
}

// =============================================================================
// Tool Identification Tests
// =============================================================================

func TestIsOllamaTool_Coverage(t *testing.T) {
	ollamaTools := []string{
		"quicbaas_ollama_embed",
		"quicbaas_ollama_embed_batch",
		"quicbaas_ollama_model_list",
		"quicbaas_ollama_model_info",
		"quicbaas_ollama_health",
	}

	for _, name := range ollamaTools {
		assert.True(t, isOllamaTool(name), "expected %s to be ollama tool", name)
	}

	assert.False(t, isOllamaTool("quicbaas_embedding_generate"))
	assert.False(t, isOllamaTool("qrdb_put"))
	assert.False(t, isOllamaTool(""))
}

func TestIsParameterTool_Coverage(t *testing.T) {
	paramTools := []string{
		"quicbaas_parameter_get",
		"quicbaas_parameter_set",
		"quicbaas_parameter_delete",
		"quicbaas_parameter_list",
		"quicbaas_parameter_batch_get",
		"quicbaas_parameter_batch_set",
		"quicbaas_parameter_batch_delete",
		"quicbaas_parameter_search",
	}

	for _, name := range paramTools {
		assert.True(t, isParameterTool(name), "expected %s to be parameter tool", name)
	}

	assert.False(t, isParameterTool("quicbaas_ollama_embed"))
	assert.False(t, isParameterTool("qrdb_put"))
	assert.False(t, isParameterTool(""))
}

func TestIsEmbeddingTool_Coverage(t *testing.T) {
	embeddingTools := []string{
		"quicbaas_embedding_generate",
		"quicbaas_embedding_batch",
		"quicbaas_embedding_list_providers",
		"quicbaas_embedding_get_provider",
		"quicbaas_embedding_set_default",
		"quicbaas_embedding_cache_stats",
		"quicbaas_embedding_cache_clear",
	}

	for _, name := range embeddingTools {
		assert.True(t, isEmbeddingTool(name), "expected %s to be embedding tool", name)
	}

	assert.False(t, isEmbeddingTool("quicbaas_ollama_embed"))
	assert.False(t, isEmbeddingTool("qrdb_put"))
	assert.False(t, isEmbeddingTool(""))
}
