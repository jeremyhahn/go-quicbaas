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

package quic

import (
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsMessagingService(t *testing.T) {
	tests := []struct {
		svc      string
		expected bool
	}{
		{"quicbaas.Messaging", true},
		{"Messaging", true},
		{"quicbaas.Scheduler", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, isMessagingService(tt.svc), tt.svc)
	}
}

// TestMessagingMethodToSubOp proves every messaging method maps to a sub-op in
// the 0x20-0x2F range, disjoint from the scheduler range (0x01-0x1F) that shares
// the same OpMsg=0x00 dispatch byte on the server.
func TestMessagingMethodToSubOp(t *testing.T) {
	cases := map[string]byte{
		"CreateChannel":   SubOpMsgCreateChannel,
		"DeleteChannel":   SubOpMsgDeleteChannel,
		"GetChannel":      SubOpMsgGetChannel,
		"ListChannels":    SubOpMsgListChannels,
		"Subscribe":       SubOpMsgSubscribe,
		"Unsubscribe":     SubOpMsgUnsubscribe,
		"Publish":         SubOpMsgPublish,
		"PublishDelta":    SubOpMsgPublishDelta,
		"GetStats":        SubOpMsgGetStats,
		"GetDedupStats":   SubOpMsgDedupStats,
		"GetChannelStats": SubOpMsgDedupStats,
	}
	for method, want := range cases {
		got, ok := MessagingMethodToSubOp(method)
		require.True(t, ok, "method %s should resolve", method)
		assert.Equal(t, want, got, method)
		assert.GreaterOrEqual(t, got, byte(0x20), method)
		assert.LessOrEqual(t, got, byte(0x2F), method)
	}
	_, ok := MessagingMethodToSubOp("Bogus")
	assert.False(t, ok)

	// All messaging methods dispatch on OpMsg (0x00).
	op, ok := MethodToOpcode("quicbaas.Messaging", "CreateChannel")
	require.True(t, ok)
	assert.Equal(t, OpMsg, op)
}

func TestTransformMessagingRequest_CreateChannel(t *testing.T) {
	out, err := transformMessagingRequest("CreateChannel", &messaging.CreateChannelRequest{
		Name:       "events",
		Codec:      messaging.CodecJSON,
		Persistent: true,
		BufferSize: 128,
		MaxHistory: 50,
		TTL:        90 * time.Second,
	})
	require.NoError(t, err)
	wire, ok := out.(*msgCreateChannelWire)
	require.True(t, ok, "got %T", out)
	assert.Equal(t, "events", wire.Name)
	assert.Equal(t, "json", wire.Codec)
	assert.True(t, wire.Persistent)
	assert.Equal(t, 128, wire.BufferSize)
	assert.Equal(t, 50, wire.MaxHistory)
	assert.Equal(t, int64(90), wire.TTLSeconds)
}

func TestTransformMessagingRequest_NameAndIDRequests(t *testing.T) {
	del, err := transformMessagingRequest("DeleteChannel", &messaging.DeleteChannelRequest{Name: "events"})
	require.NoError(t, err)
	assert.Equal(t, "events", del.(*msgNameWire).Name)

	get, err := transformMessagingRequest("GetChannel", &messaging.GetChannelRequest{Name: "events"})
	require.NoError(t, err)
	assert.Equal(t, "events", get.(*msgNameWire).Name)

	unsub, err := transformMessagingRequest("Unsubscribe", &messaging.UnsubscribeRequest{SubscriptionID: "sub-1"})
	require.NoError(t, err)
	assert.Equal(t, "sub-1", unsub.(*msgUnsubscribeWire).SubscriptionID)

	stats, err := transformMessagingRequest("GetChannelStats", &struct{ Channel string }{Channel: "events"})
	require.NoError(t, err)
	assert.Equal(t, "events", stats.(*msgDedupStatsWire).Channel)
}

func TestTransformMessagingRequest_Subscribe(t *testing.T) {
	out, err := transformMessagingRequest("Subscribe", &messaging.SubscribeRequest{
		Channel:    "events",
		SessionID:  "sess-1",
		BufferSize: 64,
		Filter: &messaging.SubscriptionFilter{
			KeyPrefix:  "users/",
			EventTypes: []messaging.EventType{messaging.EventTypePut, messaging.EventTypeDelete},
		},
	})
	require.NoError(t, err)
	wire := out.(*msgSubscribeWire)
	assert.Equal(t, "events", wire.Channel)
	assert.Equal(t, "sess-1", wire.SessionID)
	assert.Equal(t, 64, wire.BufferSize)
	assert.Equal(t, "users/", wire.KeyPrefix)
	assert.Equal(t, []string{"PUT", "DELETE"}, wire.EventTypes)
}

func TestTransformMessagingRequest_Publish(t *testing.T) {
	out, err := transformMessagingRequest("Publish", &messaging.PublishRequest{
		Channel:   "events",
		Payload:   []byte(`{"k":"v"}`),
		MessageID: "m1",
		DedupKey:  "d1",
		Metadata:  map[string]string{"src": "test"},
	})
	require.NoError(t, err)
	wire := out.(*msgPublishWire)
	assert.Equal(t, "events", wire.Channel)
	assert.JSONEq(t, `{"k":"v"}`, string(wire.Payload))
	assert.Equal(t, "m1", wire.MessageID)
	assert.Equal(t, "d1", wire.DedupKey)
}

func TestTransformMessagingRequest_PublishNonJSONPayload(t *testing.T) {
	out, err := transformMessagingRequest("Publish", &messaging.PublishRequest{
		Channel: "events",
		Payload: []byte("raw-bytes"),
	})
	require.NoError(t, err)
	wire := out.(*msgPublishWire)
	// Non-JSON bytes are encoded as a base64 JSON string value.
	assert.NotEmpty(t, wire.Payload)
}

func TestTransformMessagingRequest_ListAndStats(t *testing.T) {
	listOut, err := transformMessagingRequest("ListChannels", &messaging.ListChannelsRequest{})
	require.NoError(t, err)
	_, ok := listOut.(*msgListChannelsWire)
	assert.True(t, ok, "got %T", listOut)

	statsOut, err := transformMessagingRequest("GetStats", &struct{}{})
	require.NoError(t, err)
	_, ok = statsOut.(*msgGetStatsWire)
	assert.True(t, ok, "got %T", statsOut)
}

func TestTransformMessagingRequest_Errors(t *testing.T) {
	_, err := transformMessagingRequest("Bogus", &struct{}{})
	require.Error(t, err)

	_, err = transformMessagingRequest("CreateChannel", "not-a-request")
	require.Error(t, err)

	_, err = transformMessagingRequest("DeleteChannel", &messaging.DeleteChannelRequest{Name: ""})
	require.Error(t, err)

	_, err = transformMessagingRequest("Subscribe", 42)
	require.Error(t, err)
}

// TestDecodeMessagingResponse_Channel proves the server's messaging.Channel
// (untagged PascalCase fields, emitted via EncodePayload) round-trips into the
// SDK *messaging.Channel the client asserts on.
func TestDecodeMessagingResponse_Channel(t *testing.T) {
	created := time.Unix(1700000000, 0).UTC()
	// The server's messaging.Channel marshals untagged (PascalCase) exactly like
	// the SDK Channel, so we emit the SDK type to reproduce the wire bytes.
	want := &messaging.Channel{
		Name:        "events",
		Codec:       "json",
		CreatedAt:   created,
		Subscribers: 3,
		Persistent:  true,
		MaxHistory:  100,
		BufferSize:  256,
		TTL:         60 * time.Second,
	}
	payload, err := EncodePayload(want)
	require.NoError(t, err)

	got, err := decodeMessagingResponse("GetChannel", payload)
	require.NoError(t, err)
	ch, ok := got.(*messaging.Channel)
	require.True(t, ok, "got %T", got)
	assert.Equal(t, "events", ch.Name)
	assert.Equal(t, int64(3), ch.Subscribers)
	assert.True(t, ch.Persistent)
	assert.Equal(t, 100, ch.MaxHistory)
	assert.Equal(t, 60*time.Second, ch.TTL)
	assert.Equal(t, created, ch.CreatedAt)
}

func TestDecodeMessagingResponse_ListChannels(t *testing.T) {
	channels := []*messaging.Channel{
		{Name: "a", Codec: "json"},
		{Name: "b", Codec: "msgpack"},
	}
	payload, err := EncodePayload(channels)
	require.NoError(t, err)

	got, err := decodeMessagingResponse("ListChannels", payload)
	require.NoError(t, err)
	resp, ok := got.(*messaging.ListChannelsResponse)
	require.True(t, ok, "got %T", got)
	require.Len(t, resp.Channels, 2)
	assert.Equal(t, "a", resp.Channels[0].Name)
	assert.Equal(t, "b", resp.Channels[1].Name)

	// Empty payload yields an empty list, not nil.
	got, err = decodeMessagingResponse("ListChannels", nil)
	require.NoError(t, err)
	resp = got.(*messaging.ListChannelsResponse)
	assert.NotNil(t, resp.Channels)
	assert.Empty(t, resp.Channels)
}

func TestDecodeMessagingResponse_Subscribe(t *testing.T) {
	wire := &msgSubscribeRespWire{SubscriptionID: "sub-1", Channel: "events", ClientID: "client-1"}
	payload, err := EncodePayload(wire)
	require.NoError(t, err)

	got, err := decodeMessagingResponse("Subscribe", payload)
	require.NoError(t, err)
	sub, ok := got.(*messaging.Subscription)
	require.True(t, ok, "got %T", got)
	assert.Equal(t, "sub-1", sub.ID)
	assert.Equal(t, "events", sub.Channel)
	assert.Equal(t, "client-1", sub.ClientID)
}

func TestDecodeMessagingResponse_Stats(t *testing.T) {
	wire := &msgStatsRespWire{
		TotalChannels:      4,
		TotalSubscriptions: 7,
		MessagesPublished:  100,
		MessagesDelivered:  95,
	}
	payload, err := EncodePayload(wire)
	require.NoError(t, err)

	got, err := decodeMessagingResponse("GetStats", payload)
	require.NoError(t, err)
	stats, ok := got.(*messaging.Stats)
	require.True(t, ok, "got %T", got)
	assert.Equal(t, int64(4), stats.TotalChannels)
	assert.Equal(t, int64(7), stats.TotalSubscriptions)
	assert.Equal(t, int64(100), stats.MessagesPublished)
	assert.Equal(t, int64(95), stats.MessagesDelivered)
}

func TestDecodeMessagingResponse_DedupAndChannelStats(t *testing.T) {
	wire := &msgDedupStatsRespWire{Channel: "events", TotalMessages: 10, TotalDuplicates: 3}
	payload, err := EncodePayload(wire)
	require.NoError(t, err)

	dedup, err := decodeMessagingResponse("GetDedupStats", payload)
	require.NoError(t, err)
	ds := dedup.(*messaging.DedupStats)
	assert.Equal(t, int64(10), ds.TotalChecks)
	assert.Equal(t, int64(3), ds.DuplicatesDetected)
	assert.Equal(t, int64(7), ds.UniqueMessages)

	chStats, err := decodeMessagingResponse("GetChannelStats", payload)
	require.NoError(t, err)
	cs := chStats.(*messaging.ChannelStats)
	assert.Equal(t, "events", cs.Name)
	assert.Equal(t, int64(10), cs.MessagesPublished)
}

func TestDecodeMessagingResponse_VoidMethods(t *testing.T) {
	for _, m := range []string{"Publish", "PublishDelta", "DeleteChannel", "Unsubscribe"} {
		got, err := decodeMessagingResponse(m, []byte(`{"success":true}`))
		require.NoError(t, err, m)
		assert.Nil(t, got, m)
	}
}

func TestDecodeMessagingResponse_Errors(t *testing.T) {
	_, err := decodeMessagingResponse("Bogus", []byte(`{}`))
	require.Error(t, err)

	_, err = decodeMessagingResponse("GetChannel", []byte(`{not json`))
	require.Error(t, err)

	_, err = decodeMessagingResponse("Subscribe", []byte(`{not json`))
	require.Error(t, err)
}

func TestTransformMessagingRequest_PublishDelta(t *testing.T) {
	out, err := transformMessagingRequest("PublishDelta", &messaging.PublishDeltaRequest{
		Channel:  "events",
		Old:      map[string]int{"v": 1},
		New:      map[string]int{"v": 2},
		Metadata: map[string]string{"src": "test"},
	})
	require.NoError(t, err)
	wire := out.(*msgPublishDeltaWire)
	assert.Equal(t, "events", wire.Channel)
	assert.NotNil(t, wire.Old)
	assert.NotNil(t, wire.New)
	assert.Equal(t, "test", wire.Metadata["src"])
}

func TestTransformMessagingRequest_MoreErrors(t *testing.T) {
	// Unsubscribe with empty ID.
	_, err := transformMessagingRequest("Unsubscribe", &messaging.UnsubscribeRequest{SubscriptionID: ""})
	require.Error(t, err)

	// PublishDelta with wrong type.
	_, err = transformMessagingRequest("PublishDelta", "nope")
	require.Error(t, err)

	// Publish with wrong type.
	_, err = transformMessagingRequest("Publish", 7)
	require.Error(t, err)

	// GetChannelStats with empty channel.
	_, err = transformMessagingRequest("GetChannelStats", &struct{ Channel string }{Channel: ""})
	require.Error(t, err)

	// GetChannel with empty name.
	_, err = transformMessagingRequest("GetChannel", &messaging.GetChannelRequest{Name: ""})
	require.Error(t, err)
}

func TestDecodeMessagingResponse_ChannelEmpty(t *testing.T) {
	got, err := decodeMessagingResponse("GetChannel", nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestExtractMsgStringField(t *testing.T) {
	v, ok := extractMsgStringField(&struct{ Name string }{Name: "x"}, "Name")
	assert.True(t, ok)
	assert.Equal(t, "x", v)

	// Missing field.
	_, ok = extractMsgStringField(&struct{ Other string }{Other: "x"}, "Name")
	assert.False(t, ok)

	// Nil request.
	_, ok = extractMsgStringField(nil, "Name")
	assert.False(t, ok)

	// Non-string field.
	_, ok = extractMsgStringField(&struct{ Name int }{Name: 5}, "Name")
	assert.False(t, ok)
}
