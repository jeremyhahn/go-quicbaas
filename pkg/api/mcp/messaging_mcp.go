//go:build mcp

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

// Package mcp implements Messaging MCP integration handlers.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/messagingwatch"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// registerMsgTools registers all Messaging tool handlers on the ToolRegistry.
func (r *ToolRegistry) registerMsgTools() {
	msgTools := getAllMsgTools()
	for _, tool := range msgTools {
		r.addTool(ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: convertInputSchema(tool.InputSchema),
		})
	}
}

// executeMsgTool executes a Messaging tool and returns the result.
func (r *ToolRegistry) executeMsgTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	msgService := r.service.Messaging()
	if msgService == nil {
		return nil, api.NewServiceNotConfiguredError("messaging")
	}

	switch name {
	case "quicbaas_msg_create_channel":
		return r.handleMsgCreateChannel(ctx, msgService, args)
	case "quicbaas_msg_delete_channel":
		return r.handleMsgDeleteChannel(ctx, msgService, args)
	case "quicbaas_msg_get_channel":
		return r.handleMsgGetChannel(ctx, msgService, args)
	case "quicbaas_msg_list_channels":
		return r.handleMsgListChannels(ctx, msgService, args)
	case "quicbaas_msg_subscribe":
		return r.handleMsgSubscribe(ctx, msgService, args)
	case "quicbaas_msg_watch_channel":
		return r.handleMsgWatchChannel(ctx, msgService, args)
	case "quicbaas_msg_unsubscribe":
		return r.handleMsgUnsubscribe(ctx, msgService, args)
	case "quicbaas_msg_publish":
		return r.handleMsgPublish(ctx, msgService, args)
	case "quicbaas_msg_publish_delta":
		return r.handleMsgPublishDelta(ctx, msgService, args)
	case "quicbaas_msg_stats":
		return r.handleMsgStats(ctx, msgService, args)
	case "quicbaas_msg_channel_stats":
		return r.handleMsgChannelStats(ctx, msgService, args)
	case "quicbaas_msg_dedup_stats":
		return r.handleMsgDedupStats(ctx, msgService, args)
	default:
		return nil, api.NewUnknownToolError(name)
	}
}

// handleMsgCreateChannel handles the quicbaas_msg_create_channel tool.
func (r *ToolRegistry) handleMsgCreateChannel(ctx context.Context, svc messaging.Service, args map[string]interface{}) (interface{}, error) {
	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}

	// Build channel options
	var opts []messaging.CreateChannelOption

	// Parse codec
	if codec := getString(args, "codec"); codec != "" {
		opts = append(opts, messaging.WithCodec(codec))
	}

	// Parse buffer_size
	if bufferSize := getInt(args, "buffer_size"); bufferSize > 0 {
		opts = append(opts, messaging.WithChannelBufferSize(bufferSize))
	}

	// Parse max_subscribers
	if maxSubs := getInt(args, "max_subscribers"); maxSubs > 0 {
		opts = append(opts, messaging.WithChannelMaxSubscribers(maxSubs))
	}

	// Parse persistent
	if persistent := getString(args, "persistent"); persistent == "true" {
		opts = append(opts, messaging.WithPersistence(true))
	}

	// Parse max_history
	if maxHistory := getInt(args, "max_history"); maxHistory > 0 {
		opts = append(opts, messaging.WithMaxHistory(maxHistory))
	}

	// Parse TTL
	if ttlStr := getString(args, "ttl"); ttlStr != "" {
		ttl, err := time.ParseDuration(ttlStr)
		if err != nil {
			return nil, api.NewInvalidFormatError("ttl", err)
		}
		opts = append(opts, messaging.WithChannelTTL(ttl))
	}

	channel, err := svc.CreateChannel(ctx, name, opts...)
	if err != nil {
		return nil, api.NewOperationFailedError("create channel", err)
	}

	return map[string]interface{}{
		"name":        channel.Name,
		"codec":       channel.Codec,
		"created_at":  channel.CreatedAt.Format(time.RFC3339),
		"subscribers": channel.Subscribers,
		"persistent":  channel.Persistent,
		"max_history": channel.MaxHistory,
		"buffer_size": channel.BufferSize,
		"ttl":         channel.TTL.String(),
	}, nil
}

// handleMsgDeleteChannel handles the quicbaas_msg_delete_channel tool.
func (r *ToolRegistry) handleMsgDeleteChannel(ctx context.Context, svc messaging.Service, args map[string]interface{}) (interface{}, error) {
	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}

	err := svc.DeleteChannel(ctx, name)
	if err != nil {
		return nil, api.NewOperationFailedError("delete channel", err)
	}

	return map[string]interface{}{
		"deleted": true,
		"name":    name,
	}, nil
}

// handleMsgGetChannel handles the quicbaas_msg_get_channel tool.
func (r *ToolRegistry) handleMsgGetChannel(ctx context.Context, svc messaging.Service, args map[string]interface{}) (interface{}, error) {
	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}

	channel, err := svc.GetChannel(ctx, name)
	if err != nil {
		return nil, api.NewOperationFailedError("get channel", err)
	}

	return map[string]interface{}{
		"name":        channel.Name,
		"codec":       channel.Codec,
		"created_at":  channel.CreatedAt.Format(time.RFC3339),
		"subscribers": channel.Subscribers,
		"persistent":  channel.Persistent,
		"max_history": channel.MaxHistory,
		"buffer_size": channel.BufferSize,
		"ttl":         channel.TTL.String(),
	}, nil
}

// handleMsgListChannels handles the quicbaas_msg_list_channels tool.
func (r *ToolRegistry) handleMsgListChannels(ctx context.Context, svc messaging.Service, args map[string]interface{}) (interface{}, error) {
	channels, err := svc.ListChannels(ctx)
	if err != nil {
		return nil, api.NewOperationFailedError("list channels", err)
	}

	result := make([]map[string]interface{}, 0, len(channels))
	for _, channel := range channels {
		result = append(result, map[string]interface{}{
			"name":        channel.Name,
			"codec":       channel.Codec,
			"created_at":  channel.CreatedAt.Format(time.RFC3339),
			"subscribers": channel.Subscribers,
			"persistent":  channel.Persistent,
			"max_history": channel.MaxHistory,
			"buffer_size": channel.BufferSize,
			"ttl":         channel.TTL.String(),
		})
	}

	return map[string]interface{}{
		"channels": result,
		"count":    len(result),
	}, nil
}

// handleMsgSubscribe handles the quicbaas_msg_subscribe tool.
func (r *ToolRegistry) handleMsgSubscribe(ctx context.Context, svc messaging.Service, args map[string]interface{}) (interface{}, error) {
	channel := getString(args, "channel")
	if channel == "" {
		return nil, api.NewRequiredFieldError("channel")
	}

	// Build subscribe options
	var opts []messaging.SubscribeOption

	// Parse buffer_size
	if bufferSize := getInt(args, "buffer_size"); bufferSize > 0 {
		opts = append(opts, messaging.WithSubscriptionBufferSize(bufferSize))
	}

	// Parse session_id
	if sessionID := getString(args, "session_id"); sessionID != "" {
		opts = append(opts, messaging.WithSession(sessionID))
	}

	// Parse filter options
	var filter *messaging.SubscriptionFilter

	if keyPrefix := getString(args, "filter_key_prefix"); keyPrefix != "" {
		if filter == nil {
			filter = &messaging.SubscriptionFilter{}
		}
		filter.KeyPrefix = keyPrefix
	}

	if eventTypesJSON := getString(args, "filter_event_types"); eventTypesJSON != "" {
		var eventTypeStrs []string
		if err := json.Unmarshal([]byte(eventTypesJSON), &eventTypeStrs); err == nil {
			if filter == nil {
				filter = &messaging.SubscriptionFilter{}
			}
			for _, etStr := range eventTypeStrs {
				switch etStr {
				case "PUT":
					filter.EventTypes = append(filter.EventTypes, messaging.EventTypePut)
				case "DELETE":
					filter.EventTypes = append(filter.EventTypes, messaging.EventTypeDelete)
				}
			}
		}
	}

	if filter != nil {
		opts = append(opts, messaging.WithFilter(filter))
	}

	subscription, err := svc.Subscribe(ctx, channel, opts...)
	if err != nil {
		return nil, api.NewOperationFailedError("subscribe", err)
	}

	return map[string]interface{}{
		"subscription_id": subscription.ID,
		"channel":         subscription.Channel,
		"client_id":       subscription.ClientID,
		"created_at":      subscription.CreatedAt.Format(time.RFC3339),
	}, nil
}

// handleMsgWatchChannel opens a watch on a key prefix through the same
// mapping the gRPC and REST servers serve WatchChannel from.
func (r *ToolRegistry) handleMsgWatchChannel(ctx context.Context, svc messaging.Service, args map[string]interface{}) (interface{}, error) {
	req, err := watchRequestFromArgs(args)
	if err != nil {
		return nil, err
	}

	subscription, err := messagingwatch.Watch(ctx, svc, req)
	if err != nil {
		var required *api.RequiredFieldError
		if errors.As(err, &required) {
			return nil, err
		}
		return nil, api.NewOperationFailedError("watch channel", err)
	}

	return map[string]interface{}{
		"subscription_id": subscription.ID,
		"channel":         subscription.Channel,
		"client_id":       subscription.ClientID,
		"created_at":      subscription.CreatedAt.Format(time.RFC3339),
		"channel_name":    messagingwatch.ChannelName(req.KeyPrefix),
		"timestamp":       time.Now().Format(time.RFC3339),
	}, nil
}

// watchRequestFromArgs decodes the quicbaas_msg_watch_channel arguments.
// filter_event_types is a JSON array of event-type names; an unknown name or
// a malformed array is refused rather than dropped.
func watchRequestFromArgs(args map[string]interface{}) (messagingwatch.Request, error) {
	req := messagingwatch.Request{
		KeyPrefix:  getString(args, "key_prefix"),
		SessionID:  getString(args, "session_id"),
		BufferSize: getInt(args, "buffer_size"),
	}

	keyPrefix := getString(args, "filter_key_prefix")
	eventTypesJSON := getString(args, "filter_event_types")
	if keyPrefix == "" && eventTypesJSON == "" {
		return req, nil
	}

	req.Filter = &messaging.SubscriptionFilter{KeyPrefix: keyPrefix}
	if eventTypesJSON != "" {
		var names []string
		if err := json.Unmarshal([]byte(eventTypesJSON), &names); err != nil {
			return messagingwatch.Request{}, api.NewInvalidInputError("filter_event_types must be a JSON array of event types")
		}
		eventTypes, err := messagingwatch.ParseEventTypes(names)
		if err != nil {
			return messagingwatch.Request{}, err
		}
		req.Filter.EventTypes = eventTypes
	}
	return req, nil
}

// handleMsgUnsubscribe handles the quicbaas_msg_unsubscribe tool.
func (r *ToolRegistry) handleMsgUnsubscribe(ctx context.Context, svc messaging.Service, args map[string]interface{}) (interface{}, error) {
	subscriptionID := getString(args, "subscription_id")
	if subscriptionID == "" {
		return nil, api.NewRequiredFieldError("subscription_id")
	}

	err := svc.Unsubscribe(ctx, subscriptionID)
	if err != nil {
		return nil, api.NewOperationFailedError("unsubscribe", err)
	}

	return map[string]interface{}{
		"unsubscribed":    true,
		"subscription_id": subscriptionID,
	}, nil
}

// handleMsgPublish handles the quicbaas_msg_publish tool.
func (r *ToolRegistry) handleMsgPublish(ctx context.Context, svc messaging.Service, args map[string]interface{}) (interface{}, error) {
	channel := getString(args, "channel")
	if channel == "" {
		return nil, api.NewRequiredFieldError("channel")
	}

	payload := getString(args, "payload")
	if payload == "" {
		return nil, api.NewRequiredFieldError("payload")
	}

	// Build publish options
	var opts []messaging.PublishOption

	// Parse message_id
	if messageID := getString(args, "message_id"); messageID != "" {
		opts = append(opts, messaging.WithMessageID(messageID))
	}

	// Parse dedup_key
	if dedupKey := getString(args, "dedup_key"); dedupKey != "" {
		opts = append(opts, messaging.WithDedupKey(dedupKey))
	}

	// Parse metadata
	if metadataJSON := getString(args, "metadata"); metadataJSON != "" {
		var metadata map[string]string
		if err := json.Unmarshal([]byte(metadataJSON), &metadata); err == nil {
			opts = append(opts, messaging.WithMetadata(metadata))
		}
	}

	// Try to parse payload as JSON, otherwise send as string
	var payloadValue interface{} = payload
	var jsonPayload interface{}
	if err := json.Unmarshal([]byte(payload), &jsonPayload); err == nil {
		payloadValue = jsonPayload
	}

	result, err := svc.Publish(ctx, channel, payloadValue, opts...)
	if err != nil {
		return nil, api.NewOperationFailedError("publish", err)
	}

	return map[string]interface{}{
		"published":    true,
		"channel":      channel,
		"message_id":   result.MessageID,
		"is_duplicate": result.IsDuplicate,
	}, nil
}

// handleMsgPublishDelta handles the quicbaas_msg_publish_delta tool.
func (r *ToolRegistry) handleMsgPublishDelta(ctx context.Context, svc messaging.Service, args map[string]interface{}) (interface{}, error) {
	channel := getString(args, "channel")
	if channel == "" {
		return nil, api.NewRequiredFieldError("channel")
	}

	oldStateJSON := getString(args, "old_state")
	if oldStateJSON == "" {
		return nil, api.NewRequiredFieldError("old_state")
	}

	newStateJSON := getString(args, "new_state")
	if newStateJSON == "" {
		return nil, api.NewRequiredFieldError("new_state")
	}

	// Parse old and new state
	var oldState, newState interface{}
	if err := json.Unmarshal([]byte(oldStateJSON), &oldState); err != nil {
		return nil, api.NewInvalidFormatError("old_state JSON", err)
	}
	if err := json.Unmarshal([]byte(newStateJSON), &newState); err != nil {
		return nil, api.NewInvalidFormatError("new_state JSON", err)
	}

	// Build publish options
	var opts []messaging.PublishOption

	// Parse message_id
	if messageID := getString(args, "message_id"); messageID != "" {
		opts = append(opts, messaging.WithMessageID(messageID))
	}

	// Parse dedup_key
	if dedupKey := getString(args, "dedup_key"); dedupKey != "" {
		opts = append(opts, messaging.WithDedupKey(dedupKey))
	}

	result, err := svc.PublishDelta(ctx, channel, oldState, newState, opts...)
	if err != nil {
		return nil, api.NewOperationFailedError("publish delta", err)
	}

	return map[string]interface{}{
		"published":    true,
		"channel":      channel,
		"type":         "delta",
		"message_id":   result.MessageID,
		"is_duplicate": result.IsDuplicate,
	}, nil
}

// handleMsgStats handles the quicbaas_msg_stats tool.
func (r *ToolRegistry) handleMsgStats(ctx context.Context, svc messaging.Service, args map[string]interface{}) (interface{}, error) {
	stats := svc.GetStats()
	if stats == nil {
		return map[string]interface{}{
			"available": false,
		}, nil
	}

	return map[string]interface{}{
		"available":           true,
		"total_channels":      stats.TotalChannels,
		"total_subscriptions": stats.TotalSubscriptions,
		"messages_published":  stats.MessagesPublished,
		"messages_delivered":  stats.MessagesDelivered,
		"dedup_hits":          stats.DedupHits,
		"dropped_messages":    stats.DroppedMessages,
		"buffer_utilization":  stats.BufferUtilization,
	}, nil
}

// handleMsgChannelStats handles the quicbaas_msg_channel_stats tool.
func (r *ToolRegistry) handleMsgChannelStats(ctx context.Context, svc messaging.Service, args map[string]interface{}) (interface{}, error) {
	channelName := getString(args, "channel")
	if channelName == "" {
		return nil, api.NewRequiredFieldError("channel")
	}

	// An unknown channel is refused as NotFound, as the gRPC and REST
	// handlers refuse it.
	channel, err := svc.GetChannel(ctx, channelName)
	if err != nil {
		if messaging.IsChannelNotFound(err) {
			return nil, api.NewNotFoundError("channel", channelName)
		}
		return nil, api.NewOperationFailedError("get channel stats", err)
	}

	return map[string]interface{}{
		"name":               channel.Name,
		"subscribers":        channel.Subscribers,
		"messages_published": channel.MessagesPublished,
	}, nil
}

// handleMsgDedupStats handles the quicbaas_msg_dedup_stats tool.
func (r *ToolRegistry) handleMsgDedupStats(ctx context.Context, svc messaging.Service, args map[string]interface{}) (interface{}, error) {
	channel := getString(args, "channel")
	if channel == "" {
		return nil, api.NewRequiredFieldError("channel")
	}

	// An unknown channel is refused as NotFound, as the gRPC and REST
	// handlers refuse it.
	if _, err := svc.GetChannel(ctx, channel); err != nil {
		if messaging.IsChannelNotFound(err) {
			return nil, api.NewNotFoundError("channel", channel)
		}
		return nil, api.NewOperationFailedError("get dedup stats", err)
	}

	// Access the PushService to get dedup store
	msgSvc, ok := svc.(*messaging.PushService)
	if !ok {
		return nil, api.NewServiceUnavailableError("dedup stats")
	}

	dedupStore := msgSvc.GetDedupStore()
	if dedupStore == nil {
		return map[string]interface{}{
			"available": false,
			"channel":   channel,
			"message":   "deduplication not enabled",
		}, nil
	}

	stats, err := dedupStore.GetStats(ctx, channel)
	if err != nil {
		return nil, api.NewOperationFailedError("get dedup stats", err)
	}

	// Format top duplicates
	topDuplicates := make([]map[string]interface{}, 0, len(stats.TopDuplicates))
	for _, entry := range stats.TopDuplicates {
		topDuplicates = append(topDuplicates, map[string]interface{}{
			"message_id":   entry.MessageID,
			"payload_hash": entry.PayloadHash,
			"count":        entry.Count,
			"first_seen":   entry.FirstSeen.Format(time.RFC3339),
			"last_seen":    entry.LastSeen.Format(time.RFC3339),
		})
	}

	return map[string]interface{}{
		"available":        true,
		"channel":          stats.Channel,
		"total_messages":   stats.TotalMessages,
		"total_duplicates": stats.TotalDuplicates,
		"top_duplicates":   topDuplicates,
	}, nil
}

// isMsgTool returns true if the tool name is a Messaging tool.
func isMsgTool(name string) bool {
	switch name {
	case "quicbaas_msg_create_channel",
		"quicbaas_msg_delete_channel",
		"quicbaas_msg_get_channel",
		"quicbaas_msg_list_channels",
		"quicbaas_msg_subscribe",
		"quicbaas_msg_watch_channel",
		"quicbaas_msg_unsubscribe",
		"quicbaas_msg_publish",
		"quicbaas_msg_publish_delta",
		"quicbaas_msg_stats",
		"quicbaas_msg_channel_stats",
		"quicbaas_msg_dedup_stats":
		return true
	default:
		return false
	}
}
