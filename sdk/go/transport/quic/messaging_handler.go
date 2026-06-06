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
	"encoding/json"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// messagingServicePrefix is the service name prefix for Messaging operations.
const messagingServicePrefix = "quicbaas.Messaging"

// isMessagingService reports whether svc names the QuicBaaS Messaging service.
func isMessagingService(svc string) bool {
	return strings.HasPrefix(svc, messagingServicePrefix) || svc == "Messaging"
}

// =============================================================================
// Request transformation (SDK messaging client types -> server Msg*Request shape)
// =============================================================================
//
// The Messaging SDK client (sdk/go/client/messaging) sends request structs whose
// exported fields carry no JSON tags (PascalCase) and encode durations as
// time.Duration. The QUIC server's handler_messaging.go decodes Msg*Request
// types with snake_case tags and second-granular TTLs. This transform re-encodes
// each request into the server's wire shape so the QUIC path matches the REST
// path. Methods whose server handler takes only a name/id string are reduced to
// the minimal Msg*Request the handler expects.

// transformMessagingRequest converts an SDK messaging request for the given
// method into the wire shape the server's messaging dispatch expects.
func transformMessagingRequest(method string, req interface{}) (interface{}, error) {
	switch method {
	case "CreateChannel":
		return transformMsgCreateChannel(req)
	case "DeleteChannel":
		return transformMsgNameRequest(req, "DeleteChannel")
	case "GetChannel":
		return transformMsgNameRequest(req, "GetChannel")
	case "ListChannels":
		return &msgListChannelsWire{}, nil
	case "Subscribe":
		return transformMsgSubscribe(req)
	case "Unsubscribe":
		return transformMsgUnsubscribe(req)
	case "Publish":
		return transformMsgPublish(req)
	case "PublishDelta":
		return transformMsgPublishDelta(req)
	case "GetStats":
		return &msgGetStatsWire{}, nil
	case "GetDedupStats", "GetChannelStats":
		return transformMsgChannelStatsRequest(req)
	default:
		return nil, errors.NewInvalidInput("quic.transformMessagingRequest",
			"unsupported messaging method: "+method)
	}
}

// Server-side wire request shapes (mirror pkg/api/quic/handler_messaging.go).

type msgCreateChannelWire struct {
	Name       string `json:"name"`
	Codec      string `json:"codec,omitempty"`
	Persistent bool   `json:"persistent,omitempty"`
	BufferSize int    `json:"buffer_size,omitempty"`
	MaxHistory int    `json:"max_history,omitempty"`
	TTLSeconds int64  `json:"ttl_seconds,omitempty"`
}

type msgNameWire struct {
	Name string `json:"name"`
}

type msgListChannelsWire struct{}

type msgSubscribeWire struct {
	Channel    string   `json:"channel"`
	SessionID  string   `json:"session_id,omitempty"`
	BufferSize int      `json:"buffer_size,omitempty"`
	EventTypes []string `json:"event_types,omitempty"`
	KeyPrefix  string   `json:"key_prefix,omitempty"`
}

type msgUnsubscribeWire struct {
	SubscriptionID string `json:"subscription_id"`
}

type msgPublishWire struct {
	Channel   string            `json:"channel"`
	Payload   json.RawMessage   `json:"payload"`
	MessageID string            `json:"message_id,omitempty"`
	DedupKey  string            `json:"dedup_key,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type msgPublishDeltaWire struct {
	Channel  string            `json:"channel"`
	Old      interface{}       `json:"old"`
	New      interface{}       `json:"new"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type msgGetStatsWire struct{}

type msgDedupStatsWire struct {
	Channel string `json:"channel"`
}

func transformMsgCreateChannel(req interface{}) (interface{}, error) {
	r, ok := req.(*messaging.CreateChannelRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("quic.transformMsgCreateChannel",
			"expected *messaging.CreateChannelRequest")
	}
	return &msgCreateChannelWire{
		Name:       r.Name,
		Codec:      r.Codec,
		Persistent: r.Persistent,
		BufferSize: r.BufferSize,
		MaxHistory: r.MaxHistory,
		TTLSeconds: int64(r.TTL.Seconds()),
	}, nil
}

// transformMsgNameRequest extracts the Name field from the SDK request (which
// the client always supplies as a *DeleteChannelRequest / *GetChannelRequest)
// and produces the server's name-only wire shape.
func transformMsgNameRequest(req interface{}, op string) (interface{}, error) {
	name, ok := extractMsgStringField(req, "Name")
	if !ok || name == "" {
		return nil, errors.NewInvalidInput("quic.transformMessagingRequest",
			op+": channel name is required")
	}
	return &msgNameWire{Name: name}, nil
}

func transformMsgSubscribe(req interface{}) (interface{}, error) {
	r, ok := req.(*messaging.SubscribeRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("quic.transformMsgSubscribe",
			"expected *messaging.SubscribeRequest")
	}
	out := &msgSubscribeWire{
		Channel:    r.Channel,
		SessionID:  r.SessionID,
		BufferSize: r.BufferSize,
	}
	if r.Filter != nil {
		out.KeyPrefix = r.Filter.KeyPrefix
		for _, et := range r.Filter.EventTypes {
			out.EventTypes = append(out.EventTypes, et.String())
		}
	}
	return out, nil
}

func transformMsgUnsubscribe(req interface{}) (interface{}, error) {
	id, ok := extractMsgStringField(req, "SubscriptionID")
	if !ok || id == "" {
		return nil, errors.NewInvalidInput("quic.transformMsgUnsubscribe",
			"subscription ID is required")
	}
	return &msgUnsubscribeWire{SubscriptionID: id}, nil
}

func transformMsgPublish(req interface{}) (interface{}, error) {
	r, ok := req.(*messaging.PublishRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("quic.transformMsgPublish",
			"expected *messaging.PublishRequest")
	}
	payload, err := msgRawPayload(r.Payload)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput,
			"quic.transformMsgPublish", "failed to encode payload")
	}
	return &msgPublishWire{
		Channel:   r.Channel,
		Payload:   payload,
		MessageID: r.MessageID,
		DedupKey:  r.DedupKey,
		Metadata:  r.Metadata,
	}, nil
}

func transformMsgPublishDelta(req interface{}) (interface{}, error) {
	r, ok := req.(*messaging.PublishDeltaRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("quic.transformMsgPublishDelta",
			"expected *messaging.PublishDeltaRequest")
	}
	return &msgPublishDeltaWire{
		Channel:  r.Channel,
		Old:      r.Old,
		New:      r.New,
		Metadata: r.Metadata,
	}, nil
}

func transformMsgChannelStatsRequest(req interface{}) (interface{}, error) {
	channel, ok := extractMsgStringField(req, "Channel")
	if !ok || channel == "" {
		return nil, errors.NewInvalidInput("quic.transformMessagingRequest",
			"channel name is required")
	}
	return &msgDedupStatsWire{Channel: channel}, nil
}

// =============================================================================
// Response decoding (server wire shape -> SDK messaging client types)
// =============================================================================

// decodeMessagingResponse decodes raw wire bytes into the SDK messaging type for
// the given method. An empty payload returns nil (the client accepts nil as
// success for the void methods). Unknown methods are rejected.
func decodeMessagingResponse(method string, data []byte) (interface{}, error) {
	switch method {
	case "CreateChannel", "GetChannel":
		return decodeMsgChannel(data)
	case "ListChannels":
		return decodeMsgListChannels(data)
	case "Subscribe":
		return decodeMsgSubscription(data)
	case "Publish", "PublishDelta", "DeleteChannel", "Unsubscribe":
		// Void methods: the client ignores the body and treats success as a nil
		// error. Returning nil lets its nil-response branch pass.
		return nil, nil
	case "GetStats":
		return decodeMsgStats(data)
	case "GetDedupStats":
		return decodeMsgDedupStats(data)
	case "GetChannelStats":
		return decodeMsgChannelStats(data)
	default:
		return nil, errors.NewInvalidInput("quic.decodeMessagingResponse",
			"unsupported messaging method: "+method)
	}
}

// decodeMsgChannel decodes a server messaging.Channel. The server type and the
// SDK Channel both serialize their exported fields without JSON tags (PascalCase)
// and identically, so a direct unmarshal round-trips. CreatedAt is an RFC3339
// time string and TTL is a nanosecond duration on both sides.
func decodeMsgChannel(data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var channel messaging.Channel
	if err := json.Unmarshal(data, &channel); err != nil {
		return nil, wrapMessagingDecode("Channel", err)
	}
	return &channel, nil
}

// decodeMsgListChannels decodes the server's JSON array of *messaging.Channel
// into the SDK ListChannelsResponse envelope.
func decodeMsgListChannels(data []byte) (interface{}, error) {
	if len(data) == 0 {
		return &messaging.ListChannelsResponse{Channels: []*messaging.Channel{}}, nil
	}
	var channels []*messaging.Channel
	if err := json.Unmarshal(data, &channels); err != nil {
		return nil, wrapMessagingDecode("ListChannels", err)
	}
	if channels == nil {
		channels = []*messaging.Channel{}
	}
	return &messaging.ListChannelsResponse{Channels: channels}, nil
}

// msgSubscribeRespWire mirrors the server's MsgSubscribeResponse.
type msgSubscribeRespWire struct {
	SubscriptionID string `json:"subscription_id"`
	Channel        string `json:"channel"`
	ClientID       string `json:"client_id"`
}

func decodeMsgSubscription(data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var wire msgSubscribeRespWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, wrapMessagingDecode("Subscribe", err)
	}
	return &messaging.Subscription{
		ID:        wire.SubscriptionID,
		Channel:   wire.Channel,
		ClientID:  wire.ClientID,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// msgStatsRespWire mirrors the server's MsgGetStatsResponse.
type msgStatsRespWire struct {
	TotalChannels      int64 `json:"total_channels"`
	TotalSubscriptions int64 `json:"total_subscriptions"`
	MessagesPublished  int64 `json:"messages_published"`
	MessagesDelivered  int64 `json:"messages_delivered"`
}

func decodeMsgStats(data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var wire msgStatsRespWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, wrapMessagingDecode("GetStats", err)
	}
	return &messaging.Stats{
		TotalChannels:      wire.TotalChannels,
		TotalSubscriptions: wire.TotalSubscriptions,
		MessagesPublished:  wire.MessagesPublished,
		MessagesDelivered:  wire.MessagesDelivered,
	}, nil
}

// msgDedupStatsRespWire mirrors the server's MsgDedupStatsResponse.
type msgDedupStatsRespWire struct {
	Channel         string `json:"channel"`
	TotalMessages   int64  `json:"total_messages"`
	TotalDuplicates int64  `json:"total_duplicates"`
}

func decodeMsgDedupStats(data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var wire msgDedupStatsRespWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, wrapMessagingDecode("GetDedupStats", err)
	}
	return &messaging.DedupStats{
		TotalChecks:        wire.TotalMessages,
		DuplicatesDetected: wire.TotalDuplicates,
		UniqueMessages:     wire.TotalMessages - wire.TotalDuplicates,
	}, nil
}

func decodeMsgChannelStats(data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var wire msgDedupStatsRespWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, wrapMessagingDecode("GetChannelStats", err)
	}
	return &messaging.ChannelStats{
		Name:              wire.Channel,
		MessagesPublished: wire.TotalMessages,
	}, nil
}

func wrapMessagingDecode(method string, err error) error {
	return errors.Wrap(err, errors.CodeInternal,
		"quic.decodeMessagingResponse", "failed to unmarshal "+method+" response")
}

// extractMsgStringField marshals req to JSON and reads a single string field by
// its Go field name (the SDK request structs have no JSON tags, so the field
// name is the JSON key). Used for the name/id-only requests.
func extractMsgStringField(req interface{}, field string) (string, bool) {
	if req == nil {
		return "", false
	}
	data, err := json.Marshal(req)
	if err != nil {
		return "", false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return "", false
	}
	raw, ok := m[field]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// msgRawPayload normalizes a publish payload to a JSON RawMessage. Bytes that are
// already valid JSON are forwarded verbatim; otherwise they are encoded as a JSON
// value so the server can store them. Mirrors the REST handler's behavior.
func msgRawPayload(payload []byte) (json.RawMessage, error) {
	if len(payload) == 0 {
		return json.RawMessage("null"), nil
	}
	if json.Valid(payload) {
		return json.RawMessage(payload), nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}
