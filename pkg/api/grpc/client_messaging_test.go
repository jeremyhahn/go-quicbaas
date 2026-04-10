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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Test MessagingCreateChannel nil request validation
func TestClient_MessagingCreateChannel_NilRequest(t *testing.T) {
	client := &Client{conn: nil}
	ctx := context.Background()

	resp, err := client.MessagingCreateChannel(ctx, nil)

	assert.Nil(t, resp)
	require.Error(t, err)

	dragonErr, ok := err.(*errors.DragonError)
	require.True(t, ok, "error should be a DragonError")
	assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	assert.Contains(t, dragonErr.Error(), "request cannot be nil")
}

// Test MessagingDeleteChannel nil request validation
func TestClient_MessagingDeleteChannel_NilRequest(t *testing.T) {
	client := &Client{conn: nil}
	ctx := context.Background()

	resp, err := client.MessagingDeleteChannel(ctx, nil)

	assert.Nil(t, resp)
	require.Error(t, err)

	dragonErr, ok := err.(*errors.DragonError)
	require.True(t, ok, "error should be a DragonError")
	assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	assert.Contains(t, dragonErr.Error(), "request cannot be nil")
}

// Test MessagingGetChannel nil request validation
func TestClient_MessagingGetChannel_NilRequest(t *testing.T) {
	client := &Client{conn: nil}
	ctx := context.Background()

	resp, err := client.MessagingGetChannel(ctx, nil)

	assert.Nil(t, resp)
	require.Error(t, err)

	dragonErr, ok := err.(*errors.DragonError)
	require.True(t, ok, "error should be a DragonError")
	assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	assert.Contains(t, dragonErr.Error(), "request cannot be nil")
}

// Note: MessagingListChannels accepts nil request and creates a default.
// Testing nil request behavior requires a live connection (integration test).

// Test MessagingSubscribe nil request validation
func TestClient_MessagingSubscribe_NilRequest(t *testing.T) {
	client := &Client{conn: nil}
	ctx := context.Background()

	resp, err := client.MessagingSubscribe(ctx, nil)

	assert.Nil(t, resp)
	require.Error(t, err)

	dragonErr, ok := err.(*errors.DragonError)
	require.True(t, ok, "error should be a DragonError")
	assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	assert.Contains(t, dragonErr.Error(), "request cannot be nil")
}

// Test MessagingUnsubscribe nil request validation
func TestClient_MessagingUnsubscribe_NilRequest(t *testing.T) {
	client := &Client{conn: nil}
	ctx := context.Background()

	resp, err := client.MessagingUnsubscribe(ctx, nil)

	assert.Nil(t, resp)
	require.Error(t, err)

	dragonErr, ok := err.(*errors.DragonError)
	require.True(t, ok, "error should be a DragonError")
	assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	assert.Contains(t, dragonErr.Error(), "request cannot be nil")
}

// Test MessagingPublish nil request validation
func TestClient_MessagingPublish_NilRequest(t *testing.T) {
	client := &Client{conn: nil}
	ctx := context.Background()

	resp, err := client.MessagingPublish(ctx, nil)

	assert.Nil(t, resp)
	require.Error(t, err)

	dragonErr, ok := err.(*errors.DragonError)
	require.True(t, ok, "error should be a DragonError")
	assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	assert.Contains(t, dragonErr.Error(), "request cannot be nil")
}

// Test MessagingPublishDelta nil request validation
func TestClient_MessagingPublishDelta_NilRequest(t *testing.T) {
	client := &Client{conn: nil}
	ctx := context.Background()

	resp, err := client.MessagingPublishDelta(ctx, nil)

	assert.Nil(t, resp)
	require.Error(t, err)

	dragonErr, ok := err.(*errors.DragonError)
	require.True(t, ok, "error should be a DragonError")
	assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	assert.Contains(t, dragonErr.Error(), "request cannot be nil")
}

// Test MessagingStreamNotifications nil request validation
func TestClient_MessagingStreamNotifications_NilRequest(t *testing.T) {
	client := &Client{conn: nil}
	ctx := context.Background()

	notifChan, errChan, err := client.MessagingStreamNotifications(ctx, nil)

	assert.Nil(t, notifChan)
	assert.Nil(t, errChan)
	require.Error(t, err)

	dragonErr, ok := err.(*errors.DragonError)
	require.True(t, ok, "error should be a DragonError")
	assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	assert.Contains(t, dragonErr.Error(), "request cannot be nil")
}

// Test MessagingStreamNotificationsRaw nil request validation
func TestClient_MessagingStreamNotificationsRaw_NilRequest(t *testing.T) {
	client := &Client{conn: nil}
	ctx := context.Background()

	stream, err := client.MessagingStreamNotificationsRaw(ctx, nil)

	assert.Nil(t, stream)
	require.Error(t, err)

	dragonErr, ok := err.(*errors.DragonError)
	require.True(t, ok, "error should be a DragonError")
	assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	assert.Contains(t, dragonErr.Error(), "request cannot be nil")
}

// Test MessagingWatchChannel nil request validation
func TestClient_MessagingWatchChannel_NilRequest(t *testing.T) {
	client := &Client{conn: nil}
	ctx := context.Background()

	resp, err := client.MessagingWatchChannel(ctx, nil)

	assert.Nil(t, resp)
	require.Error(t, err)

	dragonErr, ok := err.(*errors.DragonError)
	require.True(t, ok, "error should be a DragonError")
	assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	assert.Contains(t, dragonErr.Error(), "request cannot be nil")
}

// Test MessagingGetDedupStats nil request validation
func TestClient_MessagingGetDedupStats_NilRequest(t *testing.T) {
	client := &Client{conn: nil}
	ctx := context.Background()

	resp, err := client.MessagingGetDedupStats(ctx, nil)

	assert.Nil(t, resp)
	require.Error(t, err)

	dragonErr, ok := err.(*errors.DragonError)
	require.True(t, ok, "error should be a DragonError")
	assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	assert.Contains(t, dragonErr.Error(), "request cannot be nil")
}

// Test MessagingGetChannelStats nil request validation
func TestClient_MessagingGetChannelStats_NilRequest(t *testing.T) {
	client := &Client{conn: nil}
	ctx := context.Background()

	resp, err := client.MessagingGetChannelStats(ctx, nil)

	assert.Nil(t, resp)
	require.Error(t, err)

	dragonErr, ok := err.(*errors.DragonError)
	require.True(t, ok, "error should be a DragonError")
	assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	assert.Contains(t, dragonErr.Error(), "request cannot be nil")
}

// Test convertProtoChannel with nil input
func TestConvertProtoChannel_Nil(t *testing.T) {
	result := convertProtoChannel(nil)
	assert.Nil(t, result)
}

// Test convertProtoChannel with valid input
func TestConvertProtoChannel_Valid(t *testing.T) {
	now := time.Now().Unix()
	protoChannel := &pb.Channel{
		Name:        "test-channel",
		Codec:       "json",
		CreatedAt:   now,
		Subscribers: 5,
		Persistent:  true,
		MaxHistory:  100,
		BufferSize:  1024,
		TtlSeconds:  3600,
	}

	result := convertProtoChannel(protoChannel)

	require.NotNil(t, result)
	assert.Equal(t, "test-channel", result.Name)
	assert.Equal(t, "json", result.Codec)
	assert.Equal(t, time.Unix(now, 0), result.CreatedAt)
	assert.Equal(t, int64(5), result.Subscribers)
	assert.True(t, result.Persistent)
	assert.Equal(t, int32(100), result.MaxHistory)
	assert.Equal(t, int32(1024), result.BufferSize)
	assert.Equal(t, int64(3600), result.TTLSeconds)
}

// Test convertProtoSubscription with nil input
func TestConvertProtoSubscription_Nil(t *testing.T) {
	result := convertProtoSubscription(nil)
	assert.Nil(t, result)
}

// Test convertProtoSubscription with valid input
func TestConvertProtoSubscription_Valid(t *testing.T) {
	now := time.Now().Unix()
	protoSub := &pb.Subscription{
		Id:        "sub-123",
		Channel:   "test-channel",
		ClientId:  "client-456",
		CreatedAt: now,
		Filters: &pb.SubscriptionFilter{
			EventTypes: []pb.EventType{pb.EventType_EVENT_TYPE_PUT, pb.EventType_EVENT_TYPE_DELETE},
			KeyPrefix:  "/users/",
		},
	}

	result := convertProtoSubscription(protoSub)

	require.NotNil(t, result)
	assert.Equal(t, "sub-123", result.ID)
	assert.Equal(t, "test-channel", result.Channel)
	assert.Equal(t, "client-456", result.ClientID)
	assert.Equal(t, time.Unix(now, 0), result.CreatedAt)
	require.NotNil(t, result.Filter)
	assert.Equal(t, 2, len(result.Filter.EventTypes))
	assert.Equal(t, "put", result.Filter.EventTypes[0])
	assert.Equal(t, "delete", result.Filter.EventTypes[1])
	assert.Equal(t, "/users/", result.Filter.KeyPrefix)
}

// Test convertProtoSubscription without filter
func TestConvertProtoSubscription_NoFilter(t *testing.T) {
	protoSub := &pb.Subscription{
		Id:        "sub-123",
		Channel:   "test-channel",
		ClientId:  "client-456",
		CreatedAt: time.Now().Unix(),
	}

	result := convertProtoSubscription(protoSub)

	require.NotNil(t, result)
	assert.Nil(t, result.Filter)
}

// Test convertSubscriptionFilterToProto with nil input
func TestConvertSubscriptionFilterToProto_Nil(t *testing.T) {
	result := convertSubscriptionFilterToProto(nil)
	assert.Nil(t, result)
}

// Test convertSubscriptionFilterToProto with valid input
func TestConvertSubscriptionFilterToProto_Valid(t *testing.T) {
	filter := &MessagingSubscriptionFilter{
		EventTypes: []string{"put", "delete", "unknown"},
		KeyPrefix:  "/data/",
	}

	result := convertSubscriptionFilterToProto(filter)

	require.NotNil(t, result)
	assert.Equal(t, 3, len(result.EventTypes))
	assert.Equal(t, pb.EventType_EVENT_TYPE_PUT, result.EventTypes[0])
	assert.Equal(t, pb.EventType_EVENT_TYPE_DELETE, result.EventTypes[1])
	assert.Equal(t, pb.EventType_EVENT_TYPE_UNSPECIFIED, result.EventTypes[2])
	assert.Equal(t, "/data/", result.KeyPrefix)
}

// Test convertProtoEventType
func TestConvertProtoEventType(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.EventType
		expected string
	}{
		{
			name:     "put event type",
			input:    pb.EventType_EVENT_TYPE_PUT,
			expected: "put",
		},
		{
			name:     "delete event type",
			input:    pb.EventType_EVENT_TYPE_DELETE,
			expected: "delete",
		},
		{
			name:     "unspecified event type",
			input:    pb.EventType_EVENT_TYPE_UNSPECIFIED,
			expected: "unspecified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoEventType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test convertEventTypeToProto
func TestConvertEventTypeToProto(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected pb.EventType
	}{
		{
			name:     "put string",
			input:    "put",
			expected: pb.EventType_EVENT_TYPE_PUT,
		},
		{
			name:     "delete string",
			input:    "delete",
			expected: pb.EventType_EVENT_TYPE_DELETE,
		},
		{
			name:     "unknown string",
			input:    "unknown",
			expected: pb.EventType_EVENT_TYPE_UNSPECIFIED,
		},
		{
			name:     "empty string",
			input:    "",
			expected: pb.EventType_EVENT_TYPE_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertEventTypeToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test convertProtoMessage with nil input
func TestConvertProtoMessage_Nil(t *testing.T) {
	result := convertProtoMessage(nil)
	assert.Nil(t, result)
}

// Test convertProtoMessage with valid input
func TestConvertProtoMessage_Valid(t *testing.T) {
	now := time.Now().Unix()
	protoMsg := &pb.Message{
		Id:        "msg-123",
		Type:      pb.MessageType_MESSAGE_TYPE_NOTIFICATION,
		Channel:   "test-channel",
		Payload:   []byte("test payload"),
		Timestamp: now,
		Source:    "test-source",
		Metadata:  map[string]string{"key": "value"},
		Dedup: &pb.DedupInfo{
			IsDuplicate: true,
			Count:       3,
			FirstSeen:   now - 100,
		},
	}

	result := convertProtoMessage(protoMsg)

	require.NotNil(t, result)
	assert.Equal(t, "msg-123", result.ID)
	assert.Equal(t, "notification", result.Type)
	assert.Equal(t, "test-channel", result.Channel)
	assert.Equal(t, []byte("test payload"), result.Payload)
	assert.Equal(t, time.Unix(now, 0), result.Timestamp)
	assert.Equal(t, "test-source", result.Source)
	assert.Equal(t, "value", result.Metadata["key"])
	require.NotNil(t, result.Dedup)
	assert.True(t, result.Dedup.IsDuplicate)
	assert.Equal(t, int64(3), result.Dedup.Count)
}

// Test convertProtoMessage without dedup
func TestConvertProtoMessage_NoDedup(t *testing.T) {
	protoMsg := &pb.Message{
		Id:      "msg-123",
		Type:    pb.MessageType_MESSAGE_TYPE_DATA,
		Channel: "test-channel",
	}

	result := convertProtoMessage(protoMsg)

	require.NotNil(t, result)
	assert.Nil(t, result.Dedup)
}

// Test convertProtoMessageType
func TestConvertProtoMessageType(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.MessageType
		expected string
	}{
		{
			name:     "subscribe",
			input:    pb.MessageType_MESSAGE_TYPE_SUBSCRIBE,
			expected: "subscribe",
		},
		{
			name:     "unsubscribe",
			input:    pb.MessageType_MESSAGE_TYPE_UNSUBSCRIBE,
			expected: "unsubscribe",
		},
		{
			name:     "publish",
			input:    pb.MessageType_MESSAGE_TYPE_PUBLISH,
			expected: "publish",
		},
		{
			name:     "notification",
			input:    pb.MessageType_MESSAGE_TYPE_NOTIFICATION,
			expected: "notification",
		},
		{
			name:     "ping",
			input:    pb.MessageType_MESSAGE_TYPE_PING,
			expected: "ping",
		},
		{
			name:     "pong",
			input:    pb.MessageType_MESSAGE_TYPE_PONG,
			expected: "pong",
		},
		{
			name:     "ack",
			input:    pb.MessageType_MESSAGE_TYPE_ACK,
			expected: "ack",
		},
		{
			name:     "error",
			input:    pb.MessageType_MESSAGE_TYPE_ERROR,
			expected: "error",
		},
		{
			name:     "data",
			input:    pb.MessageType_MESSAGE_TYPE_DATA,
			expected: "data",
		},
		{
			name:     "control",
			input:    pb.MessageType_MESSAGE_TYPE_CONTROL,
			expected: "control",
		},
		{
			name:     "heartbeat",
			input:    pb.MessageType_MESSAGE_TYPE_HEARTBEAT,
			expected: "heartbeat",
		},
		{
			name:     "unspecified",
			input:    pb.MessageType_MESSAGE_TYPE_UNSPECIFIED,
			expected: "unspecified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoMessageType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test convertProtoNotification with nil input
func TestConvertProtoNotification_Nil(t *testing.T) {
	result := convertProtoNotification(nil)
	assert.Nil(t, result)
}

// Test convertProtoNotification with valid input
func TestConvertProtoNotification_Valid(t *testing.T) {
	protoNotif := &pb.MessagingNotification{
		Message: &pb.Message{
			Id:      "msg-123",
			Type:    pb.MessageType_MESSAGE_TYPE_NOTIFICATION,
			Channel: "test-channel",
		},
		SubscriptionId: "sub-456",
		SequenceNumber: 42,
	}

	result := convertProtoNotification(protoNotif)

	require.NotNil(t, result)
	require.NotNil(t, result.Message)
	assert.Equal(t, "msg-123", result.Message.ID)
	assert.Equal(t, "sub-456", result.SubscriptionID)
	assert.Equal(t, uint64(42), result.SequenceNumber)
}

// Test convertProtoDeltaPayload with nil input
func TestConvertProtoDeltaPayload_Nil(t *testing.T) {
	result := convertProtoDeltaPayload(nil)
	assert.Nil(t, result)
}

// Test convertProtoDeltaPayload with valid input
func TestConvertProtoDeltaPayload_Valid(t *testing.T) {
	now := time.Now().Unix()
	protoDelta := &pb.DeltaPayload{
		EntityId:    "entity-123",
		EntityType:  "user",
		Version:     5,
		PrevVersion: 4,
		Timestamp:   now,
		Changes: []*pb.FieldChange{
			{
				Path:      "name",
				Operation: pb.OpType_OP_TYPE_SET,
				Value:     []byte(`"John"`),
				OldValue:  []byte(`"Jane"`),
			},
			{
				Path:      "age",
				Operation: pb.OpType_OP_TYPE_INCREMENT,
				Value:     []byte("1"),
			},
		},
	}

	result := convertProtoDeltaPayload(protoDelta)

	require.NotNil(t, result)
	assert.Equal(t, "entity-123", result.EntityID)
	assert.Equal(t, "user", result.EntityType)
	assert.Equal(t, uint64(5), result.Version)
	assert.Equal(t, uint64(4), result.PrevVersion)
	assert.Equal(t, time.Unix(now, 0), result.Timestamp)
	require.Equal(t, 2, len(result.Changes))
	assert.Equal(t, "name", result.Changes[0].Path)
	assert.Equal(t, "set", result.Changes[0].Operation)
	assert.Equal(t, []byte(`"John"`), result.Changes[0].Value)
	assert.Equal(t, []byte(`"Jane"`), result.Changes[0].OldValue)
	assert.Equal(t, "increment", result.Changes[1].Operation)
}

// Test convertProtoOpType
func TestConvertProtoOpType(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.OpType
		expected string
	}{
		{
			name:     "set",
			input:    pb.OpType_OP_TYPE_SET,
			expected: "set",
		},
		{
			name:     "delete",
			input:    pb.OpType_OP_TYPE_DELETE,
			expected: "delete",
		},
		{
			name:     "append",
			input:    pb.OpType_OP_TYPE_APPEND,
			expected: "append",
		},
		{
			name:     "increment",
			input:    pb.OpType_OP_TYPE_INCREMENT,
			expected: "increment",
		},
		{
			name:     "unspecified",
			input:    pb.OpType_OP_TYPE_UNSPECIFIED,
			expected: "unspecified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoOpType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test convertProtoStats with nil input
func TestConvertProtoStats_Nil(t *testing.T) {
	result := convertProtoStats(nil)
	assert.Nil(t, result)
}

// Test convertProtoStats with valid input
func TestConvertProtoStats_Valid(t *testing.T) {
	protoStats := &pb.Stats{
		TotalChannels:      10,
		TotalSubscriptions: 50,
		MessagesPublished:  1000,
		MessagesDelivered:  950,
		DedupHits:          25,
		DroppedMessages:    5,
		BufferUtilization:  0.75,
	}

	result := convertProtoStats(protoStats)

	require.NotNil(t, result)
	assert.Equal(t, int64(10), result.TotalChannels)
	assert.Equal(t, int64(50), result.TotalSubscriptions)
	assert.Equal(t, int64(1000), result.MessagesPublished)
	assert.Equal(t, int64(950), result.MessagesDelivered)
	assert.Equal(t, int64(25), result.DedupHits)
	assert.Equal(t, int64(5), result.DroppedMessages)
	assert.Equal(t, 0.75, result.BufferUtilization)
}

// Test convertProtoChannelStats with nil input
func TestConvertProtoChannelStats_Nil(t *testing.T) {
	result := convertProtoChannelStats(nil)
	assert.Nil(t, result)
}

// Test convertProtoChannelStats with valid input
func TestConvertProtoChannelStats_Valid(t *testing.T) {
	protoStats := &pb.ChannelStats{
		Name:              "test-channel",
		Subscribers:       15,
		MessagesPublished: 500,
		MessagesInHistory: 100,
	}

	result := convertProtoChannelStats(protoStats)

	require.NotNil(t, result)
	assert.Equal(t, "test-channel", result.Name)
	assert.Equal(t, int64(15), result.Subscribers)
	assert.Equal(t, int64(500), result.MessagesPublished)
	assert.Equal(t, int32(100), result.MessagesInHistory)
}

// Test convertProtoDedupStats with nil input
func TestConvertProtoDedupStats_Nil(t *testing.T) {
	result := convertProtoDedupStats(nil)
	assert.Nil(t, result)
}

// Test convertProtoDedupStats with valid input
func TestConvertProtoDedupStats_Valid(t *testing.T) {
	now := time.Now().Unix()
	protoStats := &pb.DedupStats{
		Channel:         "test-channel",
		TotalMessages:   1000,
		TotalDuplicates: 50,
		TopDuplicates: []*pb.DedupEntry{
			{
				Count:       10,
				FirstSeen:   now - 3600,
				LastSeen:    now,
				MessageId:   "msg-1",
				PayloadHash: "abc123",
			},
			{
				Count:       5,
				FirstSeen:   now - 1800,
				LastSeen:    now - 100,
				MessageId:   "msg-2",
				PayloadHash: "def456",
			},
		},
	}

	result := convertProtoDedupStats(protoStats)

	require.NotNil(t, result)
	assert.Equal(t, "test-channel", result.Channel)
	assert.Equal(t, int64(1000), result.TotalMessages)
	assert.Equal(t, int64(50), result.TotalDuplicates)
	require.Equal(t, 2, len(result.TopDuplicates))
	assert.Equal(t, int64(10), result.TopDuplicates[0].Count)
	assert.Equal(t, "msg-1", result.TopDuplicates[0].MessageID)
	assert.Equal(t, "abc123", result.TopDuplicates[0].PayloadHash)
	assert.Equal(t, time.Unix(now-3600, 0), result.TopDuplicates[0].FirstSeen)
	assert.Equal(t, time.Unix(now, 0), result.TopDuplicates[0].LastSeen)
}

// Test MessagingCreateChannelRequest struct
func TestMessagingCreateChannelRequest_Fields(t *testing.T) {
	req := &MessagingCreateChannelRequest{
		Name:           "test-channel",
		Codec:          "json",
		Persistent:     true,
		MaxHistory:     100,
		BufferSize:     1024,
		MaxSubscribers: 50,
		TTLSeconds:     3600,
	}

	assert.Equal(t, "test-channel", req.Name)
	assert.Equal(t, "json", req.Codec)
	assert.True(t, req.Persistent)
	assert.Equal(t, int32(100), req.MaxHistory)
	assert.Equal(t, int32(1024), req.BufferSize)
	assert.Equal(t, int32(50), req.MaxSubscribers)
	assert.Equal(t, int64(3600), req.TTLSeconds)
}

// Test MessagingSubscribeRequest struct
func TestMessagingSubscribeRequest_Fields(t *testing.T) {
	req := &MessagingSubscribeRequest{
		Channel:   "test-channel",
		SessionID: "session-123",
		Filter: &MessagingSubscriptionFilter{
			EventTypes: []string{"put", "delete"},
			KeyPrefix:  "/users/",
		},
		DeltaMode:           true,
		IncludeOldValues:    true,
		DedupMetadata:       true,
		DedupSkipDuplicates: false,
		BufferSize:          512,
	}

	assert.Equal(t, "test-channel", req.Channel)
	assert.Equal(t, "session-123", req.SessionID)
	require.NotNil(t, req.Filter)
	assert.Equal(t, 2, len(req.Filter.EventTypes))
	assert.True(t, req.DeltaMode)
	assert.True(t, req.IncludeOldValues)
	assert.True(t, req.DedupMetadata)
	assert.False(t, req.DedupSkipDuplicates)
	assert.Equal(t, int32(512), req.BufferSize)
}

// Test MessagingPublishRequest struct
func TestMessagingPublishRequest_Fields(t *testing.T) {
	req := &MessagingPublishRequest{
		Channel:   "test-channel",
		Payload:   []byte("test message"),
		MessageID: "msg-123",
		DedupKey:  "dedup-key",
		Codec:     "json",
		Metadata:  map[string]string{"key": "value"},
	}

	assert.Equal(t, "test-channel", req.Channel)
	assert.Equal(t, []byte("test message"), req.Payload)
	assert.Equal(t, "msg-123", req.MessageID)
	assert.Equal(t, "dedup-key", req.DedupKey)
	assert.Equal(t, "json", req.Codec)
	assert.Equal(t, "value", req.Metadata["key"])
}

// Test MessagingPublishDeltaRequest struct
func TestMessagingPublishDeltaRequest_Fields(t *testing.T) {
	req := &MessagingPublishDeltaRequest{
		Channel:   "test-channel",
		OldValue:  []byte(`{"name":"old"}`),
		NewValue:  []byte(`{"name":"new"}`),
		MessageID: "msg-123",
		DedupKey:  "dedup-key",
		Codec:     "json",
		Metadata:  map[string]string{"key": "value"},
	}

	assert.Equal(t, "test-channel", req.Channel)
	assert.Equal(t, []byte(`{"name":"old"}`), req.OldValue)
	assert.Equal(t, []byte(`{"name":"new"}`), req.NewValue)
	assert.Equal(t, "msg-123", req.MessageID)
	assert.Equal(t, "dedup-key", req.DedupKey)
	assert.Equal(t, "json", req.Codec)
	assert.Equal(t, "value", req.Metadata["key"])
}

// Test MessagingStreamNotificationsRequest struct
func TestMessagingStreamNotificationsRequest_Fields(t *testing.T) {
	req := &MessagingStreamNotificationsRequest{
		SubscriptionID: "sub-123",
		StartSequence:  100,
		IncludeHistory: true,
		HistoryLimit:   50,
	}

	assert.Equal(t, "sub-123", req.SubscriptionID)
	assert.Equal(t, uint64(100), req.StartSequence)
	assert.True(t, req.IncludeHistory)
	assert.Equal(t, int32(50), req.HistoryLimit)
}

// Test MessagingWatchChannelRequest struct
func TestMessagingWatchChannelRequest_Fields(t *testing.T) {
	req := &MessagingWatchChannelRequest{
		KeyPrefix:  "/users/",
		SessionID:  "session-123",
		BufferSize: 256,
		Filter: &MessagingSubscriptionFilter{
			EventTypes: []string{"put"},
			KeyPrefix:  "/users/",
		},
	}

	assert.Equal(t, "/users/", req.KeyPrefix)
	assert.Equal(t, "session-123", req.SessionID)
	assert.Equal(t, int32(256), req.BufferSize)
	require.NotNil(t, req.Filter)
	assert.Equal(t, 1, len(req.Filter.EventTypes))
}

// Test response type structs
func TestMessagingResponseTypes(t *testing.T) {
	t.Run("CreateChannelResponse", func(t *testing.T) {
		now := time.Now()
		resp := &MessagingCreateChannelResponse{
			Channel: &MessagingChannel{
				Name: "test-channel",
			},
			Timestamp: now,
		}
		assert.Equal(t, "test-channel", resp.Channel.Name)
		assert.Equal(t, now, resp.Timestamp)
	})

	t.Run("DeleteChannelResponse", func(t *testing.T) {
		now := time.Now()
		resp := &MessagingDeleteChannelResponse{
			Name:      "test-channel",
			Deleted:   true,
			Timestamp: now,
		}
		assert.Equal(t, "test-channel", resp.Name)
		assert.True(t, resp.Deleted)
		assert.Equal(t, now, resp.Timestamp)
	})

	t.Run("SubscribeResponse", func(t *testing.T) {
		now := time.Now()
		resp := &MessagingSubscribeResponse{
			Subscription: &MessagingSubscription{
				ID:      "sub-123",
				Channel: "test-channel",
			},
			Timestamp: now,
		}
		assert.Equal(t, "sub-123", resp.Subscription.ID)
		assert.Equal(t, now, resp.Timestamp)
	})

	t.Run("UnsubscribeResponse", func(t *testing.T) {
		now := time.Now()
		resp := &MessagingUnsubscribeResponse{
			SubscriptionID: "sub-123",
			Unsubscribed:   true,
			Timestamp:      now,
		}
		assert.Equal(t, "sub-123", resp.SubscriptionID)
		assert.True(t, resp.Unsubscribed)
		assert.Equal(t, now, resp.Timestamp)
	})

	t.Run("PublishResponse", func(t *testing.T) {
		now := time.Now()
		resp := &MessagingPublishResponse{
			MessageID:   "msg-123",
			Channel:     "test-channel",
			IsDuplicate: false,
			Timestamp:   now,
		}
		assert.Equal(t, "msg-123", resp.MessageID)
		assert.Equal(t, "test-channel", resp.Channel)
		assert.False(t, resp.IsDuplicate)
		assert.Equal(t, now, resp.Timestamp)
	})

	t.Run("PublishDeltaResponse", func(t *testing.T) {
		now := time.Now()
		resp := &MessagingPublishDeltaResponse{
			MessageID:   "msg-123",
			Channel:     "test-channel",
			IsDuplicate: true,
			Timestamp:   now,
			Delta: &MessagingDeltaPayload{
				EntityID: "entity-1",
			},
		}
		assert.Equal(t, "msg-123", resp.MessageID)
		assert.True(t, resp.IsDuplicate)
		assert.Equal(t, "entity-1", resp.Delta.EntityID)
	})

	t.Run("WatchChannelResponse", func(t *testing.T) {
		now := time.Now()
		resp := &MessagingWatchChannelResponse{
			Subscription: &MessagingSubscription{
				ID: "sub-123",
			},
			ChannelName: "watch-channel-123",
			Timestamp:   now,
		}
		assert.Equal(t, "sub-123", resp.Subscription.ID)
		assert.Equal(t, "watch-channel-123", resp.ChannelName)
		assert.Equal(t, now, resp.Timestamp)
	})

	t.Run("GetStatsResponse", func(t *testing.T) {
		now := time.Now()
		resp := &MessagingGetStatsResponse{
			Stats: &MessagingStats{
				TotalChannels: 10,
			},
			IsRunning: true,
			Timestamp: now,
		}
		assert.Equal(t, int64(10), resp.Stats.TotalChannels)
		assert.True(t, resp.IsRunning)
		assert.Equal(t, now, resp.Timestamp)
	})

	t.Run("GetDedupStatsResponse", func(t *testing.T) {
		now := time.Now()
		resp := &MessagingGetDedupStatsResponse{
			Stats: &MessagingDedupStats{
				Channel:       "test-channel",
				TotalMessages: 100,
			},
			Timestamp: now,
		}
		assert.Equal(t, "test-channel", resp.Stats.Channel)
		assert.Equal(t, int64(100), resp.Stats.TotalMessages)
		assert.Equal(t, now, resp.Timestamp)
	})

	t.Run("GetChannelStatsResponse", func(t *testing.T) {
		now := time.Now()
		resp := &MessagingGetChannelStatsResponse{
			Stats: &MessagingChannelStats{
				Name:        "test-channel",
				Subscribers: 5,
			},
			Timestamp: now,
		}
		assert.Equal(t, "test-channel", resp.Stats.Name)
		assert.Equal(t, int64(5), resp.Stats.Subscribers)
		assert.Equal(t, now, resp.Timestamp)
	})
}

// Test MessagingNotification struct
func TestMessagingNotification_Fields(t *testing.T) {
	notif := &MessagingNotification{
		Message: &MessagingMessage{
			ID:      "msg-123",
			Type:    "notification",
			Channel: "test-channel",
		},
		SubscriptionID: "sub-456",
		SequenceNumber: 42,
	}

	require.NotNil(t, notif.Message)
	assert.Equal(t, "msg-123", notif.Message.ID)
	assert.Equal(t, "notification", notif.Message.Type)
	assert.Equal(t, "sub-456", notif.SubscriptionID)
	assert.Equal(t, uint64(42), notif.SequenceNumber)
}

// Test MessagingDeltaPayload struct
func TestMessagingDeltaPayload_Fields(t *testing.T) {
	now := time.Now()
	delta := &MessagingDeltaPayload{
		EntityID:    "entity-123",
		EntityType:  "user",
		Version:     5,
		PrevVersion: 4,
		Timestamp:   now,
		Changes: []*MessagingFieldChange{
			{
				Path:      "name",
				Operation: "set",
				Value:     []byte(`"John"`),
				OldValue:  []byte(`"Jane"`),
			},
		},
	}

	assert.Equal(t, "entity-123", delta.EntityID)
	assert.Equal(t, "user", delta.EntityType)
	assert.Equal(t, uint64(5), delta.Version)
	assert.Equal(t, uint64(4), delta.PrevVersion)
	assert.Equal(t, now, delta.Timestamp)
	require.Equal(t, 1, len(delta.Changes))
	assert.Equal(t, "name", delta.Changes[0].Path)
	assert.Equal(t, "set", delta.Changes[0].Operation)
}

// Test MessagingDedupEntry struct
func TestMessagingDedupEntry_Fields(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Hour)
	entry := &MessagingDedupEntry{
		Count:       10,
		FirstSeen:   earlier,
		LastSeen:    now,
		MessageID:   "msg-123",
		PayloadHash: "abc123def456",
	}

	assert.Equal(t, int64(10), entry.Count)
	assert.Equal(t, earlier, entry.FirstSeen)
	assert.Equal(t, now, entry.LastSeen)
	assert.Equal(t, "msg-123", entry.MessageID)
	assert.Equal(t, "abc123def456", entry.PayloadHash)
}

// Test MessagingChannel struct
func TestMessagingChannel_Fields(t *testing.T) {
	now := time.Now()
	channel := &MessagingChannel{
		Name:        "test-channel",
		Codec:       "json",
		CreatedAt:   now,
		Subscribers: 10,
		Persistent:  true,
		MaxHistory:  100,
		BufferSize:  1024,
		TTLSeconds:  3600,
	}

	assert.Equal(t, "test-channel", channel.Name)
	assert.Equal(t, "json", channel.Codec)
	assert.Equal(t, now, channel.CreatedAt)
	assert.Equal(t, int64(10), channel.Subscribers)
	assert.True(t, channel.Persistent)
	assert.Equal(t, int32(100), channel.MaxHistory)
	assert.Equal(t, int32(1024), channel.BufferSize)
	assert.Equal(t, int64(3600), channel.TTLSeconds)
}

// Test MessagingSubscription struct
func TestMessagingSubscription_Fields(t *testing.T) {
	now := time.Now()
	sub := &MessagingSubscription{
		ID:        "sub-123",
		Channel:   "test-channel",
		ClientID:  "client-456",
		CreatedAt: now,
		Filter: &MessagingSubscriptionFilter{
			EventTypes: []string{"put", "delete"},
			KeyPrefix:  "/users/",
		},
	}

	assert.Equal(t, "sub-123", sub.ID)
	assert.Equal(t, "test-channel", sub.Channel)
	assert.Equal(t, "client-456", sub.ClientID)
	assert.Equal(t, now, sub.CreatedAt)
	require.NotNil(t, sub.Filter)
	assert.Equal(t, 2, len(sub.Filter.EventTypes))
	assert.Equal(t, "/users/", sub.Filter.KeyPrefix)
}

// Test MessagingStats struct
func TestMessagingStats_Fields(t *testing.T) {
	stats := &MessagingStats{
		TotalChannels:      10,
		TotalSubscriptions: 50,
		MessagesPublished:  1000,
		MessagesDelivered:  950,
		DedupHits:          25,
		DroppedMessages:    5,
		BufferUtilization:  0.75,
	}

	assert.Equal(t, int64(10), stats.TotalChannels)
	assert.Equal(t, int64(50), stats.TotalSubscriptions)
	assert.Equal(t, int64(1000), stats.MessagesPublished)
	assert.Equal(t, int64(950), stats.MessagesDelivered)
	assert.Equal(t, int64(25), stats.DedupHits)
	assert.Equal(t, int64(5), stats.DroppedMessages)
	assert.Equal(t, 0.75, stats.BufferUtilization)
}

// Test MessagingDedupStats struct
func TestMessagingDedupStats_Fields(t *testing.T) {
	now := time.Now()
	stats := &MessagingDedupStats{
		Channel:         "test-channel",
		TotalMessages:   1000,
		TotalDuplicates: 50,
		TopDuplicates: []*MessagingDedupEntry{
			{
				Count:       10,
				FirstSeen:   now.Add(-time.Hour),
				LastSeen:    now,
				MessageID:   "msg-1",
				PayloadHash: "abc123",
			},
		},
	}

	assert.Equal(t, "test-channel", stats.Channel)
	assert.Equal(t, int64(1000), stats.TotalMessages)
	assert.Equal(t, int64(50), stats.TotalDuplicates)
	require.Equal(t, 1, len(stats.TopDuplicates))
	assert.Equal(t, int64(10), stats.TopDuplicates[0].Count)
}

// Test MessagingMessage struct
func TestMessagingMessage_Fields(t *testing.T) {
	now := time.Now()
	msg := &MessagingMessage{
		ID:        "msg-123",
		Type:      "notification",
		Channel:   "test-channel",
		Payload:   []byte("test payload"),
		Timestamp: now,
		Source:    "test-source",
		Metadata:  map[string]string{"key": "value"},
		Dedup: &MessagingDedupInfo{
			IsDuplicate: false,
			Count:       1,
			FirstSeen:   now,
		},
	}

	assert.Equal(t, "msg-123", msg.ID)
	assert.Equal(t, "notification", msg.Type)
	assert.Equal(t, "test-channel", msg.Channel)
	assert.Equal(t, []byte("test payload"), msg.Payload)
	assert.Equal(t, now, msg.Timestamp)
	assert.Equal(t, "test-source", msg.Source)
	assert.Equal(t, "value", msg.Metadata["key"])
	require.NotNil(t, msg.Dedup)
	assert.False(t, msg.Dedup.IsDuplicate)
}

// Test convertProtoDedupStats with empty TopDuplicates
func TestConvertProtoDedupStats_EmptyTopDuplicates(t *testing.T) {
	protoStats := &pb.DedupStats{
		Channel:         "test-channel",
		TotalMessages:   100,
		TotalDuplicates: 10,
		TopDuplicates:   []*pb.DedupEntry{},
	}

	result := convertProtoDedupStats(protoStats)

	require.NotNil(t, result)
	assert.Equal(t, "test-channel", result.Channel)
	assert.Equal(t, 0, len(result.TopDuplicates))
}

// Test convertProtoDeltaPayload with empty Changes
func TestConvertProtoDeltaPayload_EmptyChanges(t *testing.T) {
	protoDelta := &pb.DeltaPayload{
		EntityId:   "entity-123",
		EntityType: "user",
		Version:    5,
		Changes:    []*pb.FieldChange{},
	}

	result := convertProtoDeltaPayload(protoDelta)

	require.NotNil(t, result)
	assert.Equal(t, "entity-123", result.EntityID)
	assert.Equal(t, 0, len(result.Changes))
}

// Test convertSubscriptionFilterToProto with empty EventTypes
func TestConvertSubscriptionFilterToProto_EmptyEventTypes(t *testing.T) {
	filter := &MessagingSubscriptionFilter{
		EventTypes: []string{},
		KeyPrefix:  "/data/",
	}

	result := convertSubscriptionFilterToProto(filter)

	require.NotNil(t, result)
	assert.Equal(t, 0, len(result.EventTypes))
	assert.Equal(t, "/data/", result.KeyPrefix)
}
