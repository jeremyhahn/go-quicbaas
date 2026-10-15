// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// messaging_handler.go provides typed MCP routing for the
// quicbaas.Messaging service. The generic MCP dispatch returns an opaque
// map that the SDK messaging client cannot type-assert into its concrete
// *Channel / *Subscription / *Stats response types; this handler maps each
// SDK method onto its quicbaasd MCP tool (the quicbaas_msg_* surface),
// builds the tool arguments, and decodes the tool result into the precise
// type the client expects.
//
// Void-result methods (DeleteChannel, Unsubscribe, Publish, PublishDelta)
// return nil — the messaging client accepts a nil response for those and
// treats it as success.

package mcp

import (
	"encoding/base64"
	"encoding/json"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isMCPMessagingService reports whether service resolves to the Messaging
// service.
func isMCPMessagingService(service string) bool {
	return trimServiceName(service) == "Messaging"
}

// messagingMethodToTool maps Messaging SDK method names onto quicbaasd MCP
// tool names. Methods absent from this table have no MCP tool.
var messagingMethodToTool = map[string]string{
	"CreateChannel":   "quicbaas_msg_create_channel",
	"DeleteChannel":   "quicbaas_msg_delete_channel",
	"GetChannel":      "quicbaas_msg_get_channel",
	"ListChannels":    "quicbaas_msg_list_channels",
	"Subscribe":       "quicbaas_msg_subscribe",
	"WatchChannel":    "quicbaas_msg_watch_channel",
	"Unsubscribe":     "quicbaas_msg_unsubscribe",
	"Publish":         "quicbaas_msg_publish",
	"PublishDelta":    "quicbaas_msg_publish_delta",
	"GetStats":        "quicbaas_msg_stats",
	"GetChannelStats": "quicbaas_msg_channel_stats",
	"GetDedupStats":   "quicbaas_msg_dedup_stats",
}

// mapMessagingMethodToTool returns the MCP tool name for a Messaging method.
func mapMessagingMethodToTool(method string) (string, bool) {
	tool, ok := messagingMethodToTool[method]
	return tool, ok
}

// messagingHandler transforms Messaging SDK requests/responses for the MCP
// transport.
type messagingHandler struct {
	transport *Transport
}

// newMessagingHandler constructs a Messaging MCP handler bound to t.
func newMessagingHandler(t *Transport) *messagingHandler {
	return &messagingHandler{transport: t}
}

// transformRequest builds MCP tool arguments for a Messaging method.
func (h *messagingHandler) transformRequest(method string, req interface{}) (map[string]interface{}, error) {
	switch method {
	case "CreateChannel":
		return h.transformCreateChannel(req)
	case "DeleteChannel":
		return h.transformDeleteChannel(req)
	case "GetChannel":
		return h.transformGetChannel(req)
	case "ListChannels":
		return map[string]interface{}{}, nil
	case "Subscribe":
		return h.transformSubscribe(req)
	case "WatchChannel":
		return h.transformWatchChannel(req)
	case "Unsubscribe":
		return h.transformUnsubscribe(req)
	case "Publish":
		return h.transformPublish(req)
	case "PublishDelta":
		return h.transformPublishDelta(req)
	case "GetStats":
		return map[string]interface{}{}, nil
	case "GetChannelStats":
		return h.transformGetChannelStats(req)
	case "GetDedupStats":
		return h.transformGetDedupStats(req)
	default:
		return nil, newMessagingUnsupportedError(method)
	}
}

// transformResponse decodes the MCP tool result into the concrete SDK type.
func (h *messagingHandler) transformResponse(method string, resp interface{}) (interface{}, error) {
	switch method {
	case "CreateChannel", "GetChannel":
		return h.transformChannelResponse(resp)
	case "ListChannels":
		return h.transformListChannelsResponse(resp)
	case "Subscribe":
		return h.transformSubscriptionResponse("mcp.messaging.Subscribe", resp)
	case "WatchChannel":
		return h.transformSubscriptionResponse("mcp.messaging.WatchChannel", resp)
	case "GetStats":
		return h.transformStatsResponse(resp)
	case "GetChannelStats":
		return h.transformChannelStatsResponse(resp)
	case "GetDedupStats":
		return h.transformDedupStatsResponse(resp)
	case "DeleteChannel", "Unsubscribe", "Publish", "PublishDelta":
		// Void-result methods: the messaging client accepts nil and
		// treats it as success.
		return nil, nil
	default:
		return nil, newMessagingUnsupportedError(method)
	}
}

// newMessagingUnsupportedError builds a typed error for Messaging methods
// that have no MCP tool.
func newMessagingUnsupportedError(method string) error {
	return errors.New(errors.CodeUnimplemented, "mcp.messaging."+method,
		"messaging method is not supported by the MCP transport")
}

// =============================================================================
// Request transformations
// =============================================================================

func (h *messagingHandler) transformCreateChannel(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*messaging.CreateChannelRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.messaging.CreateChannel",
			"invalid request type, expected *messaging.CreateChannelRequest")
	}
	args := map[string]interface{}{"name": r.Name}
	putStringArg(args, "codec", r.Codec)
	if r.BufferSize > 0 {
		args["buffer_size"] = r.BufferSize
	}
	if r.MaxSubs > 0 {
		args["max_subscribers"] = r.MaxSubs
	}
	if r.Persistent {
		args["persistent"] = "true"
	}
	if r.MaxHistory > 0 {
		args["max_history"] = r.MaxHistory
	}
	if r.TTL > 0 {
		args["ttl"] = r.TTL.String()
	}
	return args, nil
}

func (h *messagingHandler) transformDeleteChannel(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*messaging.DeleteChannelRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.messaging.DeleteChannel",
			"invalid request type, expected *messaging.DeleteChannelRequest")
	}
	return map[string]interface{}{"name": r.Name}, nil
}

func (h *messagingHandler) transformGetChannel(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*messaging.GetChannelRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.messaging.GetChannel",
			"invalid request type, expected *messaging.GetChannelRequest")
	}
	return map[string]interface{}{"name": r.Name}, nil
}

func (h *messagingHandler) transformSubscribe(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*messaging.SubscribeRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.messaging.Subscribe",
			"invalid request type, expected *messaging.SubscribeRequest")
	}
	args := map[string]interface{}{"channel": r.Channel}
	if err := putSubscriptionArgs(args, r.SessionID, r.BufferSize, r.Filter); err != nil {
		return nil, err
	}
	return args, nil
}

// transformWatchChannel builds the quicbaas_msg_watch_channel arguments.
func (h *messagingHandler) transformWatchChannel(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*messaging.WatchChannelRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.messaging.WatchChannel",
			"invalid request type, expected *messaging.WatchChannelRequest")
	}
	args := map[string]interface{}{"key_prefix": r.KeyPrefix}
	if err := putSubscriptionArgs(args, r.SessionID, r.BufferSize, r.Filter); err != nil {
		return nil, err
	}
	return args, nil
}

// putSubscriptionArgs adds the session, buffer size and filter arguments the
// subscribe and watch tools share.
func putSubscriptionArgs(args map[string]interface{}, sessionID string, bufferSize int, filter *messaging.SubscriptionFilter) error {
	if bufferSize > 0 {
		args["buffer_size"] = bufferSize
	}
	putStringArg(args, "session_id", sessionID)
	if filter == nil {
		return nil
	}
	putStringArg(args, "filter_key_prefix", filter.KeyPrefix)
	if len(filter.EventTypes) == 0 {
		return nil
	}
	eventTypes := make([]string, 0, len(filter.EventTypes))
	for _, et := range filter.EventTypes {
		eventTypes = append(eventTypes, et.String())
	}
	return putJSONArrayArg(args, "filter_event_types", eventTypes)
}

func (h *messagingHandler) transformUnsubscribe(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*messaging.UnsubscribeRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.messaging.Unsubscribe",
			"invalid request type, expected *messaging.UnsubscribeRequest")
	}
	return map[string]interface{}{"subscription_id": r.SubscriptionID}, nil
}

func (h *messagingHandler) transformPublish(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*messaging.PublishRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.messaging.Publish",
			"invalid request type, expected *messaging.PublishRequest")
	}
	args := map[string]interface{}{
		"channel": r.Channel,
		"payload": encodePayload(r.Payload),
	}
	putStringArg(args, "message_id", r.MessageID)
	putStringArg(args, "dedup_key", r.DedupKey)
	if len(r.Metadata) > 0 {
		metaJSON, err := json.Marshal(r.Metadata)
		if err != nil {
			return nil, errors.NewInternal("mcp.messaging.Publish",
				"failed to encode metadata: "+err.Error())
		}
		args["metadata"] = string(metaJSON)
	}
	return args, nil
}

func (h *messagingHandler) transformPublishDelta(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*messaging.PublishDeltaRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.messaging.PublishDelta",
			"invalid request type, expected *messaging.PublishDeltaRequest")
	}
	oldJSON, err := json.Marshal(r.Old)
	if err != nil {
		return nil, errors.NewInternal("mcp.messaging.PublishDelta",
			"failed to encode old_state: "+err.Error())
	}
	newJSON, err := json.Marshal(r.New)
	if err != nil {
		return nil, errors.NewInternal("mcp.messaging.PublishDelta",
			"failed to encode new_state: "+err.Error())
	}
	args := map[string]interface{}{
		"channel":   r.Channel,
		"old_state": string(oldJSON),
		"new_state": string(newJSON),
	}
	putStringArg(args, "message_id", r.MessageID)
	putStringArg(args, "dedup_key", r.DedupKey)
	return args, nil
}

// transformGetChannelStats builds the quicbaas_msg_channel_stats arguments.
func (h *messagingHandler) transformGetChannelStats(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*messaging.GetChannelStatsRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.messaging.GetChannelStats",
			"invalid request type, expected *messaging.GetChannelStatsRequest")
	}
	if r.Channel == "" {
		return nil, errors.NewInvalidInput("mcp.messaging.GetChannelStats", "channel is required")
	}
	return map[string]interface{}{"channel": r.Channel}, nil
}

func (h *messagingHandler) transformGetDedupStats(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*messaging.GetDedupStatsRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.messaging.GetDedupStats",
			"invalid request type, expected *messaging.GetDedupStatsRequest")
	}
	if r.Channel == "" {
		return nil, errors.NewInvalidInput("mcp.messaging.GetDedupStats", "channel is required")
	}
	return map[string]interface{}{"channel": r.Channel}, nil
}

// =============================================================================
// Response transformations
// =============================================================================

func (h *messagingHandler) transformChannelResponse(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.messaging.GetChannel")
	if err != nil {
		return nil, err
	}
	return decodeChannel(m), nil
}

func (h *messagingHandler) transformListChannelsResponse(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.messaging.ListChannels")
	if err != nil {
		return nil, err
	}
	out := &messaging.ListChannelsResponse{Channels: []*messaging.Channel{}}
	if raw, ok := m["channels"].([]interface{}); ok {
		for _, item := range raw {
			if cm, ok := item.(map[string]interface{}); ok {
				out.Channels = append(out.Channels, decodeChannel(cm))
			}
		}
	}
	return out, nil
}

func (h *messagingHandler) transformSubscriptionResponse(op string, resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, op)
	if err != nil {
		return nil, err
	}
	return &messaging.Subscription{
		ID:        getResultString(m, "subscription_id"),
		Channel:   getResultString(m, "channel"),
		ClientID:  getResultString(m, "client_id"),
		CreatedAt: getResultTime(m, "created_at"),
	}, nil
}

func (h *messagingHandler) transformStatsResponse(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.messaging.GetStats")
	if err != nil {
		return nil, err
	}
	return &messaging.Stats{
		TotalChannels:      getResultInt64(m, "total_channels"),
		TotalSubscriptions: getResultInt64(m, "total_subscriptions"),
		MessagesPublished:  getResultInt64(m, "messages_published"),
		MessagesDelivered:  getResultInt64(m, "messages_delivered"),
		DedupHits:          getResultInt64(m, "dedup_hits"),
		DroppedMessages:    getResultInt64(m, "dropped_messages"),
		BufferUtilization:  getResultFloat64(m, "buffer_utilization"),
	}, nil
}

// transformChannelStatsResponse decodes a quicbaas_msg_channel_stats result
// into the SDK ChannelStats.
func (h *messagingHandler) transformChannelStatsResponse(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.messaging.GetChannelStats")
	if err != nil {
		return nil, err
	}
	return &messaging.ChannelStats{
		Name:              getResultString(m, "name"),
		Subscribers:       getResultInt64(m, "subscribers"),
		MessagesPublished: getResultInt64(m, "messages_published"),
	}, nil
}

func (h *messagingHandler) transformDedupStatsResponse(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.messaging.GetDedupStats")
	if err != nil {
		return nil, err
	}
	// The quicbaas_msg_dedup_stats tool reports per-channel duplicate
	// counters; map them onto the SDK DedupStats counters as the REST and
	// gRPC transports do.
	// total_messages counts unique messages.
	unique := getResultInt64(m, "total_messages")
	duplicates := getResultInt64(m, "total_duplicates")
	return &messaging.DedupStats{
		TotalChecks:        unique + duplicates,
		DuplicatesDetected: duplicates,
		UniqueMessages:     unique,
	}, nil
}

// decodeChannel builds a *messaging.Channel from a tool result map.
func decodeChannel(m map[string]interface{}) *messaging.Channel {
	ch := &messaging.Channel{
		Name:        getResultString(m, "name"),
		Codec:       getResultString(m, "codec"),
		CreatedAt:   getResultTime(m, "created_at"),
		Subscribers: getResultInt64(m, "subscribers"),
		Persistent:  getResultBool(m, "persistent"),
		MaxHistory:  int(getResultInt64(m, "max_history")),
		BufferSize:  int(getResultInt64(m, "buffer_size")),
	}
	if ttlStr := getResultString(m, "ttl"); ttlStr != "" {
		if d, err := parseDurationLoose(ttlStr); err == nil {
			ch.TTL = d
		}
	}
	return ch
}

// encodePayload base64-encodes the message payload, matching the
// quicbaas_msg_publish tool's accepted "payload" format (JSON string or
// base64-encoded binary).
func encodePayload(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	// JSON payloads round-trip as text; non-UTF8 binary is base64-encoded.
	if json.Valid(payload) {
		return string(payload)
	}
	return base64.StdEncoding.EncodeToString(payload)
}
