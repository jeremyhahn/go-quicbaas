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

// Test that every messaging method rejects a nil request before it touches
// the connection.
func TestClient_Messaging_NilRequest(t *testing.T) {
	tests := []struct {
		name string
		call func(ctx context.Context, c *Client) ([]any, error)
	}{
		{"CreateChannel", func(ctx context.Context, c *Client) ([]any, error) {
			resp, err := c.MessagingCreateChannel(ctx, nil)
			return []any{resp}, err
		}},
		{"DeleteChannel", func(ctx context.Context, c *Client) ([]any, error) {
			resp, err := c.MessagingDeleteChannel(ctx, nil)
			return []any{resp}, err
		}},
		{"GetChannel", func(ctx context.Context, c *Client) ([]any, error) {
			resp, err := c.MessagingGetChannel(ctx, nil)
			return []any{resp}, err
		}},
		{"Subscribe", func(ctx context.Context, c *Client) ([]any, error) {
			resp, err := c.MessagingSubscribe(ctx, nil)
			return []any{resp}, err
		}},
		{"Unsubscribe", func(ctx context.Context, c *Client) ([]any, error) {
			resp, err := c.MessagingUnsubscribe(ctx, nil)
			return []any{resp}, err
		}},
		{"Publish", func(ctx context.Context, c *Client) ([]any, error) {
			resp, err := c.MessagingPublish(ctx, nil)
			return []any{resp}, err
		}},
		{"PublishDelta", func(ctx context.Context, c *Client) ([]any, error) {
			resp, err := c.MessagingPublishDelta(ctx, nil)
			return []any{resp}, err
		}},
		{"StreamNotifications", func(ctx context.Context, c *Client) ([]any, error) {
			notifChan, errChan, err := c.MessagingStreamNotifications(ctx, nil)
			return []any{notifChan, errChan}, err
		}},
		{"StreamNotificationsRaw", func(ctx context.Context, c *Client) ([]any, error) {
			stream, err := c.MessagingStreamNotificationsRaw(ctx, nil)
			return []any{stream}, err
		}},
		{"WatchChannel", func(ctx context.Context, c *Client) ([]any, error) {
			resp, err := c.MessagingWatchChannel(ctx, nil)
			return []any{resp}, err
		}},
		{"GetDedupStats", func(ctx context.Context, c *Client) ([]any, error) {
			resp, err := c.MessagingGetDedupStats(ctx, nil)
			return []any{resp}, err
		}},
		{"GetChannelStats", func(ctx context.Context, c *Client) ([]any, error) {
			resp, err := c.MessagingGetChannelStats(ctx, nil)
			return []any{resp}, err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := tt.call(context.Background(), &Client{conn: nil})

			for _, result := range results {
				assert.Nil(t, result)
			}
			require.Error(t, err)

			qbErr, ok := err.(*errors.QBError)
			require.True(t, ok, "error should be a QBError")
			assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
			assert.Contains(t, qbErr.Error(), "request cannot be nil")
		})
	}
}

// Note: MessagingListChannels accepts nil request and creates a default.
// Testing nil request behavior requires a live connection (integration test).

// testUnix is a fixed Unix timestamp for the converter tables.
const testUnix int64 = 1700000000

// Test convertProtoChannel
func TestConvertProtoChannel(t *testing.T) {
	tests := []struct {
		name     string
		input    *pb.Channel
		expected *MessagingChannel
	}{
		{name: "nil", input: nil, expected: nil},
		{
			name: "all fields",
			input: &pb.Channel{
				Name:        "test-channel",
				Codec:       "json",
				CreatedAt:   testUnix,
				Subscribers: 5,
				Persistent:  true,
				MaxHistory:  100,
				BufferSize:  1024,
				TtlSeconds:  3600,
			},
			expected: &MessagingChannel{
				Name:        "test-channel",
				Codec:       "json",
				CreatedAt:   time.Unix(testUnix, 0),
				Subscribers: 5,
				Persistent:  true,
				MaxHistory:  100,
				BufferSize:  1024,
				TTLSeconds:  3600,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, convertProtoChannel(tt.input))
		})
	}
}

// Test convertProtoSubscription
func TestConvertProtoSubscription(t *testing.T) {
	tests := []struct {
		name     string
		input    *pb.Subscription
		expected *MessagingSubscription
	}{
		{name: "nil", input: nil, expected: nil},
		{
			name: "with filter",
			input: &pb.Subscription{
				Id:        "sub-123",
				Channel:   "test-channel",
				ClientId:  "client-456",
				CreatedAt: testUnix,
				Filters: &pb.SubscriptionFilter{
					EventTypes: []pb.EventType{pb.EventType_EVENT_TYPE_PUT, pb.EventType_EVENT_TYPE_DELETE},
					KeyPrefix:  "/users/",
				},
			},
			expected: &MessagingSubscription{
				ID:        "sub-123",
				Channel:   "test-channel",
				ClientID:  "client-456",
				CreatedAt: time.Unix(testUnix, 0),
				Filter: &MessagingSubscriptionFilter{
					EventTypes: []string{"put", "delete"},
					KeyPrefix:  "/users/",
				},
			},
		},
		{
			name: "without filter",
			input: &pb.Subscription{
				Id:        "sub-123",
				Channel:   "test-channel",
				ClientId:  "client-456",
				CreatedAt: testUnix,
			},
			expected: &MessagingSubscription{
				ID:        "sub-123",
				Channel:   "test-channel",
				ClientID:  "client-456",
				CreatedAt: time.Unix(testUnix, 0),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, convertProtoSubscription(tt.input))
		})
	}
}

// Test convertSubscriptionFilterToProto
func TestConvertSubscriptionFilterToProto(t *testing.T) {
	tests := []struct {
		name     string
		input    *MessagingSubscriptionFilter
		expected *pb.SubscriptionFilter
	}{
		{name: "nil", input: nil, expected: nil},
		{
			name: "known and unknown event types",
			input: &MessagingSubscriptionFilter{
				EventTypes: []string{"put", "delete", "unknown"},
				KeyPrefix:  "/data/",
			},
			expected: &pb.SubscriptionFilter{
				EventTypes: []pb.EventType{
					pb.EventType_EVENT_TYPE_PUT,
					pb.EventType_EVENT_TYPE_DELETE,
					pb.EventType_EVENT_TYPE_UNSPECIFIED,
				},
				KeyPrefix: "/data/",
			},
		},
		{
			name: "empty event types",
			input: &MessagingSubscriptionFilter{
				EventTypes: []string{},
				KeyPrefix:  "/data/",
			},
			expected: &pb.SubscriptionFilter{
				EventTypes: []pb.EventType{},
				KeyPrefix:  "/data/",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, convertSubscriptionFilterToProto(tt.input))
		})
	}
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

// Test convertProtoMessage
func TestConvertProtoMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    *pb.Message
		expected *MessagingMessage
	}{
		{name: "nil", input: nil, expected: nil},
		{
			name: "with dedup",
			input: &pb.Message{
				Id:        "msg-123",
				Type:      pb.MessageType_MESSAGE_TYPE_NOTIFICATION,
				Channel:   "test-channel",
				Payload:   []byte("test payload"),
				Timestamp: testUnix,
				Source:    "test-source",
				Metadata:  map[string]string{"key": "value"},
				Dedup: &pb.DedupInfo{
					IsDuplicate: true,
					Count:       3,
					FirstSeen:   testUnix - 100,
				},
			},
			expected: &MessagingMessage{
				ID:        "msg-123",
				Type:      "notification",
				Channel:   "test-channel",
				Payload:   []byte("test payload"),
				Timestamp: time.Unix(testUnix, 0),
				Source:    "test-source",
				Metadata:  map[string]string{"key": "value"},
				Dedup: &MessagingDedupInfo{
					IsDuplicate: true,
					Count:       3,
					FirstSeen:   time.Unix(testUnix-100, 0),
				},
			},
		},
		{
			name: "without dedup",
			input: &pb.Message{
				Id:      "msg-123",
				Type:    pb.MessageType_MESSAGE_TYPE_DATA,
				Channel: "test-channel",
			},
			expected: &MessagingMessage{
				ID:        "msg-123",
				Type:      "data",
				Channel:   "test-channel",
				Timestamp: time.Unix(0, 0),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, convertProtoMessage(tt.input))
		})
	}
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

// Test convertProtoNotification
func TestConvertProtoNotification(t *testing.T) {
	tests := []struct {
		name     string
		input    *pb.MessagingNotification
		expected *MessagingNotification
	}{
		{name: "nil", input: nil, expected: nil},
		{
			name: "with message",
			input: &pb.MessagingNotification{
				Message: &pb.Message{
					Id:      "msg-123",
					Type:    pb.MessageType_MESSAGE_TYPE_NOTIFICATION,
					Channel: "test-channel",
				},
				SubscriptionId: "sub-456",
				SequenceNumber: 42,
			},
			expected: &MessagingNotification{
				Message: &MessagingMessage{
					ID:        "msg-123",
					Type:      "notification",
					Channel:   "test-channel",
					Timestamp: time.Unix(0, 0),
				},
				SubscriptionID: "sub-456",
				SequenceNumber: 42,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, convertProtoNotification(tt.input))
		})
	}
}

// Test convertProtoDeltaPayload
func TestConvertProtoDeltaPayload(t *testing.T) {
	tests := []struct {
		name     string
		input    *pb.DeltaPayload
		expected *MessagingDeltaPayload
	}{
		{name: "nil", input: nil, expected: nil},
		{
			name: "with changes",
			input: &pb.DeltaPayload{
				EntityId:    "entity-123",
				EntityType:  "user",
				Version:     5,
				PrevVersion: 4,
				Timestamp:   testUnix,
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
			},
			expected: &MessagingDeltaPayload{
				EntityID:    "entity-123",
				EntityType:  "user",
				Version:     5,
				PrevVersion: 4,
				Timestamp:   time.Unix(testUnix, 0),
				Changes: []*MessagingFieldChange{
					{
						Path:      "name",
						Operation: "set",
						Value:     []byte(`"John"`),
						OldValue:  []byte(`"Jane"`),
					},
					{
						Path:      "age",
						Operation: "increment",
						Value:     []byte("1"),
					},
				},
			},
		},
		{
			name: "empty changes",
			input: &pb.DeltaPayload{
				EntityId:   "entity-123",
				EntityType: "user",
				Version:    5,
				Changes:    []*pb.FieldChange{},
			},
			expected: &MessagingDeltaPayload{
				EntityID:   "entity-123",
				EntityType: "user",
				Version:    5,
				Timestamp:  time.Unix(0, 0),
				Changes:    []*MessagingFieldChange{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, convertProtoDeltaPayload(tt.input))
		})
	}
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

// Test convertProtoStats
func TestConvertProtoStats(t *testing.T) {
	tests := []struct {
		name     string
		input    *pb.Stats
		expected *MessagingStats
	}{
		{name: "nil", input: nil, expected: nil},
		{
			name: "all fields",
			input: &pb.Stats{
				TotalChannels:      10,
				TotalSubscriptions: 50,
				MessagesPublished:  1000,
				MessagesDelivered:  950,
				DedupHits:          25,
				DroppedMessages:    5,
				BufferUtilization:  0.75,
			},
			expected: &MessagingStats{
				TotalChannels:      10,
				TotalSubscriptions: 50,
				MessagesPublished:  1000,
				MessagesDelivered:  950,
				DedupHits:          25,
				DroppedMessages:    5,
				BufferUtilization:  0.75,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, convertProtoStats(tt.input))
		})
	}
}

// Test convertProtoChannelStats
func TestConvertProtoChannelStats(t *testing.T) {
	tests := []struct {
		name     string
		input    *pb.ChannelStats
		expected *MessagingChannelStats
	}{
		{name: "nil", input: nil, expected: nil},
		{
			name: "all fields",
			input: &pb.ChannelStats{
				Name:              "test-channel",
				Subscribers:       15,
				MessagesPublished: 500,
				MessagesInHistory: 100,
			},
			expected: &MessagingChannelStats{
				Name:              "test-channel",
				Subscribers:       15,
				MessagesPublished: 500,
				MessagesInHistory: 100,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, convertProtoChannelStats(tt.input))
		})
	}
}

// Test convertProtoDedupStats
func TestConvertProtoDedupStats(t *testing.T) {
	tests := []struct {
		name     string
		input    *pb.DedupStats
		expected *MessagingDedupStats
	}{
		{name: "nil", input: nil, expected: nil},
		{
			name: "with top duplicates",
			input: &pb.DedupStats{
				Channel:         "test-channel",
				TotalMessages:   1000,
				TotalDuplicates: 50,
				TopDuplicates: []*pb.DedupEntry{
					{
						Count:       10,
						FirstSeen:   testUnix - 3600,
						LastSeen:    testUnix,
						MessageId:   "msg-1",
						PayloadHash: "abc123",
					},
					{
						Count:       5,
						FirstSeen:   testUnix - 1800,
						LastSeen:    testUnix - 100,
						MessageId:   "msg-2",
						PayloadHash: "def456",
					},
				},
			},
			expected: &MessagingDedupStats{
				Channel:         "test-channel",
				TotalMessages:   1000,
				TotalDuplicates: 50,
				TopDuplicates: []*MessagingDedupEntry{
					{
						Count:       10,
						FirstSeen:   time.Unix(testUnix-3600, 0),
						LastSeen:    time.Unix(testUnix, 0),
						MessageID:   "msg-1",
						PayloadHash: "abc123",
					},
					{
						Count:       5,
						FirstSeen:   time.Unix(testUnix-1800, 0),
						LastSeen:    time.Unix(testUnix-100, 0),
						MessageID:   "msg-2",
						PayloadHash: "def456",
					},
				},
			},
		},
		{
			name: "empty top duplicates",
			input: &pb.DedupStats{
				Channel:         "test-channel",
				TotalMessages:   100,
				TotalDuplicates: 10,
				TopDuplicates:   []*pb.DedupEntry{},
			},
			expected: &MessagingDedupStats{
				Channel:         "test-channel",
				TotalMessages:   100,
				TotalDuplicates: 10,
				TopDuplicates:   []*MessagingDedupEntry{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, convertProtoDedupStats(tt.input))
		})
	}
}
